package daemon

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"syscall"

	ovsutil "github.com/digitalocean/go-openvswitch/ovs"
	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	nadutils "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/utils"
	"github.com/kubeovn/felix/ipsets"
	"github.com/kubeovn/go-iptables/iptables"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	k8sipset "k8s.io/kubernetes/pkg/proxy/ipvs/ipset"
	k8siptables "k8s.io/kubernetes/pkg/util/iptables"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	kernelModuleIPTables  = "ip_tables"
	kernelModuleIP6Tables = "ip6_tables"
)

// ControllerRuntime represents runtime specific controller members
type ControllerRuntime struct {
	iptables         map[string]*iptables.IPTables
	iptablesObsolete map[string]*iptables.IPTables
	k8siptables      map[string]k8siptables.Interface
	k8sipsets        k8sipset.Interface
	ipsets           map[string]*ipsets.IPSets
	gwCounters       map[string]*util.GwIPtableCounters

	nmSyncer  *networkManagerSyncer
	ovsClient *ovsutil.Client

	flowCache      map[string]map[string][]string // key: bridgeName -> flowKey -> flow rules
	flowCacheMutex sync.RWMutex
	flowChan       chan struct{} // channel to trigger immediate flow sync
}

type LbServiceRules struct {
	IP          string
	Port        uint16
	Protocol    string
	BridgeName  string
	DstMac      string
	UnderlayNic string
	SubnetName  string
}

func evalCommandSymlinks(cmd string) (string, error) {
	path, err := exec.LookPath(cmd)
	if err != nil {
		return "", fmt.Errorf("failed to search for command %q: %w", cmd, err)
	}
	file, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("failed to read evaluate symbolic links for file %q: %w", path, err)
	}

	return file, nil
}

func isLegacyIptablesMode() (bool, error) {
	path, err := evalCommandSymlinks("iptables")
	if err != nil {
		return false, err
	}
	pathLegacy, err := evalCommandSymlinks("iptables-legacy")
	if err != nil {
		return false, err
	}
	return path == pathLegacy, nil
}

func (c *Controller) initRuntime() error {
	ok, err := isLegacyIptablesMode()
	if err != nil {
		klog.Errorf("failed to check iptables mode: %v", err)
		return err
	}
	if !ok {
		// iptables works in nft mode, we should migrate iptables rules
		c.iptablesObsolete = make(map[string]*iptables.IPTables, 2)
	}

	c.iptables = make(map[string]*iptables.IPTables)
	c.ipsets = make(map[string]*ipsets.IPSets)
	c.gwCounters = make(map[string]*util.GwIPtableCounters)
	c.k8siptables = make(map[string]k8siptables.Interface)
	c.k8sipsets = k8sipset.New()
	c.ovsClient = ovsutil.New()

	// Initialize OpenFlow flow cache (ovn-kubernetes style)
	c.flowCache = make(map[string]map[string][]string)
	c.flowChan = make(chan struct{}, 1)

	if c.protocol == kubeovnv1.ProtocolIPv4 || c.protocol == kubeovnv1.ProtocolDual {
		ipt, err := iptables.NewWithProtocol(iptables.ProtocolIPv4)
		if err != nil {
			klog.Error(err)
			return err
		}
		c.iptables[kubeovnv1.ProtocolIPv4] = ipt
		if c.iptablesObsolete != nil {
			ok, err := kernelModuleLoaded(kernelModuleIPTables)
			if err != nil {
				klog.Errorf("failed to check kernel module %s: %v", kernelModuleIPTables, err)
			}
			if ok {
				if ipt, err = iptables.NewWithProtocolAndMode(iptables.ProtocolIPv4, "legacy"); err != nil {
					klog.Error(err)
					return err
				}
				c.iptablesObsolete[kubeovnv1.ProtocolIPv4] = ipt
			}
		}
		c.ipsets[kubeovnv1.ProtocolIPv4] = ipsets.NewIPSets(ipsets.NewIPVersionConfig(ipsets.IPFamilyV4, IPSetPrefix, nil, nil))
		c.k8siptables[kubeovnv1.ProtocolIPv4] = k8siptables.New(k8siptables.ProtocolIPv4)
	}
	if c.protocol == kubeovnv1.ProtocolIPv6 || c.protocol == kubeovnv1.ProtocolDual {
		ipt, err := iptables.NewWithProtocol(iptables.ProtocolIPv6)
		if err != nil {
			klog.Error(err)
			return err
		}
		c.iptables[kubeovnv1.ProtocolIPv6] = ipt
		if c.iptablesObsolete != nil {
			ok, err := kernelModuleLoaded(kernelModuleIP6Tables)
			if err != nil {
				klog.Errorf("failed to check kernel module %s: %v", kernelModuleIP6Tables, err)
			}
			if ok {
				if ipt, err = iptables.NewWithProtocolAndMode(iptables.ProtocolIPv6, "legacy"); err != nil {
					klog.Error(err)
					return err
				}
				c.iptablesObsolete[kubeovnv1.ProtocolIPv6] = ipt
			}
		}
		c.ipsets[kubeovnv1.ProtocolIPv6] = ipsets.NewIPSets(ipsets.NewIPVersionConfig(ipsets.IPFamilyV6, IPSetPrefix, nil, nil))
		c.k8siptables[kubeovnv1.ProtocolIPv6] = k8siptables.New(k8siptables.ProtocolIPv6)
	}

	if err = ovs.ClearU2OFlows(c.ovsClient); err != nil {
		util.LogFatalAndExit(err, "failed to clear obsolete u2o flows")
	}

	c.nmSyncer = newNetworkManagerSyncer()
	c.nmSyncer.Run(c.transferAddrsAndRoutes)

	return nil
}

