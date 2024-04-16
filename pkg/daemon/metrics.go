package daemon

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	reflectormetrics "k8s.io/client-go/tools/cache"
	clientmetrics "k8s.io/client-go/tools/metrics"
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

	// client metrics
	requestLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "rest_client_request_latency_seconds",
			Help:    "Request latency in seconds. Broken down by verb and URL.",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 10),
		},
		[]string{"verb", "url"},
	)

	requestResult = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rest_client_requests_total",
			Help: "Number of HTTP requests, partitioned by status code, method, and host.",
		},
		[]string{"code", "method", "host"},
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

	metricCniConfig = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "cni_config_file",
		Help: "cni config file in /etc/cni/net.d/",
	}, []string{
		"hostname",
		"ovn",
		"other",
	})

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

	// reflector metrics

	// TODO(directxman12): update these to be histograms once the metrics overhaul KEP
	// PRs start landing.

	reflectorSubsystem = "reflector"

	listsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Subsystem: reflectorSubsystem,
		Name:      "lists_total",
		Help:      "Total number of API lists done by the reflectors",
	}, []string{"name"})

	listsDuration = prometheus.NewSummaryVec(prometheus.SummaryOpts{
		Subsystem: reflectorSubsystem,
		Name:      "list_duration_seconds",
		Help:      "How long an API list takes to return and decode for the reflectors",
	}, []string{"name"})

	itemsPerList = prometheus.NewSummaryVec(prometheus.SummaryOpts{
		Subsystem: reflectorSubsystem,
		Name:      "items_per_list",
		Help:      "How many items an API list returns to the reflectors",
	}, []string{"name"})

	watchesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Subsystem: reflectorSubsystem,
		Name:      "watches_total",
		Help:      "Total number of API watches done by the reflectors",
	}, []string{"name"})

	shortWatchesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Subsystem: reflectorSubsystem,
		Name:      "short_watches_total",
		Help:      "Total number of short API watches done by the reflectors",
	}, []string{"name"})

	watchDuration = prometheus.NewSummaryVec(prometheus.SummaryOpts{
		Subsystem: reflectorSubsystem,
		Name:      "watch_duration_seconds",
		Help:      "How long an API watch takes to return and decode for the reflectors",
	}, []string{"name"})

	itemsPerWatch = prometheus.NewSummaryVec(prometheus.SummaryOpts{
		Subsystem: reflectorSubsystem,
		Name:      "items_per_watch",
		Help:      "How many items an API watch returns to the reflectors",
	}, []string{"name"})

	lastResourceVersion = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Subsystem: reflectorSubsystem,
		Name:      "last_resource_version",
		Help:      "Last resource version seen for the reflectors",
	}, []string{"name"})
)

func InitMetrics() {
	registerReflectorMetrics()
	registerClientMetrics()
	registerOvnSubnetGatewayMetrics()
	registerSystemParameterMetrics()
	prometheus.MustRegister(cniOperationHistogram)
	prometheus.MustRegister(cniWaitAddressResult)
	prometheus.MustRegister(cniConnectivityResult)
}

func registerOvnSubnetGatewayMetrics() {
	prometheus.MustRegister(metricOvnSubnetGatewayPacketBytes)
	prometheus.MustRegister(metricOvnSubnetGatewayPackets)
}

func registerSystemParameterMetrics() {
	prometheus.MustRegister(metricIPLocalPortRange)
	prometheus.MustRegister(metricCheckSumErr)
	prometheus.MustRegister(metricCniConfig)
	prometheus.MustRegister(metricDNSSearch)
	prometheus.MustRegister(metricTCPTwRecycle)
	prometheus.MustRegister(metricTCPMtuProbing)
	prometheus.MustRegister(metricConntrackTCPLiberal)
	prometheus.MustRegister(metricBridgeNfCallIptables)
	prometheus.MustRegister(metricTCPMem)
	prometheus.MustRegister(metricIPv6RouteMaxsize)
}

// registerClientMetrics sets up the client latency metrics from client-go
func registerClientMetrics() {
	// register the metrics with our registry
	prometheus.MustRegister(requestLatency)
	prometheus.MustRegister(requestResult)

	// register the metrics with client-go
	opts := clientmetrics.RegisterOpts{
		RequestLatency: clientmetrics.LatencyMetric(&latencyAdapter{metric: requestLatency}),
		RequestResult:  clientmetrics.ResultMetric(&resultAdapter{metric: requestResult}),
	}
	clientmetrics.Register(opts)
}

// registerReflectorMetrics sets up reflector (reconcile) loop metrics
func registerReflectorMetrics() {
	prometheus.MustRegister(listsTotal)
	prometheus.MustRegister(listsDuration)
	prometheus.MustRegister(itemsPerList)
	prometheus.MustRegister(watchesTotal)
	prometheus.MustRegister(shortWatchesTotal)
	prometheus.MustRegister(watchDuration)
	prometheus.MustRegister(itemsPerWatch)
	prometheus.MustRegister(lastResourceVersion)

	reflectormetrics.SetReflectorMetricsProvider(reflectorMetricsProvider{})
}

// this section contains adapters, implementations, and other sundry organic, artisanally
// hand-crafted syntax trees required to convince client-go that it actually wants to let
// someone use its metrics.

// Client metrics adapters (method #1 for client-go metrics),
// copied (more-or-less directly) from k8s.io/kubernetes setup code
// (which isn't anywhere in an easily-importable place).

type latencyAdapter struct {
	metric *prometheus.HistogramVec
}

func (l *latencyAdapter) Observe(_ context.Context, verb string, u url.URL, latency time.Duration) {
	url := u.String()
	last := strings.LastIndex(url, "/")
	if last != -1 {
		url = url[:last]
	}
	l.metric.WithLabelValues(verb, url).Observe(latency.Seconds())
}

type resultAdapter struct {
	metric *prometheus.CounterVec
}

func (r *resultAdapter) Increment(_ context.Context, code, method, host string) {
	r.metric.WithLabelValues(code, method, host).Inc()
}

// Reflector metrics provider (method #2 for client-go metrics),
// copied (more-or-less directly) from k8s.io/kubernetes setup code
// (which isn't anywhere in an easily-importable place).

type reflectorMetricsProvider struct{}

func (reflectorMetricsProvider) NewListsMetric(name string) reflectormetrics.CounterMetric {
	return listsTotal.WithLabelValues(name)
}

func (reflectorMetricsProvider) NewListDurationMetric(name string) reflectormetrics.SummaryMetric {
	return listsDuration.WithLabelValues(name)
}

func (reflectorMetricsProvider) NewItemsInListMetric(name string) reflectormetrics.SummaryMetric {
	return itemsPerList.WithLabelValues(name)
}

func (reflectorMetricsProvider) NewWatchesMetric(name string) reflectormetrics.CounterMetric {
	return watchesTotal.WithLabelValues(name)
}

func (reflectorMetricsProvider) NewShortWatchesMetric(name string) reflectormetrics.CounterMetric {
	return shortWatchesTotal.WithLabelValues(name)
}

func (reflectorMetricsProvider) NewWatchDurationMetric(name string) reflectormetrics.SummaryMetric {
	return watchDuration.WithLabelValues(name)
}

func (reflectorMetricsProvider) NewItemsInWatchMetric(name string) reflectormetrics.SummaryMetric {
	return itemsPerWatch.WithLabelValues(name)
}

func (reflectorMetricsProvider) NewLastResourceVersionMetric(name string) reflectormetrics.GaugeMetric {
	return lastResourceVersion.WithLabelValues(name)
}
