package controller

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	kubeovnv1 "github.com/alauda/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/alauda/kube-ovn/pkg/ovs"
	"github.com/alauda/kube-ovn/pkg/util"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
)

func (c *Controller) enqueueAddVlan(obj interface{}) {
	if !c.isLeader() {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue add vlan %s", key)
	c.addVlanQueue.Add(key)
}

func (c *Controller) enqueueUpdateVlan(old, new interface{}) {
	if !c.isLeader() {
		return
	}

	oldVlan := old.(*kubeovnv1.Vlan)
	newVlan := new.(*kubeovnv1.Vlan)

	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(new); err != nil {
		utilruntime.HandleError(err)
		return
	}

	klog.V(3).Infof("enqueue update vlan %s", key)
	if oldVlan.Spec.Subnet != newVlan.Spec.Subnet {
		c.updateVlanQueue.Add(key)
	}
}

func (c *Controller) enqueueDelVlan(obj interface{}) {
	if !c.isLeader() {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}

	klog.V(3).Infof("enqueue delete vlan %s", key)
	c.delVlanQueue.Add(key)
}

func (c *Controller) runAddVlanWorker() {
	for c.processNextAddVlanWorkItem() {
	}
}

func (c *Controller) runUpdateVlanWorker() {
	for c.processNextUpdateVlanWorkItem() {
	}
}

func (c *Controller) runDelVlanWorker() {
	for c.processNextDelVlanWorkItem() {
	}
}

func (c *Controller) processNextAddVlanWorkItem() bool {
	obj, shutdown := c.addVlanQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.addVlanQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.addVlanQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleAddVlan(key); err != nil {
			c.addVlanQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.addVlanQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextUpdateVlanWorkItem() bool {
	obj, shutdown := c.updateVlanQueue.Get()

	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.updateVlanQueue.Done(obj)
		var key string
		var ok bool

		if key, ok = obj.(string); !ok {
			c.updateVlanQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}

		if err := c.handleUpdateVlan(key); err != nil {
			c.updateVlanQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}

		c.updateVlanQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}

	return true
}

func (c *Controller) processNextDelVlanWorkItem() bool {
	obj, shutdown := c.delVlanQueue.Get()

	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.delVlanQueue.Done(obj)
		var key string
		var ok bool

		if key, ok = obj.(string); !ok {
			c.delVlanQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}

		if err := c.handleDelVlan(key); err != nil {
			c.delVlanQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}

		c.delVlanQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}

	return true
}

func (c *Controller) handleAddVlan(key string) error {
	vlan, err := c.vlansLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}

		return err
	}

	if vlan.Spec.ProviderInterfaceName == "" {
		vlan.Spec.ProviderInterfaceName = c.config.DefaultProviderName
	}

	if vlan.Spec.LogicalInterfaceName == "" {
		vlan.Spec.LogicalInterfaceName = c.config.DefaultHostInterface
	}

	subnets := []string{}
	subnetNames := strings.Split(vlan.Spec.Subnet, ",")
	for _, subnet := range subnetNames {
		s, err := c.subnetsLister.Get(subnet)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				continue
			}

			vlan.Status.SetVlanError("GetSubnetFailed", err.Error())
			bytes, err := vlan.Status.Bytes()
			if err != nil {
				klog.Error(err)
			} else {
				if _, err := c.config.KubeOvnClient.KubeovnV1().Subnets().Patch(context.Background(), vlan.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status"); err != nil {
					klog.Error("patch vlan status failed", err)
				}
			}

			return err
		}

		// vlan mode we set vlan for all subnets
		if c.config.NetworkType == util.NetworkTypeVlan && s.Spec.Vlan == "" {
			s.Spec.Vlan = vlan.Name
			if _, err = c.config.KubeOvnClient.KubeovnV1().Subnets().Update(context.Background(), s, metav1.UpdateOptions{}); err != nil {
				vlan.Status.SetVlanError("UpdateSubnetVlanFailed", err.Error())
				bytes, err := vlan.Status.Bytes()

				if err != nil {
					klog.Error(err)
				} else {
					if _, err := c.config.KubeOvnClient.KubeovnV1().Subnets().Patch(context.Background(), vlan.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status"); err != nil {
						klog.Errorf("patch vlan status failed, %v", err)
					}
				}
				return err
			}
		}

		if s.Spec.Vlan == vlan.Name {
			subnets = append(subnets, subnet)
		}
	}

	vlan.Spec.Subnet = strings.Join(subnets, ",")
	if _, err = c.config.KubeOvnClient.KubeovnV1().Vlans().Update(context.Background(), vlan, metav1.UpdateOptions{}); err != nil {
		klog.Errorf("failed to update vlan %s, %v", vlan.Name, err)
		return err
	}

	return nil
}

func (c *Controller) handleUpdateVlan(key string) error {
	vlan, err := c.vlansLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}

		return err
	}

	if err = util.ValidateVlanTag(vlan.Spec.VlanId, c.config.DefaultVlanRange); err != nil {
		return err
	}

	subnets := []string{}
	subnet, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets %v", err)
		return err
	}

	for _, s := range subnet {
		if s.Spec.Vlan == vlan.Name {
			subnets = append(subnets, s.Name)
		}
	}

	vlan.Spec.Subnet = strings.Join(subnets, ",")
	_, err = c.config.KubeOvnClient.KubeovnV1().Vlans().Update(context.Background(), vlan, metav1.UpdateOptions{})
	if err != nil {
		klog.Errorf("failed to update vlan %s, %v", vlan.Name, err)
		return err
	}

	return nil
}

func (c *Controller) handleDelVlan(key string) error {
	subnet, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets %v", err)
		return err
	}

	for _, s := range subnet {
		if s.Spec.Vlan == key {
			c.addOrUpdateSubnetQueue.Add(s.Name)
		}
	}

	return nil
}

func (c *Controller) addLocalnet(subnet *kubeovnv1.Subnet) error {
	localnetPort := ovs.PodNameToLocalnetName(subnet.Name)
	vlan, err := c.vlansLister.Get(subnet.Spec.Vlan)
	if err != nil {
		klog.Errorf("failed get vlan object %v", err)
		return err
	}

	if err := c.ovnClient.CreateLocalnetPort(subnet.Name, localnetPort, vlan.Spec.ProviderInterfaceName, strconv.Itoa(vlan.Spec.VlanId)); err != nil {
		return err
	}

	return nil
}

func (c *Controller) delLocalnet(key string) error {
	localnetPort := ovs.PodNameToLocalnetName(key)

	if err := c.ovnClient.DeleteLogicalSwitchPort(localnetPort); err != nil {
		klog.Errorf("failed to delete localnet port %s, %v", localnetPort, err)
		return err
	}

	return nil
}
