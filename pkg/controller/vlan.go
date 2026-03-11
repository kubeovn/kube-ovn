package controller

import (
	"context"
	"fmt"
	"slices"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) enqueueAddVlan(obj any) {
	key := cache.MetaObjectToName(obj.(*kubeovnv1.Vlan)).String()
	klog.V(3).Infof("enqueue add vlan %s", key)
	c.addVlanQueue.Add(key)
}

func (c *Controller) enqueueUpdateVlan(_, newObj any) {
	key := cache.MetaObjectToName(newObj.(*kubeovnv1.Vlan)).String()
	klog.V(3).Infof("enqueue update vlan %s", key)
	c.updateVlanQueue.Add(key)
}

func (c *Controller) enqueueDelVlan(obj any) {
	var vlan *kubeovnv1.Vlan
	switch t := obj.(type) {
	case *kubeovnv1.Vlan:
		vlan = t
	case cache.DeletedFinalStateUnknown:
		v, ok := t.Obj.(*kubeovnv1.Vlan)
		if !ok {
			klog.Warningf("unexpected object type: %T", t.Obj)
			return
		}
		vlan = v
	default:
		klog.Warningf("unexpected type: %T", obj)
		return
	}

	key := cache.MetaObjectToName(vlan).String()
	klog.V(3).Infof("enqueue delete vlan %s", key)
	c.delVlanQueue.Add(key)
}

func (c *Controller) handleAddVlan(key string) error {
	c.vlanKeyMutex.LockKey(key)
	defer func() { _ = c.vlanKeyMutex.UnlockKey(key) }()
	klog.Infof("handle add vlan %s", key)

	cachedVlan, err := c.vlansLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	vlan := cachedVlan.DeepCopy()
	if vlan.Spec.Provider == "" {
		vlan.Spec.Provider = c.config.DefaultProviderName
		if vlan, err = c.config.KubeOvnClient.KubeovnV1().Vlans().Update(context.Background(), vlan, metav1.UpdateOptions{}); err != nil {
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
		if subnet.Spec.Vlan == vlan.Name && !slices.Contains(vlan.Status.Subnets, subnet.Name) {
			vlan.Status.Subnets = append(vlan.Status.Subnets, subnet.Name)
			needUpdate = true
		}
	}

	if needUpdate {
		vlan, err = c.config.KubeOvnClient.KubeovnV1().Vlans().UpdateStatus(context.Background(), vlan, metav1.UpdateOptions{})
		if err != nil {
			klog.Errorf("failed to update status of vlan %s: %v", vlan.Name, err)
			return err
		}
	}

	pn, err := c.providerNetworksLister.Get(vlan.Spec.Provider)
	if err != nil {
		klog.Errorf("failed to get provider network %s: %v", vlan.Spec.Provider, err)
		return err
	}

	if err = c.checkVlanConflict(vlan); err != nil {
		klog.Errorf("failed to check vlan %s: %v", vlan.Name, err)
		return err
	}

	if !slices.Contains(pn.Status.Vlans, vlan.Name) {
		newPn := pn.DeepCopy()
		newPn.Status.Vlans = append(newPn.Status.Vlans, vlan.Name)
		_, err = c.config.KubeOvnClient.KubeovnV1().ProviderNetworks().UpdateStatus(context.Background(), newPn, metav1.UpdateOptions{})
		if err != nil {
			klog.Errorf("failed to update status of provider network %s: %v", pn.Name, err)
			return err
		}
	}

	// re-enqueue subnets that reference this vlan, so they can proceed
	// if they were previously blocked by the vlan not being ready
	for _, subnet := range subnets {
		if subnet.Spec.Vlan == vlan.Name {
			c.addOrUpdateSubnetQueue.Add(subnet.Name)
		}
	}

	return nil
}

func (c *Controller) checkVlanConflict(vlan *kubeovnv1.Vlan) error {
	if vlan.Spec.ID == 0 {
		// no conflict if vlan id is 0
		return nil
	}
	// todo: check if vlan conflict in webhook
	vlans, err := c.vlansLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list vlans: %v", err)
		return err
	}
	// check if new vlan conflict with old vlan
	var conflict bool
	var conflictErr error
	for _, v := range vlans {
		// different provider allow to have same vlan
		if vlan.Spec.Provider == v.Spec.Provider && vlan.Spec.ID == v.Spec.ID && vlan.Name != v.Name {
			conflictErr = fmt.Errorf("provider %s new vlan %s conflict with old vlan %s", vlan.Spec.Provider, vlan.Name, v.Name)
			klog.Error(conflictErr)
			conflict = true
		}
	}
	if vlan.Status.Conflict != conflict {
		vlan.Status.Conflict = conflict
		vlan, err = c.config.KubeOvnClient.KubeovnV1().Vlans().UpdateStatus(context.Background(), vlan, metav1.UpdateOptions{})
		if err != nil {
			klog.Errorf("failed to update conflict status of vlan %s: %v", vlan.Name, err)
			return err
		}
	}
	return conflictErr
}

