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
		{"qualified pod", "app/web", sourceRef{kind: sourceKindPod, namespace: "app", name: "web"}},
		{"pod alias", "po/app/web", sourceRef{kind: sourceKindPod, namespace: "app", name: "web"}},
		{"node", "node/worker-a", sourceRef{kind: sourceKindNode, name: "worker-a"}},
		{"vm", "vm/app/database", sourceRef{kind: sourceKindVM, namespace: "app", name: "database"}},
		{"vmi", "vmi/app/database", sourceRef{kind: sourceKindVMI, namespace: "app", name: "database"}},
		{"legacy node", "node//worker-a", sourceRef{kind: sourceKindNode, name: "worker-a"}},
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
	for _, value := range []string{"", "/web", "app/", "pod//web", "pod/app/", "node/app/worker", "pod/app/web/extra", "bad kind/web"} {
		t.Run(value, func(t *testing.T) {
			_, err := parseSourceRef(value)
			require.Error(t, err)
		})
	}
}

func TestTraceRequestAcceptsTargetAndSourceReferences(t *testing.T) {
	request, err := (traceOptions{source: "po/app/web", target: "addr/2001:db8::2", protocol: "icmp", engine: "ovn"}).request()
	require.NoError(t, err)
	require.Equal(t, "pod/app/web", request.reference)
	require.Equal(t, "2001:db8::2", request.destination.String())
}

func TestCaptureSourceRefUsesSharedNotation(t *testing.T) {
	ref, err := (captureOptions{source: "vmi/app/database"}).sourceRef()
	require.NoError(t, err)
	require.Equal(t, sourceRef{kind: sourceKindVMI, namespace: "app", name: "database"}, ref)

	_, err = (captureOptions{source: "node/worker-a"}).sourceRef()
	require.ErrorContains(t, err, "does not support node sources")
}

func TestCommandsAcceptPositionalSources(t *testing.T) {
	sentinel := errors.New("client creation reached")
	for _, args := range [][]string{
		{"trace", "app/web", "--target", "addr/10.0.0.2"},
		{"trace", "--source", "pod/app/web", "--target", "10.0.0.2"},
		{"capture", "app/web", "--", "-c", "1"},
		{"capture", "--source", "vmi/app/database", "--", "-c", "1"},
	} {
		t.Run(args[0]+" "+args[1], func(t *testing.T) {
			app := New(genericiooptions.IOStreams{Out: io.Discard, ErrOut: io.Discard})
			app.newClient = func() (*Client, error) { return nil, sentinel }
			require.ErrorIs(t, app.Execute(t.Context(), args), sentinel)
		})
	}
}
