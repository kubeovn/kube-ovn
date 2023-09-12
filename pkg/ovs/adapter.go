package ovs

import "github.com/prometheus/client_golang/prometheus"

// OVN NB metrics
var ovsClientRequestLatency = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "ovs_client_request_latency_milliseconds",
		Buckets: prometheus.ExponentialBuckets(1, 2, 10),
	},
	[]string{"db", "method", "code"},
)

func init() {
	registerOvsClientMetrics()
}

func registerOvsClientMetrics() {
	prometheus.MustRegister(ovsClientRequestLatency)
}
