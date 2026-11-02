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
		{"pod", "pod/web", sourceRef{kind: sourceKindPod, name: "web"}},
		{"pod alias", "po/web", sourceRef{kind: sourceKindPod, name: "web"}},
		{"pod plural", "pods/web", sourceRef{kind: sourceKindPod, name: "web"}},
		{"node", "node/worker-a", sourceRef{kind: sourceKindNode, name: "worker-a"}},
		{"node alias", "no/worker-a", sourceRef{kind: sourceKindNode, name: "worker-a"}},
		{"service", "service/api", sourceRef{kind: sourceKindService, name: "api"}},
		{"service alias", "svc/api", sourceRef{kind: sourceKindService, name: "api"}},
		{"service plural", "services/api", sourceRef{kind: sourceKindService, name: "api"}},
		{"deployment alias", "deploy/api", sourceRef{kind: sourceKindDeployment, name: "api"}},
		{"daemonset alias", "ds/agent", sourceRef{kind: sourceKindDaemonSet, name: "agent"}},
		{"vm", "vm/database", sourceRef{kind: sourceKindVM, name: "database"}},
		{"vmi", "vmi/database", sourceRef{kind: sourceKindVMI, name: "database"}},
		{"localnet", "localnet/underlay", sourceRef{kind: sourceKindLocalnet, name: "underlay"}},
		{"lsp", "lsp/localnet.underlay", sourceRef{kind: sourceKindLSP, name: "localnet.underlay"}},
		{"lsp full name", "logicalswitchport/port_1", sourceRef{kind: sourceKindLSP, name: "port_1"}},
		{"lrp", "lrp/router-underlay", sourceRef{kind: sourceKindLRP, name: "router-underlay"}},
		{"lrp full name", "logicalrouterport/port_1", sourceRef{kind: sourceKindLRP, name: "port_1"}},
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
	for _, value := range []string{"", "/web", "app/", "app/web", "pod//web", "pod/app/", "node//worker-a", "node/app/worker", "svc/app/api", "pod/app/web/extra", "bad kind/web"} {
		t.Run(value, func(t *testing.T) {
			_, err := parseSourceRef(value)
			require.Error(t, err)
		})
	}
}

func TestTraceRequestAcceptsTargetAndSourceReferences(t *testing.T) {
	request, err := (traceOptions{target: "addr/2001:db8::2", protocol: "icmp", engine: "ovn"}).request(sourceRef{kind: sourceKindPod, name: "web"})
	require.NoError(t, err)
	require.Equal(t, "pod/web", request.reference)
	require.Equal(t, "2001:db8::2", request.destination.String())
}

func TestCommandsAcceptPositionalSources(t *testing.T) {
	sentinel := errors.New("client creation reached")
	for _, args := range [][]string{
		{"trace", "-n", "app", "po/web", "--target", "addr/10.0.0.2"},
		{"trace", "-n", "app", "svc/api", "--target", "addr/10.0.0.2"},
		{"trace", "no/worker-a", "--target", "addr/10.0.0.2"},
		{"capture", "-n", "app", "pod/web", "--", "-c", "1"},
		{"capture", "-n", "app", "vmi/database", "--", "-c", "1"},
		{"capture", "-n", "app", "service/api", "--", "-c", "1"},
		{"capture", "node/worker-a", "--", "-D"},
		{"trace", "localnet/underlay", "--target", "addr/10.0.0.2", "--source-ip", "10.0.0.3", "--source-mac", "02:00:00:00:00:03"},
		{"trace", "lsp/web.app", "--target", "addr/10.0.0.2"},
		{"trace", "lrp/router-underlay", "--target", "addr/10.0.0.2"},
		{"capture", "lsp/web.app", "--", "-c", "1"},
		{"capture", "lrp/router-underlay", "--observe-at", "lsp/web.app", "--", "-c", "1"},
	} {
		t.Run(args[0]+" "+args[1], func(t *testing.T) {
			app := New(genericiooptions.IOStreams{Out: io.Discard, ErrOut: io.Discard})
			app.newClient = func() (*Client, error) { return nil, sentinel }
			require.ErrorIs(t, app.Execute(t.Context(), args), sentinel)
		})
	}
}
