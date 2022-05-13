package daemon

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"syscall"

	"github.com/alauda/felix/ipsets"
	"github.com/coreos/go-iptables/iptables"
	"github.com/vishvananda/netlink"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

// ControllerRuntime represents runtime specific controller members
type ControllerRuntime struct {
	iptables map[string]*iptables.IPTables
	ipsets   map[string]*ipsets.IPSets
}

func (c *Controller) initRuntime() error {
	c.ControllerRuntime.iptables = make(map[string]*iptables.IPTables)
	c.ControllerRuntime.ipsets = make(map[string]*ipsets.IPSets)

	if c.protocol == kubeovnv1.ProtocolIPv4 || c.protocol == kubeovnv1.ProtocolDual {
		iptables, err := iptables.NewWithProtocol(iptables.ProtocolIPv4)
		if err != nil {
			return err
		}
		c.ControllerRuntime.iptables[kubeovnv1.ProtocolIPv4] = iptables
		c.ControllerRuntime.ipsets[kubeovnv1.ProtocolIPv4] = ipsets.NewIPSets(ipsets.NewIPVersionConfig(ipsets.IPFamilyV4, IPSetPrefix, nil, nil))
	}
	if c.protocol == kubeovnv1.ProtocolIPv6 || c.protocol == kubeovnv1.ProtocolDual {
		iptables, err := iptables.NewWithProtocol(iptables.ProtocolIPv6)
		if err != nil {
			return err
		}
		c.ControllerRuntime.iptables[kubeovnv1.ProtocolIPv6] = iptables
		c.ControllerRuntime.ipsets[kubeovnv1.ProtocolIPv6] = ipsets.NewIPSets(ipsets.NewIPVersionConfig(ipsets.IPFamilyV6, IPSetPrefix, nil, nil))
	}

	return nil
}

func (c *Controller) reconcileRouters(event subnetEvent) error {
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets %v", err)
		return err
	}

	var ok bool
	var oldSubnet, newSubnet *kubeovnv1.Subnet
	if event.old != nil {
		if oldSubnet, ok = event.old.(*kubeovnv1.Subnet); !ok {
			klog.Errorf("expected old subnet in subnetEvent but got %#v", event.old)
			return nil
		}
	}
	if event.new != nil {
		if newSubnet, ok = event.new.(*kubeovnv1.Subnet); !ok {
			klog.Errorf("expected new subnet in subnetEvent but got %#v", event.new)
			return nil
		}
	}

	// handle policy routing
	rulesToAdd, rulesToDel, routesToAdd, routesToDel, err := c.diffPolicyRouting(oldSubnet, newSubnet)
	if err != nil {
		klog.Errorf("failed to get policy routing difference: %v", err)
		return err
	}
	// add new routes first
	for _, r := range routesToAdd {
		if err = netlink.RouteAdd(&r); err != nil && !errors.Is(err, syscall.EEXIST) {
			klog.Errorf("failed to add route for subnet %s: %v", newSubnet.Name, err)
			return err
		}
	}
	// next, add new rules
	for _, r := range rulesToAdd {
		if err = netlink.RuleAdd(&r); err != nil && !errors.Is(err, syscall.EEXIST) {
			klog.Errorf("failed to add network rule for subnet %s: %v", newSubnet.Name, err)
			return err
		}
	}
	// then delete old network rules
	for _, r := range rulesToDel {
		// loop to delete all matched rules
		for {
			if err = netlink.RuleDel(&r); err != nil {
				if !errors.Is(err, syscall.ENOENT) {
					klog.Errorf("failed to delete network rule for subnet %s: %v", oldSubnet.Name, err)
					return err
				}
				break
			}
		}
	}
	// last, delete old network routes
	for _, r := range routesToDel {
		if err = netlink.RouteDel(&r); err != nil && !errors.Is(err, syscall.ENOENT) {
			klog.Errorf("failed to delete route for subnet %s: %v", oldSubnet.Name, err)
			return err
		}
	}

	cidrs := make([]string, 0, len(subnets)*2)
	for _, subnet := range subnets {
		if subnet.Spec.Vlan != "" || subnet.Spec.Vpc != util.DefaultVpc || !subnet.Status.IsReady() {
			continue
		}

		for _, cidrBlock := range strings.Split(subnet.Spec.CIDRBlock, ",") {
			if _, ipNet, err := net.ParseCIDR(cidrBlock); err != nil {
				klog.Errorf("%s is not a valid cidr block", cidrBlock)
			} else {
				cidrs = append(cidrs, ipNet.String())
			}
		}
	}

	node, err := c.nodesLister.Get(c.config.NodeName)
	if err != nil {
		klog.Errorf("failed to get node %s %v", c.config.NodeName, err)
		return err
	}
	gateway, ok := node.Annotations[util.GatewayAnnotation]
	if !ok {
		klog.Errorf("annotation for node %s ovn.kubernetes.io/gateway not exists", node.Name)
		return fmt.Errorf("annotation for node ovn.kubernetes.io/gateway not exists")
	}
	nic, err := netlink.LinkByName(util.NodeNic)
	if err != nil {
		klog.Errorf("failed to get nic %s", util.NodeNic)
		return fmt.Errorf("failed to get nic %s", util.NodeNic)
	}

	existRoutes, err := getNicExistRoutes(nic, gateway)
	if err != nil {
		return err
	}

	toAdd, toDel := routeDiff(existRoutes, cidrs)
	for _, r := range toDel {
		_, cidr, _ := net.ParseCIDR(r)
		if err = netlink.RouteDel(&netlink.Route{Dst: cidr}); err != nil {
			klog.Errorf("failed to del route %v", err)
		}
	}

	for _, r := range toAdd {
		_, cidr, _ := net.ParseCIDR(r)
		for _, gw := range strings.Split(gateway, ",") {
			if util.CheckProtocol(gw) != util.CheckProtocol(r) {
				continue
			}
			if err = netlink.RouteReplace(&netlink.Route{Dst: cidr, LinkIndex: nic.Attrs().Index, Scope: netlink.SCOPE_UNIVERSE, Gw: net.ParseIP(gw)}); err != nil {
				klog.Errorf("failed to add route %v", err)
			}
		}
	}

	if oldSubnet != nil && newSubnet != nil && oldSubnet.Spec.HtbQos != "" && newSubnet.Spec.HtbQos == "" {
		if err := c.deleteSubnetQos(newSubnet); err != nil {
			klog.Errorf("failed to delete htb qos for subnet %s: %v", newSubnet.Name, err)
			return err
		}
	} else if newSubnet != nil && newSubnet.Spec.HtbQos != "" {
		if err := c.setSubnetQosPriority(newSubnet); err != nil {
			klog.Errorf("failed to set htb qos priority for subnet %s: %v", newSubnet.Name, err)
			return err
		}
	}
	return nil
}

