package util

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
)

var (
	klogLinesGaugeVec = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "klog_lines_total",
		Help: "Total number of klog messages.",
	}, []string{"level"})
	klogBytesGaugeVec = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "klog_bytes_total",
		Help: "Total size of klog messages.",
	}, []string{"level"})
)

func InitKlogMetrics() {
	registerKlogMetrics()
	go wait.Until(fetchKlogMetrics, 5*time.Second, nil)
}

func registerKlogMetrics() {
	prometheus.MustRegister(klogLinesGaugeVec)
	prometheus.MustRegister(klogBytesGaugeVec)
}

func fetchKlogMetrics() {
	klogLinesGaugeVec.WithLabelValues("INFO").Set(float64(klog.Stats.Info.Lines()))
	klogLinesGaugeVec.WithLabelValues("WARN").Set(float64(klog.Stats.Warning.Lines()))
	klogLinesGaugeVec.WithLabelValues("ERROR").Set(float64(klog.Stats.Error.Lines()))

	klogBytesGaugeVec.WithLabelValues("INFO").Set(float64(klog.Stats.Info.Bytes()))
	klogBytesGaugeVec.WithLabelValues("WARN").Set(float64(klog.Stats.Warning.Bytes()))
	klogBytesGaugeVec.WithLabelValues("ERROR").Set(float64(klog.Stats.Error.Bytes()))
}
