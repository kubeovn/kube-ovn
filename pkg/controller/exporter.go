package controller

import (
	"context"
	"math"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

var registerMetricsOnce sync.Once

// registerSubnetMetrics register subnet metrics
func (c *Controller) registerSubnetMetrics() {
	registerMetricsOnce.Do(func() {
		registerMetrics()
	})
}

func (c *Controller) resyncProviderNetworkStatus() {
	nodeList, err := c.nodesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to get nodes %v", err)
		return
	}
	pnList, err := c.providerNetworksLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to get provider networks %v", err)
		return
	}

	for _, pn := range pnList {
		ready := true
		for _, node := range nodeList {
			if !util.ContainsString(pn.Spec.ExcludeNodes, node.Name) &&
				!util.ContainsString(pn.Status.ReadyNodes, node.Name) {
				ready = false
				break
			}
		}
		if ready != pn.Status.Ready {
			newPn := pn.DeepCopy()
			newPn.Status.Ready = ready
			_, err = c.config.KubeOvnClient.KubeovnV1().ProviderNetworks().Update(context.Background(), newPn, metav1.UpdateOptions{})
			if err != nil {
				klog.Errorf("failed to update provider network %s: %v", pn.Name, err)
			}
		}
	}
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
	if subnet.Spec.Protocol == kubeovnv1.ProtocolIPv4 {
		availableIPs = subnet.Status.V4AvailableIPs
	} else if subnet.Spec.Protocol == kubeovnv1.ProtocolIPv6 {
		availableIPs = subnet.Status.V6AvailableIPs
	} else {
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
