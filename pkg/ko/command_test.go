package ko

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/kubernetes/fake"
	utilexec "k8s.io/client-go/util/exec"
)

type execCall struct {
	target Target
	argv   []string
}
type recordingExecutor struct {
	calls []execCall
	run   func(context.Context, Target, []string, Streams) error
}

func (r *recordingExecutor) Exec(ctx context.Context, target Target, argv []string, streams Streams) error {
	r.calls = append(r.calls, execCall{target: target, argv: slices.Clone(argv)})
	if r.run != nil {
		return r.run(ctx, target, argv, streams)
	}
	return nil
}

func readyPod(name, node, container string, labels map[string]string) *corev1.Pod {
	return &corev1.Pod{
		Name: name, Namespace: "ovn-system", UID: "uid-" + types.UID(name), Labels: labels,
		Spec:   corev1.PodSpec{NodeName: node, Containers: []corev1.Container{{Name: container}}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Name: container, Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}},
	}
}

func testApplication(t *testing.T, objects ...runtime.Object) (*Application, *recordingExecutor, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var out, stderr bytes.Buffer
	app := New(genericiooptions.IOStreams{Out: &out, ErrOut: &stderr})
	executor := &recordingExecutor{}
	client := &Client{Kubernetes: fake.NewClientset(objects...), Executor: executor, Namespace: "ovn-system", WorkloadNamespace: "app", DiscoveryTimeout: 10 * time.Millisecond}
	app.newClient = func() (*Client, error) { return client, nil }
	return app, executor, &out, &stderr
}

func TestPassthroughArguments(t *testing.T) {
	tests := []struct {
		name, role, binary string
		args               []string
	}{
		{"nbctl", "nb", "ovn-nbctl", []string{"--format=json", "--timeout=5", "--", "ls-add", "a b", "--", "set", "Logical_Switch", "a b", "external_ids:note=semi;colon"}},
		{"sbctl", "sb", "ovn-sbctl", []string{"--help"}},
		{"ic-nbctl", "ic-nb", "ovn-ic-nbctl", []string{"--version"}},
		{"ic-sbctl", "ic-sb", "ovn-ic-sbctl", []string{"show"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			container := "ovn-central"
			if tt.role == "ic-nb" || tt.role == "ic-sb" {
				container = "ovn-ic-server"
			}
			pod := readyPod("leader", "worker", container, map[string]string{"ovn-" + tt.role + "-leader": "true"})
			app, executor, out, stderr := testApplication(t, pod)
			data := []byte{0, 1, 255, 13, 10, 0}
			executor.run = func(_ context.Context, _ Target, _ []string, s Streams) error {
				_, err := s.Out.Write(data)
				if err != nil {
					return err
				}
				_, err = io.WriteString(s.ErrOut, "diagnostic\n")
				return err
			}
			args := append([]string{"exec", "--context", "lab", tt.name, "--namespace", "workload", "--"}, tt.args...)
			require.NoError(t, app.Execute(t.Context(), args))
			require.Equal(t, "lab", *app.config.Context)
			require.Equal(t, "workload", *app.config.Namespace)
			require.Len(t, executor.calls, 1)
			require.Equal(t, append([]string{tt.binary}, tt.args...), executor.calls[0].argv)
			require.Equal(t, data, out.Bytes())
			require.Equal(t, "diagnostic\n", stderr.String())
		})
	}
}

func TestNodePassthrough(t *testing.T) {
	for _, name := range []string{"vsctl", "ofctl", "dpctl", "appctl"} {
		t.Run(name, func(t *testing.T) {
			pod := readyPod("ovs-a", "worker", "openvswitch", map[string]string{"app": "ovs"})
			app, executor, _, _ := testApplication(t, pod, &corev1.Node{Name: "worker"})
			require.NoError(t, app.Execute(t.Context(), []string{"exec", name, "--node", "worker", "--", "--timeout=7", "--", "show"}))
			require.Equal(t, []string{"ovs-" + name, "--timeout=7", "--", "show"}, executor.calls[0].argv)
			require.Equal(t, "openvswitch", executor.calls[0].target.Container)
		})
	}
}

func TestLocalCommandsDoNotConnect(t *testing.T) {
	for _, args := range [][]string{{}, {"help"}, {"help", "exec", "nbctl"}, {"exec", "nbctl", "--help"}, {"db", "nb", "--help"}, {"capture", "--help"}, {"trace", "--help"}, {"diagnose", "--help"}, {"perf", "--help"}, {"completion", "bash"}, {"--help"}, {"version"}} {
		t.Run(fmt.Sprint(args), func(t *testing.T) {
			app, _, out, _ := testApplication(t)
			app.newClient = func() (*Client, error) { t.Fatal("local command connected to Kubernetes"); return nil, nil }
			require.NoError(t, app.Execute(t.Context(), args))
			require.NotEmpty(t, out.String())
		})
	}
}

func TestDiscoveryOnlyNeedsRequestedLeader(t *testing.T) {
	leader := readyPod("central-a", "worker", "ovn-central", map[string]string{"ovn-nb-leader": "true"})
	leader.Spec.Containers = append(leader.Spec.Containers, corev1.Container{Name: "sidecar"})
	app, executor, _, _ := testApplication(t, leader)
	require.NoError(t, app.Execute(t.Context(), []string{"exec", "nbctl", "--", "show"}))
	require.Equal(t, "central-a", executor.calls[0].target.Pod)
}

func TestDiscoveryRejectsAmbiguityAndTerminatingTargets(t *testing.T) {
	for _, terminating := range []bool{false, true} {
		t.Run(strconv.FormatBool(terminating), func(t *testing.T) {
			first := readyPod("a", "worker", "ovn-central", map[string]string{"ovn-nb-leader": "true"})
			second := first.DeepCopy()
			second.Name = "b"
			if terminating {
				first.DeletionTimestamp = new(metav1.Now())
				second.DeletionTimestamp = new(metav1.Now())
			}
			app, executor, _, _ := testApplication(t, first, second)
			err := app.Execute(t.Context(), []string{"exec", "nbctl", "--", "show"})
			require.ErrorContains(t, err, "expected one ready container")
			require.Empty(t, executor.calls)
		})
	}
}

func TestRemoteExitCode(t *testing.T) {
	err := fmt.Errorf("remote command: %w", utilexec.CodeExitError{Err: errors.New("failed"), Code: 42})
	require.Equal(t, 42, ExitCode(err))
	require.Equal(t, 130, ExitCode(context.Canceled))
	require.Equal(t, 2, ExitCode(&usageError{errors.New("usage")}))
}
