package daemon

// This file implements node-local EIP access for VPC NAT Gateway.
//
// Problem: When a VPC NAT Gateway pod runs on a node, the node itself cannot
// directly access the EIP addresses configured on the gateway because the
// EIP traffic goes through the external macvlan interface attached to the pod.
//
// Solution: Create a macvlan sub-interface on the node with the same master
// interface as the NAT Gateway's external network, and add host routes for
// each EIP pointing to this sub-interface. This allows the node to reach
// the EIP addresses via Layer 2 on the external network.
//
// Control flow:
//  1. Subnet event (subnet with NadMacvlanMasterAnnotation):
//     reconcileRouters() → reconcileSubnetMacvlan() / deleteSubnetMacvlan()
//     → Create/delete macvlan sub-interface on all nodes
//
//  2. IptablesEIP event:
//     - Add:    enqueueAddIptablesEip (daemon restart recovery for existing Ready EIPs)
//     - Update: enqueueUpdateIptablesEip (normal runtime when EIP becomes Ready)
//     - Delete: enqueueDeleteIptablesEip
//     → runIptablesEipWorker() → handleAddIptablesEipRoute() / handleDeleteIptablesEip()
//     → Add/delete host routes via the macvlan sub-interface
//
//  3. NAT GW Pod event (pod update with VpcNatGatewayLabel):
//     enqueueUpdatePod() → handleNatGwPodUpdate()
//     → enqueueEipsForNatGw() → handleAddIptablesEipRoute()
//     → Add/delete routes based on pod location and phase
//
// Prerequisites:
//   - Subnet controller must set NadMacvlanMasterAnnotation when provider NAD is macvlan type
//   - EnableNodeLocalAccessVpcNatGwEIP config flag must be true (default)

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"syscall"

	"github.com/vishvananda/netlink"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	// macvlanLinkPrefix is the prefix for macvlan sub-interfaces created for node local EIP access
	macvlanLinkPrefix = "macvl"
)

// deletedEIPs tracks EIPs that have been deleted to prevent race conditions
// between delete events and queued add events
var deletedEIPs sync.Map

// generateMacvlanName generates the macvlan sub-interface name from subnet CIDR.
// For dual-stack CIDR (e.g., "10.0.0.0/24,2001:db8::/64"), uses the first CIDR.
// e.g., for IPv4 CIDR 192.168.1.0/24, it returns "macvl19216810"
// e.g., for IPv6 CIDR 2001:db8::/32, it returns "macvl2001db8"
func generateMacvlanName(cidr string) string {
	origCidr := cidr
	// For dual-stack, use the first CIDR
	if idx := strings.Index(cidr, ","); idx != -1 {
		cidr = cidr[:idx]
	}
	// Extract network address (remove mask), then remove dots/colons
	network := strings.Split(cidr, "/")[0]
	network = strings.ReplaceAll(network, ".", "")
	network = strings.ReplaceAll(network, ":", "")
	name := macvlanLinkPrefix + network
	// Linux interface names have 15 char limit
	if len(name) > 15 {
		name = name[:15]
	}
	klog.V(3).Infof("generateMacvlanName: cidr=%s -> name=%s", origCidr, name)
	return name
}

// parseEIPDestination parses an EIP address into a host route destination (/32 for IPv4, /128 for IPv6)
func parseEIPDestination(eip string) (*net.IPNet, error) {
	ip := net.ParseIP(eip)
	if ip == nil {
		err := fmt.Errorf("invalid EIP address: %s", eip)
		klog.Error(err)
		return nil, err
	}

	mask := net.CIDRMask(32, 32)
	if ip.To4() == nil {
		mask = net.CIDRMask(128, 128)
	}
	return &net.IPNet{IP: ip, Mask: mask}, nil
}

// ensureMacvlanSubInterfaceUp ensures the macvlan sub-interface is up
func ensureMacvlanSubInterfaceUp(link netlink.Link) error {
	if link.Attrs().OperState != netlink.OperUp {
		if err := netlink.LinkSetUp(link); err != nil {
			err = fmt.Errorf("failed to set link %s up: %w", link.Attrs().Name, err)
			klog.Error(err)
			return err
		}
	}
	return nil
}

