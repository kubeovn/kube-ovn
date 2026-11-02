package ko

import (
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/cli-runtime/pkg/genericiooptions"
)

func TestParseSourceRef(t *testing.T) {
	tests := []struct {
		name, value string
		want        sourceRef
	}{
		{"bare pod", "web", sourceRef{kind: sourceKindPod, name: "web"}},
		{"pod", "pod/app/web", sourceRef{kind: sourceKindPod, namespace: "app", name: "web"}},
		{"pod alias", "po/app/web", sourceRef{kind: sourceKindPod, namespace: "app", name: "web"}},
		{"pod plural", "pods/app/web", sourceRef{kind: sourceKindPod, namespace: "app", name: "web"}},
		{"node", "node/worker-a", sourceRef{kind: sourceKindNode, name: "worker-a"}},
		{"node alias", "no/worker-a", sourceRef{kind: sourceKindNode, name: "worker-a"}},
		{"service", "service/app/api", sourceRef{kind: sourceKindService, namespace: "app", name: "api"}},
		{"service alias", "svc/app/api", sourceRef{kind: sourceKindService, namespace: "app", name: "api"}},
		{"service plural", "services/app/api", sourceRef{kind: sourceKindService, namespace: "app", name: "api"}},
		{"deployment alias", "deploy/app/api", sourceRef{kind: sourceKindDeployment, namespace: "app", name: "api"}},
		{"daemonset alias", "ds/app/agent", sourceRef{kind: sourceKindDaemonSet, namespace: "app", name: "agent"}},
		{"vm", "vm/app/database", sourceRef{kind: sourceKindVM, namespace: "app", name: "database"}},
		{"vmi", "vmi/app/database", sourceRef{kind: sourceKindVMI, namespace: "app", name: "database"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSourceRef(tt.value)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestParseSourceRefRejectsMalformedReferences(t *testing.T) {
	for _, value := range []string{"", "/web", "app/", "app/web", "pod//web", "pod/app/", "node//worker-a", "node/app/worker", "pod/app/web/extra", "bad kind/web"} {
		t.Run(value, func(t *testing.T) {
			_, err := parseSourceRef(value)
			require.Error(t, err)
		})
	}
}

func TestTraceRequestAcceptsTargetAndSourceReferences(t *testing.T) {
	request, err := (traceOptions{target: "addr/2001:db8::2", protocol: "icmp", engine: "ovn"}).request(sourceRef{kind: sourceKindPod, namespace: "app", name: "web"})
	require.NoError(t, err)
	require.Equal(t, "pod/app/web", request.reference)
	require.Equal(t, "2001:db8::2", request.destination.String())
}

func TestCommandsAcceptPositionalSources(t *testing.T) {
	sentinel := errors.New("client creation reached")
	for _, args := range [][]string{
		{"trace", "po/app/web", "--target", "addr/10.0.0.2"},
		{"trace", "svc/app/api", "--target", "addr/10.0.0.2"},
		{"trace", "no/worker-a", "--target", "addr/10.0.0.2"},
		{"capture", "pod/app/web", "--", "-c", "1"},
		{"capture", "vmi/app/database", "--", "-c", "1"},
		{"capture", "service/app/api", "--", "-c", "1"},
		{"capture", "node/worker-a", "--", "-D"},
	} {
		t.Run(args[0]+" "+args[1], func(t *testing.T) {
			app := New(genericiooptions.IOStreams{Out: io.Discard, ErrOut: io.Discard})
			app.newClient = func() (*Client, error) { return nil, sentinel }
			require.ErrorIs(t, app.Execute(t.Context(), args), sentinel)
		})
	}
}
