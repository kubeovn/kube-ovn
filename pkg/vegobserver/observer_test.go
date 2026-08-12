package vegobserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"github.com/ti-mo/conntrack"
	"github.com/ti-mo/netfilter"
	"golang.org/x/sys/unix"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

type fakeConntrackConnection struct {
	listened        bool
	dumped          bool
	listenWorkers   uint8
	cancelAfterDump context.CancelFunc
}

func (c *fakeConntrackConnection) Close() error { return nil }

func (c *fakeConntrackConnection) SetReadBuffer(int) error { return nil }

func (c *fakeConntrackConnection) Listen(_ chan<- conntrack.Event, workers uint8, _ []netfilter.NetlinkGroup) (chan error, error) {
	c.listened = true
	c.listenWorkers = workers
	return make(chan error), nil
}

func (c *fakeConntrackConnection) Dump(*conntrack.DumpOptions) ([]conntrack.Flow, error) {
	c.dumped = true
	if c.listened {
		return nil, errors.New("cannot dump using the event connection")
	}
	if c.cancelAfterDump != nil {
		c.cancelAfterDump()
	}
	return nil, nil
}

func TestConntrackCollectorUsesSeparateEventAndDumpConnections(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	eventConnection := &fakeConntrackConnection{}
	dumpConnection := &fakeConntrackConnection{cancelAfterDump: cancel}
	connections := []conntrackConnection{eventConnection, dumpConnection}
	dialIndex := 0

	settings := &runtimeSettings{config: Config{Observability: apiv1.VpcEgressGatewayObservability{Conntrack: apiv1.VpcEgressGatewayConntrackObservability{Metrics: apiv1.VpcEgressGatewayObservabilityFeature{Enabled: true}}}}}
	collector := newConntrackCollector(func() *runtimeSettings { return settings }, observerIdentity{namespace: "ns", name: "gateway", pod: "pod", node: "node"}, newObserverMetrics(), make(chan flowRecord, 1))
	collector.dial = func() (conntrackConnection, error) {
		if dialIndex >= len(connections) {
			return nil, errors.New("unexpected conntrack dial")
		}
		connection := connections[dialIndex]
		dialIndex++
		return connection, nil
	}
	require.NoError(t, collector.runSession(ctx, &bytes.Buffer{}))
	require.Equal(t, 2, dialIndex)
	require.True(t, eventConnection.listened)
	require.Equal(t, uint8(1), eventConnection.listenWorkers, "multiple decoder workers can reorder conntrack lifecycle events")
	require.False(t, eventConnection.dumped)
	require.False(t, dumpConnection.listened)
	require.True(t, dumpConnection.dumped)
}

func TestNewConntrackCollectorDoesNotPreallocateCache(t *testing.T) {
	metrics := newObserverMetrics()
	identity := observerIdentity{namespace: "ns", name: "gateway", pod: "pod", node: "node"}
	settings := func() *runtimeSettings { return &runtimeSettings{} }
	logQueue := make(chan flowRecord, 1)

	result := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			collector := newConntrackCollector(settings, identity, metrics, logQueue)
			runtime.KeepAlive(collector)
		}
	})

	require.Less(t, result.AllocedBytesPerOp(), int64(1<<20), "new collectors must allocate cache buckets on demand")
}

func TestRecordFromFlowPreservesNATIdentityAndAccountingAvailability(t *testing.T) {
	flow := conntrack.NewFlow(unix.IPPROTO_TCP, 0, netip.MustParseAddr("10.0.0.2"), netip.MustParseAddr("192.0.2.10"), 12345, 443, 30, 0)
	flow.ID, flow.Zone = 42, 7
	flow.TupleReply.IP.DestinationAddress = netip.MustParseAddr("198.51.100.5")
	record, ok := recordFromFlow(&flow, observerIdentity{namespace: "ns", name: "gateway", pod: "pod", node: "node"})
	require.True(t, ok)
	require.Equal(t, uint32(42), record.ConntrackID)
	require.Equal(t, uint16(7), record.Zone)
	require.Equal(t, []string{apiv1.ObservabilityNatTypeSNAT}, record.NatType)
	require.Equal(t, apiv1.ObservabilityProtocolTCP, record.Protocol)
	require.Nil(t, record.Counters)

	flow.CountersOrig.Packets, flow.CountersOrig.Bytes = 1, 64
	record, ok = recordFromFlow(&flow, observerIdentity{namespace: "ns", name: "gateway", pod: "pod", node: "node"})
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
	identity := observerIdentity{namespace: "ns", name: "gateway", pod: "pod", node: "node"}
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

func TestCheckHealthUsesHTTPStatus(t *testing.T) {
	statusCode := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(statusCode)
	}))
	require.NoError(t, CheckHealth(context.Background(), server.URL))
	statusCode = http.StatusServiceUnavailable
	require.ErrorContains(t, CheckHealth(context.Background(), server.URL), "503 Service Unavailable")

	server.Close()
	require.ErrorContains(t, CheckHealth(context.Background(), server.URL), "request observer health endpoint")
}

