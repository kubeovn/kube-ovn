package daemon

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
	"golang.org/x/sys/unix"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/vswitch"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const externalLBLabelPrefix = "ovn.kubernetes.io/external-lb-"

func (c *Controller) loopExternalLBInterfaces() {
	if err := c.reconcileExternalLBInterfaces(); err != nil {
		klog.Errorf("failed to reconcile external LB interfaces: %v", err)
	}
}

// A separate periodic reconciliation also repairs node exclusions, accidental
// addresses, interface renames, daemon restarts and missed informer events.
func (c *Controller) reconcileExternalLBInterfaces() error {
	c.externalLBMutex.Lock()
	defer c.externalLBMutex.Unlock()
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		return err
	}
	node, err := c.nodesLister.Get(c.config.NodeName)
	if err != nil {
		return err
	}
	desired := make(map[string]*kubeovnv1.Subnet)
	conflicts := make(map[string]bool)
	for _, subnet := range subnets {
		name := subnet.Spec.ExternalLBInterface
		if name == "" {
			continue
		}
		if _, exists := desired[name]; exists {
			conflicts[name] = true
		}
		desired[name] = subnet
	}
	var errs []error
	for name, subnet := range desired {
		eligible, err := c.externalLBNodeEligible(node, subnet)
		if conflicts[name] {
			err = fmt.Errorf("multiple subnets request external LB interface %s", name)
		}
		if err != nil {
			errs = append(errs, err)
		}
		if err != nil || !eligible {
			delete(desired, name)
		}
	}
	// Recompute readiness, retaining existing labels only after reconciliation succeeds.
	ready := util.KVPatch{}
	for name, value := range node.Labels {
		if strings.HasPrefix(name, externalLBLabelPrefix) && value != "" {
			ready[name] = nil
		}
	}
	interfaces, err := c.listVswitchInterfaces(nil)
	if err != nil {
		return err
	}
	for _, iface := range interfaces {
		if !externalLBPortOwned(iface.ExternalIDs) {
			continue
		}
		subnet := desired[iface.Name]
		if subnet != nil && iface.ExternalIDs["subnet-uid"] == string(subnet.UID) {
			continue
		}
		if err := c.deleteExternalLBInterface(iface.Name); err != nil {
			errs = append(errs, err)
		}
	}
	for name, subnet := range desired {
		if err := c.ensureExternalLBInterface(subnet); err != nil {
			errs = append(errs, fmt.Errorf("configure external LB interface %s: %w", name, err))
			continue
		}
		ready[util.ExternalLBReadyLabel(name)] = "true"
	}
	for key, value := range ready {
		if value == node.Labels[key] {
			delete(ready, key)
		}
	}
	if len(ready) != 0 {
		if err := util.PatchLabels(c.config.KubeClient.CoreV1().Nodes(), node.Name, ready); err != nil {
			errs = append(errs, err)
		}
	}
	c.pruneExternalLBFlows(subnets)
	return errors.Join(errs...)
}

func (c *Controller) externalLBNodeEligible(node *v1.Node, subnet *kubeovnv1.Subnet) (bool, error) {
	if err := util.ValidateExternalLBInterface(*subnet); err != nil {
		return false, err
	}
	if subnet.DeletionTimestamp != nil {
		return false, nil
	}
	vlan, err := c.vlansLister.Get(subnet.Spec.Vlan)
	if err != nil {
		return false, err
	}
	pn, err := c.providerNetworksLister.Get(vlan.Spec.Provider)
	if err != nil {
		return false, err
	}
	excluded, err := util.IsNodeExcludedFromProviderNetwork(node, pn)
	if err != nil {
		return false, err
	}
	return !excluded && node.Labels[fmt.Sprintf(util.ProviderNetworkReadyTemplate, pn.Name)] == "true", nil
}

func externalLBPortOwned(ids map[string]string) bool {
	return ids["vendor"] == util.CniTypeName && ids["role"] == util.ExternalLBPortRole
}

