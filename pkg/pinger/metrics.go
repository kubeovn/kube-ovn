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
			Help: "If the apiserver request is healthy on this node",
		},
		[]string{
			"nodeName",
		})
	apiserverUnhealthyGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "pinger_apiserver_unhealthy",
			Help: "If the apiserver request is unhealthy on this node",
		},
		[]string{
			"nodeName",
		})
	apiserverRequestLatencyHistogram = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "pinger_apiserver_latency_ms",
			Help:    "The latency ms histogram the node request apiserver",
			Buckets: []float64{2, 5, 10, 15, 20, 25, 30, 35, 40, 45, 50},
		},
		[]string{
			"nodeName",
		})
	internalDNSHealthyGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "pinger_internal_dns_healthy",
			Help: "If the internal dns request is healthy on this node",
		},
		[]string{
			"nodeName",
		})
	internalDNSUnhealthyGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "pinger_internal_dns_unhealthy",
			Help: "If the internal dns request is unhealthy on this node",
		},
		[]string{
			"nodeName",
		})
	internalDNSRequestLatencyHistogram = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "pinger_internal_dns_latency_ms",
			Help:    "The latency ms histogram the node request internal dns",
			Buckets: []float64{2, 5, 10, 15, 20, 25, 30, 35, 40, 45, 50},
		},
		[]string{
			"nodeName",
		})
	externalDNSHealthyGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "pinger_external_dns_healthy",
			Help: "If the external dns request is healthy on this node",
		},
		[]string{
			"nodeName",
		})
	externalDNSUnhealthyGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "pinger_external_dns_unhealthy",
			Help: "If the external dns request is unhealthy on this node",
		},
		[]string{
			"nodeName",
		})
	externalDNSRequestLatencyHistogram = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "pinger_external_dns_latency_ms",
			Help:    "The latency ms histogram the node request external dns",
			Buckets: []float64{2, 5, 10, 15, 20, 25, 30, 35, 40, 45, 50},
		},
		[]string{
			"nodeName",
		})
	podPingLatencyHistogram = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "pinger_pod_ping_latency_ms",
			Help:    "The latency ms histogram for pod peer ping",
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
			Help: "The lost count for pod peer ping",
		}, []string{
			"src_node_name",
			"src_node_ip",
			"src_pod_ip",
			"target_node_name",
			"target_node_ip",
			"target_pod_ip",
		})
	podPingTotalCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "pinger_pod_ping_count_total",
			Help: "The total count for pod peer ping",
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
			Help:    "The latency ms histogram for pod ping node",
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
			Help: "The lost count for pod ping node",
		}, []string{
			"src_node_name",
			"src_node_ip",
			"src_pod_ip",
			"target_node_name",
			"target_node_ip",
		})
	nodePingTotalCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "pinger_node_ping_count_total",
			Help: "The total count for pod ping node",
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
			Help:    "The latency ms histogram for pod ping external address",
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
			Name: "pinger_external_ping_lost_total",
			Help: "The lost count for pod ping external address",
		}, []string{
			"src_node_name",
			"src_node_ip",
			"src_pod_ip",
			"target_address",
		})

	// OVS basic info
	metricOvsHealthyStatus = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "ovs_status",
			Help:      "OVS Health Status. The values are: healthy(1), unhealthy(0).",
		},
		[]string{
			"hostname",
			"component",
		})

	metricOvsInfo = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "ovs_info",
			Help:      "This metric provides basic information about OVS. It is always set to 1.",
		},
		[]string{
			"system_id",
			"rundir",
			"hostname",
			"system_type",
			"system_version",
			"ovs_version",
			"db_version",
		})

	metricRequestErrorNums = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "failed_req_count",
			Help:      "The number of failed requests to OVS stack.",
		},
		[]string{
			"hostname",
		})

	metricLogFileSize = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "log_file_size_bytes",
			Help:      "The size of a log file associated with an OVS component. The unit is Bytes.",
		},
		[]string{
			"hostname",
			"component",
			"filename",
		})

	metricDbFileSize = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "db_file_size_bytes",
			Help:      "The size of a database file associated with an OVS component. The unit is Bytes.",
		},
		[]string{
			"hostname",
			"db_name",
		})

	// OVS datapath metrics
	metricOvsDp = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "datapath",
			Help:      "Represents an existing datapath. This metrics is always 1.",
		},
		[]string{
			"hostname",
			"datapath",
			"type",
		})

	metricOvsDpTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "dp_total",
			Help:      "Represents total number of datapaths on the system.",
		},
		[]string{
			"hostname",
		})

	metricOvsDpIf = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "dp_if",
			Help:      "Represents an existing datapath interface. This metrics is always 1.",
		},
		[]string{
			"hostname",
			"datapath",
			"port",
			"type",
			"ofPort",
		})

	metricOvsDpIfTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "dp_if_total",
			Help:      "Represents the number of ports connected to the datapath.",
		},
		[]string{
			"hostname",
			"datapath",
		})

	metricOvsDpFlowsTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "dp_flows_total",
			Help:      "The number of flows in a datapath.",
		},
		[]string{
			"hostname",
			"datapath",
		})

	metricOvsDpFlowsLookupHit = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "dp_flows_lookup_hit",
			Help:      "The number of incoming packets in a datapath matching existing flows in the datapath.",
		},
		[]string{
			"hostname",
			"datapath",
		})

	metricOvsDpFlowsLookupMissed = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "dp_flows_lookup_missed",
			Help:      "The number of incoming packets in a datapath not matching any existing flow in the datapath.",
		},
		[]string{
			"hostname",
			"datapath",
		})

	metricOvsDpFlowsLookupLost = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "dp_flows_lookup_lost",
			Help:      "The number of incoming packets in a datapath destined for userspace process but subsequently dropped before reaching userspace.",
		},
		[]string{
			"hostname",
			"datapath",
		})

	metricOvsDpMasksHit = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "dp_masks_hit",
			Help:      "The total number of masks visited for matching incoming packets.",
		},
		[]string{
			"hostname",
			"datapath",
		})

	metricOvsDpMasksTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "dp_masks_total",
			Help:      "The number of masks in a datapath.",
		},
		[]string{
			"hostname",
			"datapath",
		})

	metricOvsDpMasksHitRatio = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "dp_masks_hit_ratio",
			Help:      "The average number of masks visited per packet. It is the ration between hit and total number of packets processed by a datapath.",
		},
		[]string{
			"hostname",
			"datapath",
		})

	// OVS Interface basic info metrics
	interfaceMain = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "interface",
			Help:      "Represents OVS interface. This is the primary metric for all other interface metrics. This metrics is always 1.",
		},
		[]string{
			"hostname",
			"uuid",
			"interfaceName",
		})

	interfaceAdminState = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "interface_admin_state",
			Help:      "The administrative state of the physical network link of OVS interface. The values are: down(0), up(1), other(2).",
		},
		[]string{
			"hostname",
			"interfaceName",
		})

	interfaceLinkState = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "interface_link_state",
			Help:      "The state of the physical network link of OVS interface. The values are: down(0), up(1), other(2).",
		},
		[]string{
			"hostname",
			"interfaceName",
		})

	interfaceMacInUse = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "interface_mac_in_use",
			Help:      "The MAC address in use by OVS interface.",
		},
		[]string{
			"hostname",
			"interfaceName",
			"mac_address",
		})

	interfaceMtu = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "interface_mtu",
			Help:      "The currently configured MTU for OVS interface.",
		},
		[]string{
			"hostname",
			"interfaceName",
		})

	interfaceOfPort = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "interface_of_port",
			Help:      "Represents the OpenFlow port ID associated with OVS interface.",
		},
		[]string{
			"hostname",
			"interfaceName",
		})

	interfaceIfIndex = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "interface_if_index",
			Help:      "Represents the interface index associated with OVS interface.",
		},
		[]string{
			"hostname",
			"interfaceName",
		})

	// OVS Interface Statistics: Successful transmit and receive counters
	interfaceStatTxPackets = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "interface_tx_packets",
			Help:      "Represents the number of transmitted packets by OVS interface.",
		},
		[]string{
			"hostname",
			"interfaceName",
		})

	interfaceStatTxBytes = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "interface_tx_bytes",
			Help:      "Represents the number of transmitted bytes by OVS interface.",
		},
		[]string{
			"hostname",
			"interfaceName",
		})

	interfaceStatRxPackets = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "interface_rx_packets",
			Help:      "Represents the number of received packets by OVS interface.",
		},
		[]string{
			"hostname",
			"interfaceName",
		})

	interfaceStatRxBytes = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "interface_rx_bytes",
			Help:      "Represents the number of received bytes by OVS interface.",
		},
		[]string{
			"hostname",
			"interfaceName",
		})

	// OVS Interface Statistics: Receive errors
	interfaceStatRxCrcError = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "interface_rx_crc_err",
			Help:      "Represents the number of CRC errors for the packets received by OVS interface.",
		},
		[]string{
			"hostname",
			"interfaceName",
		})

	interfaceStatRxDropped = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "interface_rx_dropped",
			Help:      "Represents the number of input packets dropped by OVS interface.",
		},
		[]string{
			"hostname",
			"interfaceName",
		})

	interfaceStatRxErrorsTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "interface_rx_errors",
			Help:      "Represents the total number of packets with errors received by OVS interface.",
		},
		[]string{
			"hostname",
			"interfaceName",
		})

	interfaceStatRxFrameError = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "interface_rx_frame_err",
			Help:      "Represents the number of frame alignment errors on the packets received by OVS interface.",
		},
		[]string{
			"hostname",
			"interfaceName",
		})

	interfaceStatRxMissedError = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "interface_rx_missed_err",
			Help:      "Represents the number of packets with RX missed received by OVS interface.",
		},
		[]string{
			"hostname",
			"interfaceName",
		})

	interfaceStatRxOverrunError = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "interface_rx_over_err",
			Help:      "Represents the number of packets with RX overrun received by OVS interface.",
		},
		[]string{
			"hostname",
			"interfaceName",
		})

	// OVS Interface Statistics: Transmit errors
	interfaceStatTxDropped = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "interface_tx_dropped",
			Help:      "Represents the number of output packets dropped by OVS interface.",
		},
		[]string{
			"hostname",
			"interfaceName",
		})

	interfaceStatTxErrorsTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "interface_tx_errors",
			Help:      "Represents the total number of transmit errors by OVS interface.",
		},
		[]string{
			"hostname",
			"interfaceName",
		})

	interfaceStatCollisions = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "interface_collisions",
			Help:      "Represents the number of collisions on OVS interface.",
		},
		[]string{
			"hostname",
			"interfaceName",
		})

	interfaceStatRxMulticastPackets = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricNamespace,
			Name:      "interface_rx_multicast_packets",
			Help:      "Represents the count of multicast packets received by OVS interface.",
		},
		[]string{
			"hostname",
			"interfaceName",
		})
)

