package controller

import (
	"math"
	"sync"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

var registerMetricsOnce sync.Once

// registerSubnetMetrics register subnet metrics
func (c *Controller) registerSubnetMetrics() {
	registerMetricsOnce.Do(func() {
		registerMetrics()
	})
}

// resyncSubnetMetrics start to update subnet metrics
func (c *Controller) resyncSubnetMetrics() {
	c.exportSubnetMetrics()
}

func (c *Controller) exportSubnetMetrics() bool {
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnet, %v", err)
		return false
	}
	for _, subnet := range subnets {
		c.exportSubnetAvailableIPsGauge(subnet)
		c.exportSubnetUsedIPsGauge(subnet)
	}

	return true
}

func (c *Controller) exportSubnetAvailableIPsGauge(subnet *kubeovnv1.Subnet) {
	var availableIPs float64
	switch subnet.Spec.Protocol {
	case kubeovnv1.ProtocolIPv4:
		availableIPs = subnet.Status.V4AvailableIPs
	case kubeovnv1.ProtocolIPv6:
		availableIPs = subnet.Status.V6AvailableIPs
	default:
		availableIPs = math.Min(subnet.Status.V4AvailableIPs, subnet.Status.V6AvailableIPs)
	}
	metricSubnetAvailableIPs.WithLabelValues(subnet.Name, subnet.Spec.Protocol, subnet.Spec.CIDRBlock).Set(availableIPs)
}

func (c *Controller) exportSubnetUsedIPsGauge(subnet *kubeovnv1.Subnet) {
	var usingIPs float64
	if subnet.Spec.Protocol == kubeovnv1.ProtocolIPv6 {
		usingIPs = subnet.Status.V6UsingIPs
	} else {
		usingIPs = subnet.Status.V4UsingIPs
	}
	metricSubnetUsedIPs.WithLabelValues(subnet.Name, subnet.Spec.Protocol, subnet.Spec.CIDRBlock).Set(usingIPs)
}