func getNicExistRoutes(nic netlink.Link, gateway string) ([]netlink.Route, error) {
	var routes, existRoutes []netlink.Route
	var err error
	for _, gw := range strings.Split(gateway, ",") {
		if util.CheckProtocol(gw) == kubeovnv1.ProtocolIPv4 {
			routes, err = netlink.RouteList(nic, netlink.FAMILY_V4)
		} else {
			routes, err = netlink.RouteList(nic, netlink.FAMILY_V6)
		}
		if err != nil {
			return nil, err
		}
		existRoutes = append(existRoutes, routes...)
	}
	return existRoutes, nil
}

func routeDiff(existRoutes []netlink.Route, cidrs []string) (toAdd []string, toDel []string) {
	for _, route := range existRoutes {
		if route.Scope == netlink.SCOPE_LINK {
			continue
		}

		found := false
		for _, c := range cidrs {
			if route.Dst.String() == c {
				found = true
				break
			}
		}
		if !found {
			toDel = append(toDel, route.Dst.String())
		}
	}
	if len(toDel) > 0 {
		klog.Infof("route to del %v", toDel)
	}

	for _, c := range cidrs {
		found := false
		for _, r := range existRoutes {
			if r.Dst.String() == c {
				found = true
				break
			}
		}
		if !found {
			toAdd = append(toAdd, c)
		}
	}
	if len(toAdd) > 0 {
		klog.Infof("route to add %v", toAdd)
	}
	return
}

func getRulesToAdd(oldRules, newRules []netlink.Rule) []netlink.Rule {
	var toAdd []netlink.Rule

	for _, rule := range newRules {
		var found bool
		for _, r := range oldRules {
			if r.Family == rule.Family && r.Priority == rule.Priority && r.Table == rule.Table && reflect.DeepEqual(r.Src, rule.Src) {
				found = true
				break
			}
		}
		if !found {
			toAdd = append(toAdd, rule)
		}
	}

	return toAdd
}

