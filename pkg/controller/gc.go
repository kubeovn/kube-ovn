package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

var lastNoPodLSP map[string]bool

func (c *Controller) gc() error {
	gcFunctions := []func() error{
		c.gcNode,
		c.gcLogicalSwitch,
		c.gcCustomLogicalRouter,
		c.gcLogicalSwitchPort,
		c.gcLoadBalancer,
		c.gcPortGroup,
		c.gcStaticRoute,
		c.gcVpcNatGateway,
		c.gcLogicalRouterPort,
	}
	for _, gcFunc := range gcFunctions {
		if err := gcFunc(); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) gcLogicalRouterPort() error {
	klog.Infof("start to gc logical router port")
	vpcs, err := c.vpcsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list vpc, %v", err)
		return err
	}

	var exceptPeerPorts []string
	for _, vpc := range vpcs {
		for _, peer := range vpc.Status.VpcPeerings {
			exceptPeerPorts = append(exceptPeerPorts, fmt.Sprintf("%s-%s", vpc.Name, peer))
		}
	}
	lrps, err := c.ovnLegacyClient.ListLogicalEntity("logical_router_port", "peer!=[]")
	if err != nil {
		klog.Errorf("failed to list logical router port, %v", err)
		return err
	}
	for _, lrp := range lrps {
		if !util.ContainsString(exceptPeerPorts, lrp) {
			if err = c.ovnLegacyClient.DeleteLogicalRouterPort(lrp); err != nil {
				klog.Errorf("failed to delete logical router port %s, %v", lrp, err)
				return err
			}
		}
	}
	return nil
}

