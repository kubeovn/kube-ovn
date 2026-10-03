package controller

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) enqueueAddVirtualIP(obj any) {
	key := cache.MetaObjectToName(obj.(*kubeovnv1.Vip)).String()
	klog.Infof("enqueue add vip %s", key)
	c.addVirtualIPQueue.Add(key)
}

func (c *Controller) enqueueUpdateVirtualIP(oldObj, newObj any) {
	oldVip := oldObj.(*kubeovnv1.Vip)
	newVip := newObj.(*kubeovnv1.Vip)
	key := cache.MetaObjectToName(newVip).String()
	if !newVip.DeletionTimestamp.IsZero() ||
		oldVip.Spec.MacAddress != newVip.Spec.MacAddress ||
		oldVip.Spec.V4ip != newVip.Spec.V4ip ||
		oldVip.Spec.V6ip != newVip.Spec.V6ip {
		klog.Infof("enqueue update vip %s", key)
		c.updateVirtualIPQueue.Add(key)
	}
	if !slices.Equal(oldVip.Spec.Selector, newVip.Spec.Selector) ||
		oldVip.Status.Mac != newVip.Status.Mac ||
		oldVip.Status.V4ip != newVip.Status.V4ip ||
		oldVip.Status.V6ip != newVip.Status.V6ip {
		klog.Infof("enqueue update virtual parents for %s", key)
		c.updateVirtualParentsQueue.Add(key)
	}
	if oldVip.Spec.Type != newVip.Spec.Type || !slices.Equal(oldVip.Spec.AttachSubnets, newVip.Spec.AttachSubnets) {
		klog.Infof("enqueue update vip %s for attachSubnets change", key)
		c.updateVirtualIPQueue.Add(key)
	}
}

func (c *Controller) enqueueDelVirtualIP(obj any) {
	var vip *kubeovnv1.Vip
	switch t := obj.(type) {
	case *kubeovnv1.Vip:
		vip = t
	case cache.DeletedFinalStateUnknown:
		v, ok := t.Obj.(*kubeovnv1.Vip)
		if !ok {
			klog.Warningf("unexpected object type: %T", t.Obj)
			return
		}
		vip = v
	default:
		klog.Warningf("unexpected type: %T", obj)
		return
	}

	key := cache.MetaObjectToName(vip).String()
	klog.Infof("enqueue del vip %s", key)
	c.delVirtualIPQueue.Add(vip)
}

func (c *Controller) handleAddVirtualIP(key string) error {
	cachedVip, err := c.virtualIpsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	if cachedVip.Status.Mac != "" {
		if cachedVip.Spec.Type == util.SwitchLBRuleVip {
			return c.reconcileVipAttachSubnets(cachedVip)
		}
		return nil
	}
	klog.V(3).Infof("handle add vip %s", key)

	vip := cachedVip.DeepCopy()
	var sourceV4Ip, sourceV6Ip, v4ip, v6ip, mac, subnetName string
	subnetName = vip.Spec.Subnet
	if subnetName == "" {
		return fmt.Errorf("failed to create vip '%s', subnet should be set", key)
	}
	subnet, err := c.subnetsLister.Get(subnetName)
	if err != nil {
		klog.Errorf("failed to get subnet %s: %v", subnetName, err)
		return err
	}
	portName := ovs.PodNameToPortName(vip.Name, vip.Spec.Namespace, subnet.Spec.Provider)
	sourceV4Ip = vip.Spec.V4ip
	sourceV6Ip = vip.Spec.V6ip
	// v6 ip address can not use upper case
	if util.ContainsUppercase(vip.Spec.V6ip) {
		err := fmt.Errorf("vip %s v6 ip address %s can not contain upper case", vip.Name, vip.Spec.V6ip)
		klog.Error(err)
		return err
	}
	var macPointer *string
	ipStr := util.GetStringIP(sourceV4Ip, sourceV6Ip)
	if ipStr != "" || vip.Spec.MacAddress != "" {
		if vip.Spec.MacAddress != "" {
			macPointer = &vip.Spec.MacAddress
		}
		v4ip, v6ip, mac, err = c.acquireStaticIPAddress(subnet.Name, vip.Name, portName, ipStr, macPointer)
	} else {
		// Random allocate
		v4ip, v6ip, mac, err = c.acquireIPAddress(subnet.Name, vip.Name, portName)
	}
	if err != nil {
		klog.Error(err)
		return err
	}
	if vip.Spec.Type == util.SwitchLBRuleVip {
		// create a lsp use subnet gw mac, and set it option as arp_proxy
		lrpName := fmt.Sprintf("%s-%s", subnet.Spec.Vpc, subnet.Name)
		klog.Infof("get logical router port %s", lrpName)
		lrp, err := c.getLogicalRouterPort(lrpName, false)
		if err != nil {
			klog.Errorf("failed to get lrp %s: %v", lrpName, err)
			return err
		}
		if lrp.MAC == "" {
			err = fmt.Errorf("logical router port %s should have mac", lrpName)
			klog.Error(err)
			return err
		}
		mac = lrp.MAC
		ipStr := util.GetStringIP(v4ip, v6ip)
		if err := c.createLogicalSwitchPort(subnet.Name, portName, ipStr, mac, vip.Name, vip.Spec.Namespace, false, "", "", false, nil, subnet.Spec.Vpc); err != nil {
			err = fmt.Errorf("failed to create lsp %s: %w", portName, err)
			klog.Error(err)
			return err
		}
		if err := c.setLogicalSwitchPortArpProxy(portName, true); err != nil {
			err = fmt.Errorf("failed to enable lsp arp proxy for vip %s: %w", portName, err)
			klog.Error(err)
			return err
		}
	}

	if vip.Spec.Type == util.KubeHostVMVip {
		// k8s host network pod vm use vip for its nic ip
		klog.Infof("create lsp for host network pod vm nic ip %s", vip.Name)
		ipStr := util.GetStringIP(v4ip, v6ip)
		if err := c.createLogicalSwitchPort(subnet.Name, portName, ipStr, mac, vip.Name, vip.Spec.Namespace, false, "", "", false, nil, subnet.Spec.Vpc); err != nil {
			err = fmt.Errorf("failed to create lsp %s: %w", portName, err)
			klog.Error(err)
			return err
		}
	}
	if err = c.createOrUpdateVipCR(key, vip.Spec.Namespace, subnet.Name, v4ip, v6ip, mac); err != nil {
		klog.Errorf("failed to create or update vip '%s', %v", vip.Name, err)
		return err
	}
	if vip.Spec.Type == util.KubeHostVMVip {
		// vm use the vip as its real ip
		klog.Infof("created host network pod vm ip %s", key)
		return nil
	}
	// Status updates enqueue the same reconciliation. Keep parent updates on
	// their dedicated queue so the add and status workers cannot create the
	// same virtual port concurrently.
	c.updateVirtualParentsQueue.Add(key)
	if vip.Spec.Type == util.SwitchLBRuleVip {
		if err := c.reconcileVipAttachSubnets(vip); err != nil {
			return err
		}
	}

	// Trigger subnet status update after IPAM allocation and VIP persistence.
	// Parent reconciliation is queued above and runs independently.
	c.updateSubnetStatusQueue.Add(subnetName)
	return nil
}

