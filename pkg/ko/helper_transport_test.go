package ko

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/remotecommand"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	"k8s.io/streaming/pkg/httpstream/wsstream"

	"github.com/kubeovn/kube-ovn/pkg/kohelper"
)

type echoHelperRunner struct{ started chan struct{} }

func (r echoHelperRunner) Run(ctx context.Context, request kohelper.Request, stdout, stderr io.Writer) kohelper.Result {
	if request.Argv[0] == "wait" {
		close(r.started)
		<-ctx.Done()
		return kohelper.Result{Code: 130, Error: ctx.Err().Error()}
	}
	_, _ = stdout.Write(append([]byte{0, 255, 13, 10}, []byte(request.Argv[0])...))
	_, _ = stderr.Write([]byte("diagnostic"))
	if len(request.Argv) == 2 {
		return kohelper.Result{Code: 17, Error: "tool failed"}
	}
	return kohelper.Result{}
}

func TestHelperUsesIsolatedAgentProcesses(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/namespaces/ovn-system/pods/agent-a" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.MarshalWrite(w, &corev1.Pod{Name: "agent-a", Namespace: "ovn-system", Labels: map[string]string{"app": "kubectl-ko-node-agent"}})
			return
		}
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/namespaces/ovn-system/pods/agent-a/exec" ||
			r.URL.Query().Get("container") != "agent" ||
			!slices.Equal(r.URL.Query()["command"], []string{"/kube-ovn/kubectl-ko-node-agent", "--stdio"}) {
			t.Errorf("unexpected helper request: %s %s", r.Method, r.URL)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		calls.Add(1)
		conn := wsstream.NewConn(map[string]wsstream.ChannelProtocolConfig{
			remotecommand.StreamProtocolV5Name: {Binary: true, Channels: []wsstream.ChannelType{wsstream.ReadChannel, wsstream.WriteChannel, wsstream.IgnoreChannel, wsstream.WriteChannel, wsstream.IgnoreChannel}},
		})
		_, streams, err := conn.Open(w, r)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		_ = kohelper.Serve(t.Context(), &kohelper.StreamConn{Reader: streams[0], Writer: streams[1]}, echoHelperRunner{started})
		_ = json.MarshalWrite(streams[3], metav1.Status{Status: metav1.StatusSuccess})
	}))
	defer server.Close()
	cfg := &rest.Config{Host: server.URL}
	client, err := kubernetes.NewForConfig(cfg)
	require.NoError(t, err)
	executor := &helperExecutor{client: client, legacy: &remoteExecutor{client: client, config: cfg}}
	target := Target{Namespace: "ovn-system", Pod: "agent-a", Container: "agent"}
	invoke := func(argv []string, code int) {
		var stdout, stderr bytes.Buffer
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		err := executor.Exec(ctx, target, argv, Streams{Out: &stdout, ErrOut: &stderr})
		if got := ExitCode(err); got != code {
			t.Errorf("exit code = %d, want %d: %v", got, code, err)
		}
		if !bytes.Equal(stdout.Bytes(), append([]byte{0, 255, 13, 10}, []byte(argv[0])...)) || stderr.String() != "diagnostic" {
			t.Errorf("streams corrupted: stdout=%q stderr=%q", stdout.Bytes(), stderr.String())
		}
	}
	// Consecutive calls must work without waiting for a container restart.
	invoke([]string{"first"}, 0)
	invoke([]string{"second", "fail"}, 17)
	var group sync.WaitGroup
	for _, payload := range []string{"left", "right"} {
		group.Go(func() { invoke([]string{payload}, 0) })
	}
	group.Wait()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- executor.Exec(ctx, target, []string{"wait"}, Streams{Out: io.Discard}) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("helper command did not start")
	}
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
		require.Equal(t, 130, ExitCode(err))
	case <-time.After(5 * time.Second):
		t.Fatal("helper cancellation did not return")
	}
	require.EqualValues(t, 5, calls.Load(), "operations must not be replayed")
}