func (c *Controller) gcVpcNatGateway() error {
	klog.Infof("start to gc vpc nat gateway")
	gws, err := c.vpcNatGatewayLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list vpc nat gateway, %v", err)
		return err
	}

	var gwDpNames []string
	for _, gw := range gws {
		_, err = c.vpcsLister.Get(gw.Spec.Vpc)
		if err != nil {
			if !k8serrors.IsNotFound(err) {
				klog.Errorf("failed to get vpc, %v", err)
				return err
			}
			if err = c.config.KubeOvnClient.KubeovnV1().VpcNatGateways().Delete(context.Background(), gw.Name, metav1.DeleteOptions{}); err != nil {
				klog.Errorf("failed to delete vpc nat gateway, %v", err)
				return err
			}
		}
		gwDpNames = append(gwDpNames, genNatGwDpName(gw.Name))
	}

	sel, _ := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{MatchLabels: map[string]string{util.VpcNatGatewayLabel: "true"}})
	dps, err := c.config.KubeClient.AppsV1().Deployments(c.config.PodNamespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: sel.String(),
	})
	if err != nil {
		klog.Errorf("failed to list vpc nat gateway deployment, %v", err)
		return err
	}
	for _, dp := range dps.Items {
		if !util.ContainsString(gwDpNames, dp.Name) {
			err = c.config.KubeClient.AppsV1().Deployments(c.config.PodNamespace).Delete(context.Background(), dp.Name, metav1.DeleteOptions{})
			if err != nil {
				klog.Errorf("failed to delete vpc nat gateway deployment, %v", err)
				return err
			}
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
	subnetMap := make(map[string]*kubeovnv1.Subnet, len(subnets))
	for _, s := range subnets {
		subnetMap[s.Name] = s
		subnetNames = append(subnetNames, s.Name)
	}
	lss, err := c.ovnLegacyClient.ListLogicalSwitch(c.config.EnableExternalVpc)
	if err != nil {
		klog.Errorf("failed to list logical switch, %v", err)
		return err
	}
	klog.Infof("ls in ovn %v", lss)
	klog.Infof("subnet in kubernetes %v", subnetNames)
	for _, ls := range lss {
		if ls == util.InterconnectionSwitch || ls == util.ExternalGatewaySwitch {
			continue
		}
		if s := subnetMap[ls]; s != nil && isOvnSubnet(s) {
			continue
		}

		klog.Infof("gc subnet %s", ls)
		if err := c.handleDeleteLogicalSwitch(ls); err != nil {
			klog.Errorf("failed to gc subnet %s, %v", ls, err)
			return err
		}
	}
	return nil
}

func (c *Controller) gcCustomLogicalRouter() error {
	klog.Infof("start to gc logical router")
	vpcs, err := c.vpcsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list vpc, %v", err)
		return err
	}
	vpcNames := make([]string, 0, len(vpcs))
	for _, s := range vpcs {
		vpcNames = append(vpcNames, s.Name)
	}
	lrs, err := c.ovnLegacyClient.ListLogicalRouter(c.config.EnableExternalVpc)
	if err != nil {
		klog.Errorf("failed to list logical router, %v", err)
		return err
	}
	klog.Infof("lr in ovn %v", lrs)
	klog.Infof("vpc in kubernetes %v", vpcNames)
	for _, lr := range lrs {
		if lr == util.DefaultVpc {
			continue
		}
		if !util.IsStringIn(lr, vpcNames) {
			klog.Infof("gc router %s", lr)
			if err := c.deleteVpcRouter(lr); err != nil {
				klog.Errorf("failed to delete router %s, %v", lr, err)
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
	klog.Info("start to gc logical switch port")
	if err := c.markAndCleanLSP(); err != nil {
		return err
	}
	return c.markAndCleanLSP()
}

func (c *Controller) markAndCleanLSP() error {
	klog.V(4).Infof("start to gc logical switch ports")
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
	ipMap := make(map[string]struct{}, len(pods)+len(nodes))
	for _, pod := range pods {
		if isStsPod, sts := isStatefulSetPod(pod); isStsPod {
			if isStatefulSetPodToDel(c.config.KubeClient, pod, sts) {
				continue
			}
		} else if !isPodAlive(pod) {
			continue
		}
		podName := c.getNameByPod(pod)

		for k, v := range pod.Annotations {
			if !strings.Contains(k, util.AllocatedAnnotationSuffix) || v != "true" {
				continue
			}
			providerName := strings.ReplaceAll(k, util.AllocatedAnnotationSuffix, "")
			isProviderOvn, err := c.isOVNProvided(providerName, pod)
			if err != nil {
				klog.Errorf("determine if provider is ovn failed %v", err)
			}
			if !isProviderOvn {
				continue
			}
			ipMap[ovs.PodNameToPortName(podName, pod.Namespace, providerName)] = struct{}{}
		}
	}
	for _, node := range nodes {
		if node.Annotations[util.AllocatedAnnotation] == "true" {
			ipMap[fmt.Sprintf("node-%s", node.Name)] = struct{}{}
		}
	}

	// The lsp for vm pod should not be deleted if vm still exists
	vmLsps := c.getVmLsps()
	for _, vmLsp := range vmLsps {
		ipMap[vmLsp] = struct{}{}
	}

	lsps, err := c.ovnClient.ListLogicalSwitchPorts(c.config.EnableExternalVpc, nil)
	if err != nil {
		klog.Errorf("failed to list logical switch port, %v", err)
		return err
	}

	noPodLSP := map[string]bool{}
	lspMap := make(map[string]struct{}, len(lsps))
	for _, lsp := range lsps {
		lspMap[lsp.Name] = struct{}{}
		if _, ok := ipMap[lsp.Name]; ok {
			continue
		}
		if !lastNoPodLSP[lsp.Name] {
			noPodLSP[lsp.Name] = true
			continue
		}

		klog.Infof("gc logical switch port %s", lsp.Name)
		if err := c.ovnLegacyClient.DeleteLogicalSwitchPort(lsp.Name); err != nil {
			klog.Errorf("failed to delete lsp %s, %v", lsp.Name, err)
			return err
		}
		if err := c.config.KubeOvnClient.KubeovnV1().IPs().Delete(context.Background(), lsp.Name, metav1.DeleteOptions{}); err != nil {
			if !k8serrors.IsNotFound(err) {
				klog.Errorf("failed to delete ip %s, %v", lsp.Name, err)
				return err
			}
		}

		if key := lsp.ExternalIDs["pod"]; key != "" {
			c.ipam.ReleaseAddressByPod(key)
		}
	}
	lastNoPodLSP = noPodLSP

	for ipName := range ipMap {
		if _, ok := lspMap[ipName]; !ok {
			klog.Errorf("lsp lost for pod %s, please delete the pod and retry", ipName)
		}
	}

	return nil
}

func (c *Controller) gcLoadBalancer() error {
	klog.Infof("start to gc loadbalancers")
	start := time.Now()
	if !c.config.EnableLb {
		// remove lb from logical switch
		vpcs, err := c.vpcsLister.List(labels.Everything())
		if err != nil {
			return err
		}
		for _, orivpc := range vpcs {
			vpc := orivpc.DeepCopy()
			for _, subnetName := range vpc.Status.Subnets {
				subnet, err := c.subnetsLister.Get(subnetName)
				if err != nil {
					if k8serrors.IsNotFound(err) {
						continue
					}
					return err
				}
				if !isOvnSubnet(subnet) {
					continue
				}

				err = c.ovnLegacyClient.RemoveLbFromLogicalSwitch(
					vpc.Status.TcpLoadBalancer,
					vpc.Status.TcpSessionLoadBalancer,
					vpc.Status.UdpLoadBalancer,
					vpc.Status.UdpSessionLoadBalancer,
					subnetName)
				if err != nil {
					return err
				}
			}

			vpc.Status.TcpLoadBalancer = ""
			vpc.Status.TcpSessionLoadBalancer = ""
			vpc.Status.UdpLoadBalancer = ""
			vpc.Status.UdpSessionLoadBalancer = ""
			bytes, err := vpc.Status.Bytes()
			if err != nil {
				return err
			}
			_, err = c.config.KubeOvnClient.KubeovnV1().Vpcs().Patch(context.Background(), vpc.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status")
			if err != nil {
				return err
			}
		}

		// delete
		ovnLbs, err := c.ovnLegacyClient.ListLoadBalancer()
		if err != nil {
			klog.Errorf("failed to list load balancer, %v", err)
			return err
		}
		if err = c.ovnLegacyClient.DeleteLoadBalancer(ovnLbs...); err != nil {
			klog.Errorf("failed to delete load balancer, %v", err)
			return err
		}
		return nil
	}

	svcs, err := c.servicesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list svc, %v", err)
		return err
	}
	tcpVips := make(map[string]struct{}, len(svcs)*2)
	udpVips := make(map[string]struct{}, len(svcs)*2)
	tcpSessionVips := make(map[string]struct{}, len(svcs)*2)
	udpSessionVips := make(map[string]struct{}, len(svcs)*2)
	for _, svc := range svcs {
		ips := util.ServiceClusterIPs(*svc)
		for _, ip := range ips {
			for _, port := range svc.Spec.Ports {
				vip := util.JoinHostPort(ip, port.Port)
				if port.Protocol == corev1.ProtocolTCP {
					if svc.Spec.SessionAffinity == corev1.ServiceAffinityClientIP {
						tcpSessionVips[vip] = struct{}{}
					} else {
						tcpVips[vip] = struct{}{}
					}
				} else {
					if svc.Spec.SessionAffinity == corev1.ServiceAffinityClientIP {
						udpSessionVips[vip] = struct{}{}
					} else {
						udpVips[vip] = struct{}{}
					}
				}
			}
		}
	}

	vpcs, err := c.vpcsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list vpc, %v", err)
		return err
	}
	var vpcLbs []string
	for _, vpc := range vpcs {
		tcpLb, udpLb := vpc.Status.TcpLoadBalancer, vpc.Status.UdpLoadBalancer
		tcpSessLb, udpSessLb := vpc.Status.TcpSessionLoadBalancer, vpc.Status.UdpSessionLoadBalancer
		vpcLbs = append(vpcLbs, tcpLb, udpLb, tcpSessLb, udpSessLb)

		if tcpLb != "" {
			lbUuid, err := c.ovnLegacyClient.FindLoadbalancer(tcpLb)
			if err != nil {
				klog.Errorf("failed to get lb %v", err)
			}
			vips, err := c.ovnLegacyClient.GetLoadBalancerVips(lbUuid)
			if err != nil {
				klog.Errorf("failed to get tcp lb vips %v", err)
				return err
			}
			for vip := range vips {
				if _, ok := tcpVips[vip]; !ok {
					klog.Infof("gc vip %s in LB %s", vip, tcpLb)
					err := c.ovnLegacyClient.DeleteLoadBalancerVip(vip, tcpLb)
					if err != nil {
						klog.Errorf("failed to delete vip %s from tcp lb %s, %v", vip, tcpLb, err)
						return err
					}
				}
			}
		}

		if tcpSessLb != "" {
			lbUuid, err := c.ovnLegacyClient.FindLoadbalancer(tcpSessLb)
			if err != nil {
				klog.Errorf("failed to get lb %v", err)
			}
			vips, err := c.ovnLegacyClient.GetLoadBalancerVips(lbUuid)
			if err != nil {
				klog.Errorf("failed to get tcp session lb vips %v", err)
				return err
			}
			for vip := range vips {
				if _, ok := tcpSessionVips[vip]; !ok {
					klog.Infof("gc vip %s in LB %s", vip, tcpSessLb)
					err := c.ovnLegacyClient.DeleteLoadBalancerVip(vip, tcpSessLb)
					if err != nil {
						klog.Errorf("failed to delete vip %s from tcp session lb %s, %v", vip, tcpSessLb, err)
						return err
					}
				}
			}
		}

		if udpLb != "" {
			lbUuid, err := c.ovnLegacyClient.FindLoadbalancer(udpLb)
			if err != nil {
				klog.Errorf("failed to get lb %v", err)
				return err
			}
			vips, err := c.ovnLegacyClient.GetLoadBalancerVips(lbUuid)
			if err != nil {
				klog.Errorf("failed to get udp lb vips %v", err)
				return err
			}
			for vip := range vips {
				if _, ok := udpVips[vip]; !ok {
					klog.Infof("gc vip %s in LB %s", vip, udpLb)
					err := c.ovnLegacyClient.DeleteLoadBalancerVip(vip, udpLb)
					if err != nil {
						klog.Errorf("failed to delete vip %s from tcp lb %s, %v", vip, udpLb, err)
						return err
					}
				}
			}
		}

		if udpSessLb != "" {
			lbUuid, err := c.ovnLegacyClient.FindLoadbalancer(udpSessLb)
			if err != nil {
				klog.Errorf("failed to get lb %v", err)
				return err
			}
			vips, err := c.ovnLegacyClient.GetLoadBalancerVips(lbUuid)
			if err != nil {
				klog.Errorf("failed to get udp session lb vips %v", err)
				return err
			}
			for vip := range vips {
				if _, ok := udpSessionVips[vip]; !ok {
					klog.Infof("gc vip %s in LB %s", vip, udpSessLb)
					err := c.ovnLegacyClient.DeleteLoadBalancerVip(vip, udpSessLb)
					if err != nil {
						klog.Errorf("failed to delete vip %s from udp session lb %s, %v", vip, udpSessLb, err)
						return err
					}
				}
			}
		}
	}

	ovnLbs, err := c.ovnLegacyClient.ListLoadBalancer()
	if err != nil {
		klog.Errorf("failed to list load balancer, %v", err)
		return err
	}

	klog.Infof("vpcLbs: %v", vpcLbs)
	klog.Infof("ovnLbs: %v", ovnLbs)
	for _, lb := range ovnLbs {
		if util.ContainsString(vpcLbs, lb) {
			continue
		}
		klog.Infof("start to destroy load balancer %s", lb)
		if err := c.ovnLegacyClient.DeleteLoadBalancer(lb); err != nil {
			return err
		}
	}
	klog.Infof("took %.2fs to gc load balancers", time.Since(start).Seconds())
	return nil
}

func (c *Controller) gcPortGroup() error {
	klog.Infof("start to gc network policy")
	var npNames []string
	if c.config.EnableNP {
		nps, err := c.npsLister.List(labels.Everything())
		if err != nil {
			klog.Errorf("failed to list network policy, %v", err)
			return err
		}

		npNames = make([]string, 0, len(nps))
		for _, np := range nps {
			npNames = append(npNames, fmt.Sprintf("%s/%s", np.Namespace, np.Name))
		}
		// append node port group to npNames to avoid gc node port group
		nodes, err := c.nodesLister.List(labels.Everything())
		if err != nil {
			klog.Errorf("failed to list nodes, %v", err)
			return err
		}
		for _, node := range nodes {
			npNames = append(npNames, fmt.Sprintf("%s/%s", "node", node.Name))
		}

		// append overlay subnets port group to npNames to avoid gc distributed subnets port group
		subnets, err := c.subnetsLister.List(labels.Everything())
		if err != nil {
			klog.Errorf("failed to list subnets %v", err)
			return err
		}
		for _, subnet := range subnets {
			if subnet.Spec.Vpc != util.DefaultVpc || subnet.Spec.Vlan != "" || subnet.Name == c.config.NodeSwitch || subnet.Spec.GatewayType != kubeovnv1.GWDistributedType {
				continue
			}
			for _, node := range nodes {
				npNames = append(npNames, fmt.Sprintf("%s/%s", subnet.Name, node.Name))
			}
		}
	}

	pgs, err := c.ovnLegacyClient.ListNpPortGroup()
	if err != nil {
		klog.Errorf("failed to list port-group, %v", err)
		return err
	}
	for _, pg := range pgs {
		if !c.config.EnableNP || !util.IsStringIn(fmt.Sprintf("%s/%s", pg.NpNamespace, pg.NpName), npNames) {
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
	klog.Infof("start to gc static routes")
	routes, err := c.ovnLegacyClient.GetStaticRouteList(util.DefaultVpc)
	if err != nil {
		klog.Errorf("failed to list static route %v", err)
		return err
	}
	for _, route := range routes {
		if route.CIDR != "0.0.0.0/0" && route.CIDR != "::/0" && c.ipam.ContainAddress(route.CIDR) {
			klog.Infof("gc static route %s %s %s", route.Policy, route.CIDR, route.NextHop)
			exist, err := c.ovnLegacyClient.NatRuleExists(route.CIDR)
			if exist || err != nil {
				continue
			}
			if err := c.ovnLegacyClient.DeleteStaticRoute(route.CIDR, c.config.ClusterRouter); err != nil {
				klog.Errorf("failed to delete stale route %s, %v", route.NextHop, err)
			}
		}
	}
	return nil
}

func (c *Controller) isOVNProvided(providerName string, pod *corev1.Pod) (bool, error) {
	ls := pod.Annotations[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, providerName)]
	subnet, err := c.config.KubeOvnClient.KubeovnV1().Subnets().Get(context.Background(), ls, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("parse annotation logical switch %s error %v", ls, err)
		return false, err
	}
	if !strings.HasSuffix(subnet.Spec.Provider, util.OvnProvider) {
		return false, nil
	}
	return true, nil
}

func (c *Controller) getVmLsps() []string {
	var vmLsps []string

	if !c.config.EnableKeepVmIP {
		return vmLsps
	}

	nss, err := c.namespacesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list namespaces, %v", err)
		return vmLsps
	}

	for _, ns := range nss {
		vms, err := c.config.KubevirtClient.VirtualMachine(ns.Name).List(&metav1.ListOptions{})
		if err != nil {
			if !k8serrors.IsNotFound(err) {
				klog.Errorf("failed to list vm in namespace %s, %v", ns, err)
			}
			continue
		} else {
			for _, vm := range vms.Items {
				vmLsp := ovs.PodNameToPortName(vm.Name, ns.Name, util.OvnProvider)
				vmLsps = append(vmLsps, vmLsp)

				attachNets, err := util.ParsePodNetworkAnnotation(vm.Spec.Template.ObjectMeta.Annotations[util.AttachmentNetworkAnnotation], vm.Namespace)
				if err != nil {
					klog.Errorf("failed to get attachment subnet of vm %s, %v", vm.Name, err)
					continue
				}
				for _, multiNet := range attachNets {
					provider := fmt.Sprintf("%s.%s.ovn", multiNet.Name, multiNet.Namespace)
					vmLsp := ovs.PodNameToPortName(vm.Name, ns.Name, provider)
					vmLsps = append(vmLsps, vmLsp)
				}
			}
		}
	}

	return vmLsps
}