func (c *Controller) handleUpdateVirtualIP(key string) error {
	cachedVip, err := c.virtualIpsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	vip := cachedVip.DeepCopy()
	// should delete
	if !vip.DeletionTimestamp.IsZero() {
		klog.Infof("handle deleting vip %s", vip.Name)
		// Clean up resources before removing finalizer
		if vip.Spec.Type != "" {
			subnet, err := c.subnetsLister.Get(vip.Spec.Subnet)
			if err != nil {
				klog.Errorf("failed to get subnet %s: %v", vip.Spec.Subnet, err)
				return err
			}
			portName := ovs.PodNameToPortName(vip.Name, vip.Spec.Namespace, subnet.Spec.Provider)
			klog.Infof("delete vip lsp %s", portName)
			if err := c.deleteLogicalSwitchPort(portName); err != nil {
				err = fmt.Errorf("failed to delete lsp %s: %w", vip.Name, err)
				klog.Error(err)
				return err
			}
		}
		if err := c.deleteVirtualVipPorts(vip); err != nil {
			return err
		}
		// Release IP from IPAM before removing finalizer
		c.ipam.ReleaseAddressByPod(vip.Name, vip.Spec.Subnet)
		if vip.Spec.Type == util.SwitchLBRuleVip || vip.Status.Type == util.SwitchLBRuleVip {
			if err := c.detachAllVipAttachSubnets(vip); err != nil {
				return err
			}
		}

		// Now remove finalizer, which will trigger subnet status update
		if err = c.handleDelVipFinalizer(key); err != nil {
			klog.Errorf("failed to handle vip finalizer %v", err)
			return err
		}
		return nil
	}
	// v6 ip address can not use upper case
	if util.ContainsUppercase(vip.Spec.V6ip) {
		err := fmt.Errorf("vip %s v6 ip address %s can not contain upper case", vip.Name, vip.Spec.V6ip)
		klog.Error(err)
		return err
	}
	// not support change
	if vip.Status.Mac != "" && vip.Status.Mac != vip.Spec.MacAddress {
		err = errors.New("not support change mac of vip")
		klog.Errorf("%v", err)
		return err
	}
	if vip.Status.V4ip != "" && vip.Status.V4ip != vip.Spec.V4ip {
		err = errors.New("not support change v4 ip of vip")
		klog.Errorf("%v", err)
		return err
	}
	if vip.Status.V6ip != "" && vip.Status.V6ip != vip.Spec.V6ip {
		err = errors.New("not support change v6 ip of vip")
		klog.Errorf("%v", err)
		return err
	}
	// should update
	if vip.Status.Mac == "" {
		if err = c.createOrUpdateVipCR(key, vip.Spec.Namespace, vip.Spec.Subnet,
			vip.Spec.V4ip, vip.Spec.V6ip, vip.Spec.MacAddress); err != nil {
			klog.Error(err)
			return err
		}
	}
	// Always ensure finalizer is added regardless of Status
	if err = c.handleAddOrUpdateVipFinalizer(key); err != nil {
		klog.Errorf("failed to handle vip finalizer %v", err)
		return err
	}
	if vip.Spec.Type == util.SwitchLBRuleVip {
		if err := c.reconcileVipAttachSubnets(vip); err != nil {
			return err
		}
	} else if vip.Status.Type == util.SwitchLBRuleVip {
		if err := c.detachAllVipAttachSubnets(vip); err != nil {
			return err
		}
		if err := c.clearVipAttachSubnetsAnnotation(vip); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) handleDelVirtualIP(vip *kubeovnv1.Vip) error {
	// Cleanup is now handled in handleUpdateVirtualIP before finalizer removal
	// This function is kept for compatibility with the delete queue
	klog.V(3).Infof("vip %s cleanup already done in update handler", vip.Name)

	// For VIPs deleted without finalizer (race condition or direct deletion),
	// we need to ensure subnet status is updated as a safety net.
	if vip.Spec.Subnet != "" {
		c.updateSubnetStatusQueue.Add(vip.Spec.Subnet)
	}

	return nil
}

func (c *Controller) handleUpdateVirtualParents(key string) error {
	cachedVip, err := c.virtualIpsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	if cachedVip.Spec.Type == util.KubeHostVMVip {
		// vm use the vip as its real ip
		klog.Infof("created host network pod vm ip %s", key)
		return nil
	}
	// only pods in the same namespace as vip are allowed to use aap
	if (cachedVip.Status.V4ip == "" && cachedVip.Status.V6ip == "") || cachedVip.Spec.Namespace == "" {
		return nil
	}

	virtualPorts := virtualVipPorts(cachedVip)
	for _, virtualPort := range virtualPorts {
		if err = c.ensureVirtualVipPort(cachedVip, virtualPort); err != nil {
			return err
		}
	}

	// update virtual parents
	if cachedVip.Spec.Type == util.SwitchLBRuleVip {
		// switch lb rule vip no need to have virtual parents
		return nil
	}

	virtualParents, parentNodes, err := c.virtualVipParents(cachedVip)
	if err != nil {
		return err
	}
	parents := strings.Join(virtualParents, ",")
	for _, virtualPort := range virtualPorts {
		if err = c.setVirtualLogicalSwitchPortVirtualParents(virtualPort.name, parents); err != nil {
			klog.Errorf("set vip %s virtual parents %s: %v", virtualPort.name, parents, err)
			return err
		}
	}
	if err = c.syncVirtualVipPortGroups(cachedVip, virtualPorts, parentNodes); err != nil {
		klog.Errorf("sync virtual port %s distributed subnet port groups: %v", cachedVip.Name, err)
		return err
	}

	return nil
}

type virtualVipPort struct {
	name string
	ip   string
}

func virtualVipPorts(vip *kubeovnv1.Vip) []virtualVipPort {
	v4ip := cmp.Or(vip.Status.V4ip, vip.Spec.V4ip)
	v6ip := cmp.Or(vip.Status.V6ip, vip.Spec.V6ip)
	ports := make([]virtualVipPort, 0, 2)
	if util.IsValidIP(v4ip) {
		ports = append(ports, virtualVipPort{name: "vip:" + vip.Name + ":ipv4", ip: v4ip})
	}
	if util.IsValidIP(v6ip) {
		ports = append(ports, virtualVipPort{name: "vip:" + vip.Name + ":ipv6", ip: v6ip})
	}
	return ports
}

func (c *Controller) ensureVirtualVipPort(vip *kubeovnv1.Vip, virtualPort virtualVipPort) error {
	if err := c.createVirtualLogicalSwitchPort(virtualPort.name, vip.Spec.Subnet, virtualPort.ip); err != nil {
		klog.Errorf("create virtual port with vip %s from logical switch %s: %v", virtualPort.name, vip.Spec.Subnet, err)
		return err
	}
	if vip.Spec.Type == util.SwitchLBRuleVip || vip.Status.Mac == "" {
		return nil
	}

	addresses := vip.Status.Mac + " " + virtualPort.ip
	if err := c.setVirtualLogicalSwitchPortAddresses(virtualPort.name, addresses); err != nil {
		klog.Errorf("set virtual port %s addresses %s: %v", virtualPort.name, addresses, err)
		return err
	}
	return nil
}

func (c *Controller) virtualVipParents(vip *kubeovnv1.Vip) ([]string, map[string]struct{}, error) {
	// vip cloud use selector to select pods as its virtual parents
	matchLabels := make(map[string]string)
	for _, v := range vip.Spec.Selector {
		key, value, ok := strings.Cut(strings.TrimSpace(v), ":")
		if !ok {
			continue
		}
		matchLabels[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	selector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{MatchLabels: matchLabels})
	if err != nil {
		klog.Errorf("failed to convert label selector %v: %v", matchLabels, err)
		return nil, nil, err
	}
	pods, err := c.podsLister.Pods(vip.Spec.Namespace).List(selector)
	if err != nil {
		klog.Errorf("failed to list pods that meet selector requirements, %v", err)
		return nil, nil, err
	}

	var virtualParents []string
	parentNodes := make(map[string]struct{})
	for _, pod := range pods {
		if pod.Annotations == nil {
			// pod has no annotations
			continue
		}
		if aaps := strings.Split(pod.Annotations[util.AAPsAnnotation], ","); !slices.Contains(aaps, vip.Name) {
			continue
		}
		podName := c.getNameByPod(pod)
		podNets, err := c.getPodKubeovnNets(pod)
		if err != nil {
			klog.Errorf("failed to get pod nets %v", err)
			return nil, nil, err
		}
		for _, podNet := range podNets {
			// Skip non-OVN subnets that don't create OVN logical switch ports
			if !isOvnSubnet(podNet.Subnet) {
				continue
			}

			if podNet.Subnet.Name == vip.Spec.Subnet {
				portName := ovs.PodNameToPortName(podName, pod.Namespace, podNet.ProviderName)
				virtualParents = append(virtualParents, portName)
				if pod.Spec.NodeName != "" {
					parentNodes[pod.Spec.NodeName] = struct{}{}
				}
				key := cache.MetaObjectToName(pod).String()
				klog.Infof("enqueue update pod security for %s", key)
				c.updatePodSecurityQueue.Add(key)
				break
			}
		}
	}
	return virtualParents, parentNodes, nil
}

func (c *Controller) syncVirtualVipPortGroups(vip *kubeovnv1.Vip, virtualPorts []virtualVipPort, parentNodes map[string]struct{}) error {
	subnet, err := c.subnetsLister.Get(vip.Spec.Subnet)
	if err != nil {
		return fmt.Errorf("get subnet %s: %w", vip.Spec.Subnet, err)
	}
	if subnet.Spec.GatewayType != kubeovnv1.GWDistributedType ||
		subnet.Spec.Vpc != c.config.ClusterRouter ||
		(subnet.Spec.Vlan != "" && !subnet.Spec.LogicalGateway) ||
		subnet.Name == c.config.NodeSwitch {
		return nil
	}

	portGroups, err := c.listPortGroups(map[string]string{
		"subnet":         subnet.Name,
		"node":           "",
		networkPolicyKey: "",
	})
	if err != nil {
		return fmt.Errorf("list port groups for subnet %s: %w", subnet.Name, err)
	}

	portGroupsByNode := make(map[string]string, len(portGroups))
	stalePortGroupNames := make([]string, 0, len(portGroups))
	for _, portGroup := range portGroups {
		nodeName := portGroup.ExternalIDs["node"]
		if nodeName != "" {
			portGroupsByNode[nodeName] = portGroup.Name
		}
		if _, desired := parentNodes[nodeName]; !desired {
			stalePortGroupNames = append(stalePortGroupNames, portGroup.Name)
		}
	}

	for _, virtualPort := range virtualPorts {
		for nodeName := range parentNodes {
			pgName, ok := portGroupsByNode[nodeName]
			if !ok {
				continue
			}
			if err = c.updatePortGroupPorts(pgName, ovsdb.MutateOperationInsert, virtualPort.name); err != nil {
				return fmt.Errorf("add virtual port %s to port group %s: %w", virtualPort.name, pgName, err)
			}
		}
		if len(stalePortGroupNames) > 0 {
			if err = c.removePortFromPortGroups(virtualPort.name, stalePortGroupNames...); err != nil {
				return fmt.Errorf("remove virtual port %s from old port groups: %w", virtualPort.name, err)
			}
		}
	}
	return nil
}

func (c *Controller) deleteVirtualVipPorts(vip *kubeovnv1.Vip) error {
	portNames := make(map[string]struct{})
	for _, virtualPort := range virtualVipPorts(vip) {
		portNames[virtualPort.name] = struct{}{}
	}

	if vip.Spec.Subnet != "" {
		lsps, err := c.listLogicalSwitchPorts(true, map[string]string{logicalSwitchKey: vip.Spec.Subnet}, func(lsp *ovnnb.LogicalSwitchPort) bool {
			if lsp.Type != "virtual" {
				return false
			}
			if lsp.Name == vip.Name || lsp.Name == "vip:"+vip.Name+":ipv4" || lsp.Name == "vip:"+vip.Name+":ipv6" {
				return true
			}
			return false
		})
		if err != nil {
			return fmt.Errorf("list virtual ports for vip %s from subnet %s: %w", vip.Name, vip.Spec.Subnet, err)
		}
		for _, lsp := range lsps {
			portNames[lsp.Name] = struct{}{}
		}
	}

	if len(portNames) == 0 {
		portNames[vip.Name] = struct{}{}
	}
	names := make([]string, 0, len(portNames))
	for name := range portNames {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if err := c.deleteLogicalSwitchPort(name); err != nil {
			klog.Errorf("delete virtual logical switch port %s from logical switch %s: %v", name, vip.Spec.Subnet, err)
			return err
		}
	}
	return nil
}

func (c *Controller) createOrUpdateVipCR(key, ns, subnet, v4ip, v6ip, mac string) error {
	vipCR, err := c.virtualIpsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			// Create CR with finalizer, labels and status all at once
			if _, err := c.config.KubeOvnClient.KubeovnV1().Vips().Create(context.Background(), &kubeovnv1.Vip{
				Name:       key,
				Namespace:  ns,
				Finalizers: []string{util.KubeOVNControllerFinalizer},
				Labels: map[string]string{
					util.SubnetNameLabel: subnet,
					util.IPReservedLabel: "",
				},
				Spec: kubeovnv1.VipSpec{
					Namespace:  ns,
					Subnet:     subnet,
					V4ip:       v4ip,
					V6ip:       v6ip,
					MacAddress: mac,
				},
				Status: kubeovnv1.VipStatus{
					V4ip: v4ip,
					V6ip: v6ip,
					Mac:  mac,
				},
			}, metav1.CreateOptions{}); err != nil {
				err := fmt.Errorf("failed to create crd vip '%s', %w", key, err)
				klog.Error(err)
				return err
			}
		} else {
			err := fmt.Errorf("failed to get crd vip '%s', %w", key, err)
			klog.Error(err)
			return err
		}
	} else {
		vip := vipCR.DeepCopy()

		// Ensure labels are set correctly
		if vip.Labels == nil {
			vip.Labels = make(map[string]string)
		}
		vip.Labels[util.SubnetNameLabel] = subnet
		vip.Labels[util.IPReservedLabel] = ""

		if vip.Status.Mac == "" && mac != "" ||
			vip.Status.V4ip == "" && v4ip != "" ||
			vip.Status.V6ip == "" && v6ip != "" {
			// vip spec mac or ip not support to update
			// only set once during creation
			vip.Spec.Namespace = ns
			vip.Spec.V4ip = v4ip
			vip.Spec.V6ip = v6ip
			vip.Spec.MacAddress = mac

			vip.Status.V4ip = v4ip
			vip.Status.V6ip = v6ip
			vip.Status.Mac = mac
			vip.Status.Type = vip.Spec.Type

			// Ensure finalizer is added atomically with status initialization,
			// preventing a race where WaitToBeReady returns (V4ip is set) before
			// handleUpdateVirtualIP has a chance to add the finalizer.
			controllerutil.RemoveFinalizer(vip, util.DeprecatedFinalizerName)
			controllerutil.AddFinalizer(vip, util.KubeOVNControllerFinalizer)

			// Update with labels, spec, status, and finalizer in one call
			if _, err := c.config.KubeOvnClient.KubeovnV1().Vips().Update(context.Background(), vip, metav1.UpdateOptions{}); err != nil {
				err := fmt.Errorf("failed to update vip '%s', %w", key, err)
				klog.Error(err)
				return err
			}
		}
	}
	// Trigger subnet status update after CR creation or update
	c.updateSubnetStatusQueue.AddAfter(subnet, 300*time.Millisecond)
	return nil
}

