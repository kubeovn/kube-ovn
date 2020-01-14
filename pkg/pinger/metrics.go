package pinger

import "github.com/prometheus/client_golang/prometheus"

var (
	ovsUpGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "pinger_ovs_up",
			Help: "If the ovs on the node is up",
		},
		[]string{
			"nodeName",
		})
	ovsDownGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "pinger_ovs_down",
			Help: "If the ovs on the node is down",
		},
		[]string{
			"nodeName",
		})
	ovnControllerUpGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "pinger_ovn_controller_up",
			Help: "If the ovn_controller on the node is up",
		},
		[]string{
			"nodeName",
		})
	ovnControllerDownGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "pinger_ovn_controller_down",
			Help: "If the ovn_controller on the node is down",
		},
		[]string{
			"nodeName",
		})
	inconsistentPortBindingGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "pinger_inconsistent_port_binding",
			Help: "The number of mismatch port bindings between ovs and ovn-sb",
		},
		[]string{
			"nodeName",
		})
	apiserverHealthyGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "pinger_apiserver_healthy",
			Help: "if the apiserver request is healthy on this node",
		},
		[]string{
			"nodeName",
		})
	apiserverUnhealthyGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "pinger_apiserver_unhealthy",
			Help: "if the apiserver request is unhealthy on this node",
		},
		[]string{
			"nodeName",
		})
	apiserverRequestLatencyHistogram = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "pinger_apiserver_latency_ms",
			Help:    "the latency ms histogram the node request apiserver",
			Buckets: []float64{2, 5, 10, 15, 20, 25, 30, 35, 40, 45, 50},
		},
		[]string{
			"nodeName",
		})
	dnsHealthyGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "pinger_dns_healthy",
			Help: "if the dns request is healthy on this node",
		},
		[]string{
			"nodeName",
		})
	dnsUnhealthyGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "pinger_dns_unhealthy",
			Help: "if the dns request is unhealthy on this node",
		},
		[]string{
			"nodeName",
		})
	dnsRequestLatencyHistogram = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "pinger_dns_latency_ms",
			Help:    "the latency ms histogram the node request dns",
			Buckets: []float64{2, 5, 10, 15, 20, 25, 30, 35, 40, 45, 50},
		},
		[]string{
			"nodeName",
		})
	podPingLatencyHistogram = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "pinger_pod_ping_latency_ms",
			Help:    "the latency ms histogram for pod peer ping",
			Buckets: []float64{.25, .5, 1, 2, 5, 10, 30},
		},
		[]string{
			"src_node_name",
			"src_node_ip",
			"src_pod_ip",
			"target_node_name",
			"target_node_ip",
			"target_pod_ip",
		})
	podPingLostCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "pinger_pod_ping_lost_total",
			Help: "the lost count for pod peer ping",
		}, []string{
			"src_node_name",
			"src_node_ip",
			"src_pod_ip",
			"target_node_name",
			"target_node_ip",
			"target_pod_ip",
		})
	nodePingLatencyHistogram = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "pinger_node_ping_latency_ms",
			Help:    "the latency ms histogram for pod ping node",
			Buckets: []float64{.25, .5, 1, 2, 5, 10, 30},
		},
		[]string{
			"src_node_name",
			"src_node_ip",
			"src_pod_ip",
			"target_node_name",
			"target_node_ip",
		})
	nodePingLostCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "pinger_node_ping_lost_total",
			Help: "the lost count for pod ping node",
		}, []string{
			"src_node_name",
			"src_node_ip",
			"src_pod_ip",
			"target_node_name",
			"target_node_ip",
		})
	externalPingLatencyHistogram = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "pinger_external_ping_latency_ms",
			Help:    "the latency ms histogram for pod ping external address",
			Buckets: []float64{.25, .5, 1, 2, 5, 10, 30, 50, 100},
		},
		[]string{
			"src_node_name",
			"src_node_ip",
			"src_pod_ip",
			"target_address",
		})
	externalPingLostCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "pinger_node_external_lost_total",
			Help: "the lost count for pod ping external address",
		}, []string{
			"src_node_name",
			"src_node_ip",
			"src_pod_ip",
			"target_address",
		})
)