func getRoutesToAdd(oldRoutes, newRoutes []netlink.Route) []netlink.Route {
	var toAdd []netlink.Route

	for _, route := range newRoutes {
		var found bool
		for _, r := range oldRoutes {
			if r.Equal(route) {
				found = true
				break
			}
		}
		if !found {
			toAdd = append(toAdd, route)
		}
	}

	return toAdd
}

func (c *Controller) diffPolicyRouting(oldSubnet, newSubnet *kubeovnv1.Subnet) (rulesToAdd, rulesToDel []netlink.Rule, routesToAdd, routesToDel []netlink.Route, err error) {
	oldRules, oldRoutes, err := c.getPolicyRouting(oldSubnet)
	if err != nil {
		klog.Error(err)
		return
	}
	newRules, newRoutes, err := c.getPolicyRouting(newSubnet)
	if err != nil {
		klog.Error(err)
		return
	}

	rulesToAdd = getRulesToAdd(oldRules, newRules)
	rulesToDel = getRulesToAdd(newRules, oldRules)
	routesToAdd = getRoutesToAdd(oldRoutes, newRoutes)
	routesToDel = getRoutesToAdd(newRoutes, oldRoutes)

	return
}

func (c *Controller) getPolicyRouting(subnet *kubeovnv1.Subnet) ([]netlink.Rule, []netlink.Route, error) {
	if subnet == nil || subnet.Spec.ExternalEgressGateway == "" || subnet.Spec.Vpc != util.DefaultVpc {
		return nil, nil, nil
	}
	if subnet.Spec.GatewayType == kubeovnv1.GWCentralizedType && !util.GatewayContains(subnet.Spec.GatewayNode, c.config.NodeName) {
		return nil, nil, nil
	}

	protocols := make([]string, 1, 2)
	if protocol := util.CheckProtocol(subnet.Spec.ExternalEgressGateway); protocol == kubeovnv1.ProtocolDual {
		protocols[0] = kubeovnv1.ProtocolIPv4
		protocols = append(protocols, kubeovnv1.ProtocolIPv6)
	} else {
		protocols[0] = protocol
	}

	cidr := strings.Split(subnet.Spec.CIDRBlock, ",")
	egw := strings.Split(subnet.Spec.ExternalEgressGateway, ",")

	// rules
	var rules []netlink.Rule
	rule := netlink.NewRule()
	rule.Table = int(subnet.Spec.PolicyRoutingTableID)
	rule.Priority = int(subnet.Spec.PolicyRoutingPriority)
	if subnet.Spec.GatewayType == kubeovnv1.GWDistributedType {
		pods, err := c.podsLister.List(labels.Everything())
		if err != nil {
			klog.Errorf("list pods failed, %+v", err)
			return nil, nil, err
		}

		hostname := os.Getenv(util.HostnameEnv)
		for _, pod := range pods {
			if pod.Spec.HostNetwork ||
				pod.Status.PodIP == "" ||
				pod.Annotations[util.LogicalSwitchAnnotation] != subnet.Name ||
				pod.Spec.NodeName != hostname {
				continue
			}

			for i := range protocols {
				rule.Family, _ = util.ProtocolToFamily(protocols[i])

				var ip net.IP
				var maskBits int
				if len(pod.Status.PodIPs) == 2 && protocols[i] == kubeovnv1.ProtocolIPv6 {
					ip = net.ParseIP(pod.Status.PodIPs[1].IP)
					maskBits = 128
				} else if util.CheckProtocol(pod.Status.PodIP) == protocols[i] {
					ip = net.ParseIP(pod.Status.PodIP)
					maskBits = 32
					if rule.Family == netlink.FAMILY_V6 {
						maskBits = 128
					}
				}

				rule.Src = &net.IPNet{IP: ip, Mask: net.CIDRMask(maskBits, maskBits)}
				rules = append(rules, *rule)
			}
		}
	} else {
		for i := range protocols {
			rule.Family, _ = util.ProtocolToFamily(protocols[i])
			if len(cidr) == len(protocols) {
				_, rule.Src, _ = net.ParseCIDR(cidr[i])
			}
			rules = append(rules, *rule)
		}
	}

	// routes
	var routes []netlink.Route
	for i := range protocols {
		family, _ := util.ProtocolToFamily(protocols[i])
		routes = append(routes, netlink.Route{
			Protocol: netlink.RouteProtocol(family),
			Table:    int(subnet.Spec.PolicyRoutingTableID),
			Gw:       net.ParseIP(egw[i]),
		})
	}

	return rules, routes, nil
}
func (c *Controller) handlePod(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}
	klog.Infof("handle qos update for pod %s/%s", namespace, name)

	pod, err := c.podsLister.Pods(namespace).Get(name)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	if err := util.ValidatePodNetwork(pod.Annotations); err != nil {
		klog.Errorf("validate pod %s/%s failed, %v", namespace, name, err)
		c.recorder.Eventf(pod, v1.EventTypeWarning, "ValidatePodNetworkFailed", err.Error())
		return err
	}

	podName := pod.Name
	if pod.Annotations[fmt.Sprintf(util.VmTemplate, util.OvnProvider)] != "" {
		podName = pod.Annotations[fmt.Sprintf(util.VmTemplate, util.OvnProvider)]
	}

	priority := pod.Annotations[util.PriorityAnnotation]
	subnetName := pod.Annotations[util.LogicalSwitchAnnotation]
	subnetPriority := c.getSubnetQosPriority(subnetName)
	if priority == "" && subnetPriority != "" {
		priority = subnetPriority
	}

	// set default nic bandwidth
	ifaceID := ovs.PodNameToPortName(podName, pod.Namespace, util.OvnProvider)
	err = ovs.SetInterfaceBandwidth(podName, pod.Namespace, ifaceID, pod.Annotations[util.EgressRateAnnotation], pod.Annotations[util.IngressRateAnnotation], priority)
	if err != nil {
		return err
	}
	err = ovs.ConfigInterfaceMirror(c.config.EnableMirror, pod.Annotations[util.MirrorControlAnnotation], ifaceID)
	if err != nil {
		return err
	}
	// set linux-netem qos
	err = ovs.SetNetemQos(podName, pod.Namespace, ifaceID, pod.Annotations[util.NetemQosLatencyAnnotation], pod.Annotations[util.NetemQosLimitAnnotation], pod.Annotations[util.NetemQosLossAnnotation])
	if err != nil {
		return err
	}

	// set multus-nic bandwidth
	attachNets, err := util.ParsePodNetworkAnnotation(pod.Annotations[util.AttachmentNetworkAnnotation], pod.Namespace)
	if err != nil {
		return err
	}
	for _, multiNet := range attachNets {
		provider := fmt.Sprintf("%s.%s.ovn", multiNet.Name, multiNet.Namespace)
		if pod.Annotations[fmt.Sprintf(util.VmTemplate, provider)] != "" {
			podName = pod.Annotations[fmt.Sprintf(util.VmTemplate, provider)]
		}
		if pod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, provider)] == "true" {
			ifaceID = ovs.PodNameToPortName(podName, pod.Namespace, provider)
			priority := pod.Annotations[fmt.Sprintf(util.PriorityAnnotationTemplate, provider)]
			subnetName := pod.Annotations[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, provider)]
			subnetPriority := c.getSubnetQosPriority(subnetName)
			if priority == "" && subnetPriority != "" {
				priority = subnetPriority
			}

			err = ovs.SetInterfaceBandwidth(podName, pod.Namespace, ifaceID, pod.Annotations[fmt.Sprintf(util.EgressRateAnnotationTemplate, provider)], pod.Annotations[fmt.Sprintf(util.IngressRateAnnotationTemplate, provider)], priority)
			if err != nil {
				return err
			}
			err = ovs.ConfigInterfaceMirror(c.config.EnableMirror, pod.Annotations[fmt.Sprintf(util.MirrorControlAnnotationTemplate, provider)], ifaceID)
			if err != nil {
				return err
			}
			err = ovs.SetNetemQos(podName, pod.Namespace, ifaceID, pod.Annotations[fmt.Sprintf(util.NetemQosLatencyAnnotationTemplate, provider)], pod.Annotations[fmt.Sprintf(util.NetemQosLimitAnnotationTemplate, provider)], pod.Annotations[fmt.Sprintf(util.NetemQosLossAnnotationTemplate, provider)])
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Controller) loopEncapIpCheck() {
	node, err := c.nodesLister.Get(c.config.NodeName)
	if err != nil {
		klog.Errorf("failed to get node %s %v", c.config.NodeName, err)
		return
	}

	if nodeTunnelName := node.GetAnnotations()[util.TunnelInterfaceAnnotation]; nodeTunnelName != "" {
		iface, err := findInterface(nodeTunnelName)
		if err != nil {
			klog.Errorf("failed to find iface %s, %v", nodeTunnelName, err)
			return
		}
		if iface.Flags&net.FlagUp == 0 {
			klog.Errorf("iface %v is down", nodeTunnelName)
			return
		}
		addrs, err := iface.Addrs()
		if err != nil {
			klog.Errorf("failed to get iface addr. %v", err)
			return
		}
		if len(addrs) == 0 {
			klog.Errorf("iface %s has no ip address", nodeTunnelName)
			return
		}

		// if assigned iface in node annotation is down or with no ip, the error msg should be printed periodically
		if c.config.Iface == nodeTunnelName {
			klog.V(3).Infof("node tunnel interface %s not changed", nodeTunnelName)
			return
		}
		c.config.Iface = nodeTunnelName
		klog.Infof("Update node tunnel interface %v", nodeTunnelName)

		encapIP := strings.Split(addrs[0].String(), "/")[0]
		if err = setEncapIP(encapIP); err != nil {
			klog.Errorf("failed to set encap ip %s for iface %s", encapIP, c.config.Iface)
			return
		}
	}
}