// addNodeIPToLink adds node IP with host mask (/32 for IPv4, /128 for IPv6) to the macvlan sub-interface.
// This allows the node to respond to ARP/NDP requests for its IP on the macvlan interface.
// Returns error if failed, but this is not critical for EIP routing to work.
func (c *Controller) addNodeIPToLink(link netlink.Link) error {
	nodeIP := c.config.NodeIPv4
	mask := "/32"
	if nodeIP == "" {
		nodeIP = c.config.NodeIPv6
		mask = "/128"
	}
	if nodeIP == "" {
		return nil
	}

	addr, err := netlink.ParseAddr(nodeIP + mask)
	if err != nil {
		err = fmt.Errorf("failed to parse node IP %s: %w", nodeIP, err)
		klog.Error(err)
		return err
	}
	if err := netlink.AddrAdd(link, addr); err != nil && !errors.Is(err, syscall.EEXIST) {
		err = fmt.Errorf("failed to add address %s to link %s: %w", nodeIP, link.Attrs().Name, err)
		klog.Error(err)
		return err
	}
	return nil
}

// createMacvlanSubInterface creates a macvlan sub-interface for node local EIP access.
func (c *Controller) createMacvlanSubInterface(masterIface, cidr string) error {
	macvlanName := generateMacvlanName(cidr)

	// Check if sub-interface already exists
	if link, err := netlink.LinkByName(macvlanName); err == nil {
		klog.V(3).Infof("macvlan sub-interface %s already exists", macvlanName)
		return ensureMacvlanSubInterfaceUp(link)
	}

	master, err := netlink.LinkByName(masterIface)
	if err != nil {
		err = fmt.Errorf("failed to get master interface %s: %w", masterIface, err)
		klog.Error(err)
		return err
	}

	macvlan := &netlink.Macvlan{
		LinkAttrs: netlink.LinkAttrs{
			Name:        macvlanName,
			ParentIndex: master.Attrs().Index,
		},
		Mode: netlink.MACVLAN_MODE_BRIDGE,
	}

	if err := netlink.LinkAdd(macvlan); err != nil {
		err = fmt.Errorf("failed to create macvlan sub-interface %s: %w", macvlanName, err)
		klog.Error(err)
		return err
	}

	if err := netlink.LinkSetUp(macvlan); err != nil {
		_ = netlink.LinkDel(macvlan)
		err = fmt.Errorf("failed to set macvlan sub-interface %s up: %w", macvlanName, err)
		klog.Error(err)
		return err
	}

	// Add node IP to macvlan interface for ARP/NDP responses
	if c.config.EnableMacvlanNodeLocalIP {
		if err := c.addNodeIPToLink(macvlan); err != nil {
			_ = netlink.LinkDel(macvlan)
			err = fmt.Errorf("failed to add node IP to macvlan %s: %w", macvlanName, err)
			klog.Error(err)
			return err
		}
	}

	klog.Infof("created macvlan sub-interface %s with master %s for CIDR %s", macvlanName, masterIface, cidr)
	return nil
}

// deleteMacvlanSubInterface deletes the macvlan sub-interface.
func deleteMacvlanSubInterface(cidr string) error {
	macvlanName := generateMacvlanName(cidr)

	link, err := netlink.LinkByName(macvlanName)
	if err != nil {
		if _, ok := err.(netlink.LinkNotFoundError); ok {
			return nil
		}
		err = fmt.Errorf("failed to get macvlan sub-interface %s: %w", macvlanName, err)
		klog.Error(err)
		return err
	}

	if err := netlink.LinkDel(link); err != nil {
		err = fmt.Errorf("failed to delete macvlan sub-interface %s: %w", macvlanName, err)
		klog.Error(err)
		return err
	}

	klog.Infof("deleted macvlan sub-interface %s", macvlanName)
	return nil
}

