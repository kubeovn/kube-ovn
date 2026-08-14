package vegobserver

import (
	"container/list"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
	"time"

	"github.com/ti-mo/conntrack"
	"github.com/ti-mo/netfilter"
	"golang.org/x/time/rate"

	apiv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

type runtimeSettings struct {
	config  Config
	filters compiledFilters
	limiter *rate.Limiter
}

type cacheEntry struct {
	record        flowRecord
	flow          conntrack.Flow
	element       *list.Element
	observedStart bool
}

type conntrackCollector struct {
	mu       sync.Mutex
	settings func() *runtimeSettings
	identity observerIdentity
	metrics  *observerMetrics
	logQueue chan flowRecord
	cache    map[flowKey]*cacheEntry
	order    *list.List
	dial     func() (conntrackConnection, error)
}

type conntrackConnection interface {
	Close() error
	SetReadBuffer(int) error
	Listen(chan<- conntrack.Event, uint8, []netfilter.NetlinkGroup) (chan error, error)
	Dump(*conntrack.DumpOptions) ([]conntrack.Flow, error)
}

func newConntrackCollector(settings func() *runtimeSettings, identity observerIdentity, metrics *observerMetrics, logQueue chan flowRecord) *conntrackCollector {
	metrics.cacheCapacity.WithLabelValues(identity.labels()...).Set(DefaultCacheCapacity)
	// Allocate cache buckets on demand so interface-only observers do not pay for the conntrack capacity.
	return &conntrackCollector{
		settings: settings, identity: identity, metrics: metrics, logQueue: logQueue,
		cache: make(map[flowKey]*cacheEntry), order: list.New(),
		dial: func() (conntrackConnection, error) { return conntrack.Dial(nil) },
	}
}

func (c *conntrackCollector) run(ctx context.Context, diagnostics io.Writer) {
	for ctx.Err() == nil {
		settings := c.settings()
		if settings == nil || !conntrackEnabled(settings.config) {
			c.metrics.collectorUp.WithLabelValues(append(c.identity.labels(), "conntrack")...).Set(0)
			if !waitContext(ctx, 2*time.Second) {
				return
			}
			continue
		}
		if err := c.runSession(ctx, diagnostics); err != nil && ctx.Err() == nil {
			writeDiagnostic(c.metrics, c.identity, diagnostics, "conntrack", "conntrack collector: %v\n", err)
			c.metrics.errors.WithLabelValues(append(c.identity.labels(), "conntrack", "session")...).Inc()
			c.metrics.collectorUp.WithLabelValues(append(c.identity.labels(), "conntrack")...).Set(0)
			if !waitContext(ctx, 2*time.Second) {
				return
			}
		}
	}
}

func (c *conntrackCollector) runSession(ctx context.Context, diagnostics io.Writer) (returnErr error) {
	eventConnection, err := c.dial()
	if err != nil {
		return fmt.Errorf("dial conntrack event netlink: %w", err)
	}
	defer func() {
		if err := eventConnection.Close(); err != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("close conntrack event netlink: %w", err))
		}
	}()
	if err := eventConnection.SetReadBuffer(4 << 20); err != nil {
		c.metrics.errors.WithLabelValues(append(c.identity.labels(), "conntrack", "set_read_buffer")...).Inc()
		writeDiagnostic(c.metrics, c.identity, diagnostics, "conntrack", "set conntrack read buffer: %v\n", err)
	}
	events := make(chan conntrack.Event, DefaultEventBuffer)
	errorsChannel, err := eventConnection.Listen(events, 1, netfilter.GroupsCT)
	if err != nil {
		return fmt.Errorf("subscribe to conntrack events: %w", err)
	}
	dumpConnection, err := c.dial()
	if err != nil {
		return fmt.Errorf("dial conntrack dump netlink: %w", err)
	}
	flows, dumpErr := dumpConnection.Dump(nil)
	closeErr := dumpConnection.Close()
	if dumpErr != nil {
		return fmt.Errorf("dump conntrack table: %w", dumpErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close conntrack dump netlink: %w", closeErr)
	}
	c.initialize(flows)
	initialMetricsEnabled := c.settings().config.Observability.Conntrack.Metrics.Enabled
	c.metrics.collectorUp.WithLabelValues(append(c.identity.labels(), "conntrack")...).Set(1)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			settings := c.settings()
			if settings == nil || !conntrackEnabled(settings.config) {
				if initialMetricsEnabled {
					c.resetDomainMetrics()
				}
				c.clear()
				return nil
			}
			if settings.config.Observability.Conntrack.Metrics.Enabled != initialMetricsEnabled {
				if initialMetricsEnabled {
					c.resetDomainMetrics()
				}
				c.clear()
				return nil
			}
		case err, ok := <-errorsChannel:
			if !ok {
				return errors.New("conntrack event stream closed")
			}
			if err != nil {
				return fmt.Errorf("receive conntrack event: %w", err)
			}
		case event, ok := <-events:
			if !ok {
				return errors.New("conntrack event channel closed")
			}
			c.processEvent(event)
		}
	}
}