func InitPingerMetrics() {
	prometheus.MustRegister(ovsUpGauge)
	prometheus.MustRegister(ovsDownGauge)
	prometheus.MustRegister(ovnControllerUpGauge)
	prometheus.MustRegister(ovnControllerDownGauge)
	prometheus.MustRegister(inconsistentPortBindingGauge)
	prometheus.MustRegister(apiserverHealthyGauge)
	prometheus.MustRegister(apiserverUnhealthyGauge)
	prometheus.MustRegister(apiserverRequestLatencyHistogram)
	prometheus.MustRegister(internalDNSHealthyGauge)
	prometheus.MustRegister(internalDNSUnhealthyGauge)
	prometheus.MustRegister(internalDNSRequestLatencyHistogram)
	prometheus.MustRegister(externalDNSHealthyGauge)
	prometheus.MustRegister(externalDNSUnhealthyGauge)
	prometheus.MustRegister(externalDNSRequestLatencyHistogram)
	prometheus.MustRegister(podPingLatencyHistogram)
	prometheus.MustRegister(podPingLostCounter)
	prometheus.MustRegister(podPingTotalCounter)
	prometheus.MustRegister(nodePingLatencyHistogram)
	prometheus.MustRegister(nodePingLostCounter)
	prometheus.MustRegister(nodePingTotalCounter)
	prometheus.MustRegister(externalPingLatencyHistogram)
	prometheus.MustRegister(externalPingLostCounter)

	// ovs status metrics
	prometheus.MustRegister(metricOvsHealthyStatus)
	prometheus.MustRegister(metricOvsInfo)
	prometheus.MustRegister(metricRequestErrorNums)
	prometheus.MustRegister(metricLogFileSize)
	prometheus.MustRegister(metricDbFileSize)

	// ovs datapath metrics
	prometheus.MustRegister(metricOvsDp)
	prometheus.MustRegister(metricOvsDpTotal)
	prometheus.MustRegister(metricOvsDpIf)
	prometheus.MustRegister(metricOvsDpIfTotal)
	prometheus.MustRegister(metricOvsDpFlowsTotal)
	prometheus.MustRegister(metricOvsDpFlowsLookupHit)
	prometheus.MustRegister(metricOvsDpFlowsLookupMissed)
	prometheus.MustRegister(metricOvsDpFlowsLookupLost)
	prometheus.MustRegister(metricOvsDpMasksHit)
	prometheus.MustRegister(metricOvsDpMasksTotal)
	prometheus.MustRegister(metricOvsDpMasksHitRatio)

	// ovs Interface basic info metrics
	prometheus.MustRegister(interfaceMain)
	prometheus.MustRegister(interfaceAdminState)
	prometheus.MustRegister(interfaceLinkState)
	prometheus.MustRegister(interfaceMacInUse)
	prometheus.MustRegister(interfaceMtu)
	prometheus.MustRegister(interfaceOfPort)
	prometheus.MustRegister(interfaceIfIndex)

	// ovs Interface statistics metrics
	prometheus.MustRegister(interfaceStatTxPackets)
	prometheus.MustRegister(interfaceStatTxBytes)
	prometheus.MustRegister(interfaceStatRxPackets)
	prometheus.MustRegister(interfaceStatRxBytes)
	prometheus.MustRegister(interfaceStatRxCrcError)
	prometheus.MustRegister(interfaceStatRxDropped)
	prometheus.MustRegister(interfaceStatRxErrorsTotal)
	prometheus.MustRegister(interfaceStatRxFrameError)
	prometheus.MustRegister(interfaceStatRxMissedError)
	prometheus.MustRegister(interfaceStatRxOverrunError)
	prometheus.MustRegister(interfaceStatTxDropped)
	prometheus.MustRegister(interfaceStatTxErrorsTotal)
	prometheus.MustRegister(interfaceStatCollisions)
	prometheus.MustRegister(interfaceStatRxMulticastPackets)
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

func SetInternalDNSHealthyMetrics(nodeName string, latency float64) {
	internalDNSHealthyGauge.WithLabelValues(nodeName).Set(1)
	internalDNSRequestLatencyHistogram.WithLabelValues(nodeName).Observe(latency)
	internalDNSUnhealthyGauge.WithLabelValues(nodeName).Set(0)
}

func SetInternalDNSUnhealthyMetrics(nodeName string) {
	internalDNSHealthyGauge.WithLabelValues(nodeName).Set(0)
	internalDNSUnhealthyGauge.WithLabelValues(nodeName).Set(1)
}

func SetExternalDNSHealthyMetrics(nodeName string, latency float64) {
	externalDNSHealthyGauge.WithLabelValues(nodeName).Set(1)
	externalDNSRequestLatencyHistogram.WithLabelValues(nodeName).Observe(latency)
	externalDNSUnhealthyGauge.WithLabelValues(nodeName).Set(0)
}

func SetExternalDNSUnhealthyMetrics(nodeName string) {
	externalDNSHealthyGauge.WithLabelValues(nodeName).Set(0)
	externalDNSUnhealthyGauge.WithLabelValues(nodeName).Set(1)
}

func SetPodPingMetrics(srcNodeName, srcNodeIP, srcPodIP, targetNodeName, targetNodeIP, targetPodIP string, latency float64, lost, total int) {
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
	podPingTotalCounter.WithLabelValues(
		srcNodeName,
		srcNodeIP,
		srcPodIP,
		targetNodeName,
		targetNodeIP,
		targetPodIP,
	).Add(float64(total))
}

func SetNodePingMetrics(srcNodeName, srcNodeIP, srcPodIP, targetNodeName, targetNodeIP string, latency float64, lost, total int) {
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
	nodePingTotalCounter.WithLabelValues(
		srcNodeName,
		srcNodeIP,
		srcPodIP,
		targetNodeName,
		targetNodeIP,
	).Add(float64(total))
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