// addEIPRoute adds a route for EIP via macvlan sub-interface.
func addEIPRoute(eip, cidr string) error {
	macvlanName := generateMacvlanName(cidr)

	link, err := netlink.LinkByName(macvlanName)
	if err != nil {
		err = fmt.Errorf("failed to get macvlan sub-interface %s for EIP %s: %w", macvlanName, eip, err)
		klog.Error(err)
		return err
	}

	dst, err := parseEIPDestination(eip)
	if err != nil {
		return err
	}

	route := &netlink.Route{
		LinkIndex: link.Attrs().Index,
		Dst:       dst,
		Scope:     netlink.SCOPE_LINK,
	}

	if err := netlink.RouteReplace(route); err != nil {
		err = fmt.Errorf("failed to add route for EIP %s via %s: %w", eip, macvlanName, err)
		klog.Error(err)
		return err
	}

	klog.Infof("added route for EIP %s via macvlan sub-interface %s", eip, macvlanName)
	return nil
}

// deleteEIPRoute deletes the route for EIP from all macvlan sub-interfaces.
func deleteEIPRoute(eip string) error {
	dst, err := parseEIPDestination(eip)
	if err != nil {
		return err
	}

	// Find all macvlan interfaces and delete the route from each
	links, err := netlink.LinkList()
	if err != nil {
		err = fmt.Errorf("failed to list links for deleting EIP %s route: %w", eip, err)
		klog.Error(err)
		return err
	}

	deleted := false
	for _, link := range links {
		if !strings.HasPrefix(link.Attrs().Name, macvlanLinkPrefix) {
			continue
		}

		route := &netlink.Route{
			LinkIndex: link.Attrs().Index,
			Dst:       dst,
			Scope:     netlink.SCOPE_LINK,
		}
		if err := netlink.RouteDel(route); err != nil {
			if !errors.Is(err, syscall.ESRCH) {
				klog.Warningf("failed to delete route for EIP %s from %s: %v", eip, link.Attrs().Name, err)
			}
			continue
		}
		klog.Infof("deleted route for EIP %s from %s", eip, link.Attrs().Name)
		deleted = true
	}

	if !deleted {
		klog.V(3).Infof("no route found for EIP %s on any macvlan interface", eip)
	}
	return nil
}

// reconcileSubnetMacvlan handles macvlan sub-interface creation/deletion based on subnet annotation.
// Called when subnet is created/updated/deleted.
func (c *Controller) reconcileSubnetMacvlan(subnet *kubeovnv1.Subnet) error {
	masterIface := subnet.Annotations[util.NadMacvlanMasterAnnotation]
	if masterIface == "" {
		// Not an external subnet for VPC NAT GW, nothing to do
		return nil
	}

	cidr := subnet.Spec.CIDRBlock
	if cidr == "" {
		return nil
	}

	klog.V(3).Infof("reconcile macvlan sub-interface for subnet %s (cidr=%s, master=%s)", subnet.Name, cidr, masterIface)
	return c.createMacvlanSubInterface(masterIface, cidr)
}

// deleteSubnetMacvlan deletes the macvlan sub-interface for a subnet.
func (c *Controller) deleteSubnetMacvlan(subnet *kubeovnv1.Subnet) error {
	masterIface := subnet.Annotations[util.NadMacvlanMasterAnnotation]
	if masterIface == "" {
		// Not an external subnet for VPC NAT GW, nothing to do
		return nil
	}

	cidr := subnet.Spec.CIDRBlock
	if cidr == "" {
		return nil
	}

	klog.V(3).Infof("cleanup macvlan sub-interface for subnet %s", subnet.Name)
	return deleteMacvlanSubInterface(cidr)
}

// hasNatGwPodOnLocalNode checks if the NAT GW pod for the given EIP is scheduled on the local node.
// NAT GW pod name is generated from NatGwDp field using the pattern: vpc-nat-gw-{natGwDp}-0
// Note: This only checks NodeName, not pod phase. The caller should check EIP Ready status
// to ensure the NAT GW pod has successfully configured iptables rules.
func (c *Controller) hasNatGwPodOnLocalNode(eip *kubeovnv1.IptablesEIP) bool {
	if eip.Spec.NatGwDp == "" {
		klog.Errorf("iptables-eip %s has empty NatGwDp field", eip.Name)
		return false
	}

	podName := util.GenNatGwPodName(eip.Spec.NatGwDp)
	pod, err := c.podsLister.Pods(c.config.PodNamespace).Get(podName)
	if err != nil {
		klog.V(3).Infof("failed to get NAT GW pod %s: %v", podName, err)
		return false
	}

	return pod.Spec.NodeName == c.config.NodeName
}