func TestNewEventQueuedDuringInitialDumpStillProducesStart(t *testing.T) {
	flow := conntrack.NewFlow(unix.IPPROTO_TCP, 0, netip.MustParseAddr("10.0.0.2"), netip.MustParseAddr("192.0.2.10"), 12345, 443, 30, 0)
	flow.ID = 99
	flow.TupleReply.IP.DestinationAddress = netip.MustParseAddr("198.51.100.5")
	identity := observerIdentity{namespace: "ns", name: "gateway", pod: "pod", node: "node"}
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

func TestFlowLogJSONPreservesStartAndEndLifecycle(t *testing.T) {
	flow := conntrack.NewFlow(unix.IPPROTO_TCP, 0, netip.MustParseAddr("10.0.0.2"), netip.MustParseAddr("192.0.2.10"), 12345, 443, 30, 0)
	flow.ID, flow.Zone = 99, 7
	flow.TupleReply.IP.DestinationAddress = netip.MustParseAddr("198.51.100.5")
	identity := observerIdentity{namespace: "ns", name: "gateway", pod: "pod", node: "node"}
	settings := &runtimeSettings{
		config: Config{Observability: apiv1.VpcEgressGatewayObservability{Conntrack: apiv1.VpcEgressGatewayConntrackObservability{
			Log: apiv1.VpcEgressGatewayConntrackLog{Enabled: true},
		}}},
		limiter: rate.NewLimiter(100, 1000),
	}
	queue := make(chan flowRecord, 2)
	collector := newConntrackCollector(func() *runtimeSettings { return settings }, identity, newObserverMetrics(), queue)
	collector.processEvent(conntrack.Event{Type: conntrack.EventNew, Flow: &flow})
	flow.CountersOrig.Packets, flow.CountersOrig.Bytes = 1, 64
	collector.processEvent(conntrack.Event{Type: conntrack.EventDestroy, Flow: &flow})

	start, end := <-queue, <-queue
	require.Equal(t, apiv1.ObservabilityEventStart, start.Event)
	require.Equal(t, apiv1.ObservabilityEventEnd, end.Event)
	require.Equal(t, start.ConntrackID, end.ConntrackID)
	require.Equal(t, start.Zone, end.Zone)
	require.Equal(t, start.Original, end.Original)
	require.Equal(t, start.Translated, end.Translated)
	require.NotNil(t, end.Counters)

	for _, record := range []flowRecord{start, end} {
		data, err := json.Marshal(record)
		require.NoError(t, err)
		var fields map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(data, &fields))
		for _, field := range []string{
			"schemaVersion", "timestamp", "event", "conntrackID", "zone", "namespace", "name", "pod", "node",
			"addressFamily", "protocol", "protocolNumber", "natType", "original", "translated",
		} {
			require.Contains(t, fields, field)
		}
		for _, field := range []string{"original", "translated"} {
			var tuple map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(fields[field], &tuple))
			for _, tupleField := range []string{"sourceIP", "sourcePort", "destinationIP", "destinationPort"} {
				require.Contains(t, tuple, tupleField)
			}
		}
	}
}

func TestReloadConfigAppliesRuntimeSettings(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "config.json")
	initial := Config{
		Namespace: "ns", Name: "gateway", ExternalNetwork: "ns/external",
		Observability: apiv1.VpcEgressGatewayObservability{Conntrack: apiv1.VpcEgressGatewayConntrackObservability{
			Log: apiv1.VpcEgressGatewayConntrackLog{Enabled: true},
		}},
	}
	writeConfig := func(config Config) {
		t.Helper()
		data, err := json.Marshal(config)
		require.NoError(t, err)
		temporaryPath := path + ".next"
		require.NoError(t, os.WriteFile(temporaryPath, data, 0o600))
		require.NoError(t, os.Rename(temporaryPath, path))
	}
	writeConfig(initial)
	filters, err := compileFilters(initial.Observability.Conntrack.Log.Filters)
	require.NoError(t, err)
	settings := &atomic.Pointer[runtimeSettings]{}
	settings.Store(&runtimeSettings{config: initial, filters: filters, limiter: newLimiter(initial.Observability.Conntrack.Log.RateLimit)})
	identity := observerIdentity{namespace: "ns", name: "gateway", pod: "pod", node: "node"}
	metrics := newObserverMetrics()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go reloadConfig(ctx, path, identity, metrics, settings, &bytes.Buffer{})

	updated := initial
	updated.Observability.Conntrack.Log.Events = []string{apiv1.ObservabilityEventEnd}
	updated.Observability.Conntrack.Log.RateLimit = apiv1.VpcEgressGatewayConntrackLogRateLimit{RecordsPerSecond: 5, Burst: 7}
	writeConfig(updated)
	require.Eventually(t, func() bool {
		current := settings.Load()
		return current != nil &&
			len(current.config.Observability.Conntrack.Log.Events) == 1 &&
			current.config.Observability.Conntrack.Log.Events[0] == apiv1.ObservabilityEventEnd &&
			current.limiter.Limit() == 5 && current.limiter.Burst() == 7
	}, 5*time.Second, 100*time.Millisecond)
	require.Equal(t, float64(1), testutil.ToFloat64(metrics.configReloads.WithLabelValues("ns", "gateway", "pod", "node", "success")))
}
