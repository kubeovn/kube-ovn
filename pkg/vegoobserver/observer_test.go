package vegoobserver

import (
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/time/rate"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"github.com/ti-mo/conntrack"
	"golang.org/x/sys/unix"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func TestRecordFromFlowPreservesNATIdentityAndAccountingAvailability(t *testing.T) {
	flow := conntrack.NewFlow(unix.IPPROTO_TCP, 0, netip.MustParseAddr("10.0.0.2"), netip.MustParseAddr("192.0.2.10"), 12345, 443, 30, 0)
	flow.ID, flow.Zone = 42, 7
	flow.TupleReply.IP.DestinationAddress = netip.MustParseAddr("198.51.100.5")
	record, ok := recordFromFlow(&flow, []string{"ns", "gateway", "pod", "node"})
	require.True(t, ok)
	require.Equal(t, uint32(42), record.ConntrackID)
	require.Equal(t, uint16(7), record.Zone)
	require.Equal(t, []string{apiv1.ObservabilityNatTypeSNAT}, record.NatType)
	require.Equal(t, apiv1.ObservabilityProtocolTCP, record.Protocol)
	require.Nil(t, record.Counters)

	flow.CountersOrig.Packets, flow.CountersOrig.Bytes = 1, 64
	record, ok = recordFromFlow(&flow, []string{"ns", "gateway", "pod", "node"})
	require.True(t, ok)
	require.Equal(t, uint64(1), *record.Counters.OriginalPackets)
	require.Equal(t, uint64(64), *record.Counters.OriginalBytes)
}

func TestCompiledFiltersUseExcludePrecedenceAndPortRanges(t *testing.T) {
	filters, err := compileFilters(apiv1.VpcEgressGatewayConntrackLogFilters{
		Include: []apiv1.VpcEgressGatewayConntrackLogFilter{{
			Protocols: []string{apiv1.ObservabilityProtocolTCP},
			Original:  apiv1.VpcEgressGatewayConntrackTupleFilter{DestinationPorts: []apiv1.VpcEgressGatewayPortRange{{Start: 80, End: 90}}},
		}},
		Exclude: []apiv1.VpcEgressGatewayConntrackLogFilter{{
			Original: apiv1.VpcEgressGatewayConntrackTupleFilter{SourceCIDRs: []string{"10.0.0.0/24"}},
		}},
	})
	require.NoError(t, err)
	record := flowRecord{AddressFamily: "ipv4", Protocol: "tcp", NatType: []string{"snat"}, Original: flowTuple{SourceIP: "10.0.0.2", DestinationIP: "192.0.2.1", DestinationPort: 85}}
	require.False(t, filters.match(record))
	record.Original.SourceIP = "10.1.0.2"
	require.True(t, filters.match(record))
	record.Original.DestinationPort = 443
	require.False(t, filters.match(record))
}

func TestInterfaceCollectorCachesResolvedInterfacesAndUsesDeltas(t *testing.T) {
	directory := t.TempDir()
	networkStatusPath := filepath.Join(directory, "network-status")
	procNetDevPath := filepath.Join(directory, "net-dev")
	require.NoError(t, os.WriteFile(networkStatusPath, []byte(`[{"name":"ovn","interface":"eth0","default":true},{"name":"ns/external","interface":"net1"}]`), 0o600))
	require.NoError(t, os.WriteFile(procNetDevPath, []byte("Inter-| Receive | Transmit\n eth0: 10 1 0 0 0 0 0 0 20 2 0 0 0 0 0 0\n net1: 30 3 0 0 0 0 0 0 40 4 0 0 0 0 0 0\n"), 0o600))
	collector := newInterfaceCollector(networkStatusPath)
	collector.procNetDevPath = procNetDevPath
	metrics := newObserverMetrics()
	identity := []string{"ns", "gateway", "pod", "node"}
	require.NoError(t, collector.update(Config{ExternalNetwork: "ns/external"}, identity, metrics))
	require.Equal(t, float64(30), testutil.ToFloat64(metrics.interfaceCounters["rx_bytes"].WithLabelValues("ns", "gateway", "pod", "node", "net1", "external")))

	require.NoError(t, os.WriteFile(networkStatusPath, []byte(`not valid JSON`), 0o600))
	require.NoError(t, os.WriteFile(procNetDevPath, []byte("Inter-| Receive | Transmit\n eth0: 15 2 0 0 0 0 0 0 25 3 0 0 0 0 0 0\n net1: 35 4 0 0 0 0 0 0 45 5 0 0 0 0 0 0\n"), 0o600))
	require.NoError(t, collector.update(Config{ExternalNetwork: "ns/external"}, identity, metrics))
	require.Equal(t, float64(35), testutil.ToFloat64(metrics.interfaceCounters["rx_bytes"].WithLabelValues("ns", "gateway", "pod", "node", "net1", "external")))
}

func TestPrivateRegistryDoesNotExposeProcessCollectors(t *testing.T) {
	metrics := newObserverMetrics()
	families, err := metrics.registry.Gather()
	require.NoError(t, err)
	for _, family := range families {
		require.NotContains(t, family.GetName(), "go_")
		require.NotContains(t, family.GetName(), "process_")
		require.NotContains(t, family.GetName(), "promhttp_")
	}
}

func TestNewEventQueuedDuringInitialDumpStillProducesStart(t *testing.T) {
	flow := conntrack.NewFlow(unix.IPPROTO_TCP, 0, netip.MustParseAddr("10.0.0.2"), netip.MustParseAddr("192.0.2.10"), 12345, 443, 30, 0)
	flow.ID = 99
	flow.TupleReply.IP.DestinationAddress = netip.MustParseAddr("198.51.100.5")
	identity := []string{"ns", "gateway", "pod", "node"}
	metrics := newObserverMetrics()
	settings := &runtimeSettings{
		config: Config{Observability: apiv1.VpcEgressGatewayObservability{Conntrack: apiv1.VpcEgressGatewayConntrackObservability{
			Metrics: apiv1.VpcEgressGatewayObservabilityFeature{Enabled: true},
			Log:     apiv1.VpcEgressGatewayConntrackLog{Enabled: true},
		}}},
		limiter: rate.NewLimiter(100, 1000),
	}
	queue := make(chan flowRecord, 1)
	collector := newConntrackCollector(func() *runtimeSettings { return settings }, identity, metrics, queue)
	collector.initialize([]conntrack.Flow{flow})
	require.Empty(t, queue)
	collector.processEvent(conntrack.Event{Type: conntrack.EventNew, Flow: &flow})
	require.Equal(t, apiv1.ObservabilityEventStart, (<-queue).Event)
	require.Equal(t, float64(1), testutil.ToFloat64(metrics.startedFlows.WithLabelValues("ns", "gateway", "pod", "node", "ipv4", "tcp", "snat")))
}