func (c *Controller) handleEnableExternalLBAddressChange(oldSubnet, newSubnet *kubeovnv1.Subnet) error {
	var subnetName string
	var action string

	switch {
	case oldSubnet != nil && newSubnet != nil:
		subnetName = oldSubnet.Name
		if oldSubnet.Spec.EnableExternalLBAddress != newSubnet.Spec.EnableExternalLBAddress {
			klog.Infof("EnableExternalLBAddress changed for subnet %s", newSubnet.Name)
			if newSubnet.Spec.EnableExternalLBAddress {
				action = "add"
			} else {
				action = "remove"
			}
		}
	case oldSubnet != nil:
		subnetName = oldSubnet.Name
		if oldSubnet.Spec.EnableExternalLBAddress {
			klog.Infof("EnableExternalLBAddress removed for subnet %s", oldSubnet.Name)
			action = "remove"
		}
	case newSubnet != nil:
		subnetName = newSubnet.Name
		if newSubnet.Spec.EnableExternalLBAddress {
			klog.Infof("EnableExternalLBAddress added for subnet %s", newSubnet.Name)
			action = "add"
		}
	}

	if action != "" {
		services, err := c.servicesLister.List(labels.Everything())
		if err != nil {
			klog.Errorf("failed to list services: %v", err)
			return err
		}

		for _, svc := range services {
			if svc.Annotations[util.ServiceExternalIPFromSubnetAnnotation] == subnetName {
				klog.Infof("Service %s/%s has external LB address pool annotation from subnet %s, action: %s", svc.Namespace, svc.Name, subnetName, action)
				switch action {
				case "add":
					c.serviceQueue.Add(&serviceEvent{newObj: svc})
				case "remove":
					c.serviceQueue.Add(&serviceEvent{oldObj: svc})
				}
			}
		}
	}
	return nil
}

