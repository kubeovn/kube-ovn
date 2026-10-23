package ko

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/remotecommand"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/streaming/pkg/httpstream"
	"k8s.io/streaming/pkg/httpstream/spdy"
	"k8s.io/streaming/pkg/httpstream/wsstream"
)

func TestWebSocketBinaryStreamsAndExitStatus(t *testing.T) {
	for _, code := range []int{0, 42} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			var calls atomic.Int32
			payload := []byte{0, 1, 255, 13, 10, 0}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodGet || r.URL.Path != "/api/v1/namespaces/ovn-system/pods/central/exec" {
					t.Errorf("unexpected exec request %s %s", r.Method, r.URL.Path)
					http.Error(w, "bad request", http.StatusBadRequest)
					return
				}
				if got := r.URL.Query()["command"]; len(got) != 3 || got[2] != "a b;$(false)" {
					t.Errorf("argv changed: %q", got)
				}
				conn := wsstream.NewConn(map[string]wsstream.ChannelProtocolConfig{
					remotecommand.StreamProtocolV5Name: {Binary: true, Channels: []wsstream.ChannelType{wsstream.ReadChannel, wsstream.WriteChannel, wsstream.WriteChannel, wsstream.WriteChannel, wsstream.IgnoreChannel}},
				})
				_, streams, err := conn.Open(w, r)
				if err != nil {
					t.Errorf("upgrade: %v", err)
					return
				}
				defer conn.Close()
				if _, err := io.Copy(streams[1], streams[0]); err != nil {
					t.Errorf("copy stdin: %v", err)
					return
				}
				if _, err := io.WriteString(streams[2], "stderr\n"); err != nil {
					t.Errorf("write stderr: %v", err)
					return
				}
				status := metav1.Status{Status: metav1.StatusSuccess}
				if code != 0 {
					status = metav1.Status{Status: metav1.StatusFailure, Reason: remotecommand.NonZeroExitCodeReason, Details: &metav1.StatusDetails{Causes: []metav1.StatusCause{{Type: remotecommand.ExitCodeCauseType, Message: strconv.Itoa(code)}}}}
				}
				if err := json.MarshalWrite(streams[3], status); err != nil {
					t.Errorf("write status: %v", err)
				}
			}))
			defer server.Close()
			cfg := &rest.Config{Host: server.URL}
			cs, err := kubernetes.NewForConfig(cfg)
			require.NoError(t, err)
			executor := &remoteExecutor{client: cs, config: cfg}
			var stdout, stderr bytes.Buffer
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			err = executor.Exec(ctx, Target{Namespace: "ovn-system", Pod: "central", Container: "ovn-central"}, []string{"tool", "--", "a b;$(false)"}, Streams{In: bytes.NewReader(payload), Out: &stdout, ErrOut: &stderr})
			require.Equal(t, code, ExitCode(err))
			require.Equal(t, payload, stdout.Bytes())
			require.Equal(t, "stderr\n", stderr.String())
			require.EqualValues(t, 1, calls.Load(), "remote failures must not replay")
		})
	}
}

func TestExecCancellation(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn := wsstream.NewConn(map[string]wsstream.ChannelProtocolConfig{remotecommand.StreamProtocolV5Name: {Binary: true, Channels: []wsstream.ChannelType{wsstream.ReadChannel, wsstream.WriteChannel, wsstream.IgnoreChannel, wsstream.WriteChannel, wsstream.IgnoreChannel}}})
		_, streams, err := conn.Open(w, r)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		close(started)
		if _, err := io.Copy(io.Discard, streams[0]); err != nil {
			t.Logf("stream closed: %v", err)
		}
	}))
	defer server.Close()
	cfg := &rest.Config{Host: server.URL}
	cs, err := kubernetes.NewForConfig(cfg)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- (&remoteExecutor{client: cs, config: cfg}).Exec(ctx, Target{Namespace: "ns", Pod: "pod", Container: "c"}, []string{"listen"}, Streams{Out: io.Discard})
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not start")
	}
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation did not stop exec")
	}
}

func TestFallbackOnlyForHandshakeFailure(t *testing.T) {
	require.True(t, retryUpgrade(&httpstream.UpgradeFailureError{Cause: errors.New("upgrade rejected")}))
	require.False(t, retryUpgrade(errors.New("connection closed after command execution")))
	require.False(t, retryUpgrade(context.DeadlineExceeded))
}

func TestWebSocketRejectionFallsBackToSPDY(t *testing.T) {
	var upgrades, executions atomic.Int32
	payload := []byte{0, 255, 13, 10, 0, 128}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			upgrades.Add(1)
			http.Error(w, "WebSocket unavailable", http.StatusBadRequest)
			return
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method %s", r.Method)
			return
		}
		executions.Add(1)
		serveSPDY(t, w, r, payload)
	}))
	defer server.Close()
	cfg := &rest.Config{Host: server.URL}
	cs, err := kubernetes.NewForConfig(cfg)
	require.NoError(t, err)
	var stdout, stderr bytes.Buffer
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	err = (&remoteExecutor{client: cs, config: cfg}).Exec(ctx, Target{Namespace: "ns", Pod: "pod", Container: "c"}, []string{"tool"}, Streams{Out: &stdout, ErrOut: &stderr})
	require.Equal(t, 42, ExitCode(err))
	require.Equal(t, payload, stdout.Bytes())
	require.Equal(t, "remote diagnostic", stderr.String())
	require.EqualValues(t, 1, upgrades.Load())
	require.EqualValues(t, 1, executions.Load(), "SPDY remote failure must not replay the command")
}

func serveSPDY(t *testing.T, w http.ResponseWriter, r *http.Request, payload []byte) {
	t.Helper()
	if _, err := httpstream.Handshake(r, w, []string{remotecommand.StreamProtocolV4Name}); err != nil {
		t.Errorf("handshake: %v", err)
		return
	}
	type streamReply struct {
		stream  httpstream.Stream
		replied <-chan struct{}
	}
	created := make(chan streamReply, 3)
	conn := spdy.NewResponseUpgrader().UpgradeResponse(w, r, func(stream httpstream.Stream, replied <-chan struct{}) error {
		created <- streamReply{stream, replied}
		return nil
	})
	if conn == nil {
		t.Error("SPDY upgrade failed")
		return
	}
	defer conn.Close()
	streams := map[string]httpstream.Stream{}
	for range 3 {
		select {
		case item := <-created:
			<-item.replied
			streams[item.stream.Headers().Get(corev1.StreamType)] = item.stream
		case <-time.After(5 * time.Second):
			t.Error("SPDY streams did not arrive")
			return
		}
	}
	for kind, data := range map[string][]byte{corev1.StreamTypeStdout: payload, corev1.StreamTypeStderr: []byte("remote diagnostic")} {
		if _, err := streams[kind].Write(data); err != nil {
			t.Errorf("write %s: %v", kind, err)
			return
		}
		if err := streams[kind].Close(); err != nil {
			t.Errorf("close %s: %v", kind, err)
			return
		}
	}
	status := metav1.Status{Status: metav1.StatusFailure, Reason: remotecommand.NonZeroExitCodeReason, Details: &metav1.StatusDetails{Causes: []metav1.StatusCause{{Type: remotecommand.ExitCodeCauseType, Message: "42"}}}}
	if err := json.MarshalWrite(streams[corev1.StreamTypeError], status); err != nil {
		t.Errorf("write status: %v", err)
	}
}