// shouldEnqueueIptablesEip checks if an EIP should be enqueued for route processing.
// Returns true if EIP is ready and has ExternalSubnet configured.
func shouldEnqueueIptablesEip(eip *kubeovnv1.IptablesEIP) bool {
	return eip.Spec.ExternalSubnet != "" && eip.Status.Ready
}

// enqueueAddIptablesEip handles add events for IptablesEIP.
// This is primarily for daemon restart recovery: when the daemon restarts, the informer
// triggers Add events for all existing resources. Ready EIPs need to be re-processed
// to restore their routes on the node.
// Note: For newly created EIPs, they start with Ready=false and become Ready via an
// Update event, which is handled by enqueueUpdateIptablesEip.
func (c *Controller) enqueueAddIptablesEip(obj any) {
	eip := obj.(*kubeovnv1.IptablesEIP)

	// Clear deleted mark if EIP exists (handles edge case of stale mark)
	deletedEIPs.Delete(eip.Name)

	if !shouldEnqueueIptablesEip(eip) {
		return
	}

	klog.V(3).Infof("enqueue add iptables-eip %s for route recovery", eip.Name)
	c.iptablesEipQueue.Add(eip.Name)
}

// enqueueUpdateIptablesEip handles update events for IptablesEIP.
// This is for normal runtime: when an EIP transitions to Ready state (after NAT Gateway
// configures iptables rules), add its route to the node.
func (c *Controller) enqueueUpdateIptablesEip(_, newObj any) {
	eip := newObj.(*kubeovnv1.IptablesEIP)

	// Skip EIPs that are being deleted
	if eip.DeletionTimestamp != nil {
		return
	}

	// Clear deleted mark if EIP was recreated with the same name
	deletedEIPs.Delete(eip.Name)

	if !shouldEnqueueIptablesEip(eip) {
		return
	}

	klog.V(3).Infof("enqueue update iptables-eip %s", eip.Name)
	c.iptablesEipQueue.Add(eip.Name)
}

// enqueueDeleteIptablesEip handles delete events for IptablesEIP.
func (c *Controller) enqueueDeleteIptablesEip(obj any) {
	var eip *kubeovnv1.IptablesEIP
	switch t := obj.(type) {
	case *kubeovnv1.IptablesEIP:
		eip = t
	case cache.DeletedFinalStateUnknown:
		e, ok := t.Obj.(*kubeovnv1.IptablesEIP)
		if !ok {
			klog.Warningf("unexpected object type: %T", t.Obj)
			return
		}
		eip = e
	default:
		klog.Warningf("unexpected type: %T", obj)
		return
	}

	klog.V(3).Infof("enqueue delete iptables-eip %s", eip.Name)
	// Mark EIP as deleted to prevent race conditions with queued add events
	deletedEIPs.Store(eip.Name, true)
	c.handleDeleteIptablesEip(eip)
}

// handleDeleteIptablesEip handles the deletion of an IptablesEIP's routes.
// deleteEIPRoute is idempotent - it returns nil if route doesn't exist.
func (c *Controller) handleDeleteIptablesEip(eip *kubeovnv1.IptablesEIP) {
	klog.Infof("deleting iptables-eip route for %s (V4ip=%s, V6ip=%s)", eip.Name, eip.Spec.V4ip, eip.Spec.V6ip)

	if eip.Spec.V4ip != "" {
		if err := deleteEIPRoute(eip.Spec.V4ip); err != nil {
			klog.Errorf("failed to delete route for EIP %s: %v", eip.Spec.V4ip, err)
		}
	}
	if eip.Spec.V6ip != "" {
		if err := deleteEIPRoute(eip.Spec.V6ip); err != nil {
			klog.Errorf("failed to delete route for EIP %s: %v", eip.Spec.V6ip, err)
		}
	}
}

// runIptablesEipWorker runs the worker for syncing IptablesEIP routes.
func (c *Controller) runIptablesEipWorker() {
	for c.processNextIptablesEipItem() {
	}
}