func (c *Controller) handleUpdateVlan(key string) error {
	c.vlanKeyMutex.LockKey(key)
	defer func() { _ = c.vlanKeyMutex.UnlockKey(key) }()
	klog.Infof("handle update vlan %s", key)

	vlan, err := c.vlansLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	if vlan.Spec.Provider == "" {
		newVlan := vlan.DeepCopy()
		newVlan.Spec.Provider = c.config.DefaultProviderName
		if vlan, err = c.config.KubeOvnClient.KubeovnV1().Vlans().Update(context.Background(), newVlan, metav1.UpdateOptions{}); err != nil {
			klog.Errorf("failed to update vlan %s: %v", vlan.Name, err)
			return err
		}
	}
	newVlan := vlan.DeepCopy()
	if err = c.checkVlanConflict(newVlan); err != nil {
		klog.Errorf("failed to check vlan %s: %v", vlan.Name, err)
		return err
	}
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets: %v", err)
		return err
	}
	for _, subnet := range subnets {
		if subnet.Spec.Vlan == vlan.Name {
			if err = c.setLocalnetTag(subnet.Name, vlan.Spec.ID); err != nil {
				klog.Error(err)
				return err
			}
		}
	}

	return nil
}

func (c *Controller) handleDelVlan(key string) error {
	c.vlanKeyMutex.LockKey(key)
	defer func() { _ = c.vlanKeyMutex.UnlockKey(key) }()
	klog.Infof("handle delete vlan %s", key)

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
			klog.Error(err)
			return err
		}
	}

	return nil
}

func (c *Controller) updateProviderNetworkStatusForVlanDeletion(pn *kubeovnv1.ProviderNetwork, vlan string) error {
	if !slices.Contains(pn.Status.Vlans, vlan) {
		return nil
	}

	newPn := pn.DeepCopy()
	newPn.Status.Vlans = util.RemoveString(newPn.Status.Vlans, vlan)
	_, err := c.config.KubeOvnClient.KubeovnV1().ProviderNetworks().UpdateStatus(context.Background(), newPn, metav1.UpdateOptions{})
	if err != nil {
		klog.Errorf("failed to update status of provider network %s: %v", pn.Name, err)
		return err
	}
	return nil
}

func (c *Controller) setLocalnetTag(subnet string, vlanID int) error {
	localnetPort := ovs.GetLocalnetName(subnet)
	if err := c.OVNNbClient.SetLogicalSwitchPortVlanTag(localnetPort, vlanID); err != nil {
		klog.Errorf("set localnet port %s vlan tag %d: %v", localnetPort, vlanID, err)
		return err
	}

	return nil
}

func (c *Controller) delLocalnet(subnet string) error {
	localnetPort := ovs.GetLocalnetName(subnet)
	if err := c.OVNNbClient.DeleteLogicalSwitchPort(localnetPort); err != nil {
		klog.Errorf("delete localnet port %s: %v", localnetPort, err)
		return err
	}

	return nil
}