func (c *Controller) ensureExternalLBInterface(subnet *kubeovnv1.Subnet) error {
	name := subnet.Spec.ExternalLBInterface
	interfaces, err := c.listVswitchInterfaces(func(iface *vswitch.Interface) bool { return iface.Name == name })
	if err != nil {
		return err
	}
	for _, iface := range interfaces {
		if !externalLBPortOwned(iface.ExternalIDs) || iface.ExternalIDs["subnet-uid"] != string(subnet.UID) || iface.Type != "internal" {
			return fmt.Errorf("OVS interface %s is not owned by subnet %s", name, subnet.Name)
		}
	}
	ports, err := c.listVswitchPorts(func(port *vswitch.Port) bool { return port.Name == name })
	if err != nil {
		return err
	}
	for _, port := range ports {
		if !externalLBPortOwned(port.ExternalIDs) || port.ExternalIDs["subnet-uid"] != string(subnet.UID) {
			return fmt.Errorf("OVS port %s is not owned by subnet %s", name, subnet.Name)
		}
	}
	if len(interfaces) == 0 {
		if _, err := netlink.LinkByName(name); err == nil {
			return fmt.Errorf("network interface %s already exists", name)
		} else if _, ok := errors.AsType[netlink.LinkNotFoundError](err); !ok {
			return err
		}
	}
	mac := util.ExternalLBInterfaceMAC(c.config.NodeName, string(subnet.UID))
	ids := map[string]string{"vendor": util.CniTypeName, "role": util.ExternalLBPortRole, "subnet": subnet.Name, "subnet-uid": string(subnet.UID)}
	port := &vswitch.Port{Name: name, ExternalIDs: ids}
	ifaceIDs := map[string]string{"vendor": util.CniTypeName, "role": util.ExternalLBPortRole, "subnet": subnet.Name, "subnet-uid": string(subnet.UID), "iface-id": ""}
	// New ports remain unbound until the host stack has been isolated.
	if len(interfaces) != 0 {
		ifaceIDs["iface-id"] = interfaces[0].ExternalIDs["iface-id"]
	}
	iface := &vswitch.Interface{Name: name, Type: "internal", MAC: new(mac.String()), ExternalIDs: ifaceIDs}
	config := ovs.VswitchPortConfig{
		BridgeName: "br-int", Port: port, Interface: iface,
		PortFields: []any{&port.ExternalIDs}, InterfaceFields: []any{&iface.Type, &iface.MAC, &iface.ExternalIDs},
	}
	if err := ensureVswitchPort(c.vswitchTables, config); err != nil {
		return err
	}
	if err := configureExternalLBHostInterface(name, mac); err != nil {
		iface.ExternalIDs["iface-id"] = ""
		return errors.Join(err, ensureVswitchPort(c.vswitchTables, config))
	}
	iface.ExternalIDs["iface-id"] = util.ExternalLBLocalPortName(subnet.Name)
	if err := ensureVswitchPort(c.vswitchTables, config); err != nil {
		return err
	}
	_, err = c.getPortID(name)
	return err
}

func (c *Controller) deleteExternalLBInterface(name string) error {
	ports, err := c.listVswitchPorts(func(port *vswitch.Port) bool { return port.Name == name })
	if err != nil {
		return err
	}
	for _, port := range ports {
		if !externalLBPortOwned(port.ExternalIDs) {
			return fmt.Errorf("refusing to delete foreign OVS port %s", name)
		}
	}
	return deleteVswitchPort(c.vswitchTables, name)
}

// No NOARP flag: MetalLB excludes NOARP interfaces. Kernel ARP responses and
// proxy ARP are disabled independently of raw socket ARP sent by the speaker.
func configureExternalLBHostInterface(name string, mac net.HardwareAddr) error {
	link, err := netlink.LinkByName(name)
	if err != nil {
		return err
	} // OVS creates the netdev asynchronously; retry next cycle.
	// Linux ORs the global and per-interface proxy ARP switches. Reject a
	// global proxy instead of silently changing host-wide routing policy.
	for _, key := range []string{"proxy_arp", "proxy_arp_pvlan"} {
		value, err := os.ReadFile(filepath.Join("/proc/sys/net/ipv4/conf/all", key))
		if err != nil {
			return err
		}
		if strings.TrimSpace(string(value)) != "0" {
			return fmt.Errorf("external LB interface requires net.ipv4.conf.all.%s=0", key)
		}
	}
	if err := netlink.LinkSetIP6AddrGenMode(link, nl.IN6_ADDR_GEN_MODE_NONE); err != nil && !errors.Is(err, unix.EAFNOSUPPORT) {
		return fmt.Errorf("disable IPv6 address generation: %w", err)
	}
	if err := protectExternalLBInterface(link); err != nil {
		return err
	}
	return reconcileExternalLBLink(link, mac, externalLBLinkOperations{
		setMAC: netlink.LinkSetHardwareAddr, listAddresses: netlink.AddrList,
		deleteAddress: netlink.AddrDel, setUp: netlink.LinkSetUp,
	})
}

type externalLBLinkOperations struct {
	setMAC        func(netlink.Link, net.HardwareAddr) error
	listAddresses func(netlink.Link, int) ([]netlink.Addr, error)
	deleteAddress func(netlink.Link, *netlink.Addr) error
	setUp         func(netlink.Link) error
}

