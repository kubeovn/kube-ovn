package controller

import (
	"context"
	"fmt"
	"net"
	"reflect"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ipam"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) enqueueAddSubnet(obj interface{}) {
	if !c.isLeader() {
		return
	}
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
	if !c.isLeader() {
		return
	}
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
	if !c.isLeader() {
		return
	}
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
	if !newSubnet.DeletionTimestamp.IsZero() && usingIPs == 0 {
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
		oldSubnet.Spec.Vlan != newSubnet.Spec.Vlan {
		klog.V(3).Infof("enqueue update subnet %s", key)
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
	if subnet.Spec.Protocol == "" || subnet.Spec.Protocol != util.CheckProtocol(subnet.Spec.CIDRBlock) {
		subnet.Spec.Protocol = util.CheckProtocol(subnet.Spec.CIDRBlock)
		changed = true
	}
	if subnet.Spec.GatewayType == "" {
		subnet.Spec.GatewayType = kubeovnv1.GWDistributedType
		changed = true
	}
	if subnet.Spec.Vpc == "" {
		changed = true
		subnet.Spec.Vpc = util.DefaultVpc

		// Some features only work in the default VPC
		if subnet.Spec.Default && subnet.Name != c.config.DefaultLogicalSwitch {
			subnet.Spec.Default = false
		}
		if subnet.Spec.Vlan != "" {
			if _, err := c.vlansLister.Get(subnet.Spec.Vlan); err != nil {
				klog.Warningf("subnet %s reference a none exist vlan %s", subnet.Name, subnet.Spec.Vlan)
				subnet.Spec.Vlan = ""
			}
		}
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
			return false, fmt.Errorf("subnet %s cidr %s is not a valid cidrblock", subnet.Name, cidr)
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

	if !subnet.DeletionTimestamp.IsZero() && usingIps == 0 {
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
		if reason == "SetPrivateLogicalSwitchSuccess" || reason == "ResetLogicalSwitchAclSuccess" {
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
	cachedSubnet, err := c.subnetsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	subnet := cachedSubnet.DeepCopy()
	deleted, err := c.handleSubnetFinalizer(subnet)
	if err != nil {
		klog.Errorf("handle subnet finalizer failed %v", err)
		return err
	}
	if deleted {
		return nil
	}

	if cachedSubnet, err = c.subnetsLister.Get(key); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	subnet = cachedSubnet.DeepCopy()
	if err = formatSubnet(subnet, c); err != nil {
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

	if util.CheckProtocol(subnet.Spec.CIDRBlock) == kubeovnv1.ProtocolDual {
		err = calcDualSubnetStatusIP(subnet, c)
	} else {
		err = calcSubnetStatusIP(subnet, c)
	}
	if err != nil {
		klog.Errorf("calculate subnet %s used ip failed, %v", subnet.Name, err)
		return err
	}

	if err := c.ipam.AddOrUpdateSubnet(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.ExcludeIps); err != nil {
		return err
	}

	if !isOvnSubnet(subnet) {
		return nil
	}

	if err = util.ValidateSubnet(*subnet); err != nil {
		klog.Errorf("failed to validate subnet %s, %v", subnet.Name, err)
		c.patchSubnetStatus(subnet, "ValidateLogicalSwitchFailed", err.Error())
		return err
	} else {
		c.patchSubnetStatus(subnet, "ValidateLogicalSwitchSuccess", "")
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

		if util.CIDRConflict(sub.Spec.CIDRBlock, subnet.Spec.CIDRBlock) {
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

	exist, err := c.ovnClient.LogicalSwitchExists(subnet.Name, c.config.EnableExternalVpc)
	if err != nil {
		klog.Errorf("failed to list logical switch, %v", err)
		c.patchSubnetStatus(subnet, "ListLogicalSwitchFailed", err.Error())
		return err
	}

	needRouter := subnet.Spec.Vlan == "" || subnet.Spec.LogicalGateway
	if !exist {
		subnet.Status.EnsureStandardConditions()
		// If multiple namespace use same ls name, only first one will success
		if err := c.ovnClient.CreateLogicalSwitch(subnet.Name, vpc.Status.Router, subnet.Spec.Protocol, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, subnet.Spec.ExcludeIps, needRouter); err != nil {
			c.patchSubnetStatus(subnet, "CreateLogicalSwitchFailed", err.Error())
			return err
		}
	} else {
		// logical switch exists, only update other_config
		if err := c.ovnClient.SetLogicalSwitchConfig(subnet.Name, vpc.Status.Router, subnet.Spec.Protocol, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, subnet.Spec.ExcludeIps, needRouter); err != nil {
			c.patchSubnetStatus(subnet, "SetLogicalSwitchConfigFailed", err.Error())
			return err
		}
		if !needRouter {
			if err := c.ovnClient.RemoveRouterPort(subnet.Name, vpc.Status.Router); err != nil {
				klog.Errorf("failed to remove router port from %s, %v", subnet.Name, err)
				return err
			}
		}
	}

	if err = c.updateNodeAddressSetsForSubnet(subnet, false); err != nil {
		klog.Errorf("failed to update node address sets for addition of subnet %s: %v", subnet.Name, err)
		return err
	}

	if c.config.EnableLb && subnet.Name != c.config.NodeSwitch {
		if err := c.ovnClient.AddLbToLogicalSwitch(vpc.Status.TcpLoadBalancer, vpc.Status.TcpSessionLoadBalancer, vpc.Status.UdpLoadBalancer, vpc.Status.UdpSessionLoadBalancer, subnet.Name); err != nil {
			c.patchSubnetStatus(subnet, "AddLbToLogicalSwitchFailed", err.Error())
			return err
		}
	}

	if err := c.reconcileSubnet(subnet); err != nil {
		klog.Errorf("reconcile subnet for %s failed, %v", subnet.Name, err)
		return err
	}

	if subnet.Spec.Private {
		if err := c.ovnClient.SetPrivateLogicalSwitch(subnet.Name, subnet.Spec.CIDRBlock, subnet.Spec.AllowSubnets); err != nil {
			c.patchSubnetStatus(subnet, "SetPrivateLogicalSwitchFailed", err.Error())
			return err
		}
		c.patchSubnetStatus(subnet, "SetPrivateLogicalSwitchSuccess", "")
	} else {
		if err := c.ovnClient.ResetLogicalSwitchAcl(subnet.Name); err != nil {
			c.patchSubnetStatus(subnet, "ResetLogicalSwitchAclFailed", err.Error())
			return err
		}
		c.patchSubnetStatus(subnet, "ResetLogicalSwitchAclSuccess", "")
	}

	c.updateVpcStatusQueue.Add(subnet.Spec.Vpc)
	return nil
}

func (c *Controller) handleNodeAddressSetForSubnet(cidr, node, nodeIP string, af int, delete bool) error {
	if cidr == "" || nodeIP == "" || !util.CIDRContainIP(cidr, nodeIP) {
		return nil
	}

	asName := nodeUnderlayAddressSetName(node, af)
	if delete {
		if err := c.ovnClient.RemoveAddressSetAddresses(asName, cidr); err != nil {
			klog.Errorf("failed to remove CIDR %s from address set %s: %v", cidr, asName, err)
			return err
		}
	} else {
		if err := c.ovnClient.AddAddressSetAddresses(asName, cidr); err != nil {
			klog.Errorf("failed to add CIDR %s to address set %s: %v", cidr, asName, err)
			return err
		}
	}

	return nil
}

func (c *Controller) updateNodeAddressSetsForSubnet(subnet *kubeovnv1.Subnet, delete bool) error {
	if subnet.Spec.Vlan == "" || !subnet.Spec.LogicalGateway || subnet.Spec.Vpc != util.DefaultVpc {
		return nil
	}

	nodes, err := c.nodesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list nodes: %v", err)
		return err
	}

	for _, node := range nodes {
		nodeIPv4, nodeIPv6 := util.GetNodeInternalIP(*node)
		v4CIDR, v6CIDR := util.SplitStringIP(subnet.Spec.CIDRBlock)
		if err := c.handleNodeAddressSetForSubnet(v4CIDR, node.Name, nodeIPv4, 4, delete); err != nil {
			return err
		}
		if err := c.handleNodeAddressSetForSubnet(v6CIDR, node.Name, nodeIPv6, 6, delete); err != nil {
			return err
		}
	}

	return nil
}

func (c *Controller) handleUpdateSubnetStatus(key string) error {
	orisubnet, err := c.subnetsLister.Get(key)
	subnet := orisubnet.DeepCopy()
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
	return c.deleteStaticRoute(subnet.Spec.CIDRBlock, vpc.Status.Router, subnet)
}

func (c *Controller) handleDeleteLogicalSwitch(key string) (err error) {
	c.ipam.DeleteSubnet(key)

	exist, err := c.ovnClient.LogicalSwitchExists(key, c.config.EnableExternalVpc)
	if err != nil {
		klog.Errorf("failed to list logical switch, %v", err)
		return err
	}
	if !exist {
		return nil
	}

	if err = c.ovnClient.CleanLogicalSwitchAcl(key); err != nil {
		klog.Errorf("failed to delete acl of logical switch %s %v", key, err)
		return err
	}

	if err = c.ovnClient.DeleteLogicalSwitch(key); err != nil {
		klog.Errorf("failed to delete logical switch %s %v", key, err)
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

	if err := c.updateNodeAddressSetsForSubnet(subnet, true); err != nil {
		klog.Errorf("failed to update node address sets for deletion of subnet %s: %v", subnet.Name, err)
		return err
	}

	err := c.handleDeleteLogicalSwitch(subnet.Name)
	if err != nil {
		klog.Errorf("failed to delete logical switch %s %v", subnet.Name, err)
		return err
	}
	vpc, err := c.vpcsLister.Get(subnet.Spec.Vpc)
	if err == nil && vpc.Status.Router != "" {
		if err = c.ovnClient.RemoveRouterPort(subnet.Name, vpc.Status.Router); err != nil {
			klog.Errorf("failed to delete router port %s %v", subnet.Name, err)
			return err
		}
	} else {
		if k8serrors.IsNotFound(err) {
			if err = c.ovnClient.RemoveRouterPort(subnet.Name, util.DefaultVpc); err != nil {
				klog.Errorf("failed to delete router port %s %v", subnet.Name, err)
				return err
			}
		} else {
			klog.Errorf("failed to get vpc, %v", err)
			return err
		}
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

	if subnet.Name != c.config.NodeSwitch {
		if err := c.reconcileGateway(subnet); err != nil {
			klog.Errorf("reconcile centralized gateway for subnet %s failed, %v", subnet.Name, err)
			return err
		}
	}

	if err := c.reconcileVlan(subnet); err != nil {
		klog.Errorf("reconcile vlan for subnet %s failed, %v", subnet.Name, err)
		return err
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
		if ns.Annotations != nil && util.ContainsString(strings.Split(ns.Annotations[util.LogicalSwitchAnnotation], ","), subnet.Name) {
			c.addNamespaceQueue.Add(ns.Name)
		}
	}

	return nil
}

func (c *Controller) reconcileGateway(subnet *kubeovnv1.Subnet) error {
	pods, err := c.podsLister.Pods(metav1.NamespaceAll).List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list pods %v", err)
		return err
	}

	if subnet.Spec.Vlan != "" && subnet.Spec.Vpc == util.DefaultVpc {
		for _, pod := range pods {
			if pod.Annotations[util.LogicalSwitchAnnotation] == subnet.Name && pod.Annotations[util.IpAddressAnnotation] != "" {
				if err := c.deleteStaticRoute(pod.Annotations[util.IpAddressAnnotation], c.config.ClusterRouter, subnet); err != nil {
					return err
				}
			}
		}

		if err := c.deleteStaticRoute(subnet.Spec.CIDRBlock, c.config.ClusterRouter, subnet); err != nil {
			return err
		}

		if !subnet.Spec.LogicalGateway {
			if err := c.ovnClient.DeleteLogicalSwitchPort(fmt.Sprintf("%s-%s", subnet.Name, c.config.ClusterRouter)); err != nil {
				klog.Errorf("failed to delete lsp %s-%s, %v", subnet.Name, c.config.ClusterRouter, err)
				return err
			}
			if err := c.ovnClient.DeleteLogicalRouterPort(fmt.Sprintf("%s-%s", c.config.ClusterRouter, subnet.Name)); err != nil {
				klog.Errorf("failed to delete lrp %s-%s, %v", c.config.ClusterRouter, subnet.Name, err)
				return err
			}
		}
	} else {
		// if gw is distributed remove activateGateway field
		if subnet.Spec.GatewayType == kubeovnv1.GWDistributedType {
			if subnet.Spec.GatewayNode == "" {
				return nil
			}
			subnet.Spec.GatewayNode = ""
			bytes, err := subnet.Status.Bytes()
			if err != nil {
				return err
			}
			_, err = c.config.KubeOvnClient.KubeovnV1().Subnets().Patch(context.Background(), subnet.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "")
			if err != nil {
				return err
			}

			for _, pod := range pods {
				if !isPodAlive(pod) || pod.Annotations[util.IpAddressAnnotation] == "" || pod.Annotations[util.LogicalSwitchAnnotation] != subnet.Name {
					continue
				}

				node, err := c.nodesLister.Get(pod.Spec.NodeName)
				if err != nil {
					if k8serrors.IsNotFound(err) {
						continue
					} else {
						klog.Errorf("failed to get node %s, %v", pod.Spec.NodeName, err)
						return err
					}
				}
				nodeIP, err := getNodeTunlIP(node)
				if err != nil {
					klog.Errorf("failed to get node %s tunnel ip, %v", node.Name, err)
					return err
				}

				nextHop := getNextHopByTunnelIP(nodeIP)
				if pod.Annotations[util.NorthGatewayAnnotation] != "" {
					nextHop = pod.Annotations[util.NorthGatewayAnnotation]
				}

				if err := c.ovnClient.AddStaticRoute(ovs.PolicySrcIP, pod.Annotations[util.IpAddressAnnotation], nextHop, c.config.ClusterRouter, util.NormalRouteType, false); err != nil {
					klog.Errorf("add static route failed, %v", err)
					return err
				}
			}
			return c.deleteStaticRoute(subnet.Spec.CIDRBlock, c.config.ClusterRouter, subnet)
		} else {
			if subnet.Spec.GatewayNode == "" {
				klog.Errorf("subnet %s Spec.GatewayNode field must be specified for centralized gateway type", subnet.Name)
				subnet.Status.NotReady("NoReadyGateway", "")
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
				return fmt.Errorf("failed to add ecmp static route, no gateway node exists")
			}

			if c.config.EnableEcmp {
				nodeIPs := make([]string, 0, len(strings.Split(subnet.Spec.GatewayNode, ",")))
				for _, gw := range strings.Split(subnet.Spec.GatewayNode, ",") {
					// the format of gatewayNodeStr can be like 'kube-ovn-worker:172.18.0.2, kube-ovn-control-plane:172.18.0.3', which consists of node name and designative egress ip
					if strings.Contains(gw, ":") {
						gw = strings.TrimSpace(strings.Split(gw, ":")[0])
					} else {
						gw = strings.TrimSpace(gw)
					}

					node, err := c.nodesLister.Get(gw)
					if err == nil && nodeReady(node) {
						nodeTunlIP := strings.TrimSpace(node.Annotations[util.IpAddressAnnotation])
						if nodeTunlIP == "" {
							klog.Errorf("gateway node %v has no ip annotation", node.Name)
							continue
						}
						nodeIPs = append(nodeIPs, strings.Split(nodeTunlIP, ",")...)
					} else {
						klog.Errorf("gateway node %v is not ready", gw)
					}
				}
				klog.Infof("subnet %s configure gateway node, nodeIPs %v", subnet.Name, nodeIPs)

				for _, cidr := range strings.Split(subnet.Spec.CIDRBlock, ",") {
					nextHops, err := c.filterRepeatEcmpRoutes(nodeIPs, cidr)
					if err != nil {
						klog.Errorf("failed to filter ecmp static route for CIDR %s of subnet %s: %v", cidr, subnet.Name, err)
						continue
					}
					klog.Infof("subnet %s adds centralized gw %v", subnet.Name, nextHops)

					for _, nextHop := range nextHops {
						if err = c.ovnClient.AddStaticRoute(ovs.PolicySrcIP, cidr, nextHop, c.config.ClusterRouter, util.EcmpRouteType, false); err != nil {
							klog.Errorf("failed to add static route: %v", err)
							return err
						}
					}
				}
			} else {
				if err := c.deleteEcmpRouteForNode(subnet); err != nil {
					klog.Errorf("failed to delete ecmp route for subnet %s", subnet.Name)
					return err
				}

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
					subnet.Status.ActivateGateway = newActivateNode
					subnet.Status.NotReady("NoReadyGateway", "")
					bytes, err := subnet.Status.Bytes()
					if err != nil {
						return err
					}
					_, err = c.config.KubeOvnClient.KubeovnV1().Subnets().Patch(context.Background(), subnet.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status")
					return err
				}

				nextHop := getNextHopByTunnelIP(nodeTunlIPAddr)
				if err := c.ovnClient.AddStaticRoute(ovs.PolicySrcIP, subnet.Spec.CIDRBlock, nextHop, c.config.ClusterRouter, util.NormalRouteType, false); err != nil {
					klog.Errorf("failed to add static route, %v", err)
					return err
				}
				subnet.Status.ActivateGateway = newActivateNode
			}

			for _, pod := range pods {
				if isPodAlive(pod) && pod.Annotations[util.IpAddressAnnotation] != "" && pod.Annotations[util.LogicalSwitchAnnotation] == subnet.Name && pod.Annotations[util.NorthGatewayAnnotation] == "" {
					if err := c.deleteStaticRoute(pod.Annotations[util.IpAddressAnnotation], c.config.ClusterRouter, subnet); err != nil {
						return err
					}
				}
			}

			bytes, err := subnet.Status.Bytes()
			subnet.Status.Ready("ReconcileCentralizedGatewaySuccess", "")
			if err != nil {
				return err
			}
			_, err = c.config.KubeOvnClient.KubeovnV1().Subnets().Patch(context.Background(), subnet.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status")
			return err
		}
	}
	return nil
}

func (c *Controller) deleteStaticRoute(ip, router string, subnet *kubeovnv1.Subnet) error {
	for _, ipStr := range strings.Split(ip, ",") {
		if err := c.ovnClient.DeleteStaticRoute(ipStr, router); err != nil {
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
	vlan, err := c.vlansLister.Get(subnet.Spec.Vlan)
	if err != nil {
		klog.Errorf("failed to get vlan %s: %v", subnet.Spec.Vlan, err)
		return err
	}

	localnetPort := ovs.PodNameToLocalnetName(subnet.Name)
	if err := c.ovnClient.CreateLocalnetPort(subnet.Name, localnetPort, vlan.Spec.Provider, vlan.Spec.ID); err != nil {
		klog.Errorf("failed to create localnet port for subnet %s: %v", subnet.Name, err)
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

func calcDualSubnetStatusIP(subnet *kubeovnv1.Subnet, c *Controller) error {
	if err := util.CheckCidrs(subnet.Spec.CIDRBlock); err != nil {
		return err
	}
	// Get the number of pods, not ips. For one pod with two ip(v4 & v6) in dualstack, num of Items is 1
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
	v4UsingIPs := make([]string, 0, len(podUsedIPs.Items))
	v6UsingIPs := make([]string, 0, len(podUsedIPs.Items))
	for _, podUsedIP := range podUsedIPs.Items {
		// The format of podUsedIP.Spec.IPAddress is 10.244.0.0/16,fd00:10:244::/64 when protocol is dualstack
		splitIPs := strings.Split(podUsedIP.Spec.IPAddress, ",")
		v4toSubIPs = append(v4toSubIPs, splitIPs[0])
		v4UsingIPs = append(v4UsingIPs, splitIPs[0])
		if len(splitIPs) == 2 {
			v6toSubIPs = append(v6toSubIPs, splitIPs[1])
			v6UsingIPs = append(v6UsingIPs, splitIPs[1])
		}
	}
	v4availableIPs := util.AddressCount(v4CIDR) - util.CountIpNums(v4toSubIPs)
	if v4availableIPs < 0 {
		v4availableIPs = 0
	}
	v6availableIPs := util.AddressCount(v6CIDR) - util.CountIpNums(v6toSubIPs)
	if v6availableIPs < 0 {
		v6availableIPs = 0
	}

	subnet.Status.V4AvailableIPs = v4availableIPs
	subnet.Status.V6AvailableIPs = v6availableIPs
	subnet.Status.V4UsingIPs = float64(len(v4UsingIPs))
	subnet.Status.V6UsingIPs = float64(len(v6UsingIPs))

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
	for _, podUsedIP := range podUsedIPs.Items {
		toSubIPs = append(toSubIPs, podUsedIP.Spec.IPAddress)
	}
	availableIPs := util.AddressCount(cidr) - util.CountIpNums(toSubIPs)
	if availableIPs < 0 {
		availableIPs = 0
	}
	usingIPs := float64(len(podUsedIPs.Items))
	if util.CheckProtocol(subnet.Spec.CIDRBlock) == kubeovnv1.ProtocolIPv4 {
		subnet.Status.V4AvailableIPs = availableIPs
		subnet.Status.V4UsingIPs = usingIPs
	} else if util.CheckProtocol(subnet.Spec.CIDRBlock) == kubeovnv1.ProtocolIPv6 {
		subnet.Status.V6AvailableIPs = availableIPs
		subnet.Status.V6UsingIPs = usingIPs
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

func (c *Controller) filterRepeatEcmpRoutes(nodeIps []string, cidr string) ([]string, error) {
	var nextHops []string
	routes, err := c.ovnClient.GetStaticRouteList(c.config.ClusterRouter)
	if err != nil {
		klog.Errorf("failed to list static route: %v", err)
		return nextHops, err
	}
	if len(nodeIps) == 0 {
		return nextHops, fmt.Errorf("nexthop is nil for add ecmp static route")
	}

	protocol := util.CheckProtocol(cidr)
	for _, nodeIp := range nodeIps {
		if util.CheckProtocol(nodeIp) != protocol {
			continue
		}

		var found bool
		for _, route := range routes {
			if route.Policy != ovs.PolicySrcIP || route.CIDR != cidr {
				continue
			}

			if route.NextHop == nodeIp {
				klog.Infof("src-ip static route exist for cidr %s, nexthop %s", cidr, nodeIp)
				found = true
				break
			}
		}
		if !found {
			nextHops = append(nextHops, nodeIp)
		}
	}

	klog.Infof("ecmp static route to add, cidr %s, nexthop %v", cidr, nextHops)
	return nextHops, nil
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

func (c *Controller) deleteEcmpRouteForNode(subnet *kubeovnv1.Subnet) error {
	for _, gw := range strings.Split(subnet.Spec.GatewayNode, ",") {
		// the format of gatewayNodeStr can be like 'kube-ovn-worker:172.18.0.2, kube-ovn-control-plane:172.18.0.3', which consists of node name and designative egress ip
		if strings.Contains(gw, ":") {
			gw = strings.TrimSpace(strings.Split(gw, ":")[0])
		} else {
			gw = strings.TrimSpace(gw)
		}
		node, err := c.nodesLister.Get(gw)
		if err != nil {
			continue
		}

		ipStr := node.Annotations[util.IpAddressAnnotation]
		for _, ip := range strings.Split(ipStr, ",") {
			var cidrBlock string
			for _, cidrBlock = range strings.Split(subnet.Spec.CIDRBlock, ",") {
				if util.CheckProtocol(cidrBlock) != util.CheckProtocol(ip) {
					continue
				}

				exist, err := c.checkNodeEcmpRouteExist(ip, cidrBlock)
				if err != nil {
					klog.Errorf("get ecmp static route for subnet %v, error %v", subnet.Name, err)
					break
				}

				if exist {
					if subnet.Status.ActivateGateway != "" && subnet.Status.ActivateGateway == gw {
						continue
					}

					klog.Infof("subnet %v changed to active-standby mode, delete ecmp route for node %s, ip %v", subnet.Name, node.Name, ip)
					if err := c.ovnClient.DeleteMatchedStaticRoute(cidrBlock, ip, c.config.ClusterRouter); err != nil {
						klog.Errorf("failed to delete static route %s for node %s, %v", ip, node.Name, err)
						return err
					}
				}
			}
		}
	}
	return nil
}
