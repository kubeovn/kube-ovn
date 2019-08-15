package controller

import (
	"fmt"
	kubeovnv1 "github.com/alauda/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/alauda/kube-ovn/pkg/util"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
	"reflect"
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
	c.addSubnetQueue.AddRateLimited(key)
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
	c.deleteSubnetQueue.AddRateLimited(key)
}

func (c *Controller) enqueueUpdateSubnet(old, new interface{}) {
	if !c.isLeader() {
		return
	}
	oldSubnet := old.(*kubeovnv1.Subnet)
	newSubnet := new.(*kubeovnv1.Subnet)

	if oldSubnet.Spec.Private != newSubnet.Spec.Private ||
		!reflect.DeepEqual(oldSubnet.Spec.AllowSubnets, newSubnet.Spec.AllowSubnets) ||
		!reflect.DeepEqual(oldSubnet.Spec.Namespaces, newSubnet.Spec.Namespaces) {
		var key string
		var err error
		if key, err = cache.MetaNamespaceKeyFunc(new); err != nil {
			utilruntime.HandleError(err)
			return
		}
		klog.V(3).Infof("enqueue update subnet %s", key)
		c.updateSubnetQueue.AddRateLimited(key)
	}
}

func (c *Controller) runAddSubnetWorker() {
	for c.processNextAddSubnetWorkItem() {
	}
}

func (c *Controller) runUpdateSubnetWorker() {
	for c.processNextUpdateSubnetWorkItem() {
	}
}

func (c *Controller) runDeleteSubnetWorker() {
	for c.processNextDeleteSubnetWorkItem() {
	}
}

