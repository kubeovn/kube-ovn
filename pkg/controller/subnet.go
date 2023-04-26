package controller

import (
	"context"
	"fmt"
	"net"
	"reflect"
	"sort"
	"strings"
	"time"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ipam"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/ovn-org/libovsdb/ovsdb"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

func (c *Controller) enqueueAddSubnet(obj interface{}) {

	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue add subnet %s", key)
	c.addOrUpdateSubnetQueue.Add(key)
}

func (c *Controller) enqueueDeleteSubnet(obj interface{}) {

	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue delete subnet %s", key)
	subnet := obj.(*kubeovnv1.Subnet)
	c.deleteSubnetQueue.Add(obj)
	if subnet.Spec.GatewayType == kubeovnv1.GWCentralizedType {
		c.deleteRouteQueue.Add(obj)
	}
}

func (c *Controller) enqueueUpdateSubnet(old, new interface{}) {
	oldSubnet := old.(*kubeovnv1.Subnet)
	newSubnet := new.(*kubeovnv1.Subnet)

	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(new); err != nil {
		utilruntime.HandleError(err)
		return
	}

	var usingIPs float64
	if newSubnet.Spec.Protocol == kubeovnv1.ProtocolIPv6 {
		usingIPs = newSubnet.Status.V6UsingIPs
	} else {
		usingIPs = newSubnet.Status.V4UsingIPs
	}
	u2oInterconnIP := newSubnet.Status.U2OInterconnectionIP
	if !newSubnet.DeletionTimestamp.IsZero() && usingIPs == 0 || (usingIPs == 1 && u2oInterconnIP != "") {
		c.addOrUpdateSubnetQueue.Add(key)
		return
	}

	if oldSubnet.Spec.Private != newSubnet.Spec.Private ||
		oldSubnet.Spec.CIDRBlock != newSubnet.Spec.CIDRBlock ||
		!reflect.DeepEqual(oldSubnet.Spec.AllowSubnets, newSubnet.Spec.AllowSubnets) ||
		!reflect.DeepEqual(oldSubnet.Spec.Namespaces, newSubnet.Spec.Namespaces) ||
		oldSubnet.Spec.GatewayType != newSubnet.Spec.GatewayType ||
		oldSubnet.Spec.GatewayNode != newSubnet.Spec.GatewayNode ||
		oldSubnet.Spec.LogicalGateway != newSubnet.Spec.LogicalGateway ||
		oldSubnet.Spec.Gateway != newSubnet.Spec.Gateway ||
		!reflect.DeepEqual(oldSubnet.Spec.ExcludeIps, newSubnet.Spec.ExcludeIps) ||
		!reflect.DeepEqual(oldSubnet.Spec.Vips, newSubnet.Spec.Vips) ||
		oldSubnet.Spec.Vlan != newSubnet.Spec.Vlan ||
		oldSubnet.Spec.EnableDHCP != newSubnet.Spec.EnableDHCP ||
		oldSubnet.Spec.DHCPv4Options != newSubnet.Spec.DHCPv4Options ||
		oldSubnet.Spec.DHCPv6Options != newSubnet.Spec.DHCPv6Options ||
		oldSubnet.Spec.EnableIPv6RA != newSubnet.Spec.EnableIPv6RA ||
		oldSubnet.Spec.IPv6RAConfigs != newSubnet.Spec.IPv6RAConfigs ||
		oldSubnet.Spec.Protocol != newSubnet.Spec.Protocol ||
		(oldSubnet.Spec.EnableLb == nil && newSubnet.Spec.EnableLb != nil) ||
		(oldSubnet.Spec.EnableLb != nil && newSubnet.Spec.EnableLb == nil) ||
		(oldSubnet.Spec.EnableLb != nil && newSubnet.Spec.EnableLb != nil && *oldSubnet.Spec.EnableLb != *newSubnet.Spec.EnableLb) ||
		oldSubnet.Spec.EnableEcmp != newSubnet.Spec.EnableEcmp ||
		!reflect.DeepEqual(oldSubnet.Spec.Acls, newSubnet.Spec.Acls) ||
		oldSubnet.Spec.U2OInterconnection != newSubnet.Spec.U2OInterconnection {
		klog.V(3).Infof("enqueue update subnet %s", key)

		if oldSubnet.Spec.GatewayType != newSubnet.Spec.GatewayType {
			c.recorder.Eventf(newSubnet, v1.EventTypeNormal, "SubnetGatewayTypeChanged",
				"subnet gateway type changes from %s to %s ", oldSubnet.Spec.GatewayType, newSubnet.Spec.GatewayType)
		}

		if oldSubnet.Spec.GatewayNode != newSubnet.Spec.GatewayNode {
			c.recorder.Eventf(newSubnet, v1.EventTypeNormal, "SubnetGatewayNodeChanged",
				"gateway node changes from %s to %s ", oldSubnet.Spec.GatewayNode, newSubnet.Spec.GatewayNode)
		}

		c.addOrUpdateSubnetQueue.Add(key)
	}
}

func (c *Controller) runAddSubnetWorker() {
	for c.processNextAddSubnetWorkItem() {
	}
}

func (c *Controller) runUpdateSubnetStatusWorker() {
	for c.processNextUpdateSubnetStatusWorkItem() {
	}
}

func (c *Controller) runDeleteRouteWorker() {
	for c.processNextDeleteRoutePodWorkItem() {
	}
}

func (c *Controller) runDeleteSubnetWorker() {
	for c.processNextDeleteSubnetWorkItem() {
	}
}

func (c *Controller) runSyncVirtualPortsWorker() {
	for c.processNextSyncVirtualPortsWorkItem() {
	}
}

func (c *Controller) processNextSyncVirtualPortsWorkItem() bool {
	obj, shutdown := c.syncVirtualPortsQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.syncVirtualPortsQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.syncVirtualPortsQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.syncVirtualPort(key); err != nil {
			c.syncVirtualPortsQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.syncVirtualPortsQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextAddSubnetWorkItem() bool {
	obj, shutdown := c.addOrUpdateSubnetQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.addOrUpdateSubnetQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.addOrUpdateSubnetQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleAddOrUpdateSubnet(key); err != nil {
			c.addOrUpdateSubnetQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.addOrUpdateSubnetQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextDeleteRoutePodWorkItem() bool {
	obj, shutdown := c.deleteRouteQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.deleteRouteQueue.Done(obj)
		var subnet *kubeovnv1.Subnet
		var ok bool
		if subnet, ok = obj.(*kubeovnv1.Subnet); !ok {
			c.deleteRouteQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected subnet in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleDeleteRoute(subnet); err != nil {
			c.deleteRouteQueue.AddRateLimited(subnet)
			return fmt.Errorf("error syncing '%s': %s, requeuing", subnet.Name, err.Error())
		}
		c.deleteRouteQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextUpdateSubnetStatusWorkItem() bool {
	obj, shutdown := c.updateSubnetStatusQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.updateSubnetStatusQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.updateSubnetStatusQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleUpdateSubnetStatus(key); err != nil {
			c.updateSubnetStatusQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.updateSubnetStatusQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextDeleteSubnetWorkItem() bool {
	obj, shutdown := c.deleteSubnetQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.deleteSubnetQueue.Done(obj)
		var subnet *kubeovnv1.Subnet
		var ok bool
		if subnet, ok = obj.(*kubeovnv1.Subnet); !ok {
			c.deleteSubnetQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected subnet in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleDeleteSubnet(subnet); err != nil {
			c.deleteSubnetQueue.AddRateLimited(obj)
			return fmt.Errorf("error syncing '%s': %s, requeuing", subnet.Name, err.Error())
		}
		c.deleteSubnetQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func formatSubnet(subnet *kubeovnv1.Subnet, c *Controller) error {
	var err error
	changed := false

	changed, err = checkSubnetChanged(subnet)
	if err != nil {
		return err
	}
	if subnet.Spec.Provider == "" {
		subnet.Spec.Provider = util.OvnProvider
		changed = true
	}
	newCIDRBlock := subnet.Spec.CIDRBlock
	if subnet.Spec.Protocol != util.CheckProtocol(newCIDRBlock) {
		subnet.Spec.Protocol = util.CheckProtocol(subnet.Spec.CIDRBlock)
		changed = true
	}
	if subnet.Spec.GatewayType == "" {
		subnet.Spec.GatewayType = kubeovnv1.GWDistributedType
		changed = true
	}
	if subnet.Spec.Vpc == "" {
		if subnet.Spec.Provider != "" && !strings.HasSuffix(subnet.Spec.Provider, util.OvnProvider) {
			klog.Infof("subnet %s is not ovn subnet, no vpc", subnet.Name)
		} else {
			changed = true
			subnet.Spec.Vpc = util.DefaultVpc
		}
		// Some features only work in the default VPC
		if subnet.Spec.Default && subnet.Name != c.config.DefaultLogicalSwitch {
			subnet.Spec.Default = false
		}
		if subnet.Spec.Vlan != "" {
			if _, err := c.vlansLister.Get(subnet.Spec.Vlan); err != nil {
				err = fmt.Errorf("failed to get vlan %s: %s", subnet.Spec.Vlan, err)
				klog.Error(err)
				return err
			}
		}
	}

	if subnet.Spec.EnableLb == nil && subnet.Name != c.config.NodeSwitch {
		changed = true
		subnet.Spec.EnableLb = &c.config.EnableLb
	}
	// set join subnet Spec.EnableLb to nil
	if subnet.Spec.EnableLb != nil && subnet.Name == c.config.NodeSwitch {
		changed = true
		subnet.Spec.EnableLb = nil
	}

	klog.Infof("format subnet %v, changed %v", subnet.Name, changed)
	if changed {
		_, err = c.config.KubeOvnClient.KubeovnV1().Subnets().Update(context.Background(), subnet, metav1.UpdateOptions{})
		if err != nil {
			klog.Errorf("failed to update subnet %s, %v", subnet.Name, err)
			return err
		}
	}
	return nil
}

func checkSubnetChanged(subnet *kubeovnv1.Subnet) (bool, error) {
	var err error
	changed := false
	ret := false

	// changed value may be overlapped, so use ret to record value
	changed, err = checkAndUpdateCIDR(subnet)
	if err != nil {
		return changed, err
	}
	if changed {
		ret = true
	}
	changed, err = checkAndUpdateGateway(subnet)
	if err != nil {
		return changed, err
	}
	if changed {
		ret = true
	}
	changed = checkAndUpdateExcludeIps(subnet)
	if changed {
		ret = true
	}
	return ret, nil
}

func checkAndUpdateCIDR(subnet *kubeovnv1.Subnet) (bool, error) {
	changed := false
	var cidrBlocks []string
	for _, cidr := range strings.Split(subnet.Spec.CIDRBlock, ",") {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			return false, fmt.Errorf("subnet %s cidr %s is invalid", subnet.Name, cidr)
		}
		if ipNet.String() != cidr {
			changed = true
		}
		cidrBlocks = append(cidrBlocks, ipNet.String())
	}
	subnet.Spec.CIDRBlock = strings.Join(cidrBlocks, ",")
	return changed, nil
}

func checkAndUpdateGateway(subnet *kubeovnv1.Subnet) (bool, error) {
	changed := false
	var gw string
	var err error
	if subnet.Spec.Gateway == "" {
		gw, err = util.GetGwByCidr(subnet.Spec.CIDRBlock)
	} else if util.CheckProtocol(subnet.Spec.Gateway) != util.CheckProtocol(subnet.Spec.CIDRBlock) {
		gw, err = util.AppendGwByCidr(subnet.Spec.Gateway, subnet.Spec.CIDRBlock)
	} else {
		gw = subnet.Spec.Gateway
	}
	if err != nil {
		klog.Error(err)
		return false, err
	}
	if subnet.Spec.Gateway != gw {
		subnet.Spec.Gateway = gw
		changed = true
	}

	return changed, nil
}

// this func must be called after subnet.Spec.Gateway is valued
func checkAndUpdateExcludeIps(subnet *kubeovnv1.Subnet) bool {
	changed := false
	var excludeIps []string
	excludeIps = append(excludeIps, strings.Split(subnet.Spec.Gateway, ",")...)
	sort.Strings(excludeIps)
	if len(subnet.Spec.ExcludeIps) == 0 {
		subnet.Spec.ExcludeIps = excludeIps
		changed = true
	} else {
		changed = checkAndFormatsExcludeIps(subnet)
		for _, gw := range excludeIps {
			gwExists := false
			for _, excludeIP := range subnet.Spec.ExcludeIps {
				if util.ContainsIPs(excludeIP, gw) {
					gwExists = true
					break
				}
			}
			if !gwExists {
				subnet.Spec.ExcludeIps = append(subnet.Spec.ExcludeIps, gw)
				sort.Strings(subnet.Spec.ExcludeIps)
				changed = true
			}
		}
	}
	return changed
}

func (c *Controller) handleSubnetFinalizer(subnet *kubeovnv1.Subnet) (bool, error) {
	if subnet.DeletionTimestamp.IsZero() && !util.ContainsString(subnet.Finalizers, util.ControllerName) {
		subnet.Finalizers = append(subnet.Finalizers, util.ControllerName)
		if _, err := c.config.KubeOvnClient.KubeovnV1().Subnets().Update(context.Background(), subnet, metav1.UpdateOptions{}); err != nil {
			klog.Errorf("failed to add finalizer to subnet %s, %v", subnet.Name, err)
			return false, err
		}
		// wait local cache ready
		time.Sleep(1 * time.Second)
		return false, nil
	}

	usingIps := subnet.Status.V4UsingIPs
	if util.CheckProtocol(subnet.Spec.CIDRBlock) == kubeovnv1.ProtocolIPv6 {
		usingIps = subnet.Status.V6UsingIPs
	}

	u2oInterconnIP := subnet.Status.U2OInterconnectionIP
	if !subnet.DeletionTimestamp.IsZero() && usingIps == 0 || (usingIps == 1 && u2oInterconnIP != "") {
		subnet.Finalizers = util.RemoveString(subnet.Finalizers, util.ControllerName)
		if _, err := c.config.KubeOvnClient.KubeovnV1().Subnets().Update(context.Background(), subnet, metav1.UpdateOptions{}); err != nil {
			klog.Errorf("failed to remove finalizer from subnet %s, %v", subnet.Name, err)
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func (c Controller) patchSubnetStatus(subnet *kubeovnv1.Subnet, reason string, errStr string) {
	if errStr != "" {
		subnet.Status.SetError(reason, errStr)
		subnet.Status.NotValidated(reason, errStr)
		subnet.Status.NotReady(reason, errStr)
		c.recorder.Eventf(subnet, v1.EventTypeWarning, reason, errStr)
	} else {
		subnet.Status.Validated(reason, "")
		c.recorder.Eventf(subnet, v1.EventTypeNormal, reason, errStr)
		if reason == "SetPrivateLogicalSwitchSuccess" ||
			reason == "ResetLogicalSwitchAclSuccess" ||
			reason == "ReconcileCentralizedGatewaySuccess" ||
			reason == "SetNonOvnSubnetSuccess" {
			subnet.Status.Ready(reason, "")
		}
	}

	bytes, err := subnet.Status.Bytes()
	if err != nil {
		klog.Error(err)
	} else {
		if _, err := c.config.KubeOvnClient.KubeovnV1().Subnets().Patch(context.Background(), subnet.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status"); err != nil {
			klog.Error("patch subnet status failed", err)
		}
	}
}

func (c *Controller) handleAddOrUpdateSubnet(key string) error {
	var err error
	c.subnetStatusKeyMutex.Lock(key)
	defer c.subnetStatusKeyMutex.Unlock(key)

	cachedSubnet, err := c.subnetsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	klog.V(4).Infof("handle add or update subnet %s", cachedSubnet.Name)

	subnet := cachedSubnet.DeepCopy()
	if err = formatSubnet(subnet, c); err != nil {
		return err
	}

	subnet, err = c.config.KubeOvnClient.KubeovnV1().Subnets().Get(context.Background(), key, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to get subnet %s error %v", key, err)
		return err
	}

	deleted, err := c.handleSubnetFinalizer(subnet)
	if err != nil {
		klog.Errorf("handle subnet finalizer failed %v", err)
		return err
	}
	if deleted {
		return nil
	}

	if subnet.Spec.LogicalGateway && subnet.Spec.U2OInterconnection {
		err = fmt.Errorf("logicalGateway and u2oInterconnection can't be opened at the same time")
		klog.Error(err)
		return err
	}

	if subnet.Spec.Protocol == kubeovnv1.ProtocolDual {
		err = calcDualSubnetStatusIP(subnet, c)
	} else {
		err = calcSubnetStatusIP(subnet, c)
	}
	if err != nil {
		klog.Errorf("calculate subnet %s used ip failed, %v", subnet.Name, err)
		return err
	}

	if err := c.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, subnet.Spec.ExcludeIps); err != nil {
		return err
	}

	if err = util.ValidateSubnet(*subnet); err != nil {
		klog.Errorf("failed to validate subnet %s, %v", subnet.Name, err)
		c.patchSubnetStatus(subnet, "ValidateLogicalSwitchFailed", err.Error())
		return err
	} else {
		c.patchSubnetStatus(subnet, "ValidateLogicalSwitchSuccess", "")
	}

	if !isOvnSubnet(subnet) {
		// subnet provider is not ovn, and vpc is empty, should not reconcile
		c.patchSubnetStatus(subnet, "SetNonOvnSubnetSuccess", "")

		subnet.Status.EnsureStandardConditions()
		klog.Infof("non ovn subnet %s is ready", subnet.Name)
		return nil
	}

	if subnet.Spec.Vpc == "" {
		// ovn subnet, but vpc is empty, should not reconcile
		err := fmt.Errorf("subnet %s has no vpc, should not reconcile", subnet.Name)
		klog.Error(err)
		return err
	}

	vpc, err := c.vpcsLister.Get(subnet.Spec.Vpc)
	if err != nil {
		klog.Errorf("failed to get subnet's vpc '%s', %v", subnet.Spec.Vpc, err)
		return err
	}

	if !vpc.Status.Standby {
		err = fmt.Errorf("the vpc '%s' not standby yet", vpc.Name)
		klog.Error(err)
		return err
	}

	if !vpc.Status.Default {
		for _, ns := range subnet.Spec.Namespaces {
			if !util.ContainsString(vpc.Spec.Namespaces, ns) {
				err = fmt.Errorf("namespace '%s' is out of range to custom vpc '%s'", ns, vpc.Name)
				klog.Error(err)
				return err
			}
		}
	} else {
		vpcs, err := c.vpcsLister.List(labels.Everything())
		if err != nil {
			klog.Errorf("failed to list vpc, %v", err)
			return err
		}
		for _, vpc := range vpcs {
			if subnet.Spec.Vpc != vpc.Name && !vpc.Status.Default && util.IsStringsOverlap(vpc.Spec.Namespaces, subnet.Spec.Namespaces) {
				err = fmt.Errorf("namespaces %v are overlap with vpc '%s'", subnet.Spec.Namespaces, vpc.Name)
				klog.Error(err)
				return err
			}
		}
	}

	if err := c.reconcileU2OInterconnectionIP(subnet); err != nil {
		klog.Errorf("failed to reconcile underlay subnet %s to overlay interconnection %v", subnet.Name, err)
		return err
	}

	subnetList, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets %v", err)
		return err
	}

	for _, sub := range subnetList {
		if sub.Spec.Vpc != subnet.Spec.Vpc || sub.Spec.Vlan != subnet.Spec.Vlan || sub.Name == subnet.Name {
			continue
		}

		if util.CIDROverlap(sub.Spec.CIDRBlock, subnet.Spec.CIDRBlock) {
			err = fmt.Errorf("subnet %s cidr %s is conflict with subnet %s cidr %s", subnet.Name, subnet.Spec.CIDRBlock, sub.Name, sub.Spec.CIDRBlock)
			klog.Error(err)
			c.patchSubnetStatus(subnet, "ValidateLogicalSwitchFailed", err.Error())
			return err
		}

		if subnet.Spec.ExternalEgressGateway != "" && sub.Spec.ExternalEgressGateway != "" &&
			subnet.Spec.PolicyRoutingTableID == sub.Spec.PolicyRoutingTableID {
			err = fmt.Errorf("subnet %s policy routing table ID %d is conflict with subnet %s policy routing table ID %d", subnet.Name, subnet.Spec.PolicyRoutingTableID, sub.Name, sub.Spec.PolicyRoutingTableID)
			klog.Error(err)
			c.patchSubnetStatus(subnet, "ValidateLogicalSwitchFailed", err.Error())
			return err
		}
	}

	if subnet.Spec.Vlan == "" && subnet.Spec.Vpc == util.DefaultVpc {
		nodes, err := c.nodesLister.List(labels.Everything())
		if err != nil {
			klog.Errorf("failed to list nodes: %v", err)
			return err
		}
		for _, node := range nodes {
			for _, addr := range node.Status.Addresses {
				if addr.Type == v1.NodeInternalIP && util.CIDRContainIP(subnet.Spec.CIDRBlock, addr.Address) {
					err = fmt.Errorf("subnet %s cidr %s conflict with node %s address %s", subnet.Name, subnet.Spec.CIDRBlock, node.Name, addr.Address)
					klog.Error(err)
					c.patchSubnetStatus(subnet, "ValidateLogicalSwitchFailed", err.Error())
					return err
				}
			}
		}
	}

	needRouter := subnet.Spec.Vlan == "" || subnet.Spec.LogicalGateway ||
		(subnet.Status.U2OInterconnectionIP != "" && subnet.Spec.U2OInterconnection)
	// 1. overlay subnet, should add lrp, lrp ip is subnet gw
	// 2. underlay subnet use logical gw, should add lrp, lrp ip is subnet gw
	randomAllocateGW := !subnet.Spec.LogicalGateway && vpc.Spec.EnableExternal && subnet.Name == c.config.ExternalGatewaySwitch
	// 3. underlay subnet use physical gw, vpc has eip, lrp managed in vpc process, lrp ip is random allocation, not subnet gw

	gateway := subnet.Spec.Gateway
	if subnet.Status.U2OInterconnectionIP != "" && subnet.Spec.U2OInterconnection {
		gateway = subnet.Status.U2OInterconnectionIP
	}

	// create or update logical switch
	if err := c.ovnClient.CreateLogicalSwitch(subnet.Name, vpc.Status.Router, subnet.Spec.CIDRBlock, gateway, needRouter, randomAllocateGW); err != nil {
		klog.Errorf("create logical switch %s: %v", subnet.Name, err)
		return err
	}

	subnet.Status.EnsureStandardConditions()

	var dhcpOptionsUUIDs *ovs.DHCPOptionsUUIDs
	dhcpOptionsUUIDs, err = c.ovnLegacyClient.UpdateDHCPOptions(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, subnet.Spec.DHCPv4Options, subnet.Spec.DHCPv6Options, subnet.Spec.EnableDHCP)
	if err != nil {
		klog.Errorf("failed to update dhcp options for switch %s, %v", subnet.Name, err)
		return err
	}

	if needRouter {
		lrpName := fmt.Sprintf("%s-%s", vpc.Status.Router, subnet.Name)
		if err := c.ovnClient.UpdateLogicalRouterPortRA(lrpName, subnet.Spec.IPv6RAConfigs, subnet.Spec.EnableIPv6RA); err != nil {
			klog.Errorf("update ipv6 ra configs for logical router port %s, %v", lrpName, err)
			return err
		}
	}

	if subnet.Status.DHCPv4OptionsUUID != dhcpOptionsUUIDs.DHCPv4OptionsUUID || subnet.Status.DHCPv6OptionsUUID != dhcpOptionsUUIDs.DHCPv6OptionsUUID {
		subnet.Status.DHCPv4OptionsUUID = dhcpOptionsUUIDs.DHCPv4OptionsUUID
		subnet.Status.DHCPv6OptionsUUID = dhcpOptionsUUIDs.DHCPv6OptionsUUID
		bytes, err := subnet.Status.Bytes()
		if err != nil {
			klog.Error(err)
			return err
		} else {
			if _, err := c.config.KubeOvnClient.KubeovnV1().Subnets().Patch(context.Background(), subnet.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status"); err != nil {
				klog.Error("patch subnet %s dhcp options failed: %v", subnet.Name, err)
				return err
			}
		}
	}

	if c.config.EnableLb && subnet.Name != c.config.NodeSwitch {
		lbs := []string{
			vpc.Status.TcpLoadBalancer,
			vpc.Status.TcpSessionLoadBalancer,
			vpc.Status.UdpLoadBalancer,
			vpc.Status.UdpSessionLoadBalancer,
			vpc.Status.SctpLoadBalancer,
			vpc.Status.SctpSessionLoadBalancer,
		}
		if subnet.Spec.EnableLb != nil && *subnet.Spec.EnableLb {
			if err := c.ovnClient.LogicalSwitchUpdateLoadBalancers(subnet.Name, ovsdb.MutateOperationInsert, lbs...); err != nil {
				c.patchSubnetStatus(subnet, "AddLbToLogicalSwitchFailed", err.Error())
				return err
			}
		} else {
			if err := c.ovnClient.LogicalSwitchUpdateLoadBalancers(subnet.Name, ovsdb.MutateOperationDelete, lbs...); err != nil {
				klog.Error("remove load-balancer from subnet %s failed: %v", subnet.Name, err)
				return err
			}
		}
	}

	if err := c.reconcileSubnet(subnet); err != nil {
		klog.Errorf("reconcile subnet for %s failed, %v", subnet.Name, err)
		return err
	}

	if subnet.Spec.Private {
		if err := c.ovnLegacyClient.SetPrivateLogicalSwitch(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.AllowSubnets); err != nil {
			c.patchSubnetStatus(subnet, "SetPrivateLogicalSwitchFailed", err.Error())
			return err
		}
		c.patchSubnetStatus(subnet, "SetPrivateLogicalSwitchSuccess", "")
	} else {
		if err := c.ovnLegacyClient.ResetLogicalSwitchAcl(subnet.Name); err != nil {
			c.patchSubnetStatus(subnet, "ResetLogicalSwitchAclFailed", err.Error())
			return err
		}
		c.patchSubnetStatus(subnet, "ResetLogicalSwitchAclSuccess", "")
	}

	if err := c.ovnLegacyClient.CreateGatewayACL(subnet.Name, "", subnet.Spec.Gateway, subnet.Spec.CIDRBlock); err != nil {
		klog.Errorf("create gateway acl %s failed, %v", subnet.Name, err)
		return err
	}

	if err := c.ovnLegacyClient.UpdateSubnetACL(subnet.Name, subnet.Spec.Acls); err != nil {
		c.patchSubnetStatus(subnet, "SetLogicalSwitchAclsFailed", err.Error())
		return err
	}

	c.updateVpcStatusQueue.Add(subnet.Spec.Vpc)
	return nil
}

func (c *Controller) handleUpdateSubnetStatus(key string) error {
	c.subnetStatusKeyMutex.Lock(key)
	defer c.subnetStatusKeyMutex.Unlock(key)

	cachedSubnet, err := c.subnetsLister.Get(key)
	subnet := cachedSubnet.DeepCopy()
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if util.CheckProtocol(subnet.Spec.CIDRBlock) == kubeovnv1.ProtocolDual {
		return calcDualSubnetStatusIP(subnet, c)
	} else {
		return calcSubnetStatusIP(subnet, c)
	}
}

func (c *Controller) handleDeleteRoute(subnet *kubeovnv1.Subnet) error {
	vpc, err := c.vpcsLister.Get(subnet.Spec.Vpc)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return c.deleteStaticRoute(subnet.Spec.CIDRBlock, vpc.Status.Router)
}

func (c *Controller) handleDeleteLogicalSwitch(key string) (err error) {
	c.ipam.DeleteSubnet(key)

	exist, err := c.ovnClient.LogicalSwitchExists(key)
	if err != nil {
		klog.Errorf("check logical switch %s exist: %v", key, err)
		return err
	}

	// not found, skip
	if !exist {
		return nil
	}

	if err = c.ovnLegacyClient.CleanLogicalSwitchAcl(key); err != nil {
		klog.Errorf("failed to delete acl of logical switch %s %v", key, err)
		return err
	}

	if err = c.ovnLegacyClient.DeleteDHCPOptions(key, kubeovnv1.ProtocolDual); err != nil {
		klog.Errorf("failed to delete dhcp options of logical switch %s %v", key, err)
		return err
	}

	if err = c.ovnClient.DeleteLogicalSwitch(key); err != nil {
		klog.Errorf("delete logical switch %s: %v", key, err)
		return err
	}

	nss, err := c.namespacesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list namespaces, %v", err)
		return err
	}

	// re-annotate namespace
	for _, ns := range nss {
		annotations := ns.GetAnnotations()
		if annotations == nil {
			continue
		}

		if util.ContainsString(strings.Split(annotations[util.LogicalSwitchAnnotation], ","), key) {
			c.enqueueAddNamespace(ns)
		}
	}

	return c.delLocalnet(key)
}

func (c *Controller) handleDeleteSubnet(subnet *kubeovnv1.Subnet) error {
	c.updateVpcStatusQueue.Add(subnet.Spec.Vpc)
	klog.Infof("delete u2o interconnection policy route for subnet %s", subnet.Name)
	if err := c.deletePolicyRouteForU2OInterconn(subnet); err != nil {
		klog.Errorf("failed to delete policy route for underlay to overlay subnet interconnection %s, %v", subnet.Name, err)
		return err
	}

	u2oInterconnName := fmt.Sprintf(util.U2OInterconnName, subnet.Spec.Vpc, subnet.Name)
	if err := c.config.KubeOvnClient.KubeovnV1().IPs().Delete(context.Background(), u2oInterconnName, metav1.DeleteOptions{}); err != nil {
		if !k8serrors.IsNotFound(err) {
			klog.Errorf("failed to delete ip %s, %v", u2oInterconnName, err)
			return err
		}
	}

	klog.Infof("delete policy route for %s subnet %s", subnet.Spec.GatewayType, subnet.Name)
	if err := c.deletePolicyRouteByGatewayType(subnet, subnet.Spec.GatewayType, true); err != nil {
		klog.Errorf("failed to delete policy route for overlay subnet %s, %v", subnet.Name, err)
		return err
	}

	err := c.handleDeleteLogicalSwitch(subnet.Name)
	if err != nil {
		klog.Errorf("failed to delete logical switch %s %v", subnet.Name, err)
		return err
	}

	var router string
	vpc, err := c.vpcsLister.Get(subnet.Spec.Vpc)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			klog.Errorf("get vpc %s: %v", vpc.Name, err)
			return err
		}
		router = util.DefaultVpc
	} else {
		router = vpc.Status.Router
	}

	lspName := fmt.Sprintf("%s-%s", subnet.Name, router)
	lrpName := fmt.Sprintf("%s-%s", router, subnet.Name)
	if err = c.ovnClient.RemoveLogicalPatchPort(lspName, lrpName); err != nil {
		klog.Errorf("delete router port %s and %s:%v", lspName, lrpName, err)
		return err
	}

	vlans, err := c.vlansLister.List(labels.Everything())
	if err != nil && !k8serrors.IsNotFound(err) {
		klog.Errorf("failed to list vlans: %v", err)
		return err
	}

	for _, vlan := range vlans {
		if err = c.updateVlanStatusForSubnetDeletion(vlan, subnet.Name); err != nil {
			return err
		}
	}

	return nil
}

func (c *Controller) updateVlanStatusForSubnetDeletion(vlan *kubeovnv1.Vlan, subnet string) error {
	if !util.ContainsString(vlan.Status.Subnets, subnet) {
		return nil
	}

	newVlan := vlan.DeepCopy()
	newVlan.Status.Subnets = util.RemoveString(newVlan.Status.Subnets, subnet)
	_, err := c.config.KubeOvnClient.KubeovnV1().Vlans().UpdateStatus(context.Background(), newVlan, metav1.UpdateOptions{})
	if err != nil {
		klog.Errorf("failed to update status of vlan %s: %v", vlan.Name, err)
		return err
	}

	return nil
}

func (c *Controller) reconcileSubnet(subnet *kubeovnv1.Subnet) error {
	if err := c.reconcileNamespaces(subnet); err != nil {
		klog.Errorf("reconcile namespaces for subnet %s failed, %v", subnet.Name, err)
		return err
	}

	if subnet.Spec.Vpc == util.DefaultVpc {
		if err := c.reconcileOvnDefaultVpcRoute(subnet); err != nil {
			klog.Errorf("reconcile default vpc ovn route for subnet %s failed: %v", subnet.Name, err)
			return err
		}
	}

	if subnet.Spec.Vpc != util.DefaultVpc {
		if err := c.reconcileOvnCustomVpcRoute(subnet); err != nil {
			klog.Errorf("reconcile custom vpc ovn route for subnet %s failed: %v", subnet.Name, err)
			return err
		}
	}

	if err := c.reconcileVlan(subnet); err != nil {
		klog.Errorf("reconcile vlan for subnet %s failed, %v", subnet.Name, err)
		return err
	}

	if err := c.reconcileVips(subnet); err != nil {
		klog.Errorf("reconcile vips for subnet %s failed, %v", subnet.Name, err)
		return err
	}
	return nil
}

func (c *Controller) reconcileVips(subnet *kubeovnv1.Subnet) error {
	/* get all virtual port belongs to this logical switch */
	lsps, err := c.ovnClient.ListLogicalSwitchPorts(true, map[string]string{logicalSwitchKey: subnet.Name}, func(lsp *ovnnb.LogicalSwitchPort) bool {
		return lsp.Type == "virtual"
	})

	if err != nil {
		klog.Errorf("failed to find virtual port for subnet %s: %v", subnet.Name, err)
		return err
	}

	/* filter all invaild virtual port */
	existVips := make(map[string]string) // key is vip, value is port name
	for _, lsp := range lsps {
		vip, ok := lsp.Options["virtual-ip"]
		if !ok {
			continue // ingnore vip which is empty
		}

		if net.ParseIP(vip) == nil {
			continue // ingnore invalid vip
		}

		existVips[vip] = lsp.Name
	}

	/* filter virtual port to be added and old virtual port to be deleted */
	var newVips []string
	for _, vip := range subnet.Spec.Vips {
		if _, ok := existVips[vip]; !ok {
			// new virtual port to be added
			newVips = append(newVips, vip)
		} else {
			// delete old virtual port that do not need to be deleted
			delete(existVips, vip)
		}
	}

	// delete old virtual ports
	for _, lspName := range existVips {
		if err = c.ovnClient.DeleteLogicalSwitchPort(lspName); err != nil {
			klog.Errorf("delete virtual port %s lspName from logical switch %s: %v", lspName, subnet.Name, err)
			return err
		}
	}

	// add new virtual port
	if err = c.ovnClient.CreateVirtualLogicalSwitchPorts(subnet.Name, newVips...); err != nil {
		klog.Errorf("create virtual port with vips %v from logical switch %s: %v", newVips, subnet.Name, err)
		return err
	}

	c.syncVirtualPortsQueue.Add(subnet.Name)
	return nil
}

func (c *Controller) syncVirtualPort(key string) error {
	subnet, err := c.subnetsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		} else {
			klog.Errorf("failed to get subnet %s, %v", key, err)
			return err
		}
	}
	if len(subnet.Spec.Vips) == 0 {
		return nil
	}

	externalIDs := map[string]string{
		logicalSwitchKey: subnet.Name,
		"attach-vips":    "true",
	}

	lsps, err := c.ovnClient.ListNormalLogicalSwitchPorts(true, externalIDs)
	if err != nil {
		klog.Errorf("list logical switch %s ports: %v", subnet.Name, err)
		return err
	}

	for _, vip := range subnet.Spec.Vips {
		if !util.CIDRContainIP(subnet.Spec.CIDRBlock, vip) {
			klog.Errorf("vip %s is out of range to subnet %s", vip, subnet.Name)
			continue
		}

		var virtualParents []string
		for _, lsp := range lsps {
			vips, ok := lsp.ExternalIDs["vips"]
			if !ok {
				continue // ingnore vips which is empty
			}

			if strings.Contains(vips, vip) {
				virtualParents = append(virtualParents, lsp.Name)
			}
		}

		// logical switch port has no vaild vip
		if len(virtualParents) == 0 {
			continue
		}

		if err = c.ovnClient.SetLogicalSwitchPortVirtualParents(subnet.Name, strings.Join(virtualParents, ","), vip); err != nil {
			klog.Errorf("set vip %s virtual parents %v: %v", vip, virtualParents, err)
			return err
		}
	}

	return nil
}

func (c *Controller) reconcileNamespaces(subnet *kubeovnv1.Subnet) error {
	var err error

	// 1. add annotations to bind namespace
	for _, ns := range subnet.Spec.Namespaces {
		c.addNamespaceQueue.Add(ns)
	}

	// 2. update unbind namespace annotation
	namespaces, err := c.namespacesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list namespaces, %v", err)
		return err
	}

	for _, ns := range namespaces {
		// when subnet cidr changed, the ns annotation with the subnet should be updated
		if ns.Annotations != nil && util.ContainsString(strings.Split(ns.Annotations[util.LogicalSwitchAnnotation], ","), subnet.Name) {
			c.addNamespaceQueue.Add(ns.Name)
		}
	}

	return nil
}

func (c *Controller) reconcileVpcUseBfdStaticRoute(vpcName, subnetName string) error {
	// vpc enable bfd and subnet enable ecmp
	// use static ecmp route with bfd
	ovnEips, err := c.ovnEipsLister.List(labels.SelectorFromSet(labels.Set{util.OvnEipUsageLabel: util.NodeExtGwUsingEip}))
	if err != nil {
		klog.Errorf("failed to list node external ovn eip, %v", err)
		return err
	}
	if len(ovnEips) < 2 {
		err := fmt.Errorf("ha ecmp route with bfd need two eips at least")
		klog.Error(err)
		return err
	}
	needUpdate := false
	v4Exist := false
	subnet, err := c.subnetsLister.Get(subnetName)
	if err != nil {
		klog.Errorf("failed to get subnet %s, %v", subnetName, err)
		return err
	}
	vpc, err := c.config.KubeOvnClient.KubeovnV1().Vpcs().Get(context.Background(), vpcName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to get vpc %s, %v", vpcName, err)
		return err
	}
	lrpEipName := fmt.Sprintf("%s-%s", vpcName, c.config.ExternalGatewaySwitch)
	lrpEip, err := c.config.KubeOvnClient.KubeovnV1().OvnEips().Get(context.Background(), lrpEipName, metav1.GetOptions{})
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			klog.Error(err)
			return err
		}
	}
	if !lrpEip.Status.Ready || lrpEip.Status.V4Ip == "" {
		err := fmt.Errorf("lrp eip %q not ready", lrpEipName)
		klog.Error(err)
		return err
	}
	for _, eip := range ovnEips {
		if !eip.Status.Ready || eip.Status.V4Ip == "" {
			err := fmt.Errorf("ovn eip %q not ready", eip.Name)
			klog.Error(err)
			return err
		}
		bfdId, err := c.ovnLegacyClient.CreateBfd(lrpEipName, eip.Status.V4Ip, c.config.BfdMinTx, c.config.BfdMinRx, c.config.BfdDetectMult)
		if err != nil {
			klog.Error(err)
			return err
		}
		// TODO:// support v6
		v4Exist = false
		for _, route := range vpc.Spec.StaticRoutes {
			if route.Policy == kubeovnv1.PolicySrc &&
				route.NextHopIP == eip.Status.V4Ip &&
				route.ECMPMode == util.StaicRouteBfdEcmp &&
				route.CIDR == subnet.Spec.CIDRBlock {
				v4Exist = true
				break
			}
		}
		if !v4Exist {
			// add ecmp type static route with bfd
			route := &kubeovnv1.StaticRoute{
				Policy:    kubeovnv1.PolicySrc,
				CIDR:      subnet.Spec.CIDRBlock,
				NextHopIP: eip.Status.V4Ip,
				ECMPMode:  util.StaicRouteBfdEcmp,
				BfdId:     bfdId,
			}
			klog.V(3).Infof("add ecmp bfd static route %v", route)
			vpc.Spec.StaticRoutes = append(vpc.Spec.StaticRoutes, route)
			needUpdate = true
		}
	}
	if needUpdate {
		if _, err = c.config.KubeOvnClient.KubeovnV1().Vpcs().Update(context.Background(), vpc, metav1.UpdateOptions{}); err != nil {
			klog.Errorf("failed to update vpc spec static route %s, %v", vpc.Name, err)
			return err
		}
	}

	if err = c.patchVpcBfdStatus(vpc.Name); err != nil {
		klog.Errorf("failed to patch vpc %s, %v", vpc.Name, err)
		return err
	}

	return nil
}

func (c *Controller) reconcileVpcAddNormalStaticRoute(vpcName string) error {
	// vpc disable bfd and subnet disable ecmp
	// use normal type static route, not ecmp
	// dst 0.0.0.0 nexthop external switch gw
	// allow all subnet access external based snat
	// also, this will not conflict with policy route

	defualtExternalSubnet, err := c.subnetsLister.Get(c.config.ExternalGatewaySwitch)
	if err != nil {
		klog.Error("failed to get default external switch subnet %s: %v", c.config.ExternalGatewaySwitch)
		return err
	}
	gatewayV4, gatewayV6 := util.SplitStringIP(defualtExternalSubnet.Spec.Gateway)
	v4Exist, v6Exist := false, false
	needUpdate := false
	vpc, err := c.config.KubeOvnClient.KubeovnV1().Vpcs().Get(context.Background(), vpcName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to get vpc %s, %v", vpcName, err)
		return err
	}
	routeTotal := len(vpc.Spec.StaticRoutes) + 2
	routes := make([]*kubeovnv1.StaticRoute, 0, routeTotal)
	for _, route := range vpc.Spec.StaticRoutes {
		if route.Policy == kubeovnv1.PolicyDst &&
			route.NextHopIP == gatewayV4 &&
			route.CIDR == "0.0.0.0/0" {
			v4Exist = true
			continue
		}
		if route.Policy == kubeovnv1.PolicyDst &&
			route.NextHopIP == gatewayV6 &&
			route.CIDR == "::/0" {
			v6Exist = true
			continue
		}
		if route.ECMPMode != util.StaicRouteBfdEcmp {
			// filter ecmp bfd route
			routes = append(routes, route)
		}
	}
	if !v4Exist && gatewayV4 != "" {
		klog.V(3).Infof("add normal static route v4 nexthop %s", gatewayV4)
		routes = append(routes, &kubeovnv1.StaticRoute{
			Policy:    kubeovnv1.PolicyDst,
			CIDR:      "0.0.0.0/0",
			NextHopIP: gatewayV4,
		})
		needUpdate = true
	}
	if !v6Exist && gatewayV6 != "" {
		klog.V(3).Infof("add normal static route v6 nexthop %s", gatewayV6)
		routes = append(routes, &kubeovnv1.StaticRoute{
			Policy:    kubeovnv1.PolicyDst,
			CIDR:      "::/0",
			NextHopIP: gatewayV6,
		})
		needUpdate = true
	}

	if needUpdate {
		vpc.Spec.StaticRoutes = routes
		if _, err = c.config.KubeOvnClient.KubeovnV1().Vpcs().Update(context.Background(), vpc, metav1.UpdateOptions{}); err != nil {
			klog.Errorf("failed to update vpc spec static route %s, %v", vpc.Name, err)
			return err
		}
	}

	if err = c.patchVpcBfdStatus(vpc.Name); err != nil {
		klog.Errorf("failed to patch vpc %s, %v", vpc.Name, err)
		return err
	}

	return nil
}

func (c *Controller) reconcileVpcDelNormalStaticRoute(vpcName string) error {
	// normal static route is prior than ecmp bfd static route
	// if use ecmp bfd static route, normal static route should not exist
	defualtExternalSubnet, err := c.subnetsLister.Get(c.config.ExternalGatewaySwitch)
	if err != nil {
		klog.Error("failed to get default external switch subnet %s: %v", c.config.ExternalGatewaySwitch)
		return err
	}
	gatewayV4, gatewayV6 := util.SplitStringIP(defualtExternalSubnet.Spec.Gateway)
	needUpdate := false
	vpc, err := c.config.KubeOvnClient.KubeovnV1().Vpcs().Get(context.Background(), vpcName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to get vpc %s, %v", vpcName, err)
		return err
	}
	routeTotal := len(vpc.Spec.StaticRoutes)
	routes := make([]*kubeovnv1.StaticRoute, 0, routeTotal)
	for _, route := range vpc.Spec.StaticRoutes {
		if route.Policy == kubeovnv1.PolicyDst &&
			(route.NextHopIP == gatewayV4 || route.NextHopIP == gatewayV6) &&
			(route.CIDR == "0.0.0.0/0" || route.CIDR == "::/0") {
			klog.V(3).Infof("in order to use ecmp bfd route, need remove normal static route %v", route)
			needUpdate = true
		} else {
			routes = append(routes, route)
		}
	}
	if needUpdate {
		vpc.Spec.StaticRoutes = routes
		if _, err = c.config.KubeOvnClient.KubeovnV1().Vpcs().Update(context.Background(), vpc, metav1.UpdateOptions{}); err != nil {
			klog.Errorf("failed to update vpc spec static route %s, %v", vpc.Name, err)
			return err
		}
	}

	if err = c.patchVpcBfdStatus(vpc.Name); err != nil {
		klog.Errorf("failed to patch vpc %s, %v", vpc.Name, err)
		return err
	}

	return nil
}

func (c *Controller) reconcileOvnDefaultVpcRoute(subnet *kubeovnv1.Subnet) error {
	if subnet.Name == c.config.NodeSwitch {
		if err := c.addCommonRoutesForSubnet(subnet); err != nil {
			klog.Error(err)
			return err
		}
		return nil
	}

	pods, err := c.podsLister.Pods(metav1.NamespaceAll).List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list pods %v", err)
		return err
	}

	if subnet.Spec.Vlan != "" && !subnet.Spec.LogicalGateway {
		// physical switch provide gw for this underlay subnet
		for _, pod := range pods {
			if pod.Annotations[util.LogicalSwitchAnnotation] == subnet.Name && pod.Annotations[util.IpAddressAnnotation] != "" {
				if err := c.deleteStaticRoute(pod.Annotations[util.IpAddressAnnotation], c.config.ClusterRouter); err != nil {
					klog.Errorf("failed to delete static route %v", err)
					return err
				}
			}
		}

		if err := c.deleteStaticRoute(subnet.Spec.CIDRBlock, c.config.ClusterRouter); err != nil {
			klog.Errorf("failed to delete static route %v", err)
			return err
		}

		if !subnet.Spec.LogicalGateway && subnet.Name != c.config.ExternalGatewaySwitch && !subnet.Spec.U2OInterconnection {
			lspName := fmt.Sprintf("%s-%s", subnet.Name, c.config.ClusterRouter)
			klog.Infof("delete logical switch port %s", lspName)
			if err := c.ovnClient.DeleteLogicalSwitchPort(lspName); err != nil {
				klog.Errorf("failed to delete lsp %s-%s, %v", subnet.Name, c.config.ClusterRouter, err)
				return err
			}
			lrpName := fmt.Sprintf("%s-%s", c.config.ClusterRouter, subnet.Name)
			klog.Infof("delete logical router port %s", lrpName)
			if err := c.ovnClient.DeleteLogicalRouterPort(lrpName); err != nil {
				klog.Errorf("failed to delete lrp %s: %v", lrpName, err)
				return err
			}
		}

		if subnet.Spec.U2OInterconnection && subnet.Status.U2OInterconnectionIP != "" {
			if err := c.addPolicyRouteForU2OInterconn(subnet); err != nil {
				klog.Errorf("failed to add policy route for underlay to overlay subnet interconnection %s %v", subnet.Name, err)
				return err
			}
		} else {
			if err := c.deletePolicyRouteForU2OInterconn(subnet); err != nil {
				klog.Errorf("failed to delete policy route for underlay to overlay subnet interconnection %s, %v", subnet.Name, err)
				return err
			}
		}
	} else {
		if err = c.addCommonRoutesForSubnet(subnet); err != nil {
			klog.Error(err)
			return err
		}

		// if gw is distributed remove activateGateway field
		if subnet.Spec.GatewayType == kubeovnv1.GWDistributedType {
			// distributed subnet, only add distributed policy route
			if subnet.Spec.GatewayNode != "" || subnet.Status.ActivateGateway != "" {
				klog.Infof("delete old centralized policy route for subnet %s", subnet.Name)
				if err := c.deletePolicyRouteForCentralizedSubnet(subnet); err != nil {
					klog.Errorf("failed to delete policy route for centralized subnet %s, %v", subnet.Name, err)
					return err
				}
				subnet.Spec.GatewayNode = ""
				subnet.Status.ActivateGateway = ""
				c.recorder.Eventf(subnet, v1.EventTypeNormal, "ChangeToCentralizedGw", "")

				bytes, err := subnet.Status.Bytes()
				if err != nil {
					klog.Errorf("failed to marshal subnet status %v", err)
					return err
				}
				_, err = c.config.KubeOvnClient.KubeovnV1().Subnets().Patch(context.Background(), subnet.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "")
				if err != nil {
					klog.Errorf("failed to patch subnet status %v", err)
					return err
				}
			}

			nodes, err := c.nodesLister.List(labels.Everything())
			if err != nil {
				klog.Errorf("failed to list nodes: %v", err)
				return err
			}
			for _, node := range nodes {
				if err = c.createPortGroupForDistributedSubnet(node, subnet); err != nil {
					klog.Errorf("failed to create port group %v", err)
					return err
				}
				if node.Annotations[util.AllocatedAnnotation] != "true" {
					continue
				}
				nodeIP, err := getNodeTunlIP(node)
				if err != nil {
					klog.Errorf("failed to get node %s tunnel ip, %v", node.Name, err)
					return err
				}
				nextHop := getNextHopByTunnelIP(nodeIP)
				v4IP, v6IP := util.SplitStringIP(nextHop)
				if err = c.addPolicyRouteForDistributedSubnet(subnet, node.Name, v4IP, v6IP); err != nil {
					klog.Errorf("failed to add policy router for node %s and subnet %s: %v", node.Name, subnet.Name, err)
					return err
				}
			}

			for _, pod := range pods {
				if !isPodAlive(pod) {
					continue
				}
				if c.config.EnableEipSnat && (pod.Annotations[util.EipAnnotation] != "" || pod.Annotations[util.SnatAnnotation] != "") {
					continue
				}
				// Pod will add to port-group when pod get updated
				if pod.Spec.NodeName == "" {
					continue
				}

				podNets, err := c.getPodKubeovnNets(pod)
				if err != nil {
					klog.Errorf("failed to get pod nets %v", err)
					continue
				}

				podPorts := make([]string, 0, 1)
				for _, podNet := range podNets {
					if !isOvnSubnet(podNet.Subnet) {
						continue
					}

					if pod.Annotations[fmt.Sprintf(util.IpAddressAnnotationTemplate, podNet.ProviderName)] == "" || pod.Annotations[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, podNet.ProviderName)] != subnet.Name {
						continue
					}

					if pod.Annotations[util.NorthGatewayAnnotation] != "" {
						nextHop := pod.Annotations[util.NorthGatewayAnnotation]
						exist, err := c.checkRouteExist(nextHop, pod.Annotations[fmt.Sprintf(util.IpAddressAnnotationTemplate, podNet.ProviderName)], ovs.PolicySrcIP)
						if err != nil {
							klog.Errorf("failed to get static route for subnet %v, error %v", subnet.Name, err)
							return err
						}
						if exist {
							continue
						}

						if err := c.ovnLegacyClient.AddStaticRoute(ovs.PolicySrcIP, pod.Annotations[fmt.Sprintf(util.IpAddressAnnotationTemplate, podNet.ProviderName)],
							nextHop, "", "", c.config.ClusterRouter, util.NormalRouteType); err != nil {
							klog.Errorf("add static route failed, %v", err)
							return err
						}
					} else {
						podName := c.getNameByPod(pod)
						portName := ovs.PodNameToPortName(podName, pod.Namespace, podNet.ProviderName)
						podPorts = append(podPorts, portName)
					}
				}

				if pod.Annotations[util.NorthGatewayAnnotation] != "" {
					continue
				}

				pgName := getOverlaySubnetsPortGroupName(subnet.Name, pod.Spec.NodeName)
				portsToAdd := make([]string, 0, len(podPorts))
				for _, port := range podPorts {
					exist, err := c.ovnClient.LogicalSwitchPortExists(port)
					if err != nil {
						return err
					}

					if !exist {
						klog.Errorf("lsp does not exist for pod %v, please delete the pod and retry", port)
						continue
					}

					portsToAdd = append(portsToAdd, port)
				}

				if err = c.ovnClient.PortGroupAddPorts(pgName, portsToAdd...); err != nil {
					klog.Errorf("add ports to port group %s: %v", pgName, err)
					return err
				}
			}
			return nil
		} else {
			// centralized subnet
			if subnet.Spec.GatewayNode == "" {
				errMsg := fmt.Sprintf("subnet %s Spec.GatewayNode field must be specified for centralized gateway type", subnet.Name)
				klog.Errorf(errMsg)
				reason := "NoReadyGateway"
				subnet.Status.NotReady("NoReadyGateway", "")
				c.recorder.Eventf(subnet, v1.EventTypeWarning, reason, errMsg)
				bytes, err := subnet.Status.Bytes()
				if err != nil {
					return err
				}
				_, err = c.config.KubeOvnClient.KubeovnV1().Subnets().Patch(context.Background(), subnet.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status")
				return err
			}

			gwNodeExists := c.checkGwNodeExists(subnet.Spec.GatewayNode)
			if !gwNodeExists {
				klog.Errorf("failed to init centralized gateway for subnet %s, no gateway node exists", subnet.Name)
				return fmt.Errorf("failed to add ecmp policy route, no gateway node exists")
			}

			if subnet.Spec.EnableEcmp {
				// handle vpc policy route
				// 1. Default value of subnet.Spec.EnableEcmp is false, so the field subnet.Status.ActivateGateway has value when centralized subnet is created
				// 2. Change subnet.Spec.EnableEcmp from false to true, ecmp route is added based on gatewayNode, not ActivateGateway
				// 3. Change subnet.Spec.EnableEcmp from true to false, the ActivateGateway still works and ecmp route does not update, which is incorrect
				// 4. So delete ActivateGateway field when ecmp is enabled, and when value changed, the policy route will be updated correctly
				if subnet.Status.ActivateGateway != "" {
					subnet.Status.ActivateGateway = ""
					bytes, err := subnet.Status.Bytes()
					if err != nil {
						return err
					}
					if _, err = c.config.KubeOvnClient.KubeovnV1().Subnets().Patch(context.Background(), subnet.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, ""); err != nil {
						klog.Errorf("failed to patch for removing subnet activeGateway of subnet %s", subnet.Name)
						return err
					}
				}

				// centralized subnet, enable ecmp, add ecmp policy route
				gatewayNodes := strings.Split(subnet.Spec.GatewayNode, ",")
				nodeV4Ips := make([]string, 0, len(gatewayNodes))
				nodeV6Ips := make([]string, 0, len(gatewayNodes))
				nameV4IpMap := make(map[string]string, len(gatewayNodes))
				nameV6IpMap := make(map[string]string, len(gatewayNodes))
				for _, gw := range gatewayNodes {
					// the format of gatewayNodeStr can be like 'kube-ovn-worker:172.18.0.2, kube-ovn-control-plane:172.18.0.3', which consists of node name and designative egress ip
					if strings.Contains(gw, ":") {
						gw = strings.TrimSpace(strings.Split(gw, ":")[0])
					} else {
						gw = strings.TrimSpace(gw)
					}

					node, err := c.nodesLister.Get(gw)
					if err != nil {
						klog.Errorf("failed to get gw node %s, %v", gw, err)
						continue
					}

					if nodeReady(node) {
						nexthopNodeIP := strings.TrimSpace(node.Annotations[util.IpAddressAnnotation])
						if nexthopNodeIP == "" {
							klog.Errorf("gateway node %v has no ip annotation", node.Name)
							continue
						}
						nexthopV4, nexthopV6 := util.SplitStringIP(nexthopNodeIP)
						if nexthopV4 != "" {
							nameV4IpMap[node.Name] = nexthopV4
							nodeV4Ips = append(nodeV4Ips, nexthopV4)
						}
						if nexthopV6 != "" {
							nameV6IpMap[node.Name] = nexthopV6
							nodeV6Ips = append(nodeV6Ips, nexthopV6)
						}
					} else {
						klog.Errorf("gateway node %v is not ready", gw)
					}
				}
				v4Cidr, v6Cidr := util.SplitStringIP(subnet.Spec.CIDRBlock)
				if nodeV4Ips != nil && v4Cidr != "" {
					sort.Strings(nodeV4Ips)
					exist, err := c.ovnLegacyClient.VpcHasPolicyRoute(c.config.ClusterRouter, nodeV4Ips, util.GatewayRouterPolicyPriority)
					if err != nil {
						klog.Errorf("failed to check if vpc %s has v4 ecmp policy route for centralized subnet %s, %v", c.config.ClusterRouter, subnet.Name, err)
						return err
					}
					if !exist {
						klog.Infof("delete old distributed policy route for subnet %s", subnet.Name)
						if err := c.deletePolicyRouteByGatewayType(subnet, kubeovnv1.GWDistributedType, false); err != nil {
							klog.Errorf("failed to delete policy route for overlay subnet %s, %v", subnet.Name, err)
							return err
						}
						klog.Infof("subnet %s configure ecmp policy route, nexthops %v", subnet.Name, nodeV4Ips)
						if err = c.updatePolicyRouteForCentralizedSubnet(subnet.Name, v4Cidr, nodeV4Ips, nameV4IpMap); err != nil {
							klog.Errorf("failed to add v4 ecmp policy route for centralized subnet %s: %v", subnet.Name, err)
							return err
						}
					}
				}
				if nodeV6Ips != nil && v6Cidr != "" {
					sort.Strings(nodeV6Ips)
					exist, err := c.ovnLegacyClient.VpcHasPolicyRoute(c.config.ClusterRouter, nodeV6Ips, util.GatewayRouterPolicyPriority)
					if err != nil {
						klog.Errorf("failed to check if vpc %s has v6 ecmp policy route for centralized subnet %s, %v", c.config.ClusterRouter, subnet.Name, err)
						return err
					}
					if !exist {
						klog.Infof("delete old distributed policy route for subnet %s", subnet.Name)
						if err := c.deletePolicyRouteByGatewayType(subnet, kubeovnv1.GWDistributedType, false); err != nil {
							klog.Errorf("failed to delete policy route for overlay subnet %s, %v", subnet.Name, err)
							return err
						}
						klog.Infof("subnet %s configure ecmp policy route, nexthops %v", subnet.Name, nodeV6Ips)
						if err = c.updatePolicyRouteForCentralizedSubnet(subnet.Name, v6Cidr, nodeV6Ips, nameV6IpMap); err != nil {
							klog.Errorf("failed to add v6 ecmp policy route for centralized subnet %s: %v", subnet.Name, err)
							return err
						}
					}
				}
			} else {
				// centralized subnet, not enable ecmp, no ecmp and distributed policy route about this subnet
				// use vpc spec policy route to control policy route diff update

				// check if activateGateway still ready
				if subnet.Status.ActivateGateway != "" && util.GatewayContains(subnet.Spec.GatewayNode, subnet.Status.ActivateGateway) {
					node, err := c.nodesLister.Get(subnet.Status.ActivateGateway)
					if err == nil && nodeReady(node) {
						klog.Infof("subnet %s uses the old activate gw %s", subnet.Name, node.Name)
						return nil
					}
				}

				klog.Info("find a new activate node")
				// need a new activate gateway
				newActivateNode := ""
				var nodeTunlIPAddr []net.IP
				for _, gw := range strings.Split(subnet.Spec.GatewayNode, ",") {
					// the format of gatewayNodeStr can be like 'kube-ovn-worker:172.18.0.2, kube-ovn-control-plane:172.18.0.3', which consists of node name and designative egress ip
					if strings.Contains(gw, ":") {
						gw = strings.TrimSpace(strings.Split(gw, ":")[0])
					} else {
						gw = strings.TrimSpace(gw)
					}
					node, err := c.nodesLister.Get(gw)
					if err == nil && nodeReady(node) {
						newActivateNode = node.Name
						nodeTunlIPAddr, err = getNodeTunlIP(node)
						if err != nil {
							return err
						}
						klog.Infof("subnet %s uses a new activate gw %s", subnet.Name, node.Name)
						break
					}
				}
				if newActivateNode == "" {
					klog.Warningf("all subnet %s gws are not ready", subnet.Name)
					c.recorder.Eventf(subnet, v1.EventTypeWarning, "NoActiveGatewayFound", fmt.Sprintf("subnet %s gws are not ready", subnet.Name))
					subnet.Status.ActivateGateway = newActivateNode
					bytes, err := subnet.Status.Bytes()
					if err != nil {
						return err
					}
					if _, err = c.config.KubeOvnClient.KubeovnV1().Subnets().Patch(context.Background(), subnet.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status"); err != nil {
						klog.Errorf("failed to patch subnet %s NoReadyGateway status: %v", subnet.Name, err)
					}
					return err
				}

				nextHop := getNextHopByTunnelIP(nodeTunlIPAddr)
				klog.Infof("subnet %s configure new gateway node, nextHop %s", subnet.Name, nextHop)
				if err = c.addPolicyRouteForCentralizedSubnet(subnet, newActivateNode, nil, strings.Split(nextHop, ",")); err != nil {
					klog.Errorf("failed to add active-backup policy route for centralized subnet %s: %v", subnet.Name, err)
					return err
				}
				klog.Infof("delete old distributed policy route for subnet %s", subnet.Name)
				if err := c.deletePolicyRouteByGatewayType(subnet, kubeovnv1.GWDistributedType, false); err != nil {
					klog.Errorf("failed to delete policy route for overlay subnet %s, %v", subnet.Name, err)
					return err
				}
				subnet.Status.ActivateGateway = newActivateNode
				c.patchSubnetStatus(subnet, "ReconcileCentralizedGatewaySuccess", "")
			}
		}
	}
	return nil
}

func (c *Controller) reconcileOvnCustomVpcRoute(subnet *kubeovnv1.Subnet) error {
	// in custom vpc, subnet gw type is unmeaning
	// 1. vpc out to public network through vpc nat gw pod, the static route is auto managed by admin user
	// 2. vpc out to public network through ovn snat, with bfd ecmp, the static route is auto managed here
	// 3. vpc out to public network through ovn snat, without bfd ecmp, the static route is auto managed in vpc update process

	vpc, err := c.vpcsLister.Get(subnet.Spec.Vpc)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	if vpc.Spec.EnableExternal && vpc.Spec.EnableBfd && subnet.Spec.EnableEcmp {
		klog.V(3).Infof("add bfd and external static ecmp route for vpc %s, subnet %s", vpc.Name, subnet.Name)
		// handle vpc static route
		// use static ecmp route with bfd
		// bfd ecmp static route depend on subnet cidr
		if err := c.reconcileVpcUseBfdStaticRoute(vpc.Name, subnet.Name); err != nil {
			klog.Errorf("failed to reconcile vpc %q bfd static route", vpc.Name)
			return err
		}
	}
	return nil
}

func (c *Controller) deleteStaticRoute(ip, router string) error {
	for _, ipStr := range strings.Split(ip, ",") {
		if err := c.ovnLegacyClient.DeleteStaticRoute(ipStr, router); err != nil {
			klog.Errorf("failed to delete static route %s, %v", ipStr, err)
			return err
		}
	}

	return nil
}

func (c *Controller) reconcileVlan(subnet *kubeovnv1.Subnet) error {
	if subnet.Spec.Vlan == "" {
		return nil
	}
	klog.Infof("reconcile vlan %v", subnet.Spec.Vlan)
	isExternalGatewaySwitch := !subnet.Spec.LogicalGateway && subnet.Name == c.config.ExternalGatewaySwitch
	if isExternalGatewaySwitch {
		// external gw deal this vlan subnet, just skip
		klog.Infof("skip reconcile vlan subnet %s", c.config.ExternalGatewaySwitch)
		return nil
	}

	vlan, err := c.vlansLister.Get(subnet.Spec.Vlan)
	if err != nil {
		klog.Errorf("failed to get vlan %s: %v", subnet.Spec.Vlan, err)
		return err
	}

	localnetPort := ovs.GetLocalnetName(subnet.Name)
	if err := c.ovnClient.CreateLocalnetLogicalSwitchPort(subnet.Name, localnetPort, vlan.Spec.Provider, vlan.Spec.ID); err != nil {
		klog.Errorf("create localnet port for subnet %s: %v", subnet.Name, err)
		return err
	}

	if !util.ContainsString(vlan.Status.Subnets, subnet.Name) {
		newVlan := vlan.DeepCopy()
		newVlan.Status.Subnets = append(newVlan.Status.Subnets, subnet.Name)
		_, err = c.config.KubeOvnClient.KubeovnV1().Vlans().UpdateStatus(context.Background(), newVlan, metav1.UpdateOptions{})
		if err != nil {
			klog.Errorf("failed to update status of vlan %s: %v", vlan.Name, err)
			return err
		}
	}

	return nil
}

func (c *Controller) reconcileU2OInterconnectionIP(subnet *kubeovnv1.Subnet) error {

	needCalcIP := false
	if subnet.Spec.U2OInterconnection {
		if subnet.Status.U2OInterconnectionIP == "" {
			u2oInterconnName := fmt.Sprintf(util.U2OInterconnName, subnet.Spec.Vpc, subnet.Name)
			u2oInterconnLrpName := fmt.Sprintf("%s-%s", subnet.Spec.Vpc, subnet.Name)
			v4ip, v6ip, _, err := c.acquireIpAddress(subnet.Name, u2oInterconnName, u2oInterconnLrpName)
			if err != nil {
				klog.Errorf("failed to acquire underlay to overlay interconnection ip address for subnet %s, %v", subnet.Name, err)
				return err
			}
			switch subnet.Spec.Protocol {
			case kubeovnv1.ProtocolIPv4:
				subnet.Status.U2OInterconnectionIP = v4ip
			case kubeovnv1.ProtocolIPv6:
				subnet.Status.U2OInterconnectionIP = v6ip
			case kubeovnv1.ProtocolDual:
				subnet.Status.U2OInterconnectionIP = fmt.Sprintf("%s,%s", v4ip, v6ip)
			}
			if err := c.createOrUpdateCrdIPs(u2oInterconnName, subnet.Status.U2OInterconnectionIP, "", subnet.Name, "default", "", "", "", nil); err != nil {
				klog.Errorf("failed to create or update IPs of %s : %v", u2oInterconnLrpName, err)
				return err
			}

			needCalcIP = true
		}
	} else {
		if subnet.Status.U2OInterconnectionIP != "" {
			u2oInterconnName := fmt.Sprintf(util.U2OInterconnName, subnet.Spec.Vpc, subnet.Name)
			c.ipam.ReleaseAddressByPod(u2oInterconnName)
			subnet.Status.U2OInterconnectionIP = ""

			if err := c.config.KubeOvnClient.KubeovnV1().IPs().Delete(context.Background(), u2oInterconnName, metav1.DeleteOptions{}); err != nil {
				if !k8serrors.IsNotFound(err) {
					klog.Errorf("failed to delete ip %s, %v", u2oInterconnName, err)
					return err
				}
			}

			needCalcIP = true
		}
	}

	if needCalcIP {
		klog.Infof("reconcile underlay subnet %s  to overlay interconnection with U2OInterconnection %v U2OInterconnectionIP %s ",
			subnet.Name, subnet.Spec.U2OInterconnection, subnet.Status.U2OInterconnectionIP)
		if subnet.Spec.Protocol == kubeovnv1.ProtocolDual {
			if err := calcDualSubnetStatusIP(subnet, c); err != nil {
				return err
			}
		} else {
			if err := calcSubnetStatusIP(subnet, c); err != nil {
				return err
			}
		}
	}
	return nil
}

func calcDualSubnetStatusIP(subnet *kubeovnv1.Subnet, c *Controller) error {
	if err := util.CheckCidrs(subnet.Spec.CIDRBlock); err != nil {
		return err
	}
	// Get the number of pods, not ips. For one pod with two ip(v4 & v6) in dual-stack, num of Items is 1
	podUsedIPs, err := c.config.KubeOvnClient.KubeovnV1().IPs().List(context.Background(), metav1.ListOptions{
		LabelSelector: fields.OneTermEqualSelector(subnet.Name, "").String(),
	})
	if err != nil {
		return err
	}

	// subnet.Spec.ExcludeIps contains both v4 and v6 addresses
	v4ExcludeIps, v6ExcludeIps := util.SplitIpsByProtocol(subnet.Spec.ExcludeIps)
	// gateway always in excludeIPs
	cidrBlocks := strings.Split(subnet.Spec.CIDRBlock, ",")
	v4toSubIPs := util.ExpandExcludeIPs(v4ExcludeIps, cidrBlocks[0])
	v6toSubIPs := util.ExpandExcludeIPs(v6ExcludeIps, cidrBlocks[1])
	_, v4CIDR, _ := net.ParseCIDR(cidrBlocks[0])
	_, v6CIDR, _ := net.ParseCIDR(cidrBlocks[1])
	v4availableIPs := util.AddressCount(v4CIDR) - util.CountIpNums(v4toSubIPs)
	v6availableIPs := util.AddressCount(v6CIDR) - util.CountIpNums(v6toSubIPs)

	usingIPs := float64(len(podUsedIPs.Items))

	vipSelectors := fields.AndSelectors(fields.OneTermEqualSelector(util.SubnetNameLabel, subnet.Name),
		fields.OneTermEqualSelector(util.IpReservedLabel, "")).String()
	vips, err := c.config.KubeOvnClient.KubeovnV1().Vips().List(context.Background(), metav1.ListOptions{
		LabelSelector: vipSelectors,
	})
	if err != nil {
		return err
	}
	usingIPs += float64(len(vips.Items))

	if subnet.Name == util.VpcExternalNet {
		eips, err := c.iptablesEipsLister.List(
			labels.SelectorFromSet(labels.Set{util.SubnetNameLabel: subnet.Name}))
		if err != nil {
			return err
		}
		usingIPs += float64(len(eips))
	}
	v4availableIPs = v4availableIPs - usingIPs
	if v4availableIPs < 0 {
		v4availableIPs = 0
	}
	v6availableIPs = v6availableIPs - usingIPs
	if v6availableIPs < 0 {
		v6availableIPs = 0
	}

	v4UsingIPStr, v6UsingIPStr, v4AvailableIPStr, v6AvailableIPStr := c.ipam.GetSubnetIPRangeString(subnet.Name)

	if subnet.Status.V4AvailableIPs == v4availableIPs &&
		subnet.Status.V6AvailableIPs == v6availableIPs &&
		subnet.Status.V4UsingIPs == usingIPs &&
		subnet.Status.V6UsingIPs == usingIPs &&
		subnet.Status.V4UsingIPRange == v4UsingIPStr &&
		subnet.Status.V6UsingIPRange == v6UsingIPStr &&
		subnet.Status.V4AvailableIPRange == v4AvailableIPStr &&
		subnet.Status.V6AvailableIPRange == v6AvailableIPStr {
		return nil
	}

	subnet.Status.V4AvailableIPs = v4availableIPs
	subnet.Status.V6AvailableIPs = v6availableIPs
	subnet.Status.V4UsingIPs = usingIPs
	subnet.Status.V6UsingIPs = usingIPs
	subnet.Status.V4UsingIPRange = v4UsingIPStr
	subnet.Status.V6UsingIPRange = v6UsingIPStr
	subnet.Status.V4AvailableIPRange = v4AvailableIPStr
	subnet.Status.V6AvailableIPRange = v6AvailableIPStr

	bytes, err := subnet.Status.Bytes()
	if err != nil {
		return err
	}
	_, err = c.config.KubeOvnClient.KubeovnV1().Subnets().Patch(context.Background(), subnet.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status")
	return err
}

func calcSubnetStatusIP(subnet *kubeovnv1.Subnet, c *Controller) error {
	_, cidr, err := net.ParseCIDR(subnet.Spec.CIDRBlock)
	if err != nil {
		return err
	}
	podUsedIPs, err := c.config.KubeOvnClient.KubeovnV1().IPs().List(context.Background(), metav1.ListOptions{
		LabelSelector: fields.OneTermEqualSelector(subnet.Name, "").String(),
	})
	if err != nil {
		return err
	}
	// gateway always in excludeIPs
	toSubIPs := util.ExpandExcludeIPs(subnet.Spec.ExcludeIps, subnet.Spec.CIDRBlock)
	availableIPs := util.AddressCount(cidr) - util.CountIpNums(toSubIPs)
	usingIPs := float64(len(podUsedIPs.Items))
	vipSelectors := fields.AndSelectors(fields.OneTermEqualSelector(util.SubnetNameLabel, subnet.Name),
		fields.OneTermEqualSelector(util.IpReservedLabel, "")).String()
	vips, err := c.config.KubeOvnClient.KubeovnV1().Vips().List(context.Background(), metav1.ListOptions{
		LabelSelector: vipSelectors,
	})
	if err != nil {
		return err
	}
	usingIPs += float64(len(vips.Items))
	if subnet.Name == util.VpcExternalNet {
		eips, err := c.iptablesEipsLister.List(
			labels.SelectorFromSet(labels.Set{util.SubnetNameLabel: subnet.Name}))
		if err != nil {
			return err
		}
		usingIPs += float64(len(eips))
	}

	availableIPs = availableIPs - usingIPs
	if availableIPs < 0 {
		availableIPs = 0
	}

	v4UsingIPStr, v6UsingIPStr, v4AvailableIPStr, v6AvailableIPStr := c.ipam.GetSubnetIPRangeString(subnet.Name)
	cachedFloatFields := [4]float64{
		subnet.Status.V4AvailableIPs,
		subnet.Status.V4UsingIPs,
		subnet.Status.V6AvailableIPs,
		subnet.Status.V6UsingIPs,
	}
	cachedStringFields := [4]string{
		subnet.Status.V4UsingIPRange,
		subnet.Status.V4AvailableIPRange,
		subnet.Status.V6UsingIPRange,
		subnet.Status.V6AvailableIPRange,
	}

	if subnet.Spec.Protocol == kubeovnv1.ProtocolIPv4 {
		subnet.Status.V4AvailableIPs = availableIPs
		subnet.Status.V4UsingIPs = usingIPs
		subnet.Status.V4UsingIPRange = v4UsingIPStr
		subnet.Status.V4AvailableIPRange = v4AvailableIPStr
		subnet.Status.V6AvailableIPs = 0
		subnet.Status.V6UsingIPs = 0
	} else {
		subnet.Status.V6AvailableIPs = availableIPs
		subnet.Status.V6UsingIPs = usingIPs
		subnet.Status.V6UsingIPRange = v6UsingIPStr
		subnet.Status.V6AvailableIPRange = v6AvailableIPStr
		subnet.Status.V4AvailableIPs = 0
		subnet.Status.V4UsingIPs = 0
	}
	if cachedFloatFields == [4]float64{
		subnet.Status.V4AvailableIPs,
		subnet.Status.V4UsingIPs,
		subnet.Status.V6AvailableIPs,
		subnet.Status.V6UsingIPs,
	} && cachedStringFields == [4]string{
		subnet.Status.V4UsingIPRange,
		subnet.Status.V4AvailableIPRange,
		subnet.Status.V6UsingIPRange,
		subnet.Status.V6AvailableIPRange,
	} {
		return nil
	}

	bytes, err := subnet.Status.Bytes()
	if err != nil {
		return err
	}
	_, err = c.config.KubeOvnClient.KubeovnV1().Subnets().Patch(context.Background(), subnet.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status")
	return err
}

func isOvnSubnet(subnet *kubeovnv1.Subnet) bool {
	return subnet.Spec.Provider == "" || subnet.Spec.Provider == util.OvnProvider || strings.HasSuffix(subnet.Spec.Provider, "ovn")
}

func checkAndFormatsExcludeIps(subnet *kubeovnv1.Subnet) bool {
	var excludeIps []string
	mapIps := make(map[string]ipam.IPRange, len(subnet.Spec.ExcludeIps))

	for _, excludeIP := range subnet.Spec.ExcludeIps {
		ips := strings.Split(excludeIP, "..")
		if len(ips) == 1 {
			if _, ok := mapIps[excludeIP]; !ok {
				ipr := ipam.IPRange{Start: ipam.IP(ips[0]), End: ipam.IP(ips[0])}
				mapIps[excludeIP] = ipr
			}
		} else {
			if _, ok := mapIps[excludeIP]; !ok {
				ipr := ipam.IPRange{Start: ipam.IP(ips[0]), End: ipam.IP(ips[1])}
				mapIps[excludeIP] = ipr
			}
		}
	}
	newMap := filterRepeatIPRange(mapIps)
	for _, v := range newMap {
		if v.Start == v.End {
			excludeIps = append(excludeIps, string(v.Start))
		} else {
			excludeIps = append(excludeIps, string(v.Start)+".."+string(v.End))
		}
	}
	sort.Strings(excludeIps)
	klog.V(3).Infof("excludeips before format is %v, after format is %v", subnet.Spec.ExcludeIps, excludeIps)
	if !reflect.DeepEqual(subnet.Spec.ExcludeIps, excludeIps) {
		subnet.Spec.ExcludeIps = excludeIps
		return true
	}
	return false
}

func filterRepeatIPRange(mapIps map[string]ipam.IPRange) map[string]ipam.IPRange {
	for ka, a := range mapIps {
		for kb, b := range mapIps {
			if ka == kb && a == b {
				continue
			}

			if b.End.LessThan(a.Start) || b.Start.GreaterThan(a.End) {
				continue
			}

			if (a.Start.Equal(b.Start) || a.Start.GreaterThan(b.Start)) &&
				(a.End.Equal(b.End) || a.End.LessThan(b.End)) {
				delete(mapIps, ka)
				continue
			}

			if (a.Start.Equal(b.Start) || a.Start.GreaterThan(b.Start)) &&
				a.End.GreaterThan(b.End) {
				ipr := ipam.IPRange{Start: b.Start, End: a.End}
				delete(mapIps, ka)
				mapIps[kb] = ipr
				continue
			}

			if (a.End.Equal(b.End) || a.End.LessThan(b.End)) &&
				a.Start.LessThan(b.Start) {
				ipr := ipam.IPRange{Start: a.Start, End: b.End}
				delete(mapIps, ka)
				mapIps[kb] = ipr
				continue
			}

			// a contains b
			mapIps[kb] = a
			delete(mapIps, ka)
		}
	}
	return mapIps
}

func (c *Controller) checkGwNodeExists(gatewayNode string) bool {
	found := false
	for _, gwName := range strings.Split(gatewayNode, ",") {
		// the format of gatewayNode can be like 'kube-ovn-worker:172.18.0.2, kube-ovn-control-plane:172.18.0.3', which consists of node name and designative egress ip
		if strings.Contains(gwName, ":") {
			gwName = strings.TrimSpace(strings.Split(gwName, ":")[0])
		} else {
			gwName = strings.TrimSpace(gwName)
		}

		gwNode, err := c.nodesLister.Get(gwName)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				klog.Errorf("gw node %s does not exist, %v", gwName, err)
				continue
			}
		}
		if gwNode != nil {
			found = true
			break
		}
	}
	return found
}

func (c *Controller) addCommonRoutesForSubnet(subnet *kubeovnv1.Subnet) error {
	for _, cidr := range strings.Split(subnet.Spec.CIDRBlock, ",") {
		if cidr == "" {
			continue
		}

		var gateway string
		protocol := util.CheckProtocol(cidr)
		for _, gw := range strings.Split(subnet.Spec.Gateway, ",") {
			if util.CheckProtocol(gw) == protocol {
				gateway = gw
				break
			}
		}
		if gateway == "" {
			return fmt.Errorf("failed to get gateway of CIDR %s", cidr)
		}

		// policy route
		af := 4
		if protocol == kubeovnv1.ProtocolIPv6 {
			af = 6
		}
		match := fmt.Sprintf("ip%d.dst == %s", af, cidr)
		exist, err := c.ovnLegacyClient.PolicyRouteExists(util.SubnetRouterPolicyPriority, match)
		if err != nil {
			return err
		}
		if !exist {
			externalIDs := map[string]string{"vendor": util.CniTypeName, "subnet": subnet.Name}
			klog.Infof("add policy route for router: %s, match %s, action %s, nexthop %s, extrenalID %v", c.config.ClusterRouter, match, "allow", "", externalIDs)
			if err = c.ovnLegacyClient.AddPolicyRoute(c.config.ClusterRouter, util.SubnetRouterPolicyPriority, match, "allow", "", externalIDs); err != nil {
				klog.Errorf("failed to add logical router policy for CIDR %s of subnet %s: %v", cidr, subnet.Name, err)
				return err
			}
		}

	}
	return nil
}

func getOverlaySubnetsPortGroupName(subnetName, nodeName string) string {
	return strings.Replace(fmt.Sprintf("%s.%s", subnetName, nodeName), "-", ".", -1)
}

func (c *Controller) createPortGroupForDistributedSubnet(node *v1.Node, subnet *kubeovnv1.Subnet) error {
	if subnet.Spec.Vlan != "" && !subnet.Spec.LogicalGateway {
		return nil
	}
	if subnet.Spec.Vpc != util.DefaultVpc || subnet.Name == c.config.NodeSwitch {
		return nil
	}

	pgName := getOverlaySubnetsPortGroupName(subnet.Name, node.Name)
	if err := c.ovnClient.CreatePortGroup(pgName, map[string]string{networkPolicyKey: subnet.Name + "/" + node.Name}); err != nil {
		klog.Errorf("create port group for subnet %s and node %s: %v", subnet.Name, node.Name, err)
		return err
	}

	return nil
}

func (c *Controller) updatePolicyRouteForCentralizedSubnet(subnetName, cidr string, nextHops []string, nameIpMap map[string]string) error {
	ipSuffix := "ip4"
	if util.CheckProtocol(cidr) == kubeovnv1.ProtocolIPv6 {
		ipSuffix = "ip6"
	}
	match := fmt.Sprintf("%s.src == %s", ipSuffix, cidr)

	// there's no way to update policy route when gatewayNode changed for subnet, so delete and readd policy route
	// The delete operation is processed in AddPolicyRoute if the policy route is inconsistent, so no need delete here

	nextHopIp := strings.Join(nextHops, ",")
	externalIDs := map[string]string{
		"vendor": util.CniTypeName,
		"subnet": subnetName,
	}
	// It's difficult to delete policy route when delete node,
	// add map nodeName:nodeIP to external_ids to help process when delete node
	for node, ip := range nameIpMap {
		externalIDs[node] = ip
	}
	klog.Infof("add ecmp policy route for router: %s, match %s, action %s, nexthop %s, extrenalID %s", c.config.ClusterRouter, match, "allow", nextHopIp, externalIDs)
	if err := c.ovnLegacyClient.AddPolicyRoute(c.config.ClusterRouter, util.GatewayRouterPolicyPriority, match, "reroute", nextHopIp, externalIDs); err != nil {
		klog.Errorf("failed to add policy route for centralized subnet %s: %v", subnetName, err)
		return err
	}
	return nil
}

func (c *Controller) addPolicyRouteForCentralizedSubnet(subnet *kubeovnv1.Subnet, nodeName string, ipNameMap map[string]string, nodeIPs []string) error {
	for _, nodeIP := range nodeIPs {
		// node v4ip v6ip
		for _, cidrBlock := range strings.Split(subnet.Spec.CIDRBlock, ",") {
			if util.CheckProtocol(cidrBlock) != util.CheckProtocol(nodeIP) {
				continue
			}
			// Check for repeat policy route is processed in AddPolicyRoute

			var nextHops []string
			nameIpMap := map[string]string{}
			nextHops = append(nextHops, nodeIP)
			tmpName := nodeName
			if nodeName == "" {
				tmpName = ipNameMap[nodeIP]
			}
			nameIpMap[tmpName] = nodeIP
			if err := c.updatePolicyRouteForCentralizedSubnet(subnet.Name, cidrBlock, nextHops, nameIpMap); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Controller) deletePolicyRouteForCentralizedSubnet(subnet *kubeovnv1.Subnet) error {
	for _, cidr := range strings.Split(subnet.Spec.CIDRBlock, ",") {
		ipSuffix := "ip4"
		if util.CheckProtocol(cidr) == kubeovnv1.ProtocolIPv6 {
			ipSuffix = "ip6"
		}
		match := fmt.Sprintf("%s.src == %s", ipSuffix, cidr)
		klog.Infof("delete policy route for router: %s, priority: %d, match %s", c.config.ClusterRouter, util.GatewayRouterPolicyPriority, match)
		if err := c.ovnLegacyClient.DeletePolicyRoute(c.config.ClusterRouter, util.GatewayRouterPolicyPriority, match); err != nil {
			klog.Errorf("failed to delete policy route for centralized subnet %s: %v", subnet.Name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) addPolicyRouteForDistributedSubnet(subnet *kubeovnv1.Subnet, nodeName, nodeIPv4, nodeIPv6 string) error {
	if subnet.Spec.Vlan != "" && !subnet.Spec.LogicalGateway {
		return nil
	}
	if subnet.Spec.Vpc != util.DefaultVpc || subnet.Name == c.config.NodeSwitch {
		return nil
	}

	pgName := getOverlaySubnetsPortGroupName(subnet.Name, nodeName)
	for _, cidrBlock := range strings.Split(subnet.Spec.CIDRBlock, ",") {
		ipSuffix, nodeIP := "ip4", nodeIPv4
		if util.CheckProtocol(cidrBlock) == kubeovnv1.ProtocolIPv6 {
			ipSuffix, nodeIP = "ip6", nodeIPv6
		}
		if nodeIP == "" {
			continue
		}

		pgAs := fmt.Sprintf("%s_%s", pgName, ipSuffix)
		match := fmt.Sprintf("%s.src == $%s", ipSuffix, pgAs)
		exist, err := c.ovnLegacyClient.PolicyRouteExists(util.GatewayRouterPolicyPriority, match)
		if err != nil {
			return err
		}
		if exist {
			continue
		}

		externalIDs := map[string]string{
			"vendor": util.CniTypeName,
			"subnet": subnet.Name,
			"node":   nodeName,
		}
		klog.Infof("add policy route for router: %s, match %s, action %s, nexthop %s, extrenalID %v", c.config.ClusterRouter, match, "allow", "", externalIDs)
		if err = c.ovnLegacyClient.AddPolicyRoute(c.config.ClusterRouter, util.GatewayRouterPolicyPriority, match, "reroute", nodeIP, externalIDs); err != nil {
			klog.Errorf("failed to add logical router policy for port-group address-set %s: %v", pgAs, err)
			return err
		}
	}
	return nil
}

func (c *Controller) deletePolicyRouteForDistributedSubnet(subnet *kubeovnv1.Subnet, nodeName string) error {
	pgName := getOverlaySubnetsPortGroupName(subnet.Name, nodeName)
	for _, cidrBlock := range strings.Split(subnet.Spec.CIDRBlock, ",") {
		ipSuffix := "ip4"
		if util.CheckProtocol(cidrBlock) == kubeovnv1.ProtocolIPv6 {
			ipSuffix = "ip6"
		}
		pgAs := fmt.Sprintf("%s_%s", pgName, ipSuffix)
		match := fmt.Sprintf("%s.src == $%s", ipSuffix, pgAs)
		klog.Infof("delete policy route for router: %s, priority: %d, match %s", c.config.ClusterRouter, util.GatewayRouterPolicyPriority, match)
		if err := c.ovnLegacyClient.DeletePolicyRoute(c.config.ClusterRouter, util.GatewayRouterPolicyPriority, match); err != nil {
			klog.Errorf("failed to delete policy route for subnet %s: %v", subnet.Name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) deletePolicyRouteByGatewayType(subnet *kubeovnv1.Subnet, gatewayType string, isDelete bool) error {
	if (subnet.Spec.Vlan != "" && !subnet.Spec.LogicalGateway) || subnet.Spec.Vpc != util.DefaultVpc {
		return nil
	}

	for _, cidr := range strings.Split(subnet.Spec.CIDRBlock, ",") {
		if cidr == "" || !isDelete {
			continue
		}

		af := 4
		if util.CheckProtocol(cidr) == kubeovnv1.ProtocolIPv6 {
			af = 6
		}
		match := fmt.Sprintf("ip%d.dst == %s", af, cidr)
		klog.Infof("delete policy route for router: %s, priority: %d, match %s", c.config.ClusterRouter, util.SubnetRouterPolicyPriority, match)
		if err := c.ovnLegacyClient.DeletePolicyRoute(c.config.ClusterRouter, util.SubnetRouterPolicyPriority, match); err != nil {
			klog.Errorf("failed to delete logical router policy for CIDR %s of subnet %s: %v", cidr, subnet.Name, err)
			return err
		}
	}
	if subnet.Name == c.config.NodeSwitch {
		return nil
	}

	if gatewayType == kubeovnv1.GWDistributedType {
		nodes, err := c.nodesLister.List(labels.Everything())
		if err != nil {
			klog.Errorf("list nodes: %v", err)
			return err
		}
		for _, node := range nodes {
			pgName := getOverlaySubnetsPortGroupName(subnet.Name, node.Name)
			if err = c.ovnClient.DeletePortGroup(pgName); err != nil {
				klog.Errorf("delete port group for subnet %s and node %s: %v", subnet.Name, node.Name, err)
				return err
			}

			if err = c.deletePolicyRouteForDistributedSubnet(subnet, node.Name); err != nil {
				klog.Errorf("delete policy route for subnet %s and node %s: %v", subnet.Name, node.Name, err)
				return err
			}
		}
	}

	if gatewayType == kubeovnv1.GWCentralizedType {
		klog.Infof("delete policy route for centralized subnet %s", subnet.Name)
		if err := c.deletePolicyRouteForCentralizedSubnet(subnet); err != nil {
			klog.Errorf("delete policy route for subnet %s: %v", subnet.Name, err)
			return err
		}
	}

	return nil
}

func (c *Controller) addPolicyRouteForU2OInterconn(subnet *kubeovnv1.Subnet) error {

	var v4Gw, v6Gw string
	for _, gw := range strings.Split(subnet.Spec.Gateway, ",") {
		switch util.CheckProtocol(gw) {
		case kubeovnv1.ProtocolIPv4:
			v4Gw = gw
		case kubeovnv1.ProtocolIPv6:
			v6Gw = gw
		}
	}

	externalIDs := map[string]string{
		"vendor":           util.CniTypeName,
		"subnet":           subnet.Name,
		"isU2ORoutePolicy": "true",
	}

	nodes, err := c.nodesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list nodes: %v", err)
		return err
	}

	var nodesIPv4, nodesIPv6 []string
	for _, node := range nodes {
		nodeIPv4, nodeIPv6 := util.GetNodeInternalIP(*node)

		if nodeIPv4 != "" {
			nodesIPv4 = append(nodesIPv4, nodeIPv4)
		}
		if nodeIPv6 != "" {
			nodesIPv6 = append(nodesIPv6, nodeIPv6)
		}
	}

	u2oExcludeIp4Ag := strings.Replace(fmt.Sprintf(util.U2OExcludeIPAg, subnet.Name, "ip4"), "-", ".", -1)
	u2oExcludeIp6Ag := strings.Replace(fmt.Sprintf(util.U2OExcludeIPAg, subnet.Name, "ip6"), "-", ".", -1)
	if err := c.ovnLegacyClient.CreateAddressSet(u2oExcludeIp4Ag); err != nil {
		klog.Errorf("failed to create address set %s %v", u2oExcludeIp4Ag, err)
		return err
	}

	if err := c.ovnLegacyClient.CreateAddressSet(u2oExcludeIp6Ag); err != nil {
		klog.Errorf("failed to create address set %s %v", u2oExcludeIp6Ag, err)
		return err
	}

	if len(nodesIPv4) > 0 {
		if err := c.ovnLegacyClient.SetAddressesToAddressSet(nodesIPv4, u2oExcludeIp4Ag); err != nil {
			klog.Errorf("failed to set v4 address set %s with address %v err %v", u2oExcludeIp4Ag, nodesIPv4, err)
			return err
		}
	}

	if len(nodesIPv6) > 0 {
		if err := c.ovnLegacyClient.SetAddressesToAddressSet(nodesIPv6, u2oExcludeIp6Ag); err != nil {
			klog.Errorf("failed to set v6 address set %s with address %v err %v", u2oExcludeIp6Ag, nodesIPv6, err)
			return err
		}
	}

	for _, cidrBlock := range strings.Split(subnet.Spec.CIDRBlock, ",") {
		ipSuffix := "ip4"
		nextHop := v4Gw
		U2OexcludeIPAs := u2oExcludeIp4Ag
		if util.CheckProtocol(cidrBlock) == kubeovnv1.ProtocolIPv6 {
			ipSuffix = "ip6"
			nextHop = v6Gw
			U2OexcludeIPAs = u2oExcludeIp6Ag
		}

		match1 := fmt.Sprintf("%s.dst == %s && %s.dst != $%s", ipSuffix, cidrBlock, ipSuffix, U2OexcludeIPAs)
		match2 := fmt.Sprintf("%s.dst == $%s && %s.src == %s", ipSuffix, U2OexcludeIPAs, ipSuffix, cidrBlock)
		match3 := fmt.Sprintf("%s.src == %s", ipSuffix, cidrBlock)

		/*
			policy1:
			prio 31000 match: "ip4.dst == underlay subnet cidr && ip4.dst != node ips"  action: allow

			policy2:
			prio 31000 match: "ip4.dst == node ips && ip4.src == underlay subnet cidr"  action: allow

			policy3:
			prio 29000 match: "ip4.src == underlay subnet cidr"                         action: reroute physical gw

			comment:
			policy1 and policy2 allow overlay pod access underlay but when overlay pod access node ip, it should go join subnet,
			policy3: underlay pod first access u2o interconnection lrp and then reoute to physical gw
		*/
		klog.Infof("add u2o interconnection policy for router: %s, match %s, action %s", subnet.Spec.Vpc, match1, "allow")
		if err := c.ovnLegacyClient.AddPolicyRoute(subnet.Spec.Vpc, util.SubnetRouterPolicyPriority, match1, "allow", "", externalIDs); err != nil {
			klog.Errorf("failed to add u2o interconnection policy1 for subnet %s %v", subnet.Name, err)
			return err
		}

		klog.Infof("add u2o interconnection policy for router: %s, match %s, action %s", subnet.Spec.Vpc, match2, "allow")
		if err := c.ovnLegacyClient.AddPolicyRoute(subnet.Spec.Vpc, util.SubnetRouterPolicyPriority, match2, "allow", "", externalIDs); err != nil {
			klog.Errorf("failed to add u2o interconnection policy2 for subnet %s %v", subnet.Name, err)
			return err
		}

		klog.Infof("add u2o interconnection policy for router: %s, match %s, action %s, nexthop %s", subnet.Spec.Vpc, match3, "reroute", nextHop)
		if err := c.ovnLegacyClient.AddPolicyRoute(subnet.Spec.Vpc, util.GatewayRouterPolicyPriority, match3, "reroute", nextHop, externalIDs); err != nil {
			klog.Errorf("failed to add u2o interconnection policy3 for subnet %s %v", subnet.Name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) deletePolicyRouteForU2OInterconn(subnet *kubeovnv1.Subnet) error {

	results, err := c.ovnLegacyClient.CustomFindEntity("Logical_Router_Policy", []string{"_uuid", "match", "priority"},
		"external_ids:isU2ORoutePolicy=\"true\"",
		fmt.Sprintf("external_ids:vendor=\"%s\"", util.CniTypeName),
		fmt.Sprintf("external_ids:subnet=\"%s\"", subnet.Name))
	if err != nil {
		klog.Errorf("customFindEntity failed, %v", err)
		return err
	}

	if len(results) == 0 {
		return nil
	}

	var uuids []string
	for _, result := range results {
		uuids = append(uuids, result["_uuid"][0])
		klog.Infof("delete u2o interconnection policy for router %s with match %s priority %s ", subnet.Spec.Vpc, result["match"], result["priority"])
	}

	if err := c.ovnLegacyClient.DeletePolicyRouteByUUID(subnet.Spec.Vpc, uuids); err != nil {
		klog.Errorf("failed to delete u2o interconnection policy for subnet %s: %v", subnet.Name, err)
		return err
	}

	u2oExcludeIp4Ag := strings.Replace(fmt.Sprintf(util.U2OExcludeIPAg, subnet.Name, "ip4"), "-", ".", -1)
	u2oExcludeIp6Ag := strings.Replace(fmt.Sprintf(util.U2OExcludeIPAg, subnet.Name, "ip6"), "-", ".", -1)

	if err := c.ovnLegacyClient.DeleteAddressSet(u2oExcludeIp4Ag); err != nil {
		klog.Errorf("failed to delete address set %s %v", u2oExcludeIp4Ag, err)
		return err
	}

	if err := c.ovnLegacyClient.DeleteAddressSet(u2oExcludeIp6Ag); err != nil {
		klog.Errorf("failed to delete address set %s %v", u2oExcludeIp6Ag, err)
		return err
	}

	return nil
}