func init() {
	prometheus.MustRegister(ovsUpGauge)
	prometheus.MustRegister(ovsDownGauge)
	prometheus.MustRegister(ovnControllerUpGauge)
	prometheus.MustRegister(ovnControllerDownGauge)
	prometheus.MustRegister(inconsistentPortBindingGauge)
	prometheus.MustRegister(apiserverHealthyGauge)
	prometheus.MustRegister(apiserverUnhealthyGauge)
	prometheus.MustRegister(apiserverRequestLatencyHistogram)
	prometheus.MustRegister(dnsHealthyGauge)
	prometheus.MustRegister(dnsUnhealthyGauge)
	prometheus.MustRegister(dnsRequestLatencyHistogram)
	prometheus.MustRegister(podPingLatencyHistogram)
	prometheus.MustRegister(podPingLostCounter)
	prometheus.MustRegister(nodePingLatencyHistogram)
	prometheus.MustRegister(nodePingLostCounter)
	prometheus.MustRegister(externalPingLatencyHistogram)
	prometheus.MustRegister(externalPingLostCounter)
}

func SetOvsUpMetrics(nodeName string) {
	ovsUpGauge.WithLabelValues(nodeName).Set(1)
	ovsDownGauge.WithLabelValues(nodeName).Set(0)
}

func SetOvsDownMetrics(nodeName string) {
	ovsUpGauge.WithLabelValues(nodeName).Set(0)
	ovsDownGauge.WithLabelValues(nodeName).Set(1)
}

func SetOvnControllerUpMetrics(nodeName string) {
	ovnControllerUpGauge.WithLabelValues(nodeName).Set(1)
	ovnControllerDownGauge.WithLabelValues(nodeName).Set(0)
}

func SetOvnControllerDownMetrics(nodeName string) {
	ovnControllerUpGauge.WithLabelValues(nodeName).Set(0)
	ovnControllerDownGauge.WithLabelValues(nodeName).Set(1)
}

func SetApiserverHealthyMetrics(nodeName string, latency float64) {
	apiserverHealthyGauge.WithLabelValues(nodeName).Set(1)
	apiserverRequestLatencyHistogram.WithLabelValues(nodeName).Observe(latency)
	apiserverUnhealthyGauge.WithLabelValues(nodeName).Set(0)
}

func SetApiserverUnhealthyMetrics(nodeName string) {
	apiserverHealthyGauge.WithLabelValues(nodeName).Set(0)
	apiserverUnhealthyGauge.WithLabelValues(nodeName).Set(1)
}

func SetDnsHealthyMetrics(nodeName string, latency float64) {
	dnsHealthyGauge.WithLabelValues(nodeName).Set(1)
	dnsRequestLatencyHistogram.WithLabelValues(nodeName).Observe(latency)
	dnsUnhealthyGauge.WithLabelValues(nodeName).Set(0)
}

func SetDnsUnhealthyMetrics(nodeName string) {
	dnsHealthyGauge.WithLabelValues(nodeName).Set(0)
	dnsUnhealthyGauge.WithLabelValues(nodeName).Set(1)
}

func SetPodPingMetrics(srcNodeName, srcNodeIP, srcPodIP, targetNodeName, targetNodeIP, targetPodIP string, latency float64, lost int) {
	podPingLatencyHistogram.WithLabelValues(
		srcNodeName,
		srcNodeIP,
		srcPodIP,
		targetNodeName,
		targetNodeIP,
		targetPodIP,
	).Observe(latency)
	podPingLostCounter.WithLabelValues(
		srcNodeName,
		srcNodeIP,
		srcPodIP,
		targetNodeName,
		targetNodeIP,
		targetPodIP,
	).Add(float64(lost))
}

func SetNodePingMetrics(srcNodeName, srcNodeIP, srcPodIP, targetNodeName, targetNodeIP string, latency float64, lost int) {
	nodePingLatencyHistogram.WithLabelValues(
		srcNodeName,
		srcNodeIP,
		srcPodIP,
		targetNodeName,
		targetNodeIP,
	).Observe(latency)
	nodePingLostCounter.WithLabelValues(
		srcNodeName,
		srcNodeIP,
		srcPodIP,
		targetNodeName,
		targetNodeIP,
	).Add(float64(lost))
}

func SetExternalPingMetrics(srcNodeName, srcNodeIP, srcPodIP, targetAddress string, latency float64, lost int) {
	externalPingLatencyHistogram.WithLabelValues(
		srcNodeName,
		srcNodeIP,
		srcPodIP,
		targetAddress,
	).Observe(latency)
	externalPingLostCounter.WithLabelValues(
		srcNodeName,
		srcNodeIP,
		srcPodIP,
		targetAddress,
	).Add(float64(lost))
}
