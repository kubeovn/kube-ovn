package vegobserver

import "github.com/prometheus/client_golang/prometheus"

var identityLabels = []string{"namespace", "name", "pod", "node"}

type observerMetrics struct {
	registry *prometheus.Registry

	collectorUp       *prometheus.GaugeVec
	configReloads     *prometheus.CounterVec
	conntrackEvents   *prometheus.CounterVec
	errors            *prometheus.CounterVec
	cacheEntries      *prometheus.GaugeVec
	cacheCapacity     *prometheus.GaugeVec
	cacheEvictions    *prometheus.CounterVec
	logRecords        *prometheus.CounterVec
	logDrops          *prometheus.CounterVec
	accounting        *prometheus.GaugeVec
	interfaceCounters map[string]*prometheus.CounterVec
	activeFlows       *prometheus.GaugeVec
	startedFlows      *prometheus.CounterVec
	endedFlows        *prometheus.CounterVec
	packets           *prometheus.CounterVec
	bytes             *prometheus.CounterVec
}

func newObserverMetrics() *observerMetrics {
	m := &observerMetrics{registry: prometheus.NewRegistry()}
	m.collectorUp = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "kube_ovn_vpc_egress_gateway_observability_collector_up", Help: "Whether an observer collector is operating successfully."}, append(identityLabels, "collector"))
	m.configReloads = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "kube_ovn_vpc_egress_gateway_observability_config_reload_total", Help: "Observer configuration reload attempts."}, append(identityLabels, "result"))
	m.conntrackEvents = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "kube_ovn_vpc_egress_gateway_observability_conntrack_events_total", Help: "Conntrack events processed by the observer."}, append(identityLabels, "event"))
	m.errors = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "kube_ovn_vpc_egress_gateway_observability_errors_total", Help: "Observer collector errors."}, append(identityLabels, "collector", "operation"))
	m.cacheEntries = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "kube_ovn_vpc_egress_gateway_observability_conntrack_cache_entries", Help: "Current entries in the conntrack flow cache."}, identityLabels)
	m.cacheCapacity = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "kube_ovn_vpc_egress_gateway_observability_conntrack_cache_capacity", Help: "Maximum conntrack flow cache entries."}, identityLabels)
	m.cacheEvictions = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "kube_ovn_vpc_egress_gateway_observability_conntrack_cache_evictions_total", Help: "Conntrack cache evictions."}, identityLabels)
	m.logRecords = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "kube_ovn_vpc_egress_gateway_observability_log_records_total", Help: "Flow log records written."}, append(identityLabels, "event"))
	m.logDrops = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "kube_ovn_vpc_egress_gateway_observability_log_drops_total", Help: "Flow log records dropped."}, append(identityLabels, "reason"))
	m.accounting = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "kube_ovn_vpc_egress_gateway_observability_conntrack_accounting_available", Help: "Whether conntrack accounting counters are available."}, identityLabels)

	interfaceLabels := append(identityLabels, "interface", "type")
	m.interfaceCounters = map[string]*prometheus.CounterVec{
		"rx_bytes":   prometheus.NewCounterVec(prometheus.CounterOpts{Name: "kube_ovn_vpc_egress_gateway_interface_rx_bytes_total", Help: "Bytes received by a gateway interface."}, interfaceLabels),
		"tx_bytes":   prometheus.NewCounterVec(prometheus.CounterOpts{Name: "kube_ovn_vpc_egress_gateway_interface_tx_bytes_total", Help: "Bytes transmitted by a gateway interface."}, interfaceLabels),
		"rx_packets": prometheus.NewCounterVec(prometheus.CounterOpts{Name: "kube_ovn_vpc_egress_gateway_interface_rx_packets_total", Help: "Packets received by a gateway interface."}, interfaceLabels),
		"tx_packets": prometheus.NewCounterVec(prometheus.CounterOpts{Name: "kube_ovn_vpc_egress_gateway_interface_tx_packets_total", Help: "Packets transmitted by a gateway interface."}, interfaceLabels),
	}
	directionalLabels := append(interfaceLabels, "direction")
	m.interfaceCounters["drops"] = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "kube_ovn_vpc_egress_gateway_interface_drops_total", Help: "Packets dropped by a gateway interface."}, directionalLabels)
	m.interfaceCounters["errors"] = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "kube_ovn_vpc_egress_gateway_interface_errors_total", Help: "Packet errors observed by a gateway interface."}, directionalLabels)

	flowLabels := append(identityLabels, "address_family", "protocol", "nat_type")
	m.activeFlows = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "kube_ovn_vpc_egress_gateway_conntrack_nat_flows_active", Help: "Active NAT conntrack flows."}, flowLabels)
	m.startedFlows = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "kube_ovn_vpc_egress_gateway_conntrack_nat_flows_started_total", Help: "NAT conntrack flows started."}, flowLabels)
	m.endedFlows = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "kube_ovn_vpc_egress_gateway_conntrack_nat_flows_ended_total", Help: "NAT conntrack flows ended."}, flowLabels)
	directionLabels := append(flowLabels, "direction")
	m.packets = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "kube_ovn_vpc_egress_gateway_conntrack_nat_packets_total", Help: "Packets observed in NAT conntrack flows when accounting is available."}, directionLabels)
	m.bytes = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "kube_ovn_vpc_egress_gateway_conntrack_nat_bytes_total", Help: "Bytes observed in NAT conntrack flows when accounting is available."}, directionLabels)

	collectors := []prometheus.Collector{m.collectorUp, m.configReloads, m.conntrackEvents, m.errors, m.cacheEntries, m.cacheCapacity, m.cacheEvictions, m.logRecords, m.logDrops, m.accounting, m.activeFlows, m.startedFlows, m.endedFlows, m.packets, m.bytes}
	for _, collector := range m.interfaceCounters {
		collectors = append(collectors, collector)
	}
	m.registry.MustRegister(collectors...)
	return m
}
