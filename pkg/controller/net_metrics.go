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
)

func registerMetrics() {
	prometheus.MustRegister(metricSubnetAvailableIPs)
	prometheus.MustRegister(metricSubnetUsedIPs)
}