func (c *Controller) setSubnetQosPriority(subnet *kubeovnv1.Subnet) error {
	htbQos, err := c.htbQosLister.Get(subnet.Spec.HtbQos)
	if err != nil {
		klog.Errorf("failed to get htbqos %s: %v", subnet.Spec.HtbQos, err)
		return err
	}

	pods, err := c.podsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list pods, %v", err)
		return err
	}

	qosIfaceUidMap, err := ovs.ListExternalIds("qos")
	if err != nil {
		return err
	}
	queueIfaceUidMap, err := ovs.ListExternalIds("queue")
	if err != nil {
		return err
	}

	for _, pod := range pods {
		if pod.Spec.HostNetwork ||
			pod.DeletionTimestamp != nil ||
			pod.Status.PodIP == "" {
			continue
		}
		podName := pod.Name
		if pod.Annotations[fmt.Sprintf(util.VmTemplate, util.OvnProvider)] != "" {
			podName = pod.Annotations[fmt.Sprintf(util.VmTemplate, util.OvnProvider)]
		}

		// set priority for eth0 interface in pod
		if pod.Annotations[util.LogicalSwitchAnnotation] == subnet.Name {
			ifaceID := ovs.PodNameToPortName(podName, pod.Namespace, util.OvnProvider)
			priority := htbQos.Spec.Priority
			if pod.Annotations[util.PriorityAnnotation] != "" {
				priority = pod.Annotations[util.PriorityAnnotation]
			}
			if err = ovs.SetPodQosPriority(podName, pod.Namespace, ifaceID, priority, qosIfaceUidMap, queueIfaceUidMap); err != nil {
				klog.Errorf("failed to set htbqos priority for pod %s/%s, iface %v: %v", pod.Namespace, pod.Name, ifaceID, err)
				return err
			}
		}

		// set priority for attach interface provided by kube-ovn in pod
		attachNets, err := util.ParsePodNetworkAnnotation(pod.Annotations[util.AttachmentNetworkAnnotation], pod.Namespace)
		if err != nil {
			return err
		}
		for _, multiNet := range attachNets {
			provider := fmt.Sprintf("%s.%s.ovn", multiNet.Name, multiNet.Namespace)
			if subnet.Spec.Provider != provider {
				continue
			}
			if pod.Annotations[fmt.Sprintf(util.VmTemplate, provider)] != "" {
				podName = pod.Annotations[fmt.Sprintf(util.VmTemplate, provider)]
			}

			if pod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, provider)] == "true" {
				ifaceID := ovs.PodNameToPortName(podName, pod.Namespace, provider)
				priority := htbQos.Spec.Priority
				if pod.Annotations[fmt.Sprintf(util.PriorityAnnotationTemplate, provider)] != "" {
					priority = pod.Annotations[fmt.Sprintf(util.PriorityAnnotationTemplate, provider)]
				}

				if err = ovs.SetPodQosPriority(podName, pod.Namespace, ifaceID, priority, qosIfaceUidMap, queueIfaceUidMap); err != nil {
					klog.Errorf("failed to set htbqos priority for pod %s/%s, iface %v: %v", pod.Namespace, pod.Name, ifaceID, err)
					return err
				}
			}
		}
	}
	return nil
}