// handleU2OInterconnectionMACChange handles U2O interconnection MAC address changes.
// When U2O (Underlay to Overlay) interconnection is enabled, the svc local flow's destination
// MAC must point to the LRP (Logical Router Port) MAC. Otherwise, without U2O enabled (no LRP exists),
// the flow would hit the rules created by build_lswitch_dnat_mod_dl_dst_rules instead.
func (c *Controller) handleU2OInterconnectionMACChange(oldSubnet, newSubnet *kubeovnv1.Subnet) error {
	if oldSubnet == nil || newSubnet == nil {
		return nil
	}

	oldMAC := oldSubnet.Status.U2OInterconnectionMAC
	newMAC := newSubnet.Status.U2OInterconnectionMAC

	if oldMAC == newMAC {
		return nil
	}

	if newMAC == "" && oldMAC == "" {
		return nil
	}

	klog.Infof("U2OInterconnectionMAC changed for subnet %s: %s -> %s",
		oldSubnet.Name, oldMAC, newMAC)

	// Find all services using this subnet and re-sync them
	services, err := c.servicesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list services: %v", err)
		return err
	}

	for _, svc := range services {
		if svc.Annotations[util.ServiceExternalIPFromSubnetAnnotation] == oldSubnet.Name {
			klog.Infof("Re-syncing service %s/%s due to U2OInterconnectionMAC change in subnet %s",
				svc.Namespace, svc.Name, oldSubnet.Name)
			c.serviceQueue.Add(&serviceEvent{newObj: svc})
		}
	}
	return nil
}

