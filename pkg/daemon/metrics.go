package daemon

import "github.com/prometheus/client_golang/prometheus"

var (
	nodeName              = ""
	cniOperationHistogram = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "cni_op_latency_second",
			Help:    "the latency second for cni operations",
			Buckets: []float64{.1, .25, .5, 1, 2, 4, 8, 16, 32, 64, 128, 256},
		}, []string{
			"node_name",
			"method",
			"status_code",
		})
)

func init() {
	prometheus.MustRegister(cniOperationHistogram)
}