func (c *Controller) processNextAddSubnetWorkItem() bool {
	obj, shutdown := c.addSubnetQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.addSubnetQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.addSubnetQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleAddSubnet(key); err != nil {
			c.addSubnetQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.addSubnetQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextUpdateSubnetWorkItem() bool {
	obj, shutdown := c.updateSubnetQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.updateSubnetQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.updateSubnetQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleUpdateSubnet(key); err != nil {
			c.updateSubnetQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.updateSubnetQueue.Forget(obj)
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
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.deleteSubnetQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleDeleteSubnet(key); err != nil {
			c.deleteSubnetQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
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
	changed := false
	if subnet.Spec.Protocol == "" || subnet.Spec.Protocol != util.CheckProtocol(subnet.Spec.CIDRBlock) {
		subnet.Spec.Protocol = util.CheckProtocol(subnet.Spec.CIDRBlock)
		changed = true
	}
	if subnet.Spec.GatewayType == "" {
		subnet.Spec.GatewayType = kubeovnv1.GWDistributedType
		changed = true
	}
	if subnet.Spec.Default && subnet.Name != c.config.DefaultLogicalSwitch {
		subnet.Spec.Default = false
		changed = true
	}
	if subnet.Spec.Gateway == "" {
		gw, err := util.FirstSubnetIP(subnet.Spec.CIDRBlock)
		if err != nil {
			klog.Error(err)
			return err
		}
		subnet.Spec.Gateway = gw
		changed = true
	}

	if changed {
		_, err := c.config.KubeOvnClient.KubeovnV1().Subnets().Update(subnet)
		if err != nil {
			klog.Errorf("failed to update subnet %s, %v", subnet.Name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) handleAddSubnet(key string) error {
	subnet, err := c.subnetsLister.Get(key)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}

	if err = formatSubnet(subnet, c); err != nil {
		return err
	}

	exist, err := c.ovnClient.LogicalSwitchExists(subnet.Name)
	if err != nil {
		klog.Errorf("failed to list logical switch, %v", err)
		return err
	}

	if !exist {
		if err = util.ValidateSubnet(*subnet); err != nil {
			klog.Error(err)
			subnet.TypeMeta.Kind = "Subnet"
			subnet.TypeMeta.APIVersion = "kubeovn.io/v1"
			c.recorder.Eventf(subnet, v1.EventTypeWarning, "ValidateLogicalSwitchFailed", err.Error())
			return err
		}
		subnetList, err := c.subnetsLister.List(labels.Everything())
		if err != nil {
			klog.Errorf("failed to list subnets %v", err)
			return err
		}
		for _, sub := range subnetList {
			if sub.Name != subnet.Name && util.CIDRConflict(sub.Spec.CIDRBlock, subnet.Spec.CIDRBlock) {
				err = fmt.Errorf("subnet %s cidr %s conflict with subnet %s cidr %s", subnet.Name, subnet.Spec.CIDRBlock, sub.Name, sub.Spec.CIDRBlock)
				klog.Error(err)
				subnet.TypeMeta.Kind = "Subnet"
				subnet.TypeMeta.APIVersion = "kubeovn.io/v1"
				c.recorder.Eventf(subnet, v1.EventTypeWarning, "ValidateLogicalSwitchFailed", err.Error())
				return err
			}
		}
		// If multiple namespace use same ls name, only first one will success
		err = c.ovnClient.CreateLogicalSwitch(subnet.Name, subnet.Spec.Protocol, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, subnet.Spec.ExcludeIps)
		if err != nil {
			return err
		}
	}

	if err := c.reconcileSubnet(subnet); err != nil {
		klog.Errorf("failed to reconcile subnet %s, %v", subnet.Name, err)
		return err
	}

	if subnet.Spec.Private {
		return c.ovnClient.SetPrivateLogicalSwitch(subnet.Name, subnet.Spec.Protocol, subnet.Spec.CIDRBlock, subnet.Spec.AllowSubnets)
	}
	return c.ovnClient.CleanLogicalSwitchAcl(subnet.Name)
}

func (c *Controller) handleUpdateSubnet(key string) error {
	subnet, err := c.subnetsLister.Get(key)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}

	if err = formatSubnet(subnet, c); err != nil {
		return err
	}

	exist, err := c.ovnClient.LogicalSwitchExists(subnet.Name)
	if err != nil {
		klog.Errorf("failed to list logical switch, %v", err)
		return err
	}
	if !exist {
		return nil
	}

	if err = util.ValidateSubnet(*subnet); err != nil {
		klog.Error(err)
		subnet.TypeMeta.Kind = "Subnet"
		subnet.TypeMeta.APIVersion = "kubeovn.io/v1"
		c.recorder.Eventf(subnet, v1.EventTypeWarning, "ValidateLogicalSwitchFailed", err.Error())
		return err
	}

	if err := c.reconcileSubnet(subnet); err != nil {
		klog.Errorf("failed to reconcile subnet %s, %v", subnet.Name, err)
		return err
	}

	if subnet.Spec.Private {
		return c.ovnClient.SetPrivateLogicalSwitch(subnet.Name, subnet.Spec.Protocol, subnet.Spec.CIDRBlock, subnet.Spec.AllowSubnets)
	}

	return c.ovnClient.CleanLogicalSwitchAcl(subnet.Name)
}

func (c *Controller) handleDeleteSubnet(key string) error {
	exist, err := c.ovnClient.LogicalSwitchExists(key)
	if err != nil {
		klog.Errorf("failed to list logical switch, %v", err)
		return err
	}
	if !exist {
		return nil
	}

	err = c.ovnClient.CleanLogicalSwitchAcl(key)
	if err != nil {
		klog.Errorf("failed to delete acl of logical switch %s %v", key, err)
		return err
	}
	err = c.ovnClient.DeleteLogicalSwitch(key)
	if err != nil {
		klog.Errorf("failed to delete logical switch %s %v", key, err)
		return err
	}
	return nil
}

func (c *Controller) reconcileSubnet(subnet *kubeovnv1.Subnet) error {
	// 1. unbind from previous subnet
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		return err
	}

	namespaceMap := map[string]bool{}
	for _, ns := range subnet.Spec.Namespaces {
		namespaceMap[ns] = true
	}

	for _, sub := range subnets {
		if sub.Name == subnet.Name || len(sub.Spec.Namespaces) == 0 {
			continue
		}

		changed := false
		reservedNamespaces := []string{}
		for _, ns := range sub.Spec.Namespaces {
			if namespaceMap[ns] {
				changed = true
			} else {
				reservedNamespaces = append(reservedNamespaces, ns)
			}
		}
		if changed {
			sub.Spec.Namespaces = reservedNamespaces
			_, err := c.config.KubeOvnClient.KubeovnV1().Subnets().Update(sub)
			if err != nil {
				klog.Errorf("failed to unbind namespace from subnet %s, %v", sub.Name, err)
				return err
			}
		}
	}

	// 2. add annotations to bind namespace
	for _, ns := range subnet.Spec.Namespaces {
		c.addNamespaceQueue.AddRateLimited(ns)
	}

	// 3. update unbind namespace annotation
	namespaces, err := c.namespacesLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list namespaces, %v", err)
		return err
	}

	for _, ns := range namespaces {
		if ns.Annotations != nil && ns.Annotations[util.LogicalSwitchAnnotation] == subnet.Name && !namespaceMap[ns.Name] {
			c.addNamespaceQueue.AddRateLimited(ns.Name)
		}
	}

	return nil
}
