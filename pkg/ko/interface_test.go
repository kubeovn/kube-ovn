package ko

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestInvalidInterfaceArgumentsNeverConnect(t *testing.T) {
	cases := [][]string{
		{"trace", "--pod", "node//worker", "--dst-ip", "10.0.0.1"},
		{"trace", "--node", "app/pod", "--dst-ip", "10.0.0.1"},
		{"capture", "--pod", "a/b/c", "--", "-c1"},
		{"capture", "--pod", "/pod", "--", "-c1"},
		{"diagnose", "node", ""},
		{"diagnose", "subnet", ""},
		{"exec", "vsctl", "--node", "app/pod", "--", "show"},
		{"acl", "listen", "--node", "app/pod"},
		{"db", "nb", "restore", "--source-node", "bad/name", "--yes"},
		{"nbctl", "show"},
		{"nb", "status"},
		{"tcpdump", "app/web"},
		{"log"},
		{"log", "invalid"},
		{"log", "ovn", "ovs"},
		{"log", "ovn", "--component", "ovs"},
		{"reload"},
		{"env-check"},
		{"acl-sample", "decode", "1"},
		{"exec", "nbctl", "show"},
		{"exec", "vsctl", "--", "show"},
		{"exec", "ofctl", "--node", "worker", "show", "--", "extra"},
		{"capture", "--", "-c1"},
		{"capture", "--pod", "app/web", "-c1"},
		{"trace", "--dst-ip", "10.0.0.1"},
		{"trace", "--pod", "a", "--node", "b", "--dst-ip", "10.0.0.1"},
		{"trace", "--pod", "a", "--dst-ip", "bad"},
		{"trace", "--pod", "a", "--dst-ip", "10.0.0.1", "--protocol", "tcp"},
		{"trace", "--pod", "a", "--dst-ip", "10.0.0.1", "--dst-port", "80"},
		{"trace", "--pod", "a", "--dst-ip", "10.0.0.1", "--arp-op", "reply"},
		{"trace", "--pod", "a", "--dst-ip", "2001:db8::1", "--protocol", "arp"},
		{"trace", "--pod", "a", "--dst-ip", "10.0.0.1", "--dst-mac", "invalid"},
		{"trace", "--pod", "a", "--dst-ip", "10.0.0.1", "--engine", "ovs"},
		{"diagnose", "node"},
		{"diagnose", "subnet", "s", "--tcp-port", "65536"},
		{"diagnose", "cluster", "--external-address", "example.com"},
		{"diagnose", "node", "worker", "--external-address", "fe80::1%eth0"},
		{"diagnose", "subnet", "s", "--external-address", "192.0.2.1,192.0.2.2"},
		{"diagnose", "connectivity"},
		{"diagnose", "connectivity", "--target", "tcp://example.com:53"},
		{"diagnose", "connectivity", "--target", "udp://[::1]:0"},
		{"logs", "all"},
		{"logs", "--component", "invalid"},
		{"logs", "--item-timeout", "0s"},
		{"perf", "run", "--duration", "5"},
		{"perf", "run", "--duration", "1500ms"},
		{"perf", "run", "--duration", "0s"},
		{"perf", "recovery"},
		{"db", "nb", "restore"},
		{"acl", "listen"},
		{"exec", "nbctl", "--timeout", "-1s", "--", "show"},
		{"exec", "nbctl", "--discovery-timeout", "0s", "--", "show"},
	}
	for _, args := range cases {
		t.Run(fmt.Sprint(args), func(t *testing.T) {
			app, executor, _, _ := testApplication(t)
			app.newClient = func() (*Client, error) { t.Fatal("invalid invocation connected to Kubernetes"); return nil, nil }
			err := app.Execute(t.Context(), args)
			require.Error(t, err)
			require.Equal(t, 2, ExitCode(err))
			require.Empty(t, executor.calls)
		})
	}
}

func TestCommandTimeoutAfterSubcommandBoundsRemoteExecution(t *testing.T) {
	app, executor, _, _ := testApplication(t, readyPod("nb", "node", "ovn-central", map[string]string{"ovn-nb-leader": "true"}))
	executor.run = func(ctx context.Context, _ Target, args []string, _ Streams) error {
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.Positive(t, time.Until(deadline))
		require.LessOrEqual(t, time.Until(deadline), time.Second)
		require.Equal(t, []string{"ovn-nbctl", "--timeout=9", "--", "show"}, args)
		return errors.New("remote failure")
	}
	err := app.Execute(t.Context(), []string{"exec", "nbctl", "--timeout", "1s", "--", "--timeout=9", "--", "show"})
	require.ErrorContains(t, err, "remote failure")
	require.Equal(t, 1, ExitCode(err))
}

func TestDiagnosticEndpointSyntax(t *testing.T) {
	actual, err := diagnosticTargets([]string{"tcp://192.0.2.1:443", "udp://[2001:db8::1]:53"})
	require.NoError(t, err)
	require.Equal(t, "tcp-192.0.2.1-443,udp-2001:db8::1-53", actual)
	for _, target := range []string{"http://192.0.2.1:80", "tcp://192.0.2.1", "tcp://192.0.2.1:443/path", "tcp://user@192.0.2.1:443", "udp://[fe80::1%25eth0]:53", "tcp://192.0.2.1:443?"} {
		_, err := diagnosticTargets([]string{target})
		require.Error(t, err, target)
	}
}

func TestTraceNamedOptions(t *testing.T) {
	request, err := (traceOptions{node: "worker", destination: "2001:db8::2", protocol: "tcp", port: 443, engine: "ovn"}).request()
	require.NoError(t, err)
	require.Equal(t, "node//worker", request.reference)
	require.Equal(t, uint16(443), request.port)
	require.Equal(t, "2001:db8::2", request.destination.String())
}

func TestDiagnosticFlagsOverrideEnvironment(t *testing.T) {
	t.Setenv("TCP_CONN_CHECK_PORT", "invalid")
	t.Setenv("UDP_CONN_CHECK_PORT", "invalid")
	app, _, _, _ := testApplication(t)
	sentinel := errors.New("validation completed")
	app.newClient = func() (*Client, error) { return nil, sentinel }
	err := app.Execute(t.Context(), []string{"diagnose", "subnet", "s", "--tcp-port", "08100", "--udp-port", "8101"})
	require.ErrorIs(t, err, sentinel)
}

func TestValidNamedInterfacesReachClientCreation(t *testing.T) {
	for _, args := range [][]string{
		{"trace", "--pod", "app/web", "--dst-ip", "10.0.0.1"},
		{"trace", "--node", "worker", "--dst-ip", "2001:db8::1", "--protocol", "udp", "--dst-port", "53", "--engine", "ovn"},
		{"diagnose", "connectivity", "--target", "tcp://192.0.2.1:443", "--target", "udp://[2001:db8::1]:53"},
		{"diagnose", "cluster", "--external-address", "192.0.2.1", "--external-address", "2001:db8::1"},
		{"logs"},
		{"restart"},
		{"perf", "run", "--duration", "1m"},
		{"perf", "recovery", "--yes"},
		{"db", "nb", "restore", "--source-node", "worker", "--dry-run"},
	} {
		t.Run(fmt.Sprint(args), func(t *testing.T) {
			app, _, _, _ := testApplication(t)
			sentinel := errors.New("validation completed")
			app.newClient = func() (*Client, error) { return nil, sentinel }
			require.ErrorIs(t, app.Execute(t.Context(), args), sentinel)
		})
	}
}