func (c *Controller) reconcileRouters(event *subnetEvent) error {
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets %v", err)
		return err
	}

	if event != nil {
		var ok bool
		var oldSubnet, newSubnet *kubeovnv1.Subnet
		if event.oldObj != nil {
			if oldSubnet, ok = event.oldObj.(*kubeovnv1.Subnet); !ok {
				klog.Errorf("expected old subnet in subnetEvent but got %#v", event.oldObj)
				return nil
			}
		}
		if event.newObj != nil {
			if newSubnet, ok = event.newObj.(*kubeovnv1.Subnet); !ok {
				klog.Errorf("expected new subnet in subnetEvent but got %#v", event.newObj)
				return nil
			}
		}

		if err = c.handleEnableExternalLBAddressChange(oldSubnet, newSubnet); err != nil {
			klog.Errorf("failed to handle enable external lb address change: %v", err)
			return err
		}

		if err = c.handleU2OInterconnectionMACChange(oldSubnet, newSubnet); err != nil {
			klog.Errorf("failed to handle u2o interconnection mac change: %v", err)
			return err
		}
		// handle policy routing
		rulesToAdd, rulesToDel, routesToAdd, routesToDel, err := c.diffPolicyRouting(oldSubnet, newSubnet)
		if err != nil {
			klog.Errorf("failed to get policy routing difference: %v", err)
			return err
		}
		// add new routes first
		for _, r := range routesToAdd {
			if err = netlink.RouteReplace(&r); err != nil && !errors.Is(err, syscall.EEXIST) {
				klog.Errorf("failed to replace route for subnet %s: %v", newSubnet.Name, err)
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
	}

	node, err := c.nodesLister.Get(c.config.NodeName)
	if err != nil {
		klog.Errorf("failed to get node %s %v", c.config.NodeName, err)
		return err
	}
	nodeIPv4, nodeIPv6 := util.GetNodeInternalIP(*node)
	var joinIPv4, joinIPv6 string
	if len(node.Annotations) != 0 {
		joinIPv4, joinIPv6 = util.SplitStringIP(node.Annotations[util.IPAddressAnnotation])
	}

	joinCIDR := make([]string, 0, 2)
	cidrs := make([]string, 0, len(subnets)*2)
	for _, subnet := range subnets {
		// The route for overlay subnet cidr via ovn0 should not be deleted even though subnet.Status has changed to not ready
		if subnet.Spec.Vpc != c.config.ClusterRouter ||
			(subnet.Spec.Vlan != "" && !subnet.Spec.LogicalGateway && (!subnet.Spec.U2OInterconnection || (subnet.Spec.EnableLb != nil && *subnet.Spec.EnableLb))) ||
			!subnet.Status.IsValidated() {
			continue
		}

		for cidrBlock := range strings.SplitSeq(subnet.Spec.CIDRBlock, ",") {
			if _, ipNet, err := net.ParseCIDR(cidrBlock); err != nil {
				klog.Errorf("%s is not a valid cidr block", cidrBlock)
			} else {
				if nodeIPv4 != "" && util.CIDRContainIP(cidrBlock, nodeIPv4) {
					continue
				}
				if nodeIPv6 != "" && util.CIDRContainIP(cidrBlock, nodeIPv6) {
					continue
				}
				cidrs = append(cidrs, ipNet.String())
				if subnet.Name == c.config.NodeSwitch {
					joinCIDR = append(joinCIDR, ipNet.String())
				}
			}
		}
	}

	gateway, ok := node.Annotations[util.GatewayAnnotation]
	if !ok {
		err = fmt.Errorf("gateway annotation for node %s does not exist", node.Name)
		klog.Error(err)
		return err
	}
	nic, err := netlink.LinkByName(util.NodeNic)
	if err != nil {
		klog.Errorf("failed to get nic %s", util.NodeNic)
		return fmt.Errorf("failed to get nic %s", util.NodeNic)
	}

	allRoutes, err := getNicExistRoutes(nil, gateway)
	if err != nil {
		klog.Error(err)
		return err
	}
	nodeNicRoutes, err := getNicExistRoutes(nic, gateway)
	if err != nil {
		klog.Error(err)
		return err
	}
	toAdd, toDel := routeDiff(nodeNicRoutes, allRoutes, cidrs, joinCIDR, joinIPv4, joinIPv6, gateway, net.ParseIP(nodeIPv4), net.ParseIP(nodeIPv6))
	for _, r := range toDel {
		if err = netlink.RouteDel(&netlink.Route{Dst: r.Dst}); err != nil {
			klog.Errorf("failed to del route %v", err)
		}
	}

	for _, r := range toAdd {
		r.LinkIndex = nic.Attrs().Index
		if err = netlink.RouteReplace(&r); err != nil {
			klog.Errorf("failed to replace route %v: %v", r, err)
		}
	}

	return nil
}

func genLBServiceRules(service *v1.Service, bridgeName, underlayNic, dstMac, subnetName string) []LbServiceRules {
	var lbServiceRules []LbServiceRules
	for _, ingress := range service.Status.LoadBalancer.Ingress {
		for _, port := range service.Spec.Ports {
			lbServiceRules = append(lbServiceRules, LbServiceRules{
				IP:          ingress.IP,
				Port:        uint16(port.Port), // #nosec G115
				Protocol:    string(port.Protocol),
				DstMac:      dstMac,
				UnderlayNic: underlayNic,
				BridgeName:  bridgeName,
				SubnetName:  subnetName,
			})
		}
	}
	return lbServiceRules
}

func (c *Controller) diffExternalLBServiceRules(oldService, newService *v1.Service, isSubnetExternalLBEnabled bool) (lbServiceRulesToAdd, lbServiceRulesToDel []LbServiceRules, err error) {
	var oldlbServiceRules, newlbServiceRules []LbServiceRules

	if oldService != nil && oldService.Annotations[util.ServiceExternalIPFromSubnetAnnotation] != "" {
		oldSubnetName := oldService.Annotations[util.ServiceExternalIPFromSubnetAnnotation]
		oldBridgeName, underlayNic, dstMac, err := c.getExtInfoBySubnet(oldSubnetName)
		if err != nil {
			klog.Errorf("failed to get provider network by subnet %s: %v", oldSubnetName, err)
			return nil, nil, err
		}

		oldlbServiceRules = genLBServiceRules(oldService, oldBridgeName, underlayNic, dstMac, oldSubnetName)
	}

	if isSubnetExternalLBEnabled && newService != nil && newService.Annotations[util.ServiceExternalIPFromSubnetAnnotation] != "" {
		newSubnetName := newService.Annotations[util.ServiceExternalIPFromSubnetAnnotation]
		newBridgeName, underlayNic, dstMac, err := c.getExtInfoBySubnet(newSubnetName)
		if err != nil {
			klog.Errorf("failed to get provider network by subnet %s: %v", newSubnetName, err)
			return nil, nil, err
		}
		newlbServiceRules = genLBServiceRules(newService, newBridgeName, underlayNic, dstMac, newSubnetName)
	}

	for _, oldRule := range oldlbServiceRules {
		found := slices.Contains(newlbServiceRules, oldRule)
		if !found {
			lbServiceRulesToDel = append(lbServiceRulesToDel, oldRule)
		}
	}

	for _, newRule := range newlbServiceRules {
		found := slices.Contains(oldlbServiceRules, newRule)
		if !found {
			lbServiceRulesToAdd = append(lbServiceRulesToAdd, newRule)
		}
	}

	return lbServiceRulesToAdd, lbServiceRulesToDel, nil
}

func (c *Controller) getExtInfoBySubnet(subnetName string) (string, string, string, error) {
	subnet, err := c.subnetsLister.Get(subnetName)
	if err != nil {
		klog.Errorf("failed to get subnet %s: %v", subnetName, err)
		return "", "", "", err
	}

	dstMac := subnet.Status.U2OInterconnectionMAC
	if dstMac == "" {
		dstMac = util.MasqueradeExternalLBAccessMac
		klog.Infof("Subnet %s has no U2OInterconnectionMAC, using default MAC %s", subnetName, dstMac)
	} else {
		klog.Infof("Using U2OInterconnectionMAC %s for subnet %s", dstMac, subnetName)
	}

	vlanName := subnet.Spec.Vlan
	if vlanName == "" {
		return "", "", "", errors.New("vlan not specified in subnet")
	}

	vlan, err := c.vlansLister.Get(vlanName)
	if err != nil {
		klog.Errorf("failed to get vlan %s: %v", vlanName, err)
		return "", "", "", err
	}

	providerNetworkName := vlan.Spec.Provider
	if providerNetworkName == "" {
		return "", "", "", errors.New("provider network not specified in vlan")
	}

	pn, err := c.providerNetworksLister.Get(providerNetworkName)
	if err != nil {
		klog.Errorf("failed to get provider network %s: %v", providerNetworkName, err)
		return "", "", "", err
	}

	underlayNic := pn.Spec.DefaultInterface
	for _, item := range pn.Spec.CustomInterfaces {
		if slices.Contains(item.Nodes, c.config.NodeName) {
			underlayNic = item.Interface
			break
		}
	}
	bridgeName := util.ExternalBridgeName(providerNetworkName)
	klog.Infof("Provider network: %s, Underlay NIC: %s, DstMac: %s", providerNetworkName, underlayNic, dstMac)
	return bridgeName, underlayNic, dstMac, nil
}

func (c *Controller) reconcileServices(event *serviceEvent) error {
	if event == nil {
		return nil
	}
	var ok bool
	var oldService, newService *v1.Service
	if event.oldObj != nil {
		if oldService, ok = event.oldObj.(*v1.Service); !ok {
			klog.Errorf("expected old service in serviceEvent but got %#v", event.oldObj)
			return nil
		}
	}

	if event.newObj != nil {
		if newService, ok = event.newObj.(*v1.Service); !ok {
			klog.Errorf("expected new service in serviceEvent but got %#v", event.newObj)
			return nil
		}
	}

	// check is the lb service IP related subnet's EnableExternalLBAddress
	isSubnetExternalLBEnabled := false
	if newService != nil && newService.Annotations[util.ServiceExternalIPFromSubnetAnnotation] != "" {
		subnet, err := c.subnetsLister.Get(newService.Annotations[util.ServiceExternalIPFromSubnetAnnotation])
		if err != nil {
			klog.Errorf("failed to get subnet %s: %v", newService.Annotations[util.ServiceExternalIPFromSubnetAnnotation], err)
			return err
		}
		isSubnetExternalLBEnabled = subnet.Spec.EnableExternalLBAddress
	}

	lbServiceRulesToAdd, lbServiceRulesToDel, err := c.diffExternalLBServiceRules(oldService, newService, isSubnetExternalLBEnabled)
	if err != nil {
		klog.Errorf("failed to get ip port difference: %v", err)
		return err
	}

	if len(lbServiceRulesToAdd) > 0 {
		for _, rule := range lbServiceRulesToAdd {
			klog.Infof("Adding LB service rule: %+v", rule)
			if err := c.AddOrUpdateUnderlaySubnetSvcLocalFlowCache(rule.IP, rule.Port, rule.Protocol, rule.DstMac, rule.UnderlayNic, rule.BridgeName, rule.SubnetName); err != nil {
				klog.Errorf("failed to update underlay subnet svc local openflow cache: %v", err)
				return err
			}
		}
	}

	if len(lbServiceRulesToDel) > 0 {
		for _, rule := range lbServiceRulesToDel {
			klog.Infof("Delete LB service rule: %+v", rule)
			c.deleteUnderlaySubnetSvcLocalFlowCache(rule.BridgeName, rule.IP, rule.Port, rule.Protocol)
		}
	}

	return nil
}

func getNicExistRoutes(nic netlink.Link, gateway string) ([]netlink.Route, error) {
	var routes, existRoutes []netlink.Route
	var err error
	for gw := range strings.SplitSeq(gateway, ",") {
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

func routeDiff(nodeNicRoutes, allRoutes []netlink.Route, cidrs, joinCIDR []string, joinIPv4, joinIPv6, gateway string, srcIPv4, srcIPv6 net.IP) (toAdd, toDel []netlink.Route) {
	// joinIPv6 is not used for now
	_ = joinIPv6

	for _, route := range nodeNicRoutes {
		if route.Scope == netlink.SCOPE_LINK || route.Dst == nil || route.Dst.IP.IsLinkLocalUnicast() {
			continue
		}

		found := slices.Contains(cidrs, route.Dst.String())
		if !found {
			toDel = append(toDel, route)
		}
		conflict := false
		for _, ar := range allRoutes {
			if ar.Dst != nil && ar.Dst.String() == route.Dst.String() && ar.LinkIndex != route.LinkIndex {
				// route conflict
				conflict = true
				break
			}
		}
		if conflict {
			toDel = append(toDel, route)
		}
	}
	if len(toDel) > 0 {
		klog.Infof("routes to delete: %v", toDel)
	}

	ipv4, ipv6 := util.SplitStringIP(gateway)
	gwV4, gwV6 := net.ParseIP(ipv4), net.ParseIP(ipv6)
	for _, c := range cidrs {
		var src, gw net.IP
		switch util.CheckProtocol(c) {
		case kubeovnv1.ProtocolIPv4:
			src, gw = srcIPv4, gwV4
		case kubeovnv1.ProtocolIPv6:
			src, gw = srcIPv6, gwV6
		}

		found := false
		for _, ar := range allRoutes {
			if ar.Dst != nil && ar.Dst.String() == c {
				if slices.Contains(joinCIDR, c) {
					// Only compare Dst for join subnets
					found = true
					klog.V(3).Infof("[routeDiff] joinCIDR route already exists in allRoutes: %v", ar)
					break
				} else if (ar.Src == nil && src == nil) || (ar.Src != nil && src != nil && ar.Src.Equal(src)) {
					// For non-join subnets, both Dst and Src must be the same
					found = true
					klog.V(3).Infof("[routeDiff] route already exists in allRoutes: %v", ar)
					break
				}
			}
		}
		if found {
			continue
		}
		for _, r := range nodeNicRoutes {
			if r.Dst == nil || r.Dst.String() != c {
				continue
			}
			if (src == nil && r.Src == nil) || (src != nil && r.Src != nil && src.Equal(r.Src)) {
				found = true
				break
			}
		}
		if !found {
			var priority int
			scope := netlink.SCOPE_UNIVERSE
			proto := netlink.RouteProtocol(syscall.RTPROT_STATIC)
			if slices.Contains(joinCIDR, c) {
				if util.CheckProtocol(c) == kubeovnv1.ProtocolIPv4 {
					src = net.ParseIP(joinIPv4)
				} else {
					src, priority = nil, 256
				}
				gw, scope = nil, netlink.SCOPE_LINK
				proto = netlink.RouteProtocol(unix.RTPROT_KERNEL)
			}
			_, cidr, _ := net.ParseCIDR(c)
			toAdd = append(toAdd, netlink.Route{
				Dst:      cidr,
				Src:      src,
				Gw:       gw,
				Protocol: proto,
				Scope:    scope,
				Priority: priority,
			})
		}
	}
	if len(toAdd) > 0 {
		klog.Infof("routes to add: %v", toAdd)
	}
	return toAdd, toDel
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
		return rulesToAdd, rulesToDel, routesToAdd, routesToDel, err
	}
	newRules, newRoutes, err := c.getPolicyRouting(newSubnet)
	if err != nil {
		klog.Error(err)
		return rulesToAdd, rulesToDel, routesToAdd, routesToDel, err
	}

	rulesToAdd = getRulesToAdd(oldRules, newRules)
	rulesToDel = getRulesToAdd(newRules, oldRules)
	routesToAdd = getRoutesToAdd(oldRoutes, newRoutes)
	routesToDel = getRoutesToAdd(newRoutes, oldRoutes)

	return rulesToAdd, rulesToDel, routesToAdd, routesToDel, err
}

func (c *Controller) getPolicyRouting(subnet *kubeovnv1.Subnet) ([]netlink.Rule, []netlink.Route, error) {
	if subnet == nil || subnet.Spec.ExternalEgressGateway == "" || subnet.Spec.Vpc != c.config.ClusterRouter {
		return nil, nil, nil
	}
	if subnet.Spec.GatewayType == kubeovnv1.GWCentralizedType {
		node, err := c.nodesLister.Get(c.config.NodeName)
		if err != nil {
			klog.Errorf("failed to get node %s: %v", c.config.NodeName, err)
			return nil, nil, err
		}
		isGatewayNode := util.GatewayContains(subnet.Spec.GatewayNode, c.config.NodeName) ||
			(subnet.Spec.GatewayNode == "" && util.MatchLabelSelectors(subnet.Spec.GatewayNodeSelectors, node.Labels))
		if !isGatewayNode {
			return nil, nil, nil
		}
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

		for _, pod := range pods {
			if pod.Status.PodIP == "" ||
				pod.Annotations[util.LogicalSwitchAnnotation] != subnet.Name {
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
		routes = append(routes, netlink.Route{
			Protocol: netlink.RouteProtocol(syscall.RTPROT_STATIC),
			Table:    int(subnet.Spec.PolicyRoutingTableID),
			Gw:       net.ParseIP(egw[i]),
		})
	}

	return rules, routes, nil
}

func (c *Controller) handleUpdatePod(key string) error {
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
		klog.Error(err)
		return err
	}

	if err := util.ValidatePodNetwork(pod.Annotations); err != nil {
		klog.Errorf("validate pod %s/%s failed, %v", namespace, name, err)
		c.recorder.Eventf(pod, v1.EventTypeWarning, "ValidatePodNetworkFailed", err.Error())
		return err
	}

	podName := pod.Name
	if pod.Annotations[fmt.Sprintf(util.VMAnnotationTemplate, util.OvnProvider)] != "" {
		podName = pod.Annotations[fmt.Sprintf(util.VMAnnotationTemplate, util.OvnProvider)]
	}

	// set default nic bandwidth
	//  ovsIngress and ovsEgress are derived from the pod's egress and ingress rate annotations respectively, their roles are reversed from the OVS interface perspective.
	ifaceID := ovs.PodNameToPortName(podName, pod.Namespace, util.OvnProvider)
	ovsIngress := pod.Annotations[util.EgressRateAnnotation]
	ovsEgress := pod.Annotations[util.IngressRateAnnotation]
	err = ovs.SetInterfaceBandwidth(podName, pod.Namespace, ifaceID, ovsIngress, ovsEgress)
	if err != nil {
		klog.Error(err)
		return err
	}
	err = ovs.ConfigInterfaceMirror(c.config.EnableMirror, pod.Annotations[util.MirrorControlAnnotation], ifaceID)
	if err != nil {
		klog.Error(err)
		return err
	}
	// set linux-netem qos
	err = ovs.SetNetemQos(podName, pod.Namespace, ifaceID, pod.Annotations[util.NetemQosLatencyAnnotation], pod.Annotations[util.NetemQosLimitAnnotation], pod.Annotations[util.NetemQosLossAnnotation], pod.Annotations[util.NetemQosJitterAnnotation])
	if err != nil {
		klog.Error(err)
		return err
	}

	// set multus-nic bandwidth
	attachNets, err := nadutils.ParsePodNetworkAnnotation(pod)
	if err != nil {
		if _, ok := err.(*nadv1.NoK8sNetworkError); ok {
			return nil
		}
		klog.Error(err)
		return err
	}
	for _, multiNet := range attachNets {
		provider := fmt.Sprintf("%s.%s.%s", multiNet.Name, multiNet.Namespace, util.OvnProvider)
		if pod.Annotations[fmt.Sprintf(util.VMAnnotationTemplate, provider)] != "" {
			podName = pod.Annotations[fmt.Sprintf(util.VMAnnotationTemplate, provider)]
		}
		if pod.Annotations[fmt.Sprintf(util.AllocatedAnnotationTemplate, provider)] == "true" {
			ifaceID = ovs.PodNameToPortName(podName, pod.Namespace, provider)

			err = ovs.SetInterfaceBandwidth(podName, pod.Namespace, ifaceID, pod.Annotations[fmt.Sprintf(util.EgressRateAnnotationTemplate, provider)], pod.Annotations[fmt.Sprintf(util.IngressRateAnnotationTemplate, provider)])
			if err != nil {
				klog.Error(err)
				return err
			}
			err = ovs.ConfigInterfaceMirror(c.config.EnableMirror, pod.Annotations[fmt.Sprintf(util.MirrorControlAnnotationTemplate, provider)], ifaceID)
			if err != nil {
				klog.Error(err)
				return err
			}
			err = ovs.SetNetemQos(podName, pod.Namespace, ifaceID, pod.Annotations[fmt.Sprintf(util.NetemQosLatencyAnnotationTemplate, provider)], pod.Annotations[fmt.Sprintf(util.NetemQosLimitAnnotationTemplate, provider)], pod.Annotations[fmt.Sprintf(util.NetemQosLossAnnotationTemplate, provider)], pod.Annotations[fmt.Sprintf(util.NetemQosJitterAnnotationTemplate, provider)])
			if err != nil {
				klog.Error(err)
				return err
			}
		}
	}

	return nil
}

func (c *Controller) loopEncapIPCheck() {
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
		if iface.Name != c.config.tunnelIface {
			klog.Infof("use %s as tunnel interface", iface.Name)
			c.config.tunnelIface = iface.Name
		}

		// if assigned iface in node annotation is down or with no ip, the error msg should be printed periodically
		if c.config.Iface == nodeTunnelName {
			klog.V(3).Infof("node tunnel interface %s not changed", nodeTunnelName)
			return
		}
		c.config.Iface = nodeTunnelName
		klog.Infof("Update node tunnel interface %v", nodeTunnelName)

		c.config.DefaultEncapIP = strings.Split(addrs[0].String(), "/")[0]
		if err = c.config.setEncapIPs(); err != nil {
			klog.Errorf("failed to set encap ip %s for iface %s", c.config.DefaultEncapIP, c.config.Iface)
			return
		}
	}
}

func (c *Controller) ovnMetricsUpdate() {
	c.setOvnSubnetGatewayMetric()

	resetSysParaMetrics()
	c.setIPLocalPortRangeMetric()
	c.setCheckSumErrMetric()
	c.setDNSSearchMetric()
	c.setTCPTwRecycleMetric()
	c.setTCPMtuProbingMetric()
	c.setConntrackTCPLiberalMetric()
	c.setBridgeNfCallIptablesMetric()
	c.setIPv6RouteMaxsizeMetric()
	c.setTCPMemMetric()
}

func resetSysParaMetrics() {
	metricIPLocalPortRange.Reset()
	metricCheckSumErr.Reset()
	metricDNSSearch.Reset()
	metricTCPTwRecycle.Reset()
	metricTCPMtuProbing.Reset()
	metricConntrackTCPLiberal.Reset()
	metricBridgeNfCallIptables.Reset()
	metricTCPMem.Reset()
	metricIPv6RouteMaxsize.Reset()
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

func kernelModuleLoaded(module string) (bool, error) {
	data, err := os.ReadFile("/proc/modules")
	if err != nil {
		klog.Errorf("failed to read /proc/modules: %v", err)
		return false, err
	}

	for line := range strings.SplitSeq(string(data), "\n") {
		if fields := strings.Fields(line); len(fields) != 0 && fields[0] == module {
			return true, nil
		}
	}

	return false, nil
}