func (c *Controller) podReuseVip(vipName, portName string, keepVIP bool) error {
	// when pod use static vip, label vip reserved for pod
	oriVip, err := c.virtualIpsLister.Get(vipName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	vip := oriVip.DeepCopy()
	if vip.Labels == nil {
		vip.Labels = map[string]string{}
	}
	var op string

	if vip.Labels[util.IPReservedLabel] != "" {
		if keepVIP && vip.Labels[util.IPReservedLabel] == portName {
			return nil
		}
		return fmt.Errorf("vip '%s' is in use by pod %s", vip.Name, vip.Labels[util.IPReservedLabel])
	}
	op = "replace"
	vip.Labels[util.IPReservedLabel] = portName
	patchPayloadTemplate := `[{ "op": "%s", "path": "/metadata/labels", "value": %s }]`
	raw, _ := json.Marshal(vip.Labels)
	patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
	if _, err = c.config.KubeOvnClient.KubeovnV1().Vips().Patch(context.Background(), vip.Name, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{}); err != nil {
		klog.Errorf("failed to patch label for vip '%s', %v", vip.Name, err)
		return err
	}
	c.ipam.ReleaseAddressByPod(vipName, vip.Spec.Subnet)
	return nil
}

func (c *Controller) releaseVip(key string) (bool, error) {
	// clean vip label when pod delete
	oriVip, err := c.virtualIpsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return false, nil
		}
		klog.Error(err)
		return false, err
	}
	vip := oriVip.DeepCopy()
	if vip.Labels[util.IPReservedLabel] == "" {
		return false, nil
	}
	vip.Labels[util.IPReservedLabel] = ""
	klog.V(3).Infof("clean reserved label from vip %s", key)
	patchPayloadTemplate := `[{ "op": "replace", "path": "/metadata/labels", "value": %s }]`
	raw, _ := json.Marshal(vip.Labels)
	patchPayload := fmt.Sprintf(patchPayloadTemplate, raw)
	if _, err := c.config.KubeOvnClient.KubeovnV1().Vips().Patch(context.Background(), vip.Name,
		types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{}); err != nil {
		klog.Errorf("failed to patch label for vip '%s', %v", vip.Name, err)
		return false, err
	}
	mac := &vip.Status.Mac
	if vip.Status.Mac == "" {
		mac = nil
	}
	if _, _, _, err = c.ipam.GetStaticAddress(key, vip.Name, vip.Status.V4ip, mac, vip.Spec.Subnet, false); err != nil {
		klog.Errorf("failed to recover IPAM from vip CR %s: %v", vip.Name, err)
	}
	return true, nil
}

