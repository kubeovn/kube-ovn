package controller

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

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
	metrics.Registry.MustRegister(metricSubnetAvailableIPs)
	metrics.Registry.MustRegister(metricSubnetUsedIPs)
	metrics.Registry.MustRegister(metricCentralSubnetInfo)
	metrics.Registry.MustRegister(metricSubnetIPAMInfo)
	metrics.Registry.MustRegister(metricSubnetIPAssignedInfo)
}
