package ovs

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

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
	metrics.Registry.MustRegister(ovsClientRequestLatency)
}
