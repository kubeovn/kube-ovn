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

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

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
		// already ok
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
		lrp, err := c.OVNNbClient.GetLogicalRouterPort(lrpName, false)
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
		if err := c.OVNNbClient.CreateLogicalSwitchPort(subnet.Name, portName, ipStr, mac, vip.Name, vip.Spec.Namespace, false, "", "", false, nil, subnet.Spec.Vpc); err != nil {
			err = fmt.Errorf("failed to create lsp %s: %w", portName, err)
			klog.Error(err)
			return err
		}
		if err := c.OVNNbClient.SetLogicalSwitchPortArpProxy(portName, true); err != nil {
			err = fmt.Errorf("failed to enable lsp arp proxy for vip %s: %w", portName, err)
			klog.Error(err)
			return err
		}
	}

	if vip.Spec.Type == util.KubeHostVMVip {
		// k8s host network pod vm use vip for its nic ip
		klog.Infof("create lsp for host network pod vm nic ip %s", vip.Name)
		ipStr := util.GetStringIP(v4ip, v6ip)
		if err := c.OVNNbClient.CreateLogicalSwitchPort(subnet.Name, portName, ipStr, mac, vip.Name, vip.Spec.Namespace, false, "", "", false, nil, subnet.Spec.Vpc); err != nil {
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
	if err := c.handleUpdateVirtualParents(key); err != nil {
		err := fmt.Errorf("error syncing virtual parents for vip '%s': %s", key, err.Error())
		klog.Error(err)
		return err
	}

	// Trigger subnet status update after all operations complete
	// At this point: IPAM allocated, VIP CR created with labels+status+finalizer
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
			if err := c.OVNNbClient.DeleteLogicalSwitchPort(portName); err != nil {
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
		if err = c.OVNNbClient.SetVirtualLogicalSwitchPortVirtualParents(virtualPort.name, parents); err != nil {
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
		ports = append(ports, virtualVipPort{name: vip.Name, ip: v4ip})
	}
	if util.IsValidIP(v6ip) {
		name := vip.Name
		if len(ports) > 0 {
			name = fmt.Sprintf("%s-ipv6-%s", vip.Name, util.Sha256Hash([]byte(v6ip))[:8])
		}
		ports = append(ports, virtualVipPort{name: name, ip: v6ip})
	}
	return ports
}

func (c *Controller) ensureVirtualVipPort(vip *kubeovnv1.Vip, virtualPort virtualVipPort) error {
	if err := c.OVNNbClient.CreateVirtualLogicalSwitchPort(virtualPort.name, vip.Spec.Subnet, virtualPort.ip); err != nil {
		klog.Errorf("create virtual port with vip %s from logical switch %s: %v", virtualPort.name, vip.Spec.Subnet, err)
		return err
	}
	if vip.Spec.Type == util.SwitchLBRuleVip || vip.Status.Mac == "" {
		return nil
	}

	addresses := vip.Status.Mac + " " + virtualPort.ip
	if err := c.OVNNbClient.SetVirtualLogicalSwitchPortAddresses(virtualPort.name, addresses); err != nil {
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

	portGroups, err := c.OVNNbClient.ListPortGroups(map[string]string{
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
			if err = c.OVNNbClient.PortGroupAddPorts(pgName, virtualPort.name); err != nil {
				return fmt.Errorf("add virtual port %s to port group %s: %w", virtualPort.name, pgName, err)
			}
		}
		if len(stalePortGroupNames) > 0 {
			if err = c.OVNNbClient.RemovePortFromPortGroups(virtualPort.name, stalePortGroupNames...); err != nil {
				return fmt.Errorf("remove virtual port %s from old port groups: %w", virtualPort.name, err)
			}
		}
	}
	return nil
}

func (c *Controller) deleteVirtualVipPorts(vip *kubeovnv1.Vip) error {
	portNames := make(map[string]struct{})
	virtualIPs := make(map[string]struct{})
	for _, virtualPort := range virtualVipPorts(vip) {
		portNames[virtualPort.name] = struct{}{}
		virtualIPs[virtualPort.ip] = struct{}{}
	}

	if vip.Spec.Subnet != "" {
		lsps, err := c.OVNNbClient.ListLogicalSwitchPorts(true, map[string]string{logicalSwitchKey: vip.Spec.Subnet}, func(lsp *ovnnb.LogicalSwitchPort) bool {
			if lsp.Type != "virtual" {
				return false
			}
			if lsp.Name == vip.Name {
				return true
			}
			if !strings.HasPrefix(lsp.Name, vip.Name+"-ipv6-") {
				return false
			}
			if len(virtualIPs) == 0 {
				return true
			}
			_, ok := virtualIPs[lsp.Options["virtual-ip"]]
			return ok
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
		if err := c.OVNNbClient.DeleteLogicalSwitchPort(name); err != nil {
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