func (c *Controller) handleAddOrUpdateVipFinalizer(key string) error {
	cachedVip, err := c.virtualIpsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	if !cachedVip.DeletionTimestamp.IsZero() {
		return nil
	}
	newVip := cachedVip.DeepCopy()
	controllerutil.RemoveFinalizer(newVip, util.DeprecatedFinalizerName)
	controllerutil.AddFinalizer(newVip, util.KubeOVNControllerFinalizer)
	patch, err := util.GenerateMergePatchPayload(cachedVip, newVip)
	if err != nil {
		klog.Errorf("failed to generate patch payload for ovn eip '%s', %v", cachedVip.Name, err)
		return err
	}
	if _, err := c.config.KubeOvnClient.KubeovnV1().Vips().Patch(context.Background(), cachedVip.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to add finalizer for vip '%s', %v", cachedVip.Name, err)
		return err
	}

	// Trigger subnet status update after finalizer is processed as a fallback
	// This handles cases where finalizer was not added during creation
	// AddFinalizer is idempotent, so this is safe even if finalizer already exists
	c.updateSubnetStatusQueue.Add(cachedVip.Spec.Subnet)
	return nil
}

func (c *Controller) handleDelVipFinalizer(key string) error {
	cachedVip, err := c.virtualIpsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	if len(cachedVip.GetFinalizers()) == 0 {
		return nil
	}
	newVip := cachedVip.DeepCopy()
	controllerutil.RemoveFinalizer(newVip, util.DeprecatedFinalizerName)
	controllerutil.RemoveFinalizer(newVip, util.KubeOVNControllerFinalizer)
	patch, err := util.GenerateMergePatchPayload(cachedVip, newVip)
	if err != nil {
		klog.Errorf("failed to generate patch payload for ovn eip '%s', %v", cachedVip.Name, err)
		return err
	}
	if _, err := c.config.KubeOvnClient.KubeovnV1().Vips().Patch(context.Background(), cachedVip.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to remove finalizer from vip '%s', %v", cachedVip.Name, err)
		return err
	}

	// Trigger subnet status update after finalizer is removed
	// This ensures subnet status reflects the IP release
	// Add delay to ensure API server completes the finalizer removal
	c.updateSubnetStatusQueue.AddAfter(cachedVip.Spec.Subnet, 300*time.Millisecond)
	return nil
}

