package controller

import (
	"sync"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog"

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
	metricSubnetAvailableIPs.WithLabelValues(subnet.Name, subnet.Spec.Protocol, subnet.Spec.CIDRBlock).Set(subnet.Status.AvailableIPs)
}

func (c *Controller) exportSubnetUsedIPsGauge(subnet *kubeovnv1.Subnet) {
	metricSubnetUsedIPs.WithLabelValues(subnet.Name, subnet.Spec.Protocol, subnet.Spec.CIDRBlock).Set(subnet.Status.UsingIPs)
}
