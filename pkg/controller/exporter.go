package controller

import (
	"math"
	"strconv"
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

var registerMetricsOnce sync.Once

// registerSubnetMetrics register subnet metrics
func (c *Controller) registerSubnetMetrics() {
	registerMetricsOnce.Do(func() {
		registerMetrics()
	})
}

func resetSubnetMetrics() {
	metricSubnetAvailableIPs.Reset()
	metricSubnetUsedIPs.Reset()
	metricCentralSubnetInfo.Reset()
	metricSubnetIPAMInfo.Reset()
	metricSubnetIPAssignedInfo.Reset()
}

func (c *Controller) exportSubnetMetrics() {
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnet, %v", err)
		return
	}

	resetSubnetMetrics()
	for _, subnet := range subnets {
		if !subnet.Status.IsValidated() {
			continue
		}

		c.exportSubnetAvailableIPsGauge(subnet)
		c.exportSubnetUsedIPsGauge(subnet)
		c.exportSubnetIPAMInfo(subnet)
		c.exportSubnetIPAssignedInfo(subnet)

		if subnet.Spec.GatewayType == kubeovnv1.GWCentralizedType {
			c.exportCentralizedSubnetInfo(subnet)
		}
	}
}

func (c *Controller) exportSubnetAvailableIPsGauge(subnet *kubeovnv1.Subnet) {
	var availableIPs float64
	switch subnet.Spec.Protocol {
	case kubeovnv1.ProtocolIPv4:
		availableIPs = subnet.Status.V4AvailableIPs.Float64()
	case kubeovnv1.ProtocolIPv6:
		availableIPs = subnet.Status.V6AvailableIPs.Float64()
	default:
		availableIPs = math.Min(subnet.Status.V4AvailableIPs.Float64(), subnet.Status.V6AvailableIPs.Float64())
	}
	metricSubnetAvailableIPs.WithLabelValues(subnet.Name, subnet.Spec.Protocol, subnet.Spec.CIDRBlock).Set(availableIPs)
}

func (c *Controller) exportSubnetUsedIPsGauge(subnet *kubeovnv1.Subnet) {
	var usingIPs float64
	if subnet.Spec.Protocol == kubeovnv1.ProtocolIPv6 {
		usingIPs = subnet.Status.V6UsingIPs.Float64()
	} else {
		usingIPs = subnet.Status.V4UsingIPs.Float64()
	}
	metricSubnetUsedIPs.WithLabelValues(subnet.Name, subnet.Spec.Protocol, subnet.Spec.CIDRBlock).Set(usingIPs)
}

func (c *Controller) exportCentralizedSubnetInfo(subnet *kubeovnv1.Subnet) {
	lrPolicyList, err := c.OVNNbClient.GetLogicalRouterPoliciesByExtID(c.config.ClusterRouter, "subnet", subnet.Name)
	if err != nil {
		klog.Errorf("failed to list lr policy for subnet %s: %v", subnet.Name, err)
		return
	}

	for _, lrPolicy := range lrPolicyList {
		if lrPolicy.Action == ovnnb.LogicalRouterPolicyActionReroute {
			metricCentralSubnetInfo.WithLabelValues(subnet.Name, strconv.FormatBool(subnet.Spec.EnableEcmp), subnet.Spec.GatewayNode, subnet.Status.ActivateGateway, lrPolicy.Match, strings.Join(lrPolicy.Nexthops, ",")).Set(1)
			break
		}
	}
}

func (c *Controller) exportSubnetIPAMInfo(subnet *kubeovnv1.Subnet) {
	ipamSubnet, ok := c.ipam.Subnets[subnet.Name]
	if !ok {
		klog.Errorf("failed to get subnet %s in ipam", subnet.Name)
		return
	}

	ipamSubnet.Mutex.RLock()
	defer ipamSubnet.Mutex.RUnlock()

	switch subnet.Spec.Protocol {
	case kubeovnv1.ProtocolIPv4:
		metricSubnetIPAMInfo.WithLabelValues(subnet.Name, subnet.Spec.CIDRBlock, ipamSubnet.V4Free.String(), ipamSubnet.V4Reserved.String(), ipamSubnet.V4Available.String(), ipamSubnet.V4Using.String()).Set(1)

	case kubeovnv1.ProtocolIPv6:
		metricSubnetIPAMInfo.WithLabelValues(subnet.Name, subnet.Spec.CIDRBlock, ipamSubnet.V6Free.String(), ipamSubnet.V6Reserved.String(), ipamSubnet.V6Available.String(), ipamSubnet.V6Using.String()).Set(1)

	case kubeovnv1.ProtocolDual:
		cidrV4, cidrV6 := util.SplitStringIP(subnet.Spec.CIDRBlock)
		metricSubnetIPAMInfo.WithLabelValues(subnet.Name, cidrV4, ipamSubnet.V4Free.String(), ipamSubnet.V4Reserved.String(), ipamSubnet.V4Available.String(), ipamSubnet.V4Using.String()).Set(1)
		metricSubnetIPAMInfo.WithLabelValues(subnet.Name, cidrV6, ipamSubnet.V6Free.String(), ipamSubnet.V6Reserved.String(), ipamSubnet.V6Available.String(), ipamSubnet.V6Using.String()).Set(1)
	}
}

func (c *Controller) exportSubnetIPAssignedInfo(subnet *kubeovnv1.Subnet) {
	ipamSubnet, ok := c.ipam.Subnets[subnet.Name]
	if !ok {
		klog.Errorf("failed to get subnet %s in ipam", subnet.Name)
		return
	}

	ipamSubnet.Mutex.RLock()
	defer ipamSubnet.Mutex.RUnlock()

	if subnet.Spec.Protocol == kubeovnv1.ProtocolIPv4 || subnet.Spec.Protocol == kubeovnv1.ProtocolDual {
		for ip, pod := range ipamSubnet.V4IPToPod {
			metricSubnetIPAssignedInfo.WithLabelValues(subnet.Name, ip, pod).Set(1)
		}
	}
	if subnet.Spec.Protocol == kubeovnv1.ProtocolIPv6 || subnet.Spec.Protocol == kubeovnv1.ProtocolDual {
		for ip, pod := range ipamSubnet.V6IPToPod {
			metricSubnetIPAssignedInfo.WithLabelValues(subnet.Name, ip, pod).Set(1)
		}
	}
}