func normalizeVipAttachSubnets(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func vipAttachSubnets(vip *kubeovnv1.Vip) []string {
	values := slices.Clone(vip.Spec.AttachSubnets)
	if annotation := vip.Annotations[util.VipAttachSubnetsAnnotation]; annotation != "" {
		values = append(values, strings.Split(annotation, ",")...)
	}
	return normalizeVipAttachSubnets(values)
}

func vipVpcLoadBalancerNames(vpc *kubeovnv1.Vpc) []string {
	return normalizeVipAttachSubnets([]string{
		vpc.Status.TCPLoadBalancer,
		vpc.Status.TCPSessionLoadBalancer,
		vpc.Status.UDPLoadBalancer,
		vpc.Status.UDPSessionLoadBalancer,
		vpc.Status.SctpLoadBalancer,
		vpc.Status.SctpSessionLoadBalancer,
	})
}

func vipAddresses(vip *kubeovnv1.Vip) []string {
	addresses := make([]string, 0, 2)
	for _, address := range []string{vip.Status.V4ip, vip.Status.V6ip, vip.Spec.V4ip, vip.Spec.V6ip} {
		address = strings.TrimSpace(address)
		if address == "" || slices.Contains(addresses, address) {
			continue
		}
		addresses = append(addresses, address)
	}
	return addresses
}

func vipMatchesServiceAddress(vip *kubeovnv1.Vip, svc *corev1.Service) bool {
	if vip == nil || svc == nil || vip.Spec.Type != util.SwitchLBRuleVip ||
		vip.Spec.Subnet == "" || serviceScopedLBOwner(svc).kind != switchLBRuleLBOwnerKind {
		return false
	}
	for serviceVIP := range strings.SplitSeq(svc.Annotations[util.SwitchLBRuleVipsAnnotation], ",") {
		serviceVIP = strings.TrimSpace(serviceVIP)
		if serviceVIP == "" {
			continue
		}
		if slices.Contains(vipAddresses(vip), serviceVIP) {
			return true
		}
	}
	return false
}

// switchLBRuleServicesForVip matches the Service's home logical switch as well as
// its address. Subnet names are cluster-scoped, so overlapping IPs in independent
// VPCs cannot cause attachments to cross network boundaries.
func (c *Controller) switchLBRuleServicesForVip(vip *kubeovnv1.Vip) ([]*corev1.Service, error) {
	if c.servicesLister == nil {
		return nil, nil
	}
	services, err := c.servicesLister.List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("list services for vip %s: %w", vip.Name, err)
	}
	matched := make([]*corev1.Service, 0)
	for _, svc := range services {
		// Check the owner, type and address before resolving the Service network.
		if !vipMatchesServiceAddress(vip, svc) {
			continue
		}
		subnetName := svc.Annotations[util.LogicalSwitchAnnotation]
		if subnetName == "" {
			endpointSlices, err := c.endpointSlicesLister.EndpointSlices(svc.Namespace).List(labels.Set{discoveryv1.LabelServiceName: svc.Name}.AsSelector())
			if err != nil {
				return nil, fmt.Errorf("list endpoint slices for service %s/%s: %w", svc.Namespace, svc.Name, err)
			}
			endpointSlices = filterServiceEndpointSlices(svc, endpointSlices)
			if err := c.replaceEndpointSliceSecondaryIPs(svc, endpointSlices); err != nil {
				return nil, err
			}
			_, subnetName, err = c.getVpcAndSubnetForEndpoints(endpointSlices, svc)
			if err != nil {
				return nil, err
			}
		}
		if vip.Spec.Subnet == subnetName {
			matched = append(matched, svc)
		}
	}
	return matched, nil
}