func (c *Controller) clearQos(podName, podNamespace, ifaceID string) error {
	if htbQos, _ := ovs.IsHtbQos(ifaceID); !htbQos {
		return nil
	}

	if err := ovs.ClearPortQosBinding(ifaceID); err != nil {
		klog.Errorf("failed to delete qos bingding info for interface %s: %v", ifaceID, err)
		return err
	}

	if err := ovs.ClearPodBandwidth(podName, podNamespace, ifaceID); err != nil {
		klog.Errorf("failed to delete htbqos record for pod %s/%s, iface %v: %v", podNamespace, podName, ifaceID, err)
		return err
	}

	if err := ovs.ClearHtbQosQueue(podName, podNamespace, ifaceID); err != nil {
		klog.Errorf("failed to delete htbqos queue for pod %s/%s, iface %v: %v", podNamespace, podName, ifaceID, err)
		return err
	}
	return nil
}
func (c *Controller) operateMod() {
	modules, ok := os.LookupEnv(util.KoENV)
	if !ok || modules == "" {
		err := removeAllMods(util.KoDir)
		if err != nil {
			klog.Errorf("remove all module in %s failed", util.KoDir)
		}
		return
	}
	for _, module := range strings.Split(modules, ",") {
		isFileExist, _ := isFile(module, util.KoDir)
		if !isFileExist && isMod(module) {
			err := removeKo(module)
			if err != nil {
				klog.Errorf("remove module %s failed %v", module, err)
			}
		} else if !isMod(module) && isFileExist {
			err := insertKo(module)
			if err != nil {
				klog.Errorf("insert module %s failed: %v", module, err)
			}
			klog.Infof("insert module %s", module)
		}
	}
}

