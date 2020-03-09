package controller

import (
	"fmt"
	"github.com/alauda/kube-ovn/pkg/ovs"
	"github.com/alauda/kube-ovn/pkg/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog"
	"strings"
)

func (c *Controller) gc() error {
	gcFunctions := []func() error{
		c.gcLogicalSwitch,
		c.gcNode,
		c.gcLogicalSwitch,
		c.gcLogicalSwitchPort,
		c.gcLoadBalancer,
		c.gcPortGroup,
		c.gcStaticRoute,
	}
	for _, gcFunc := range gcFunctions {
		if err := gcFunc(); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) gcLogicalSwitch() error {
	klog.Infof("start to gc logical switch")
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnet, %v", err)
		return err
	}
	subnetNames := make([]string, 0, len(subnets))
	for _, s := range subnets {
		subnetNames = append(subnetNames, s.Name)
	}
	lss, err := c.ovnClient.ListLogicalSwitch()
	if err != nil {
		klog.Errorf("failed to list logical switch, %v", err)
		return err
	}
	klog.Infof("ls in ovn %v", lss)
	klog.Infof("subnet in kubernetes %v", subnetNames)
	for _, ls := range lss {
		if !util.IsStringIn(ls, subnetNames) {
			klog.Infof("gc subnet %s", ls)
			if err := c.handleDeleteSubnet(ls); err != nil {
				klog.Errorf("failed to gc subnet %s, %v", ls, err)
				return err
			}
		}
	}
	return nil
}

func (c *Controller) gcNode() error {
	klog.Infof("start to gc nodes")
	nodes, err := c.nodesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list node, %v", err)
		return err
	}
	nodeNames := make([]string, 0, len(nodes))
	for _, no := range nodes {
		nodeNames = append(nodeNames, no.Name)
	}
	ips, err := c.ipsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list ip, %v", err)
		return err
	}
	ipNodeNames := make([]string, 0, len(ips))
	for _, ip := range ips {
		if !strings.Contains(ip.Name, ".") {
			ipNodeNames = append(ipNodeNames, strings.TrimPrefix(ip.Name, "node-"))
		}
	}
	for _, no := range ipNodeNames {
		if !util.IsStringIn(no, nodeNames) {
			klog.Infof("gc node %s", no)
			if err := c.handleDeleteNode(no); err != nil {
				klog.Errorf("failed to gc node %s, %v", no, err)
				return err
			}
		}
	}
	return nil
}

func (c *Controller) gcLogicalSwitchPort() error {
	klog.Infof("start to gc logical switch ports")
	pods, err := c.podsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list ip, %v", err)
		return err
	}
	nodes, err := c.nodesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list node, %v", err)
		return err
	}
	ipNames := make([]string, 0, len(pods)+len(nodes))
	for _, pod := range pods {
		ipNames = append(ipNames, fmt.Sprintf("%s.%s", pod.Name, pod.Namespace))
	}
	for _, node := range nodes {
		ipNames = append(ipNames, fmt.Sprintf("node-%s", node.Name))
	}
	lsps, err := c.ovnClient.ListLogicalSwitchPort()
	if err != nil {
		klog.Errorf("failed to list logical switch port, %v", err)
		return err
	}
	for _, lsp := range lsps {
		if !util.IsStringIn(lsp, ipNames) {
			if strings.Contains(lsp, ".") {
				klog.Infof("gc logical switch port %s", lsp)
				podName := strings.Split(lsp, ".")[0]
				podNameSpace := strings.Split(lsp, ".")[1]
				if err := c.handleDeletePod(fmt.Sprintf("%s/%s", podNameSpace, podName)); err != nil {
					klog.Errorf("failed to gc port %s, %v", lsp, err)
					return err
				}
			}
		}
	}
	return nil
}

