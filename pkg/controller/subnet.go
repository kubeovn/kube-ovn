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

func (c *Controller) handleAddSubnet(key string) error {
	subnet, err := c.subnetsLister.Get(key)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	existSubnets, err := c.ovnClient.ListLogicalSwitch()
	if err != nil {
		klog.Errorf("failed to list exist subnet")
		return err
	}
	for _, s := range existSubnets {
		if s == subnet.Name {
			return nil
		}
	}

	if err = util.ValidateSubnet(*subnet); err != nil {
		klog.Error(err)
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
			c.recorder.Eventf(subnet, v1.EventTypeWarning, "ValidateLogicalSwitchFailed", err.Error())
			return err
		}
	}
	// If multiple namespace use same ls name, only first one will success
	err = c.ovnClient.CreateLogicalSwitch(subnet.Name, subnet.Spec.Protocol, subnet.Spec.CIDRBlock, subnet.Spec.Gateway, subnet.Spec.ExcludeIps)
	if err != nil {
		return err
	}

	if subnet.Spec.Private {
		return c.ovnClient.SetPrivateLogicalSwitch(subnet.Name, subnet.Spec.Protocol, subnet.Spec.AllowSubnets)
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
	if err = util.ValidateSubnet(*subnet); err != nil {
		klog.Error(err)
		c.recorder.Eventf(subnet, v1.EventTypeWarning, "ValidateLogicalSwitchFailed", err.Error())
		return err
	}

	if subnet.Spec.Private {
		return c.ovnClient.SetPrivateLogicalSwitch(subnet.Name, subnet.Spec.Protocol, subnet.Spec.AllowSubnets)
	}

	return c.ovnClient.CleanLogicalSwitchAcl(subnet.Name)
}

func (c *Controller) handleDeleteSubnet(key string) error {
	err := c.ovnClient.CleanLogicalSwitchAcl(key)
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