func TestHelperRejectsComponentTargets(t *testing.T) {
	for _, label := range []string{"kube-ovn-cni", "ovs", "ovs-ovn", "ovs-dpdk", "openvswitch", "ovn-central", "ovn-ic-server", "kube-ovn-pinger", "kube-ovn-controller", "kube-ovn-monitor"} {
		t.Run(label, func(t *testing.T) {
			client := fake.NewClientset(&corev1.Pod{Name: "component", Namespace: "ovn-system", Labels: map[string]string{"app": label}})
			// Even a container named agent must not bypass component protection.
			executor := &helperExecutor{client: client}
			for _, container := range []string{"agent", "openvswitch", "cni-server"} {
				err := executor.Exec(t.Context(), Target{Namespace: "ovn-system", Pod: "component", Container: container}, []string{"show"}, Streams{})
				require.ErrorContains(t, err, "component exec is disabled")
			}
		})
	}
}

func TestHelperRejectsInteractiveStreams(t *testing.T) {
	client := fake.NewClientset(&corev1.Pod{Name: "agent", Namespace: "ovn-system", Labels: map[string]string{"app": "kubectl-ko-node-agent"}})
	executor := &helperExecutor{client: client}
	err := executor.Exec(t.Context(), Target{Namespace: "ovn-system", Pod: "agent", Container: "agent"}, []string{"sh"}, Streams{In: bytes.NewBufferString("input")})
	require.ErrorContains(t, err, "interactive exec is not supported")
	err = executor.Exec(t.Context(), Target{Namespace: "ovn-system", Pod: "agent", Container: "agent"}, []string{"sh"}, Streams{TTY: true})
	require.ErrorContains(t, err, "interactive exec is not supported")
}

func TestHelperWaitsForProcessStatus(t *testing.T) {
	for _, code := range []int{0, 29} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			finished := make(chan struct{})
			release := make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/namespaces/ovn-system/pods/agent-a" {
					w.Header().Set("Content-Type", "application/json")
					_ = json.MarshalWrite(w, &corev1.Pod{Name: "agent-a", Namespace: "ovn-system", Labels: map[string]string{"app": "kubectl-ko-node-agent"}})
					return
				}
				calls.Add(1)
				conn := wsstream.NewConn(map[string]wsstream.ChannelProtocolConfig{
					remotecommand.StreamProtocolV5Name: {Binary: true, Channels: []wsstream.ChannelType{wsstream.ReadChannel, wsstream.WriteChannel, wsstream.IgnoreChannel, wsstream.WriteChannel, wsstream.IgnoreChannel}},
				})
				_, streams, err := conn.Open(w, r)
				if err != nil {
					t.Errorf("upgrade: %v", err)
					return
				}
				defer conn.Close()
				_ = kohelper.Serve(t.Context(), &kohelper.StreamConn{Reader: streams[0], Writer: streams[1]}, echoHelperRunner{})
				close(finished)
				<-release
				status := metav1.Status{Status: metav1.StatusSuccess}
				if code != 0 {
					status = metav1.Status{Status: metav1.StatusFailure, Reason: remotecommand.NonZeroExitCodeReason, Details: &metav1.StatusDetails{Causes: []metav1.StatusCause{{Type: remotecommand.ExitCodeCauseType, Message: strconv.Itoa(code)}}}}
				}
				_ = json.MarshalWrite(streams[3], status)
			}))
			t.Cleanup(func() { unblock(); server.Close() })
			cfg := &rest.Config{Host: server.URL}
			client, err := kubernetes.NewForConfig(cfg)
			require.NoError(t, err)
			executor := &helperExecutor{client: client, legacy: &remoteExecutor{client: client, config: cfg}}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				done <- executor.Exec(ctx, Target{Namespace: "ovn-system", Pod: "agent-a", Container: "agent"}, []string{"success"}, Streams{Out: io.Discard})
			}()
			select {
			case <-finished:
			case <-ctx.Done():
				t.Fatal("helper did not finish")
			}
			select {
			case err := <-done:
				t.Fatalf("returned before process status: %v", err)
			case <-time.After(100 * time.Millisecond):
			}
			unblock()
			require.Equal(t, code, ExitCode(<-done))
			require.EqualValues(t, 1, calls.Load(), "process failure must not replay the request")
		})
	}
}