func (c *Controller) serviceScopedVipLoadBalancerNames(vip *kubeovnv1.Vip) ([]string, error) {
	services, err := c.switchLBRuleServicesForVip(vip)
	if err != nil {
		return nil, err
	}
	addresses := vipAddresses(vip)
	if len(addresses) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{})
	names := make([]string, 0)
	for _, svc := range services {
		for _, port := range svc.Spec.Ports {
			for _, address := range addresses {
				matched := false
				for serviceVIP := range strings.SplitSeq(svc.Annotations[util.SwitchLBRuleVipsAnnotation], ",") {
					if strings.TrimSpace(serviceVIP) == address {
						matched = true
						break
					}
				}
				if !matched {
					continue
				}
				name := serviceScopedExternalLBName(svc, port.Protocol, address)
				if name == "" {
					continue
				}
				if _, ok := seen[name]; ok {
					continue
				}
				seen[name] = struct{}{}
				names = append(names, name)
			}
		}
	}
	return names, nil
}

func (c *Controller) enqueueServicesForVip(vip *kubeovnv1.Vip) {
	if c.servicesLister == nil || c.addOrUpdateEndpointSliceQueue == nil {
		return
	}
	services, err := c.switchLBRuleServicesForVip(vip)
	if err != nil {
		klog.Errorf("failed to list services for vip %s endpoint reconcile: %v", vip.Name, err)
		return
	}
	for _, svc := range services {
		c.enqueueEndpointSliceService(cache.MetaObjectToName(svc).String(), svc)
	}
}