func rotateLog() {
	output, err := exec.Command("logrotate", "/etc/logrotate.d/openvswitch").CombinedOutput()
	if err != nil {
		klog.Errorf("failed to rotate openvswitch log %q", output)
	}
	output, err = exec.Command("logrotate", "/etc/logrotate.d/ovn").CombinedOutput()
	if err != nil {
		klog.Errorf("failed to rotate ovn log %q", output)
	}
	output, err = exec.Command("logrotate", "/etc/logrotate.d/kubeovn").CombinedOutput()
	if err != nil {
		klog.Errorf("failed to rotate kube-ovn log %q", output)
	}
}

func isMod(modName string) bool {
	out, err := exec.Command("lsmod").CombinedOutput()
	if err != nil {
		klog.Errorf("list module %s failed: %v", modName, err)
	}
	names := strings.Split(modName, ".")
	return strings.Contains(string(out), names[0])
}

func insertKo(koName string) error {
	file := util.KoDir + koName
	out, err := exec.Command("insmod", file).CombinedOutput()
	if err != nil {
		return fmt.Errorf("insert module %s failed: %v", koName, err)
	}
	if string(out) != "" {
		return fmt.Errorf("insert module %s failed: %v", koName, string(out))
	}
	return nil
}

func removeAllMods(dir string) error {
	kos, err := readKos(dir)
	if err != nil {
		return fmt.Errorf("access kos in %s failed: %v", dir, err)
	}
	for _, ko := range *kos {
		err := removeKo(ko)
		if err != nil {
			return fmt.Errorf("remove module %s failed: %v", ko, err)
		}
	}
	return nil
}

func removeKo(koName string) error {
	out, err := exec.Command("rmmod", koName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("remove module %s failed: %v", koName, err)
	}
	if string(out) != "" {
		return fmt.Errorf("remove module %s faied: %v", koName, string(out))
	}
	return nil
}

func readKos(dir string) (*[]string, error) {
	kos := new([]string)
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			klog.Errorf("failed to access path %q: %v", path, err)
		}
		if d.IsDir() {
			return nil
		}
		isMatch, _ := regexp.MatchString(".[.]ko$", d.Name())
		if isMatch {
			*kos = append(*kos, d.Name())
		}
		return nil
	})
	if err != nil {
		return kos, fmt.Errorf("error when walking the path %q: %v", dir, err)
	}
	return kos, nil
}

func isFile(filename string, dir string) (bool, string) {
	isFile := false
	fileFullName := ""
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			klog.Errorf("failed to access path %q: %v", path, err)
		}
		if d.IsDir() {
			return nil
		}
		if strings.Contains(d.Name(), filename) {
			isFile = true
			fileFullName = filename
		}
		return nil
	})
	if err != nil {
		klog.Errorf("error when walking the path %q: %v", dir, err)
	}
	return isFile, fileFullName
}

func recompute() {
	output, err := exec.Command("ovn-appctl", "-t", "ovn-controller", "recompute").CombinedOutput()
	if err != nil {
		klog.Errorf("failed to recompute ovn-controller %q", output)
	}
}
