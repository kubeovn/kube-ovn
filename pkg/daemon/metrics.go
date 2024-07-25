package daemon

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	nodeName              = ""
	cniOperationHistogram = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "cni_op_latency_seconds",
			Help:    "The latency seconds for cni operations",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 10),
		}, []string{
			"node_name",
			"method",
			"status_code",
		})

	cniWaitAddressResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cni_wait_address_seconds_total",
			Help: "Latency that cni wait controller to assign an address",
		},
		[]string{"node_name"},
	)

	cniWaitRouteResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cni_wait_route_seconds_total",
			Help: "Latency that cni wait controller to add routed annotation to pod",
		},
		[]string{"node_name"},
	)

	cniConnectivityResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cni_wait_connectivity_seconds_total",
			Help: "Latency that cni wait address ready in overlay network",
		},
		[]string{"node_name"},
	)

	metricOvnSubnetGatewayPacketBytes = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ovn_subnet_gateway_packet_bytes",
			Help: "the ovn subnet gateway packet bytes.",
		}, []string{
			"hostname",
			"subnet_name",
			"cidr",
			"direction",
			"protocol",
		},
	)

	metricOvnSubnetGatewayPackets = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ovn_subnet_gateway_packets",
			Help: "the ovn subnet gateway packet num.",
		}, []string{
			"hostname",
			"subnet_name",
			"cidr",
			"direction",
			"protocol",
		},
	)

	metricIPLocalPortRange = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ip_local_port_range",
		Help: "value of system parameter /proc/sys/net/ipv4/ip_local_port_range, which should not conflict with the nodeport range",
	}, []string{
		"hostname",
		"start",
		"end",
	})

	metricCheckSumErr = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "checksum_err_count",
			Help: "Value of InCsumErrors for cmd `netstat -us`, checksum is error when value is greater than 0",
		},
		[]string{"hostname"})

	metricDNSSearch = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dns_search_domain",
		Help: "search domain in /etc/resolv.conf",
	}, []string{
		"hostname",
		"additional",
	})

	metricTCPTwRecycle = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "tcp_tw_recycle",
		Help: "value of system parameter /proc/sys/net/ipv4/tcp_tw_recycle, the recommended value is 0",
	}, []string{
		"hostname",
	})

	metricTCPMtuProbing = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "tcp_mtu_probing",
		Help: "value of system parameter /proc/sys/net/ipv4/tcp_mtu_probing, the recommended value is 1",
	}, []string{
		"hostname",
	})

	metricConntrackTCPLiberal = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "nf_conntrack_tcp_be_liberal",
		Help: "value of system parameter /proc/sys/net/netfilter/nf_conntrack_tcp_be_liberal, the recommended value is 1",
	}, []string{
		"hostname",
	})

	metricBridgeNfCallIptables = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "bridge_nf_call_iptables",
		Help: "value of system parameter /proc/sys/net/bridge/bridge-nf-call-iptables, the recommended value is 1 for overlay, and 0 for underlay network",
	}, []string{
		"hostname",
	})

	metricTCPMem = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "tcp_mem",
		Help: "value of system parameter /proc/sys/net/ipv4/tcp_mem, recommend a large number value",
	}, []string{
		"hostname",
		"minimum",
		"pressure",
		"maximum",
	})

	metricIPv6RouteMaxsize = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "max_size",
		Help: "value of system parameter /proc/sys/net/ipv6/route/max_size, recommend a large number value, at least 16384",
	}, []string{
		"hostname",
	})
)

func InitMetrics() {
	registerOvnSubnetGatewayMetrics()
	registerSystemParameterMetrics()
	metrics.Registry.MustRegister(cniOperationHistogram)
	metrics.Registry.MustRegister(cniWaitAddressResult)
	metrics.Registry.MustRegister(cniConnectivityResult)
}

func registerOvnSubnetGatewayMetrics() {
	metrics.Registry.MustRegister(metricOvnSubnetGatewayPacketBytes)
	metrics.Registry.MustRegister(metricOvnSubnetGatewayPackets)
}

func registerSystemParameterMetrics() {
	metrics.Registry.MustRegister(metricIPLocalPortRange)
	metrics.Registry.MustRegister(metricCheckSumErr)
	metrics.Registry.MustRegister(metricDNSSearch)
	metrics.Registry.MustRegister(metricTCPTwRecycle)
	metrics.Registry.MustRegister(metricTCPMtuProbing)
	metrics.Registry.MustRegister(metricConntrackTCPLiberal)
	metrics.Registry.MustRegister(metricBridgeNfCallIptables)
	metrics.Registry.MustRegister(metricTCPMem)
	metrics.Registry.MustRegister(metricIPv6RouteMaxsize)
}
