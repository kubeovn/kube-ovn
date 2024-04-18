package controller

import "github.com/prometheus/client_golang/prometheus"

var (
	metricSubnetAvailableIPs = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "subnet_available_ip_count",
			Help: "The available num of ip address in subnet.",
		},
		[]string{
			"subnet_name",
			"protocol",
			"subnet_cidr",
		})

	metricSubnetUsedIPs = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "subnet_used_ip_count",
			Help: "The used num of ip address in subnet.",
		},
		[]string{
			"subnet_name",
			"protocol",
			"subnet_cidr",
		})

	metricCentralSubnetInfo = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "centralized_subnet_info",
			Help: "Provide information for centralized subnet.",
		},
		[]string{
			"subnet_name",
			"enable_ecmp",
			"gateway_node",
			"active_gateway",
			"match",
			"nexthops",
		})

	metricSubnetIPAMInfo = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "subnet_ipam_info",
			Help: "Provide information for subnet ip address management.",
		},
		[]string{
			"subnet_name",
			"cidr",
			"free",
			"reserved",
			"available",
			"using",
		})

	metricSubnetIPAssignedInfo = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "subnet_ip_assign_info",
			Help: "Provide information for subnet ip address assigned info.",
		},
		[]string{
			"subnet_name",
			"ip",
			"pod_name",
		})
)

func registerMetrics() {
	prometheus.MustRegister(metricSubnetAvailableIPs)
	prometheus.MustRegister(metricSubnetUsedIPs)
	prometheus.MustRegister(metricCentralSubnetInfo)
	prometheus.MustRegister(metricSubnetIPAMInfo)
	prometheus.MustRegister(metricSubnetIPAssignedInfo)
}