func (c *Controller) hasOtherVipAttachSubnet(vpcName, subnetName, vipName string) bool {
	if c.virtualIpsLister == nil {
		return false
	}
	vips, err := c.virtualIpsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list vips while checking attach subnet %s: %v", subnetName, err)
		return true
	}
	for _, candidate := range vips {
		if candidate.Name == vipName || candidate.Spec.Type != util.SwitchLBRuleVip {
			continue
		}
		homeSubnet, err := c.subnetsLister.Get(candidate.Spec.Subnet)
		if err != nil || homeSubnet.Spec.Vpc != vpcName {
			continue
		}
		if slices.Contains(vipAttachSubnets(candidate), subnetName) {
			return true
		}
	}
	return false
}

func (c *Controller) enqueueVipAttachSubnetsForVpc(vpcName string) {
	if c.updateVirtualIPQueue == nil || c.virtualIpsLister == nil {
		return
	}
	vips, err := c.virtualIpsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list vips for vpc %s load balancer update: %v", vpcName, err)
		return
	}
	for _, vip := range vips {
		if vip.Spec.Type != util.SwitchLBRuleVip {
			continue
		}
		homeSubnet, err := c.subnetsLister.Get(vip.Spec.Subnet)
		if err == nil && homeSubnet.Spec.Vpc == vpcName {
			c.updateVirtualIPQueue.Add(vip.Name)
		}
	}
}