func reconcileExternalLBLink(link netlink.Link, mac net.HardwareAddr, ops externalLBLinkOperations) error {
	if !bytes.Equal(link.Attrs().HardwareAddr, mac) {
		if err := ops.setMAC(link, mac); err != nil {
			return err
		}
	}
	addresses, err := ops.listAddresses(link, netlink.FAMILY_ALL)
	if err != nil {
		return err
	}
	for _, address := range addresses {
		if err := ops.deleteAddress(link, &address); err != nil {
			return err
		}
	}
	return ops.setUp(link)
}

func protectExternalLBInterface(link netlink.Link) error {
	if link == nil || link.Attrs() == nil || link.Attrs().Index == 0 {
		return errors.New("external LB interface has no link index")
	}
	if int64(link.Attrs().Index) > 1<<31-1 {
		return fmt.Errorf("external LB interface has invalid link index %d", link.Attrs().Index)
	}

	// The CNI server is intentionally non-root and container runtimes mount
	// /proc/sys read-only. Configure per-interface sysctls through rtnetlink
	// instead; this uses the daemon's existing CAP_NET_ADMIN capability and
	// avoids exposing a writable host /proc or granting CAP_SYS_ADMIN.
	request := externalLBInterfaceSysctlRequest(link.Attrs().Index)
	if _, err := request.Execute(unix.NETLINK_ROUTE, 0); err != nil {
		return fmt.Errorf("set external LB interface sysctls through rtnetlink: %w", err)
	}
	return nil
}

func externalLBInterfaceSysctlRequest(index int) *nl.NetlinkRequest {
	request := nl.NewNetlinkRequest(unix.RTM_SETLINK, unix.NLM_F_ACK)
	message := nl.NewIfInfomsg(unix.AF_UNSPEC)
	message.Index = int32(index) //nolint:gosec // validated by protectExternalLBInterface
	request.AddData(message)
	spec := nl.NewRtAttr(unix.IFLA_AF_SPEC, nil)
	ipv4 := spec.AddRtAttr(unix.AF_INET, nil)
	ipv4Conf := ipv4.AddRtAttr(int(unix.IFLA_INET_CONF)|int(unix.NLA_F_NESTED), nil)
	ipv4Conf.AddRtAttr(ipv4DevconfArpIgnore, nl.Uint32Attr(8))
	ipv4Conf.AddRtAttr(ipv4DevconfProxyArp, nl.Uint32Attr(0))
	ipv4Conf.AddRtAttr(ipv4DevconfProxyArpPvlan, nl.Uint32Attr(0))
	ipv4Conf.AddRtAttr(ipv4DevconfForwarding, nl.Uint32Attr(0))
	request.AddData(spec)
	return request
}

const (
	ipv4DevconfForwarding    = 1
	ipv4DevconfProxyArp      = 3
	ipv4DevconfArpIgnore     = 19
	ipv4DevconfProxyArpPvlan = 25
)

func (c *Controller) pruneExternalLBFlows(subnets []*kubeovnv1.Subnet) {
	legacy := make(map[string]bool)
	for _, subnet := range subnets {
		legacy[subnet.Name] = subnet.Spec.EnableExternalLBAddress && subnet.Spec.ExternalLBInterface == "" && subnet.DeletionTimestamp == nil
	}
	changed := false
	c.flowCacheMutex.Lock()
	for _, rules := range c.flowCache {
		for key, rule := range rules {
			if !legacy[rule.SubnetName] {
				delete(rules, key)
				changed = true
			}
		}
	}
	c.flowCacheMutex.Unlock()
	if changed {
		c.requestFlowSync()
	}
}

// Use the cached bridge and MAC for removals: the Subnet or provider may already
// have disappeared. Look up only this Service's keys rather than scanning every VIP.
func (c *Controller) cachedExternalLBServiceRules(service *v1.Service) []LbServiceRules {
	subnet := service.Annotations[util.ServiceExternalIPFromSubnetAnnotation]
	if subnet == "" {
		return nil
	}
	c.flowCacheMutex.RLock()
	defer c.flowCacheMutex.RUnlock()
	var result []LbServiceRules
	for _, rules := range c.flowCache {
		for _, ingress := range service.Status.LoadBalancer.Ingress {
			for _, port := range service.Spec.Ports {
				key := buildFlowKey(flowKindUnderlayService, ingress.IP, uint16(port.Port), string(port.Protocol), "") // #nosec G115
				if rule, ok := rules[key]; ok && rule.SubnetName == subnet {
					result = append(result, rule)
				}
			}
		}
	}
	return result
}
