package controller

import (
	"context"
	"fmt"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
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

	key, err := cache.MetaNamespaceKeyFunc(new)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}

	klog.V(3).Infof("enqueue update vlan %s", key)
	c.updateVlanQueue.Add(key)
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
	cachedVlan, err := c.vlansLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}

		return err
	}

	vlan := cachedVlan.DeepCopy()
	if vlan.Spec.Provider == "" {
		vlan.Spec.Provider = c.config.DefaultProviderName
		if _, err = c.config.KubeOvnClient.KubeovnV1().Vlans().Update(context.Background(), vlan, metav1.UpdateOptions{}); err != nil {
			klog.Errorf("failed to update vlan %s, %v", vlan.Name, err)
			return err
		}
	}

	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets: %v", err)
		return err
	}

	var needUpdate bool
	for _, subnet := range subnets {
		if subnet.Spec.Vlan == vlan.Name && !util.ContainsString(vlan.Status.Subnets, subnet.Name) {
			vlan.Status.Subnets = append(vlan.Status.Subnets, subnet.Name)
			needUpdate = true
		}
	}

	if needUpdate {
		bytes, err := vlan.Status.Bytes()
		if err != nil {
			klog.Error(err)
			return err
		}

		_, err = c.config.KubeOvnClient.KubeovnV1().Vlans().Patch(context.Background(), vlan.Name, types.MergePatchType, bytes, metav1.PatchOptions{})
		if err != nil {
			klog.Errorf("failed to patch vlan %s: %v", vlan.Name, err)
			return err
		}
	}

	pn, err := c.providerNetworksLister.Get(vlan.Spec.Provider)
	if err != nil {
		klog.Errorf("failed to get provider network %s: %v", vlan.Spec.Provider, err)
		return err
	}

	if !util.ContainsString(pn.Status.Vlans, vlan.Name) {
		status := pn.Status.DeepCopy()
		status.Vlans = append(status.Vlans, vlan.Name)
		bytes, err := status.Bytes()
		if err != nil {
			klog.Error(err)
			return err
		}

		_, err = c.config.KubeOvnClient.KubeovnV1().ProviderNetworks().Patch(context.Background(), pn.Name, types.MergePatchType, bytes, metav1.PatchOptions{})
		if err != nil {
			klog.Errorf("failed to patch provider network %s: %v", pn.Name, err)
			return err
		}
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

	if vlan.Spec.Provider == "" {
		newVlan := vlan.DeepCopy()
		newVlan.Spec.Provider = c.config.DefaultProviderName
		if _, err = c.config.KubeOvnClient.KubeovnV1().Vlans().Update(context.Background(), newVlan, metav1.UpdateOptions{}); err != nil {
			klog.Errorf("failed to update vlan %s: %v", vlan.Name, err)
			return err
		}
	}

	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets: %v", err)
		return err
	}
	for _, subnet := range subnets {
		if subnet.Spec.Vlan == vlan.Name {
			if err = c.setLocalnetTag(subnet.Name, vlan.Spec.ID); err != nil {
				return err
			}
		}
	}

	return nil
}

func (c *Controller) handleDelVlan(key string) error {
	subnet, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets: %v", err)
		return err
	}

	for _, s := range subnet {
		if s.Spec.Vlan == key {
			c.addOrUpdateSubnetQueue.Add(s.Name)
		}
	}

	providerNetworks, err := c.providerNetworksLister.List(labels.Everything())
	if err != nil && !k8serrors.IsNotFound(err) {
		klog.Errorf("failed to list provider networks: %v", err)
		return err
	}

	for _, pn := range providerNetworks {
		if err = c.updateProviderNetworkStatusForVlanDeletion(pn, key); err != nil {
			return err
		}
	}

	return nil
}

func (c *Controller) updateProviderNetworkStatusForVlanDeletion(pn *kubeovnv1.ProviderNetwork, vlan string) error {
	if !util.ContainsString(pn.Status.Vlans, vlan) {
		return nil
	}

	status := pn.Status.DeepCopy()
	status.Vlans = util.RemoveString(status.Vlans, vlan)
	if len(status.Vlans) == 0 {
		bytes := []byte(`[{ "op": "remove", "path": "/status/vlans"}]`)
		_, err := c.config.KubeOvnClient.KubeovnV1().ProviderNetworks().Patch(context.Background(), pn.Name, types.JSONPatchType, bytes, metav1.PatchOptions{})
		if err != nil {
			klog.Errorf("failed to patch provider network %s: %v", pn.Name, err)
			return err
		}
	} else {
		bytes, err := status.Bytes()
		if err != nil {
			klog.Error(err)
			return err
		}

		_, err = c.config.KubeOvnClient.KubeovnV1().ProviderNetworks().Patch(context.Background(), pn.Name, types.MergePatchType, bytes, metav1.PatchOptions{})
		if err != nil {
			klog.Errorf("failed to patch provider network %s: %v", pn.Name, err)
			return err
		}
	}

	return nil
}

func (c *Controller) setLocalnetTag(subnet string, vlanID int) error {
	localnetPort := ovs.PodNameToLocalnetName(subnet)
	if err := c.ovnClient.SetPortTag(localnetPort, vlanID); err != nil {
		klog.Errorf("failed to set vlan tag of localnet port %s: %v", localnetPort, err)
		return err
	}

	return nil
}

func (c *Controller) delLocalnet(subnet string) error {
	localnetPort := ovs.PodNameToLocalnetName(subnet)
	if err := c.ovnClient.DeleteLogicalSwitchPort(localnetPort); err != nil {
		klog.Errorf("failed to delete localnet port %s: %v", localnetPort, err)
		return err
	}

	return nil
}
