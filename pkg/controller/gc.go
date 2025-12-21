package controller

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	nadutils "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/utils"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	"github.com/scylladb/go-set/strset"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"k8s.io/utils/set"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

var lastNoPodLSP = strset.New()

func (c *Controller) gc() error {
	if c.config.GCInterval == 0 {
		klog.Infof("gc is disabled")
		return nil
	}
	gcFunctions := []func() error{
		c.gcNode,
		c.gcChassis,
		c.gcLogicalSwitch,
		c.gcCustomLogicalRouter,
		// The lsp gc is processed periodically by markAndCleanLSP, will not gc lsp when init
		c.gcLoadBalancer,
		c.gcNetworkPolicy,
		c.gcSecurityGroup,
		c.gcAddressSet,
		c.gcRoutePolicy,
		c.gcStaticRoute,
		c.gcVpcNatGateway,
		c.gcLogicalRouterPort,
		c.gcIP,
		c.gcVip,
		c.gcLbSvcPods,
		c.gcVPCDNS,
	}
	for _, gcFunc := range gcFunctions {
		if err := gcFunc(); err != nil {
			klog.Error(err)
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

	exceptPeerPorts := strset.New()
	for _, vpc := range vpcs {
		for _, peer := range vpc.Status.VpcPeerings {
			exceptPeerPorts.Add(fmt.Sprintf("%s-%s", vpc.Name, peer))
		}
	}

	if err = c.OVNNbClient.DeleteLogicalRouterPorts(
		map[string]string{"vendor": util.CniTypeName},
		logicalRouterPortFilter(exceptPeerPorts)); err != nil {
		klog.Errorf("delete non-existent peer logical router port: %v", err)
		return err
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

	var gwStsNames []string
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
		gwStsNames = append(gwStsNames, util.GenNatGwName(gw.Name))
	}

	stss, err := c.config.KubeClient.AppsV1().StatefulSets(c.config.PodNamespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: labels.Set{util.VpcNatGatewayLabel: "true"}.AsSelector().String(),
	})
	if err != nil {
		klog.Errorf("failed to list vpc nat gateway statefulset, %v", err)
		return err
	}
	for _, sts := range stss.Items {
		if !slices.Contains(gwStsNames, sts.Name) {
			err = c.config.KubeClient.AppsV1().StatefulSets(c.config.PodNamespace).Delete(context.Background(), sts.Name, metav1.DeleteOptions{})
			if err != nil {
				klog.Errorf("failed to delete vpc nat gateway statefulset, %v", err)
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

	lss, err := c.OVNNbClient.ListLogicalSwitchNames(c.config.EnableExternalVpc, nil)
	if err != nil {
		klog.Errorf("failed to list logical switch: %v", err)
		return err
	}

	subnetNames := set.New[string]()
	subnetMap := make(map[string]*kubeovnv1.Subnet, len(subnets))
	for _, s := range subnets {
		subnetMap[s.Name] = s
		subnetNames.Insert(s.Name)
	}

	klog.Infof("logical switch in ovn: %v", lss)
	klog.Infof("subnet in kubernetes: %v", subnetNames)
	for _, ls := range lss {
		if ls == util.InterconnectionSwitch ||
			ls == util.ExternalGatewaySwitch ||
			ls == c.config.ExternalGatewaySwitch {
			continue
		}
		if s := subnetMap[ls]; s != nil && isOvnSubnet(s) {
			continue
		}

		klog.Infof("gc logical switch %s", ls)
		if err = c.handleDeleteLogicalSwitch(ls); err != nil {
			klog.Errorf("failed to gc logical switch %q: %v", ls, err)
			return err
		}
	}

	klog.Infof("start to gc dhcp options")
	dhcpOptions, err := c.OVNNbClient.ListDHCPOptions(c.config.EnableExternalVpc, nil)
	if err != nil {
		klog.Errorf("failed to list dhcp options, %v", err)
		return err
	}
	uuidToDeleteList := []string{}
	for _, item := range dhcpOptions {
		if len(item.ExternalIDs) == 0 || !subnetNames.Has(item.ExternalIDs["ls"]) {
			uuidToDeleteList = append(uuidToDeleteList, item.UUID)
		}
	}
	klog.Infof("gc dhcp options %v", uuidToDeleteList)
	if len(uuidToDeleteList) > 0 {
		if err = c.OVNNbClient.DeleteDHCPOptionsByUUIDs(uuidToDeleteList...); err != nil {
			klog.Errorf("failed to delete dhcp options by uuids, %v", err)
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

	lrs, err := c.OVNNbClient.ListLogicalRouterNames(c.config.EnableExternalVpc, nil)
	if err != nil {
		klog.Errorf("failed to list logical router, %v", err)
		return err
	}

	vpcNames := set.New[string]()
	for _, v := range vpcs {
		vpcNames.Insert(v.Name)
	}

	klog.Infof("lr in ovn: %v", lrs)
	klog.Infof("vpc in kubernetes: %v", vpcNames)

	for _, lr := range lrs {
		if lr == c.config.ClusterRouter || vpcNames.Has(lr) {
			continue
		}

		klog.Infof("gc logical router %s", lr)
		if err = c.deleteVpcRouter(lr); err != nil {
			klog.Errorf("failed to gc logical router %q: %v", lr, err)
			return err
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
	nodeNames := strset.NewWithSize(len(nodes))
	for _, node := range nodes {
		nodeNames.Add(node.Name)
	}
	ips, err := c.ipsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list ip, %v", err)
		return err
	}

	for _, ip := range ips {
		if strings.HasPrefix(ip.Name, util.NodeLspPrefix) && !strings.Contains(ip.Name, ".") {
			if node := ip.Name[len(util.NodeLspPrefix):]; !nodeNames.Has(node) {
				klog.Infof("gc node %s", node)
				if err := c.deleteNode(node); err != nil {
					klog.Errorf("failed to gc node %s: %v", node, err)
					return err
				}
			}
		}
	}

	policies, err := c.OVNNbClient.ListLogicalRouterPolicies(c.config.ClusterRouter, util.NodeRouterPolicyPriority, map[string]string{"vendor": util.CniTypeName}, false)
	if err != nil {
		klog.Errorf("failed to list logical router policies on lr %s: %v", c.config.ClusterRouter, err)
		return err
	}
	gatewayRouterPolicies, err := c.OVNNbClient.ListLogicalRouterPolicies(c.config.ClusterRouter, util.GatewayRouterPolicyPriority, map[string]string{"vendor": util.CniTypeName}, false)
	if err != nil {
		klog.Errorf("failed to list logical router policies priority %d on lr %s: %v", util.GatewayRouterPolicyPriority, c.config.ClusterRouter, err)
		return err
	}
	policies = append(policies, gatewayRouterPolicies...)
	for _, policy := range policies {
		// skip the policy for centralized subnet
		if _, ok := policy.ExternalIDs["node"]; !ok {
			continue
		}
		if nodeNames.Has(policy.ExternalIDs["node"]) {
			continue
		}
		klog.Infof("gc logical router policy %q priority %d on lr %s", policy.Match, policy.Priority, c.config.ClusterRouter)
		if err = c.OVNNbClient.DeleteLogicalRouterPolicy(c.config.ClusterRouter, policy.Priority, policy.Match); err != nil {
			klog.Errorf("failed to delete logical router policy %q on lr %s", policy.Match, c.config.ClusterRouter)
			return err
		}
	}

	return nil
}

func (c *Controller) gcVip() error {
	klog.Infof("start to gc vips")
	selector, err := util.LabelSelectorNotEmpty(util.IPReservedLabel)
	if err != nil {
		klog.Errorf("failed to generate selector for label %s: %v", util.IPReservedLabel, err)
		return err
	}
	vips, err := c.virtualIpsLister.List(selector)
	if err != nil {
		klog.Errorf("failed to list vips: %v", err)
		return err
	}
	for _, vip := range vips {
		portName := vip.Labels[util.IPReservedLabel]
		portNameSplits := strings.Split(portName, ".")
		if len(portNameSplits) >= 2 {
			podName := portNameSplits[0]
			namespace := portNameSplits[1]
			_, err := c.podsLister.Pods(namespace).Get(podName)
			if err != nil {
				if k8serrors.IsNotFound(err) {
					if err = c.releaseVip(vip.Name); err != nil {
						klog.Errorf("failed to clean label from vip %s, %v", vip.Name, err)
						return err
					}
					return nil
				}
				klog.Error(err)
				return err
			}
		}
	}
	return nil
}

func (c *Controller) checkIPOwnerExists(ip *kubeovnv1.IP) (bool, error) {
	// Check if Subnet exists
	if _, ok := c.ipam.Subnets[ip.Spec.Subnet]; !ok {
		return false, nil
	}

	// Check if Node exists
	if ip.Spec.Namespace == "" && ip.Spec.NodeName == ip.Spec.PodName {
		_, err := c.nodesLister.Get(ip.Spec.NodeName)
		if err != nil && k8serrors.IsNotFound(err) {
			return false, nil
		}
		return true, err
	}

	// Check if VM exists
	if ip.Spec.PodType == util.KindVirtualMachine {
		_, err := c.config.KubevirtClient.VirtualMachine(ip.Spec.Namespace).Get(context.Background(), ip.Spec.PodName, metav1.GetOptions{})
		if err != nil && k8serrors.IsNotFound(err) {
			return false, nil
		}
		return true, err
	}

	// Check if StatefulSet exists
	if ip.Spec.PodType == util.KindStatefulSet {
		// Extract StatefulSet name from pod name by removing the last part after '-'
		// e.g., "vpc-nat-gw-rg-f6d4e7973976430-default-sto-1-0" -> "vpc-nat-gw-rg-f6d4e7973976430-default-sto-1"
		stsName := ip.Spec.PodName
		if lastDash := strings.LastIndex(stsName, "-"); lastDash != -1 {
			stsName = stsName[:lastDash]
		}

		_, err := c.config.KubeClient.AppsV1().StatefulSets(ip.Spec.Namespace).Get(context.Background(), stsName, metav1.GetOptions{})
		if err != nil && k8serrors.IsNotFound(err) {
			return false, nil
		}
		return true, err
	}

	// Check if Normal Pod exists
	if ip.Spec.PodType == "" {
		_, err := c.podsLister.Pods(ip.Spec.Namespace).Get(ip.Spec.PodName)
		if err != nil && k8serrors.IsNotFound(err) {
			return false, nil
		}
		return true, err
	}

	return true, nil
}

func (c *Controller) gcIP() error {
	klog.Infof("start to gc ips")
	ips, err := c.ipsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list ip, %v", err)
		return err
	}
	for _, ip := range ips {
		exist, err := c.checkIPOwnerExists(ip)
		if err != nil {
			klog.Errorf("failed to check ip owner exists, %v", err)
			continue
		}
		if !exist {
			klog.Infof("gc ip %s", ip.Name)
			if err := c.config.KubeOvnClient.KubeovnV1().IPs().Delete(context.Background(), ip.Name, metav1.DeleteOptions{}); err != nil {
				klog.Errorf("failed to gc ip %s, %v", ip.Name, err)
			}
		}
	}
	return nil
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
	ipMap := strset.NewWithSize(len(pods) + len(nodes))
	for _, pod := range pods {
		if isStsPod, stsName, stsUID := isStatefulSetPod(pod); isStsPod {
			if isStatefulSetPodToGC(c.config.KubeClient, pod, stsName, stsUID) {
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
			ipMap.Add(ovs.PodNameToPortName(podName, pod.Namespace, providerName))
		}
	}
	for _, node := range nodes {
		if node.Annotations[util.AllocatedAnnotation] == "true" {
			ipMap.Add(util.NodeLspName(node.Name))
		}

		if _, err := c.ovnEipsLister.Get(node.Name); err == nil {
			// node external gw lsp is managed by ovn eip cr, skip gc its lsp
			ipMap.Add(node.Name)
		}
	}

	// The lsp for vm pod should not be deleted if vm still exists
	ipMap.Add(c.getVMLsps()...)

	vips, err := c.virtualIpsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list virtual ip, %v", err)
		return err
	}
	vipsMap := strset.NewWithSize(len(vips))
	for _, vip := range vips {
		if vip.Spec.Type != "" {
			subnetName := vip.Spec.Subnet
			if subnetName == "" {
				klog.Errorf("failed to gc vip '%s', subnet should be set", vip.Name)
				continue
			}

			subnet, err := c.subnetsLister.Get(subnetName)
			if err != nil {
				klog.Errorf("failed to get subnet %s: %v", subnetName, err)
				continue
			}

			portName := ovs.PodNameToPortName(vip.Name, vip.Spec.Namespace, subnet.Spec.Provider)
			vipsMap.Add(portName)
		}
	}
	lsps, err := c.OVNNbClient.ListNormalLogicalSwitchPorts(c.config.EnableExternalVpc, nil)
	if err != nil {
		klog.Errorf("failed to list logical switch port, %v", err)
		return err
	}

	noPodLSP := strset.New()
	lspMap := strset.NewWithSize(len(lsps))
	for _, lsp := range lsps {
		lspMap.Add(lsp.Name)
		if ipMap.Has(lsp.Name) {
			continue
		}
		if vipsMap.Has(lsp.Name) {
			// skip gc lsp for k8s host network vm pod or switch lb rule
			continue
		}
		if !lastNoPodLSP.Has(lsp.Name) {
			noPodLSP.Add(lsp.Name)
			continue
		}

		klog.Infof("gc logical switch port %s with uuid %s", lsp.Name, lsp.UUID)
		if err := c.OVNNbClient.DeleteLogicalSwitchPortByUUID(lsp.ExternalIDs[ovs.LogicalSwitchKey], lsp.UUID); err != nil {
			klog.Errorf("failed to delete lsp %s: %v", lsp.Name, err)
			return err
		}
		ipCR, err := c.config.KubeOvnClient.KubeovnV1().IPs().Get(context.Background(), lsp.Name, metav1.GetOptions{})
		if err != nil {
			if k8serrors.IsNotFound(err) {
				// ip cr not found, skip lsp gc
				continue
			}
			klog.Errorf("failed to get ip %s, %v", lsp.Name, err)
			return err
		}
		if ipCR.Labels[util.IPReservedLabel] != "true" {
			klog.Infof("gc ip %s", ipCR.Name)
			if err := c.config.KubeOvnClient.KubeovnV1().IPs().Delete(context.Background(), ipCR.Name, metav1.DeleteOptions{}); err != nil {
				if k8serrors.IsNotFound(err) {
					// ip cr not found, skip lsp gc
					continue
				}
				klog.Errorf("failed to delete ip %s, %v", ipCR.Name, err)
				return err
			}
			if ipCR.Spec.Subnet == "" {
				klog.Errorf("ip %s has no subnet", ipCR.Name)
				// ip cr no subnet, skip lsp gc
				continue
			}
			if key := lsp.ExternalIDs["pod"]; key != "" {
				c.ipam.ReleaseAddressByPod(key, ipCR.Spec.Subnet)
			}
		} else {
			klog.Infof("gc skip reserved ip %s", ipCR.Name)
		}
	}
	lastNoPodLSP = noPodLSP

	ipMap.Each(func(ipName string) bool {
		if !lspMap.Has(ipName) {
			klog.Errorf("lsp lost for pod %s, please delete the pod and retry", ipName)
		}
		return true
	})

	return nil
}

func (c *Controller) gcLoadBalancer() error {
	klog.Infof("start to gc load balancers")

	dnats, err := c.ovnDnatRulesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list dnats, %v", err)
		return err
	}

	vpcLbs := strset.NewWithSize(len(dnats))
	for _, dnat := range dnats {
		vpcLbs.Add(dnat.Name)
	}

	if !c.config.EnableLb {
		// remove lb from logical switch
		vpcs, err := c.vpcsLister.List(labels.Everything())
		if err != nil {
			klog.Error(err)
			return err
		}
		for _, cachedVpc := range vpcs {
			vpc := cachedVpc.DeepCopy()
			for _, subnetName := range vpc.Status.Subnets {
				subnet, err := c.subnetsLister.Get(subnetName)
				if err != nil {
					if k8serrors.IsNotFound(err) {
						continue
					}
					klog.Error(err)
					return err
				}
				if !isOvnSubnet(subnet) {
					continue
				}
				lbs := []string{vpc.Status.TCPLoadBalancer, vpc.Status.TCPSessionLoadBalancer, vpc.Status.UDPLoadBalancer, vpc.Status.UDPSessionLoadBalancer, vpc.Status.SctpLoadBalancer, vpc.Status.SctpSessionLoadBalancer}
				if err := c.OVNNbClient.LogicalSwitchUpdateLoadBalancers(subnetName, ovsdb.MutateOperationDelete, lbs...); err != nil {
					klog.Error(err)
					return err
				}
			}

			vpc.Status.TCPLoadBalancer = ""
			vpc.Status.TCPSessionLoadBalancer = ""
			vpc.Status.UDPLoadBalancer = ""
			vpc.Status.UDPSessionLoadBalancer = ""
			vpc.Status.SctpLoadBalancer = ""
			vpc.Status.SctpSessionLoadBalancer = ""
			bytes, err := vpc.Status.Bytes()
			if err != nil {
				klog.Error(err)
				return err
			}
			_, err = c.config.KubeOvnClient.KubeovnV1().Vpcs().Patch(context.Background(), vpc.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status")
			if err != nil {
				klog.Error(err)
				return err
			}
		}
		// lbs will remove from logical switch automatically when delete lbs
		// Only delete load balancers that belong to kube-ovn (vendor=kube-ovn)
		// This prevents deleting load balancers managed by external systems like OpenStack Neutron
		if err = c.OVNNbClient.DeleteLoadBalancers(func(lb *ovnnb.LoadBalancer) bool {
			if lb.ExternalIDs["vendor"] != util.CniTypeName {
				return false
			}
			return !vpcLbs.Has(lb.Name)
		}); err != nil {
			klog.Errorf("delete all load balancers: %v", err)
			return err
		}
		return nil
	}

	svcs, err := c.servicesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list svc, %v", err)
		return err
	}

	var (
		tcpVips         = strset.NewWithSize(len(svcs) * 2)
		udpVips         = strset.NewWithSize(len(svcs) * 2)
		sctpVips        = strset.NewWithSize(len(svcs) * 2)
		tcpSessionVips  = strset.NewWithSize(len(svcs) * 2)
		udpSessionVips  = strset.NewWithSize(len(svcs) * 2)
		sctpSessionVips = strset.NewWithSize(len(svcs) * 2)
	)

	for _, svc := range svcs {
		for _, ip := range getVipIps(svc) {
			for _, port := range svc.Spec.Ports {
				vip := util.JoinHostPort(ip, port.Port)
				switch port.Protocol {
				case corev1.ProtocolTCP:
					if svc.Spec.SessionAffinity == corev1.ServiceAffinityClientIP {
						tcpSessionVips.Add(vip)
					} else {
						tcpVips.Add(vip)
					}
				case corev1.ProtocolUDP:
					if svc.Spec.SessionAffinity == corev1.ServiceAffinityClientIP {
						udpSessionVips.Add(vip)
					} else {
						udpVips.Add(vip)
					}
				case corev1.ProtocolSCTP:
					if svc.Spec.SessionAffinity == corev1.ServiceAffinityClientIP {
						sctpSessionVips.Add(vip)
					} else {
						sctpVips.Add(vip)
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

	var (
		removeVip         func(lbName string, svcVips *strset.Set) error
		ignoreHealthCheck = true
	)

	removeVip = func(lbName string, svcVips *strset.Set) error {
		if lbName == "" {
			return nil
		}

		var (
			lb  *ovnnb.LoadBalancer
			err error
		)

		if lb, err = c.OVNNbClient.GetLoadBalancer(lbName, true); err != nil {
			klog.Errorf("get LB %s: %v", lbName, err)
			return err
		}

		if lb == nil {
			klog.Infof("load balancer %q already deleted", lbName)
			return nil
		}

		for vip := range lb.Vips {
			if !svcVips.Has(vip) {
				if err = c.OVNNbClient.LoadBalancerDeleteVip(lbName, vip, ignoreHealthCheck); err != nil {
					klog.Errorf("failed to delete vip %s from LB %s: %v", vip, lbName, err)
					return err
				}
			}
		}
		return nil
	}

	for _, vpc := range vpcs {
		var (
			tcpLb, udpLb, sctpLb             = vpc.Status.TCPLoadBalancer, vpc.Status.UDPLoadBalancer, vpc.Status.SctpLoadBalancer
			tcpSessLb, udpSessLb, sctpSessLb = vpc.Status.TCPSessionLoadBalancer, vpc.Status.UDPSessionLoadBalancer, vpc.Status.SctpSessionLoadBalancer
		)

		vpcLbs.Add(tcpLb, udpLb, sctpLb, tcpSessLb, udpSessLb, sctpSessLb)
		if err = removeVip(tcpLb, tcpVips); err != nil {
			klog.Error(err)
			return err
		}
		if err = removeVip(tcpSessLb, tcpSessionVips); err != nil {
			klog.Error(err)
			return err
		}
		if err = removeVip(udpLb, udpVips); err != nil {
			klog.Error(err)
			return err
		}
		if err = removeVip(udpSessLb, udpSessionVips); err != nil {
			klog.Error(err)
			return err
		}
		if err = removeVip(sctpLb, sctpVips); err != nil {
			klog.Error(err)
			return err
		}
		if err = removeVip(sctpSessLb, sctpSessionVips); err != nil {
			klog.Error(err)
			return err
		}
	}

	// delete lbs
	if err = c.OVNNbClient.DeleteLoadBalancers(
		func(lb *ovnnb.LoadBalancer) bool {
			return !vpcLbs.Has(lb.Name)
		},
	); err != nil {
		klog.Errorf("delete load balancers: %v", err)
		return err
	}
	return nil
}

func (c *Controller) gcAddressSet() error {
	klog.Infof("start to gc address set")
	// Only list address sets that belong to kube-ovn (vendor=kube-ovn)
	// This prevents deleting address sets managed by external systems like OpenStack Neutron
	addressSets, err := c.OVNNbClient.ListAddressSets(map[string]string{"vendor": util.CniTypeName})
	if err != nil {
		klog.Errorf("failed to list address set,%v", err)
		return err
	}

	asList := make([]string, 0)
	for _, as := range addressSets {
		sg := as.ExternalIDs[sgKey]
		if sg == "" {
			continue
		}
		// if address set not found associated port group, delete it
		if pg, err := c.OVNNbClient.GetPortGroup(ovs.GetSgPortGroupName(sg), true); err == nil && pg == nil {
			klog.Infof("ready to gc address set %s", as.Name)
			asList = append(asList, as.Name)
		}
	}
	if len(asList) == 0 {
		return nil
	}

	if err = c.OVNNbClient.DeleteAddressSet(asList...); err != nil {
		klog.Errorf("failed to delete address set %v: %v", asList, err)
		return err
	}

	return nil
}

func (c *Controller) gcSecurityGroup() error {
	klog.Infof("start to gc security group residual port groups")
	// get security group
	sgs, err := c.config.KubeOvnClient.KubeovnV1().SecurityGroups().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		klog.Errorf("failed to list security group,%v", err)
		return err
	}
	sgSet := strset.NewWithSize(len(sgs.Items))
	for _, sg := range sgs.Items {
		sgSet.Add(sg.Name)
	}

	// Only list port groups that belong to kube-ovn (vendor=kube-ovn)
	// This prevents deleting port groups managed by external systems like OpenStack Neutron
	pgs, err := c.OVNNbClient.ListPortGroups(map[string]string{"vendor": util.CniTypeName})
	if err != nil {
		klog.Errorf("failed to list port group,%v", err)
		return err
	}

	needToDelPgs := make([]string, 0)
	denyAllPg := ovs.GetSgPortGroupName(util.DenyAllSecurityGroup)
	defaultPg := ovs.GetSgPortGroupName(util.DefaultSecurityGroupName)
	for _, pg := range pgs {
		if pg.Name == denyAllPg || pg.Name == defaultPg || pg.ExternalIDs[networkPolicyKey] != "" {
			continue
		}
		// if port group not exist in security group, delete it
		if !sgSet.Has(pg.ExternalIDs["sg"]) {
			klog.Infof("ready to gc port group %s", pg.Name)
			needToDelPgs = append(needToDelPgs, pg.Name)
		}
	}
	if len(needToDelPgs) == 0 {
		return nil
	}
	if err = c.OVNNbClient.DeletePortGroup(needToDelPgs...); err != nil {
		klog.Errorf("failed to gc port group list,%v", err)
		return err
	}
	return nil
}

func (c *Controller) gcNetworkPolicy() error {
	klog.Infof("start to gc network policy")

	npNames := strset.New()
	delPgNames := strset.New()

	if c.config.EnableNP {
		nps, err := c.npsLister.List(labels.Everything())
		if err != nil {
			klog.Errorf("failed to list network policy, %v", err)
			return err
		}

		for _, np := range nps {
			npName := np.Name
			nameArray := []rune(np.Name)
			if !unicode.IsLetter(nameArray[0]) {
				npName = "np" + np.Name
			}

			npNames.Add(fmt.Sprintf("%s/%s", np.Namespace, npName))
		}
	}

	// append node port group to npNames to avoid gc node port group
	nodes, err := c.nodesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list nodes, %v", err)
		return err
	}

	for _, node := range nodes {
		npNames.Add(fmt.Sprintf("%s/%s", "node", node.Name))
	}

	// append overlay subnets port group to npNames to avoid gc distributed subnets port group
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets %v", err)
		return err
	}
	for _, subnet := range subnets {
		if subnet.Spec.Vpc != c.config.ClusterRouter || (subnet.Spec.Vlan != "" && !subnet.Spec.LogicalGateway) || subnet.Name == c.config.NodeSwitch || subnet.Spec.GatewayType != kubeovnv1.GWDistributedType {
			continue
		}

		for _, node := range nodes {
			npNames.Add(fmt.Sprintf("%s/%s", subnet.Name, node.Name))
		}
	}

	// list all np port groups which externalIDs[np]!=""
	pgs, err := c.OVNNbClient.ListPortGroups(map[string]string{networkPolicyKey: ""})
	if err != nil {
		klog.Errorf("list np port group: %v", err)
		return err
	}

	for _, pg := range pgs {
		np := strings.Split(pg.ExternalIDs[networkPolicyKey], "/")
		if len(np) != 2 {
			// not np port group
			continue
		}
		if !npNames.Has(pg.ExternalIDs[networkPolicyKey]) {
			klog.Infof("gc port group '%s' network policy '%s'", pg.Name, pg.ExternalIDs[networkPolicyKey])
			delPgNames.Add(pg.Name)
			if c.config.EnableNP {
				c.deleteNpQueue.Add(pg.ExternalIDs[networkPolicyKey])
			}
		}
	}
	// gc port group
	// the pgName in the network policy is generated differently from the node/subnet pgName
	// so processes port group gc separately
	// ensure that the port group can be correctly gc
	if err := c.OVNNbClient.DeletePortGroup(delPgNames.List()...); err != nil {
		klog.Errorf("failed to gc port group %v: %v", delPgNames.List(), err)
		return err
	}

	return nil
}

func (c *Controller) gcRoutePolicy() error {
	klog.Infof("start to gc route policy")

	policies, err := c.OVNNbClient.ListLogicalRouterPolicies(c.config.ClusterRouter, util.NorthGatewayRoutePolicyPriority, nil, true)
	if err != nil {
		klog.Errorf("failed to list route policy, %v", err)
		return err
	}

	podIPs := []string{}
	pods, err := c.podsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list pods, %v", err)
		return err
	}
	for _, pod := range pods {
		if pod.Annotations != nil && pod.Annotations[util.NorthGatewayAnnotation] != "" {
			podIPs = append(podIPs, strings.Split(pod.Annotations[util.IPAddressAnnotation], ",")...)
		}
	}

	for _, policy := range policies {
		parts := strings.Split(policy.Match, "==")
		if len(parts) != 2 {
			continue
		}
		srcIP := strings.TrimSpace(parts[1])
		if !slices.Contains(podIPs, srcIP) {
			klog.Infof("gc route policy %s", policy.Match)
			if err := c.OVNNbClient.DeleteLogicalRouterPolicy(c.config.ClusterRouter, policy.Priority, policy.Match); err != nil {
				klog.Errorf("failed to delete route policy %s: %v", policy.Match, err)
				return err
			}
		}
	}

	return nil
}

func (c *Controller) gcStaticRoute() error {
	klog.Infof("start to gc static routes")
	routes, err := c.OVNNbClient.ListLogicalRouterStaticRoutes(c.config.ClusterRouter, nil, nil, "", nil)
	if err != nil {
		klog.Errorf("failed to list static route %v", err)
		return err
	}
	defaultVpc, err := c.vpcsLister.Get(c.config.ClusterRouter)
	if err != nil {
		klog.Errorf("failed to get default vpc, %v", err)
		return err
	}
	var keepStaticRoute bool
	for _, route := range routes {
		keepStaticRoute = false
		for _, item := range defaultVpc.Spec.StaticRoutes {
			if route.IPPrefix == item.CIDR && route.Nexthop == item.NextHopIP && route.RouteTable == item.RouteTable {
				keepStaticRoute = true
				break
			}
		}
		if keepStaticRoute {
			continue
		}
		if route.IPPrefix != "0.0.0.0/0" && route.IPPrefix != "::/0" && c.ipam.ContainAddress(route.IPPrefix) {
			exist, err := c.OVNNbClient.NatExists(c.config.ClusterRouter, "", "", route.IPPrefix)
			if err != nil {
				klog.Errorf("failed to get NatRule by LogicalIP %s, %v", route.IPPrefix, err)
				continue
			}
			if exist {
				continue
			}
			klog.Infof("gc static route %s %v %s %s", route.RouteTable, route.Policy, route.IPPrefix, route.Nexthop)
			if err = c.deleteStaticRouteFromVpc(
				c.config.ClusterRouter,
				route.RouteTable,
				route.IPPrefix,
				route.Nexthop,
				reversePolicy(*route.Policy),
			); err != nil {
				klog.Errorf("failed to delete stale route %s %v %s %s: %v", route.RouteTable, route.Policy, route.IPPrefix, route.Nexthop, err)
			}
		}
	}
	return nil
}

func (c *Controller) gcChassis() error {
	klog.Infof("start to gc chassis")
	chassises, err := c.OVNSbClient.ListChassis()
	if err != nil {
		klog.Errorf("failed to get all chassis, %v", err)
	}
	chassisNodes := make(map[string]string, len(*chassises))
	for _, chassis := range *chassises {
		chassisNodes[chassis.Name] = chassis.Hostname
	}
	nodes, err := c.nodesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list nodes, %v", err)
		return err
	}
	for _, node := range nodes {
		chassisName := node.Annotations[util.ChassisAnnotation]
		if chassisName == "" {
			// kube-ovn-cni not ready to set chassis annotation
			continue
		}
		if hostname, exist := chassisNodes[chassisName]; exist {
			if hostname == node.Name {
				// node is alive, matched chassis should be alive
				delete(chassisNodes, chassisName)
				continue
			}
			// maybe node name changed, delete chassis
			klog.Infof("gc node %s chassis %s", node.Name, chassisName)
			if err := c.OVNSbClient.DeleteChassis(chassisName); err != nil {
				klog.Errorf("failed to delete node %s chassis %s %v", node.Name, chassisName, err)
				return err
			}
		}
	}

	for chassisName, hostname := range chassisNodes {
		klog.Infof("gc node %s chassis %s", hostname, chassisName)
		if err := c.OVNSbClient.DeleteChassis(chassisName); err != nil {
			klog.Errorf("failed to delete node %s chassis %s %v", hostname, chassisName, err)
			return err
		}
	}

	return nil
}

func (c *Controller) isOVNProvided(providerName string, pod *corev1.Pod) (bool, error) {
	if ls, ok := pod.Annotations[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, providerName)]; ok {
		subnet, err := c.subnetsLister.Get(ls)
		if err != nil {
			klog.Errorf("parse annotation logical switch %s error %v", ls, err)
			return false, err
		}
		if !isOvnSubnet(subnet) {
			return false, nil
		}
		return true, nil
	}
	return false, nil
}

func (c *Controller) getVMLsps() []string {
	var vmLsps []string

	if !c.config.EnableKeepVMIP {
		return vmLsps
	}

	nss, err := c.namespacesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list namespaces, %v", err)
		return vmLsps
	}

	for _, ns := range nss {
		vms, err := c.config.KubevirtClient.VirtualMachine(ns.Name).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			if !k8serrors.IsNotFound(err) {
				klog.Errorf("failed to list vm in namespace %s, %v", ns, err)
			}
			continue
		}
		for _, vm := range vms.Items {
			defaultMultus := false
			for _, network := range vm.Spec.Template.Spec.Networks {
				if network.Multus != nil && network.Multus.Default {
					defaultMultus = true
					break
				}
			}
			if !defaultMultus {
				vmLsp := ovs.PodNameToPortName(vm.Name, ns.Name, util.OvnProvider)
				vmLsps = append(vmLsps, vmLsp)
			}

			nadAnnotation := vm.Spec.Template.ObjectMeta.Annotations[nadv1.NetworkAttachmentAnnot]
			if nadAnnotation != "" {
				attachNets, err := nadutils.ParseNetworkAnnotation(nadAnnotation, vm.Namespace)
				if err != nil {
					klog.Errorf("failed to get attachment subnet of vm %s, %v", vm.Name, err)
					continue
				}
				for _, multiNet := range attachNets {
					provider := fmt.Sprintf("%s.%s.%s", multiNet.Name, multiNet.Namespace, util.OvnProvider)
					vmLsp := ovs.PodNameToPortName(vm.Name, ns.Name, provider)
					vmLsps = append(vmLsps, vmLsp)
				}
			}

			for _, network := range vm.Spec.Template.Spec.Networks {
				if network.Multus != nil && network.Multus.NetworkName != "" {
					items := strings.Split(network.Multus.NetworkName, "/")
					if len(items) != 2 {
						items = []string{vm.GetNamespace(), items[0]}
					}
					provider := fmt.Sprintf("%s.%s.%s", items[1], items[0], util.OvnProvider)
					vmLsp := ovs.PodNameToPortName(vm.Name, ns.Name, provider)
					vmLsps = append(vmLsps, vmLsp)
				}
			}
		}
	}

	return vmLsps
}

func (c *Controller) gcLbSvcPods() error {
	klog.Infof("start to gc lb svc pods")
	nss, err := c.namespacesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list namespaces, %v", err)
		return err
	}

	for _, ns := range nss {
		dps, err := c.config.KubeClient.AppsV1().Deployments(ns.Name).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			if !k8serrors.IsNotFound(err) {
				klog.Errorf("failed to list lb svc deployment in namespace %s, %v", ns.Name, err)
			}
			continue
		}

		for _, dp := range dps.Items {
			if !strings.HasPrefix(dp.Name, "lb-svc-") {
				continue
			}
			if _, ok := dp.Spec.Template.Labels["service"]; !ok {
				continue
			}

			svcName := strings.TrimPrefix(dp.Name, "lb-svc-")
			_, err := c.servicesLister.Services(ns.Name).Get(svcName)
			if err != nil && k8serrors.IsNotFound(err) {
				klog.Infof("gc lb svc deployment %s in ns %s", dp.Name, ns.Name)
				if err := c.config.KubeClient.AppsV1().Deployments(ns.Name).Delete(context.Background(), dp.Name, metav1.DeleteOptions{}); err != nil {
					if !k8serrors.IsNotFound(err) {
						klog.Errorf("failed to delete lb svc deployment in namespace %s, %v", ns.Name, err)
					}
				}
			}
		}
	}
	return nil
}