func (c *conntrackCollector) initialize(flows []conntrack.Flow) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.resetLocked()
	settings := c.settings()
	for i := range flows {
		record, ok := recordFromFlow(&flows[i], c.identity)
		if !ok {
			continue
		}
		c.insertLocked(flows[i], record, false)
		if settings != nil && settings.config.Observability.Conntrack.Metrics.Enabled {
			c.metrics.activeFlows.WithLabelValues(c.flowLabels(record)...).Inc()
			c.addCountersLocked(conntrack.Flow{}, flows[i], record)
		}
	}
	c.metrics.cacheEntries.WithLabelValues(c.identity.labels()...).Set(float64(len(c.cache)))
}

func (c *conntrackCollector) processEvent(event conntrack.Event) {
	if event.Flow == nil {
		return
	}
	record, ok := recordFromFlow(event.Flow, c.identity)
	if !ok {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	settings := c.settings()
	if settings == nil {
		return
	}
	key := flowKey{zone: event.Flow.Zone, id: event.Flow.ID}
	entry := c.cache[key]
	metricsOn := settings.config.Observability.Conntrack.Metrics.Enabled
	switch event.Type {
	case conntrack.EventNew, conntrack.EventUpdate:
		isNew := entry == nil
		if isNew {
			c.insertLocked(*event.Flow, record, event.Type == conntrack.EventNew)
			if metricsOn {
				c.metrics.activeFlows.WithLabelValues(c.flowLabels(record)...).Inc()
				c.addCountersLocked(conntrack.Flow{}, *event.Flow, record)
			}
		} else {
			if metricsOn {
				c.addCountersLocked(entry.flow, *event.Flow, record)
			}
			entry.flow, entry.record = *event.Flow, record
			c.order.MoveToBack(entry.element)
		}
		eventName := "update"
		if event.Type == conntrack.EventNew {
			eventName = "new"
		}
		c.metrics.conntrackEvents.WithLabelValues(append(c.identity.labels(), eventName)...).Inc()
		observedStart := event.Type == conntrack.EventNew && (isNew || !entry.observedStart)
		if observedStart {
			if !isNew {
				entry.observedStart = true
			}
			if metricsOn {
				c.metrics.startedFlows.WithLabelValues(c.flowLabels(record)...).Inc()
			}
			c.enqueueLogLocked(record, apiv1.ObservabilityEventStart, settings)
		}
	case conntrack.EventDestroy:
		c.metrics.conntrackEvents.WithLabelValues(append(c.identity.labels(), "end")...).Inc()
		if entry != nil {
			if metricsOn {
				c.addCountersLocked(entry.flow, *event.Flow, record)
				c.metrics.activeFlows.WithLabelValues(c.flowLabels(entry.record)...).Dec()
			}
			c.order.Remove(entry.element)
			delete(c.cache, key)
		}
		if metricsOn {
			c.metrics.endedFlows.WithLabelValues(c.flowLabels(record)...).Inc()
		}
		c.enqueueLogLocked(record, apiv1.ObservabilityEventEnd, settings)
	}
	c.metrics.cacheEntries.WithLabelValues(c.identity.labels()...).Set(float64(len(c.cache)))
}

func (c *conntrackCollector) insertLocked(flow conntrack.Flow, record flowRecord, observedStart bool) {
	key := flowKey{zone: flow.Zone, id: flow.ID}
	if existing := c.cache[key]; existing != nil {
		existing.flow, existing.record = flow, record
		c.order.MoveToBack(existing.element)
		return
	}
	if len(c.cache) >= DefaultCacheCapacity {
		oldest := c.order.Front()
		oldKey := oldest.Value.(flowKey)
		oldEntry := c.cache[oldKey]
		settings := c.settings()
		if settings != nil && settings.config.Observability.Conntrack.Metrics.Enabled {
			c.metrics.activeFlows.WithLabelValues(c.flowLabels(oldEntry.record)...).Dec()
		}
		delete(c.cache, oldKey)
		c.order.Remove(oldest)
		c.metrics.cacheEvictions.WithLabelValues(c.identity.labels()...).Inc()
	}
	element := c.order.PushBack(key)
	c.cache[key] = &cacheEntry{record: record, flow: flow, element: element, observedStart: observedStart}
}

func (c *conntrackCollector) enqueueLogLocked(record flowRecord, event string, settings *runtimeSettings) {
	logConfig := settings.config.Observability.Conntrack.Log
	if !logConfig.Enabled || !eventEnabled(logConfig.Events, event) {
		return
	}
	record.Event = event
	if !settings.filters.match(record) {
		return
	}
	if !settings.limiter.Allow() {
		c.metrics.logDrops.WithLabelValues(append(c.identity.labels(), "rate_limit")...).Inc()
		return
	}
	select {
	case c.logQueue <- record:
	default:
		c.metrics.logDrops.WithLabelValues(append(c.identity.labels(), "queue_full")...).Inc()
	}
}

func (c *conntrackCollector) addCountersLocked(previous, current conntrack.Flow, record flowRecord) {
	if !accountingAvailable(&current) {
		return
	}
	c.metrics.accounting.WithLabelValues(c.identity.labels()...).Set(1)
	labels := c.flowLabels(record)
	for _, direction := range []struct {
		name string
		old  conntrack.Counter
		new  conntrack.Counter
	}{{"original", previous.CountersOrig, current.CountersOrig}, {"reply", previous.CountersReply, current.CountersReply}} {
		metricLabels := append(labels, direction.name)
		c.metrics.packets.WithLabelValues(metricLabels...).Add(counterDelta(direction.old.Packets, direction.new.Packets))
		c.metrics.bytes.WithLabelValues(metricLabels...).Add(counterDelta(direction.old.Bytes, direction.new.Bytes))
	}
}

func (c *conntrackCollector) flowLabels(record flowRecord) []string {
	return append(c.identity.labels(), record.AddressFamily, record.Protocol, metricNatType(record.NatType))
}

func (c *conntrackCollector) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.resetLocked()
}

