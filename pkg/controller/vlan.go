package controller

import (
	"fmt"
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
	"strconv"
	"strings"
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
				if _, err := c.config.KubeOvnClient.KubeovnV1().Subnets().Patch(vlan.Name, types.MergePatchType, bytes, "status"); err != nil {
					klog.Error("patch vlan status failed", err)
				}
			}

			return err
		}

		s.Spec.Vlan = vlan.Name
		_, err = c.config.KubeOvnClient.KubeovnV1().Subnets().Update(s)
		if err != nil {
			vlan.Status.SetVlanError("UpdateSubnetVlanFailed", err.Error())
			bytes, err := vlan.Status.Bytes()

			if err != nil {
				klog.Error(err)
			} else {
				if _, err := c.config.KubeOvnClient.KubeovnV1().Subnets().Patch(vlan.Name, types.MergePatchType, bytes, "status"); err != nil {
					klog.Errorf("patch vlan status failed, %v", err)
				}
			}
			return err
		}

		subnets = append(subnets, subnet)
	}

	vlan.Spec.Subnet = strings.Join(subnets, ",")

	_, err = c.config.KubeOvnClient.KubeovnV1().Vlans().Update(vlan)
	if err != nil {
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

	if err = util.ValidateVlan(vlan.Spec.VlanId, c.config.DefaultVlanRange); err != nil {
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
	_, err = c.config.KubeOvnClient.KubeovnV1().Vlans().Update(vlan)
	if err != nil {
		klog.Errorf("failed to update vlan %s, %v", vlan.Name, err)
		return err
	}

	return nil
}

func (c *Controller) handleDelVlan(key string) error {
	vlan, err := c.vlansLister.Get(key)

	err = c.config.KubeOvnClient.KubeovnV1().Vlans().Delete(key, &metav1.DeleteOptions{})
	if err != nil {
		klog.Errorf("failed to delete vlan %s, %v", vlan.Name, err)
		return err
	}

	subnet, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets %v", err)
		return err
	}

	for _, s := range subnet {
		if s.Spec.Vlan == vlan.Name {
			c.updateSubnetQueue.Add(s.Name)
		}
	}

	return nil
}

func (c *Controller) addLocalnet(subnet *kubeovnv1.Subnet) error {
	localnetPort := ovs.PodNameToLocalnetName(subnet.Name)
	ports, err := c.ovnClient.ListLogicalSwitchPort()
	if err != nil {
		klog.Errorf("failed list logical switch port, %v", err)
		return err
	}

	for _, port := range ports {
		if port == localnetPort {
			klog.Infof("has exists localnet port %s", localnetPort)
			return nil
		}
	}

	vlan, err := c.config.KubeOvnClient.KubeovnV1().Vlans().Get(subnet.Spec.Vlan, metav1.GetOptions{})
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

	if err := c.ovnClient.DeletePort(localnetPort); err != nil {
		klog.Errorf("failed to delete localnet port %s, %v", localnetPort, err)
		return err
	}

	return nil
}

func (c *Controller) addPortVlan(port, ip, mac, vlan string) error {
	vlanCrd, err := c.vlansLister.Get(vlan)
	if err != nil {
		klog.Errorf("failed get vlan crd, %v", err)
		return err
	}

	if err := util.ValidateVlan(vlanCrd.Spec.VlanId, c.config.DefaultVlanRange); err != nil {
		return err
	}

	lsps, err := c.ovnClient.ListLogicalSwitchPort()
	if err != nil {
		klog.Errorf("failed to list logical switch port, %v", err)
		return err
	}

	found := false
	for _, lsp := range lsps {
		if lsp == port {
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("failed to find logical switch port: %s", port)
	}

	if err = c.ovnClient.SetLogicSwitchPortTag(port, strconv.Itoa(vlanCrd.Spec.VlanId)); err != nil {
		klog.Errorf("failed set port %s tag, %v", port, err)
		return err
	}

	if ip != "" || mac != "" {
		if err = c.ovnClient.SetLogicalSwitchPortAddress(port, ip, mac); err != nil {
			klog.Errorf("failed set port %s address, %v", port, err)
			return err
		}
	}

	return nil
}

func (c *Controller) updatePortVlan(port, vlanID string) error {
	err := c.ovnClient.SetLogicSwitchPortTag(port, vlanID)
	if err != nil {
		klog.Errorf("failed to update ovn port: %s tag: %s, %v", port, vlanID, err)
		return err
	}

	return nil
}