// processNextIptablesEipItem processes the next work item.
func (c *Controller) processNextIptablesEipItem() bool {
	key, shutdown := c.iptablesEipQueue.Get()
	if shutdown {
		return false
	}

	err := func(key string) error {
		defer c.iptablesEipQueue.Done(key)
		if err := c.handleAddIptablesEipRoute(key); err != nil {
			c.iptablesEipQueue.AddRateLimited(key)
			return fmt.Errorf("error adding EIP route for %q: %w, requeuing", key, err)
		}
		c.iptablesEipQueue.Forget(key)
		return nil
	}(key)
	if err != nil {
		klog.Error(err)
		return true
	}
	return true
}

// handleAddIptablesEipRoute adds the route for an IptablesEIP.
func (c *Controller) handleAddIptablesEipRoute(eipName string) error {
	klog.Infof("handling add iptables-eip route for %s", eipName)

	// Check if EIP was deleted - skip if it was to prevent race conditions
	if _, deleted := deletedEIPs.Load(eipName); deleted {
		klog.V(3).Infof("iptables-eip %s was deleted, skipping add", eipName)
		return nil
	}

	eip, err := c.iptablesEipsLister.Get(eipName)
	if err != nil {
		klog.V(3).Infof("iptables-eip %s not found: %v", eipName, err)
		return nil
	}

	// Get subnet info first - needed for both add and delete operations
	subnet, err := c.subnetsLister.Get(eip.Spec.ExternalSubnet)
	if err != nil {
		err = fmt.Errorf("failed to get subnet %s for iptables-eip %s: %w", eip.Spec.ExternalSubnet, eipName, err)
		klog.Error(err)
		return err
	}

	cidr := subnet.Spec.CIDRBlock
	if cidr == "" {
		err = fmt.Errorf("subnet %s has empty CIDRBlock", subnet.Name)
		klog.Error(err)
		return err
	}

	// If NAT GW pod is not on this node, delete routes (if any) and return
	if !c.hasNatGwPodOnLocalNode(eip) {
		klog.V(3).Infof("NAT GW pod for iptables-eip %s not on local node, deleting routes if exist", eipName)
		c.deleteEIPRoutesIfExist(eip)
		return nil
	}

	// Only add routes for ready EIPs. An EIP becomes ready after NAT Gateway
	// pod successfully configures iptables rules. Before that, adding routes
	// would cause traffic to be blackholed.
	if !eip.Status.Ready {
		klog.V(3).Infof("iptables-eip %s not ready, skipping route", eipName)
		return nil
	}

	// Wait for subnet to have macvlan master interface annotation
	// This annotation is set by controller when provider NAD is macvlan type.
	// Return error to trigger retry, as the annotation may not be set yet during daemon restart.
	if subnet.Annotations[util.NadMacvlanMasterAnnotation] == "" {
		err = fmt.Errorf("subnet %s has no nad-macvlan-master annotation yet, will retry", subnet.Name)
		klog.Error(err)
		return err
	}

	var errs []error
	if eip.Spec.V4ip != "" {
		if err := addEIPRoute(eip.Spec.V4ip, cidr); err != nil {
			klog.Errorf("failed to add IPv4 route for iptables-eip %s (V4ip=%s, subnet=%s): %v", eipName, eip.Spec.V4ip, subnet.Name, err)
			errs = append(errs, err)
		}
	}
	if eip.Spec.V6ip != "" {
		if err := addEIPRoute(eip.Spec.V6ip, cidr); err != nil {
			klog.Errorf("failed to add IPv6 route for iptables-eip %s (V6ip=%s, subnet=%s): %v", eipName, eip.Spec.V6ip, subnet.Name, err)
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// isVpcNatGwPod checks if a pod is a VPC NAT Gateway pod
func isVpcNatGwPod(pod *corev1.Pod) bool {
	return pod.Labels[util.VpcNatGatewayLabel] == "true"
}

// getNatGwNameFromPod extracts the NAT GW name from a NAT GW pod via label.
func getNatGwNameFromPod(pod *corev1.Pod) string {
	return pod.Labels[util.VpcNatGatewayNameLabel]
}

// enqueueEipsForNatGw enqueues all EIPs associated with the given NAT GW for route processing.
// This is called when a NAT GW pod node changes or phase changes.
// The EIP handler will decide whether to add or delete routes based on current state.
func (c *Controller) enqueueEipsForNatGw(natGwName string) {
	eips, err := c.iptablesEipsLister.List(labels.SelectorFromSet(labels.Set{
		util.VpcNatGatewayNameLabel: natGwName,
	}))
	if err != nil {
		klog.Errorf("failed to list EIPs for NAT GW %s: %v", natGwName, err)
		return
	}

	for _, eip := range eips {
		if eip.Spec.ExternalSubnet != "" {
			klog.Infof("enqueue iptables-eip %s for NAT GW %s pod event", eip.Name, natGwName)
			c.iptablesEipQueue.Add(eip.Name)
		}
	}
}

// deleteEIPRoutesIfExist deletes EIP routes if they exist.
// This is a best-effort operation - errors are logged but not returned.
func (c *Controller) deleteEIPRoutesIfExist(eip *kubeovnv1.IptablesEIP) {
	if eip.Spec.V4ip != "" {
		if err := deleteEIPRoute(eip.Spec.V4ip); err != nil {
			klog.V(3).Infof("failed to delete IPv4 route for EIP %s (may not exist): %v", eip.Name, err)
		}
	}
	if eip.Spec.V6ip != "" {
		if err := deleteEIPRoute(eip.Spec.V6ip); err != nil {
			klog.V(3).Infof("failed to delete IPv6 route for EIP %s (may not exist): %v", eip.Name, err)
		}
	}
}

// handleNatGwPodUpdate handles NAT GW pod update events.
// When a NAT GW pod moves to/from this node or phase changes, enqueue all its EIPs for route processing.
// The EIP handler will decide whether to add or delete routes based on current pod location.
func (c *Controller) handleNatGwPodUpdate(oldPod, newPod *corev1.Pod) {
	if !isVpcNatGwPod(newPod) {
		return
	}

	oldNodeName := oldPod.Spec.NodeName
	newNodeName := newPod.Spec.NodeName

	// Skip if pod is not related to this node (neither old nor new node is this node)
	if oldNodeName != c.config.NodeName && newNodeName != c.config.NodeName {
		return
	}

	natGwName := getNatGwNameFromPod(newPod)
	if natGwName == "" {
		return
	}

	// Case 1: Pod moved from this node to another node - enqueue to delete routes
	if oldNodeName == c.config.NodeName && newNodeName != c.config.NodeName {
		klog.Infof("NAT GW pod %s moved from this node to %s, enqueuing EIPs to delete routes", newPod.Name, newNodeName)
		c.enqueueEipsForNatGw(natGwName)
		return
	}

	// Case 2: Pod moved from another node to this node - enqueue to add routes
	if oldNodeName != c.config.NodeName && newNodeName == c.config.NodeName {
		if newPod.Status.Phase == corev1.PodRunning {
			klog.Infof("NAT GW pod %s moved to this node, enqueuing EIPs to add routes", newPod.Name)
			c.enqueueEipsForNatGw(natGwName)
		}
		return
	}

	// Case 3: Pod is on this node and phase changed to Running - enqueue to add routes
	if oldPod.Status.Phase != corev1.PodRunning && newPod.Status.Phase == corev1.PodRunning {
		klog.Infof("NAT GW pod %s became running on this node, enqueuing EIPs to add routes", newPod.Name)
		c.enqueueEipsForNatGw(natGwName)
		return
	}

	// Case 4: Pod is on this node and phase changed from Running to non-Running
	// (e.g., being deleted with DeletionTimestamp set, or terminated) - enqueue to delete routes
	if oldPod.Status.Phase == corev1.PodRunning && newPod.Status.Phase != corev1.PodRunning {
		klog.Infof("NAT GW pod %s no longer running on this node (phase: %s), enqueuing EIPs to delete routes", newPod.Name, newPod.Status.Phase)
		c.enqueueEipsForNatGw(natGwName)
	}
}