// reconcileVipAttachSubnets attaches the home VPC's LBs to every subnet listed in
// vip.Spec.AttachSubnets and detaches them from any subnet that was previously
// attached (tracked via annotation) but is no longer in the spec.
func (c *Controller) reconcileVipAttachSubnets(vip *kubeovnv1.Vip) error {
	homeSubnet, err := c.subnetsLister.Get(vip.Spec.Subnet)
	if err != nil {
		klog.Errorf("failed to get subnet %s for vip %s: %v", vip.Spec.Subnet, vip.Name, err)
		return err
	}
	vpc, err := c.vpcsLister.Get(homeSubnet.Spec.Vpc)
	if err != nil {
		klog.Errorf("failed to get vpc %s for vip %s: %v", homeSubnet.Spec.Vpc, vip.Name, err)
		return err
	}
	lbs := vipVpcLoadBalancerNames(vpc)
	serviceLBs, err := c.serviceScopedVipLoadBalancerNames(vip)
	if err != nil {
		return err
	}
	lbs = normalizeVipAttachSubnets(append(lbs, serviceLBs...))
	c.enqueueServicesForVip(vip)
	if len(lbs) == 0 {
		return nil
	}

	previous := vipAttachSubnets(vip)
	desired := normalizeVipAttachSubnets(vip.Spec.AttachSubnets)
	for _, subnet := range previous {
		if slices.Contains(desired, subnet) || c.hasOtherVipAttachSubnet(vpc.Name, subnet, vip.Name) {
			continue
		}
		if err := c.updateLogicalSwitchLoadBalancers(subnet, ovsdb.MutateOperationDelete, lbs...); err != nil {
			return fmt.Errorf("failed to detach vpc %s load balancers from subnet %s for vip %s: %w", vpc.Name, subnet, vip.Name, err)
		}
	}
	for _, subnet := range desired {
		if err := c.updateLogicalSwitchLoadBalancers(subnet, ovsdb.MutateOperationInsert, lbs...); err != nil {
			return fmt.Errorf("failed to attach vpc %s load balancers to subnet %s for vip %s: %w", vpc.Name, subnet, vip.Name, err)
		}
	}

	desiredAnnotation := strings.Join(desired, ",")
	if vip.Annotations[util.VipAttachSubnetsAnnotation] == desiredAnnotation {
		return nil
	}
	patch := fmt.Sprintf(`{"metadata":{"annotations":{%q:%q}}}`, util.VipAttachSubnetsAnnotation, desiredAnnotation)
	if _, err := c.config.KubeOvnClient.KubeovnV1().Vips().Patch(
		context.Background(), vip.Name, types.MergePatchType, []byte(patch), metav1.PatchOptions{},
	); err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("failed to update attach-subnets annotation for vip %s: %w", vip.Name, err)
	}
	return nil
}

// detachAllVipAttachSubnets removes the home VPC's LBs from every subnet that was
// listed in vip.Spec.AttachSubnets or tracked by the annotation on deletion or type changes.
func (c *Controller) detachAllVipAttachSubnets(vip *kubeovnv1.Vip) error {
	// A type change must still find the load balancers owned by the old type.
	// Keep the informer object unchanged so Service reconciliation sees the new type.
	if vip.Spec.Type != util.SwitchLBRuleVip && vip.Status.Type == util.SwitchLBRuleVip {
		vip = vip.DeepCopy()
		vip.Spec.Type = util.SwitchLBRuleVip
	}
	toDetach := vipAttachSubnets(vip)
	if len(toDetach) == 0 {
		return nil
	}

	homeSubnet, err := c.subnetsLister.Get(vip.Spec.Subnet)
	if err != nil {
		klog.Errorf("failed to get subnet %s for vip %s: %v", vip.Spec.Subnet, vip.Name, err)
		return err
	}
	vpc, err := c.vpcsLister.Get(homeSubnet.Spec.Vpc)
	if err != nil {
		klog.Errorf("failed to get vpc %s for vip %s: %v", homeSubnet.Spec.Vpc, vip.Name, err)
		return err
	}
	lbs := vipVpcLoadBalancerNames(vpc)
	serviceLBs, err := c.serviceScopedVipLoadBalancerNames(vip)
	if err != nil {
		return err
	}
	lbs = normalizeVipAttachSubnets(append(lbs, serviceLBs...))
	c.enqueueServicesForVip(vip)
	if len(lbs) == 0 {
		return nil
	}
	for _, subnet := range toDetach {
		if c.hasOtherVipAttachSubnet(vpc.Name, subnet, vip.Name) {
			continue
		}
		if err := c.updateLogicalSwitchLoadBalancers(subnet, ovsdb.MutateOperationDelete, lbs...); err != nil {
			return fmt.Errorf("failed to detach vpc %s load balancers from subnet %s for vip %s: %w", vpc.Name, subnet, vip.Name, err)
		}
	}
	return nil
}

func (c *Controller) clearVipAttachSubnetsAnnotation(vip *kubeovnv1.Vip) error {
	if vip.Annotations == nil || vip.Annotations[util.VipAttachSubnetsAnnotation] == "" {
		return nil
	}
	patch := fmt.Sprintf(`{"metadata":{"annotations":{%q:null}}}`, util.VipAttachSubnetsAnnotation)
	if _, err := c.config.KubeOvnClient.KubeovnV1().Vips().Patch(
		context.Background(), vip.Name, types.MergePatchType, []byte(patch), metav1.PatchOptions{},
	); err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("failed to clear attach-subnets annotation for vip %s: %w", vip.Name, err)
	}
	return nil
}

func (c *Controller) syncVipFinalizer(cl client.Client) error {
	// migrate deprecated finalizer to new finalizer
	vips := &kubeovnv1.VipList{}
	return migrateFinalizers(cl, vips, func(i int) (client.Object, client.Object) {
		if i < 0 || i >= len(vips.Items) {
			return nil, nil
		}
		return vips.Items[i].DeepCopy(), vips.Items[i].DeepCopy()
	})
}