func (c *conntrackCollector) resetLocked() {
	c.cache = make(map[flowKey]*cacheEntry)
	c.order.Init()
	c.metrics.cacheEntries.WithLabelValues(c.identity.labels()...).Set(0)
	c.metrics.accounting.WithLabelValues(c.identity.labels()...).Set(0)
	c.metrics.activeFlows.Reset()
}

func (c *conntrackCollector) resetDomainMetrics() {
	c.metrics.activeFlows.Reset()
	c.metrics.startedFlows.Reset()
	c.metrics.endedFlows.Reset()
	c.metrics.packets.Reset()
	c.metrics.bytes.Reset()
	c.metrics.accounting.WithLabelValues(c.identity.labels()...).Set(0)
}

func writeFlowLogs(ctx context.Context, writer io.Writer, identity observerIdentity, metrics *observerMetrics, queue <-chan flowRecord) {
	encoder := json.NewEncoder(writer)
	for {
		select {
		case <-ctx.Done():
			return
		case record := <-queue:
			if err := encoder.Encode(record); err != nil {
				metrics.logDrops.WithLabelValues(append(identity.labels(), "write_error")...).Inc()
				continue
			}
			metrics.logRecords.WithLabelValues(append(identity.labels(), record.Event)...).Inc()
		}
	}
}

func eventEnabled(events []string, event string) bool {
	if len(events) == 0 {
		return true
	}
	return slices.Contains(events, event)
}

func newLimiter(config apiv1.VpcEgressGatewayConntrackLogRateLimit) *rate.Limiter {
	recordsPerSecond, burst := int32(100), int32(1000)
	if config.RecordsPerSecond > 0 {
		recordsPerSecond = config.RecordsPerSecond
	}
	if config.Burst > 0 {
		burst = config.Burst
	}
	return rate.NewLimiter(rate.Limit(recordsPerSecond), int(burst))
}

func waitContext(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