func (c *Controller) gcVPCDNS() error {
	if !c.config.EnableLb {
		return nil
	}

	klog.Infof("start to gc vpc dns")
	vds, err := c.vpcDNSLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list vpc-dns, %v", err)
		return err
	}

	labelSelector := labels.Set{util.VpcDNSNameLabel: "true"}.AsSelector()
	deps, err := c.config.KubeClient.AppsV1().Deployments(c.config.PodNamespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: labelSelector.String(),
	})
	if err != nil {
		klog.Errorf("failed to list vpc-dns deployment, %s", err)
		return err
	}

	for _, dep := range deps.Items {
		canFind := false
		for _, vd := range vds {
			name := genVpcDNSDpName(vd.Name)
			if dep.Name == name {
				canFind = true
				break
			}
		}
		if !canFind {
			err := c.config.KubeClient.AppsV1().Deployments(c.config.PodNamespace).Delete(context.Background(),
				dep.Name, metav1.DeleteOptions{})
			if err != nil {
				klog.Errorf("failed to delete vpc-dns deployment, %s", err)
				return err
			}
		}
	}

	slrs, err := c.switchLBRuleLister.List(labelSelector)
	if err != nil {
		klog.Errorf("failed to list vpc-dns SwitchLBRules, %s", err)
		return err
	}

	for _, slr := range slrs {
		canFind := false
		for _, vd := range vds {
			name := genVpcDNSDpName(vd.Name)
			if slr.Name == name {
				canFind = true
				break
			}
		}
		if !canFind {
			err := c.config.KubeOvnClient.KubeovnV1().SwitchLBRules().Delete(context.Background(),
				slr.Name, metav1.DeleteOptions{})
			if err != nil {
				klog.Errorf("failed to delete vpc-dns SwitchLBRule, %s", err)
				return err
			}
		}
	}
	return nil
}

func logicalRouterPortFilter(exceptPeerPorts *strset.Set) func(lrp *ovnnb.LogicalRouterPort) bool {
	return func(lrp *ovnnb.LogicalRouterPort) bool {
		// Only delete logical router ports that belong to kube-ovn (vendor=kube-ovn)
		// This prevents deleting LRPs managed by external systems like OpenStack Neutron
		if lrp.ExternalIDs["vendor"] != util.CniTypeName {
			return false
		}

		if exceptPeerPorts.Has(lrp.Name) {
			return false // ignore except lrp
		}

		return lrp.Peer != nil && len(*lrp.Peer) != 0
	}
}