func (c *Controller) gcLoadBalancer() error {
	klog.Infof("start to gc loadbalancers")
	svcs, err := c.servicesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list svc, %v", err)
		return err
	}
	tcpVips := []string{}
	udpVips := []string{}
	for _, svc := range svcs {
		ip := svc.Spec.ClusterIP
		for _, port := range svc.Spec.Ports {
			if port.Protocol == corev1.ProtocolTCP {
				tcpVips = append(tcpVips, fmt.Sprintf("%s:%d", ip, port.Port))
			} else {
				udpVips = append(udpVips, fmt.Sprintf("%s:%d", ip, port.Port))
			}
		}
	}

	lbUuid, err := c.ovnClient.FindLoadbalancer(c.config.ClusterTcpLoadBalancer)
	if err != nil {
		klog.Errorf("failed to get lb %v", err)
	}
	vips, err := c.ovnClient.GetLoadBalancerVips(lbUuid)
	if err != nil {
		klog.Errorf("failed to get udp lb vips %v", err)
		return err
	}
	for vip := range vips {
		if !util.IsStringIn(vip, tcpVips) {
			err := c.ovnClient.DeleteLoadBalancerVip(vip, c.config.ClusterTcpLoadBalancer)
			if err != nil {
				klog.Errorf("failed to delete vip %s from tcp lb, %v", vip, err)
				return err
			}
		}
	}

	lbUuid, err = c.ovnClient.FindLoadbalancer(c.config.ClusterUdpLoadBalancer)
	if err != nil {
		klog.Errorf("failed to get lb %v", err)
		return err
	}
	vips, err = c.ovnClient.GetLoadBalancerVips(lbUuid)
	if err != nil {
		klog.Errorf("failed to get udp lb vips %v", err)
		return err
	}
	for vip := range vips {
		if !util.IsStringIn(vip, udpVips) {
			err := c.ovnClient.DeleteLoadBalancerVip(vip, c.config.ClusterUdpLoadBalancer)
			if err != nil {
				klog.Errorf("failed to delete vip %s from tcp lb, %v", vip, err)
				return err
			}
		}
	}
	return nil
}

func (c *Controller) gcPortGroup() error {
	klog.Infof("start to gc network policy")
	nps, err := c.npsLister.List(labels.Everything())
	npNames := make([]string, 0, len(nps))
	for _, np := range nps {
		npNames = append(npNames, fmt.Sprintf("%s/%s", np.Namespace, np.Name))
	}
	if err != nil {
		klog.Errorf("failed to list network policy, %v", err)
		return err
	}
	pgs, err := c.ovnClient.ListPortGroup()
	if err != nil {
		klog.Errorf("failed to list port-group, %v", err)
		return err
	}
	for _, pg := range pgs {
		if !util.IsStringIn(fmt.Sprintf("%s/%s", pg.NpNamespace, pg.NpName), npNames) {
			klog.Infof("gc port group %s", pg.Name)
			if err := c.handleDeleteNp(fmt.Sprintf("%s/%s", pg.NpNamespace, pg.NpName)); err != nil {
				klog.Errorf("failed to gc np %s/%s, %v", pg.NpNamespace, pg.NpName, err)
				return err
			}
		}
	}
	return nil
}

func (c *Controller) gcStaticRoute() error {
	routes, err := c.ovnClient.ListStaticRoute()
	if err != nil {
		klog.Errorf("failed to list static route %v", err)
		return err
	}
	for _, route := range routes {
		if route.Policy == ovs.PolicyDstIP {
			if !c.ipam.ContainAddress(route.NextHop) {
				klog.Infof("gc static route %s %s %s", route.Policy, route.CIDR, route.NextHop)
				if err := c.ovnClient.DeleteStaticRouteByNextHop(route.NextHop); err != nil {
					klog.Errorf("failed to delete stale nexthop route %s, %v", route.NextHop, err)
				}
			}
		} else {
			if !c.ipam.ContainAddress(route.CIDR) {
				klog.Infof("gc static route %s %s %s", route.Policy, route.CIDR, route.NextHop)
				if err := c.ovnClient.DeleteStaticRoute(route.CIDR, c.config.ClusterRouter); err != nil {
					klog.Errorf("failed to delete stale route %s, %v", route.NextHop, err)
				}
			}
		}
	}
	return nil
}
