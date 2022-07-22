package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

var (
	// external underlay vlan macvlan network attachment definition provider
	MACVLAN_NAD_PROVIDER = fmt.Sprintf("%s.%s", util.VpcExternalNet, ATTACHMENT_NS)
)

func (c *Controller) enqueueAddIptablesEip(obj interface{}) {
	if !c.isLeader() {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.addIptablesEipQueue.Add(key)
}

func (c *Controller) enqueueUpdateIptablesEip(old, new interface{}) {
	if !c.isLeader() {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(new); err != nil {
		utilruntime.HandleError(err)
		return
	}
	oldEip := old.(*kubeovnv1.IptablesEIP)
	newEip := new.(*kubeovnv1.IptablesEIP)
	if !newEip.DeletionTimestamp.IsZero() ||
		oldEip.Spec.V4ip != newEip.Spec.V4ip ||
		oldEip.Status.Redo != newEip.Status.Redo {
		c.updateIptablesEipQueue.Add(key)
	}
	c.updateSubnetStatusQueue.Add(util.VpcExternalNet)
}

func (c *Controller) enqueueDelIptablesEip(obj interface{}) {
	if !c.isLeader() {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.delIptablesEipQueue.Add(key)
	c.updateSubnetStatusQueue.Add(util.VpcExternalNet)
}

func (c *Controller) runAddIptablesEipWorker() {
	for c.processNextAddIptablesEipWorkItem() {
	}
}

func (c *Controller) runUpdateIptablesEipWorker() {
	for c.processNextUpdateIptablesEipWorkItem() {
	}
}

func (c *Controller) runResetIptablesEipWorker() {
	for c.processNextResetIptablesEipWorkItem() {
	}
}

func (c *Controller) runDelIptablesEipWorker() {
	for c.processNextDeleteIptablesEipWorkItem() {
	}
}

func (c *Controller) processNextAddIptablesEipWorkItem() bool {
	obj, shutdown := c.addIptablesEipQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.addIptablesEipQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.addIptablesEipQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleAddIptablesEip(key); err != nil {
			c.addIptablesEipQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.addIptablesEipQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextResetIptablesEipWorkItem() bool {
	obj, shutdown := c.resetIptablesEipQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.resetIptablesEipQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.resetIptablesEipQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleResetIptablesEip(key); err != nil {
			c.resetIptablesEipQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.resetIptablesEipQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextUpdateIptablesEipWorkItem() bool {
	obj, shutdown := c.updateIptablesEipQueue.Get()
	if shutdown {
		return false
	}
	err := func(obj interface{}) error {
		defer c.updateIptablesEipQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.updateIptablesEipQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleUpdateIptablesEip(key); err != nil {
			c.updateIptablesEipQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.updateIptablesEipQueue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextDeleteIptablesEipWorkItem() bool {
	obj, shutdown := c.delIptablesEipQueue.Get()
	if shutdown {
		return false
	}
	err := func(obj interface{}) error {
		defer c.delIptablesEipQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.delIptablesEipQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected eip in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleDelIptablesEip(key); err != nil {
			c.delIptablesEipQueue.AddRateLimited(obj)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.delIptablesEipQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) handleAddIptablesEip(key string) error {
	if vpcNatEnabled != "true" {
		time.Sleep(10 * time.Second)
		if vpcNatEnabled != "true" {
			return fmt.Errorf("failed to add vpc nat eip, vpcNatEnabled='%s'", vpcNatEnabled)
		}
	}

	c.vpcNatGwKeyMutex.Lock(key)
	defer c.vpcNatGwKeyMutex.Unlock(key)

	cachedEip, err := c.iptablesEipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if cachedEip.Spec.MacAddress != "" {
		// already ok
		return nil
	}
	eip := cachedEip.DeepCopy()
	klog.V(3).Infof("handle add eip %s", key)
	var v4ip, v6ip, mac, eipV4Cidr, v4Gw string
	portName := ovs.PodNameToPortName(eip.Name, eip.Namespace, MACVLAN_NAD_PROVIDER)
	if eip.Spec.V4ip != "" {
		if v4ip, v6ip, mac, err = c.acquireStaticEip(eip.Name, eip.Namespace, portName, eip.Spec.V4ip); err != nil {
			return err
		}
	} else {
		// Random allocate
		if v4ip, v6ip, mac, err = c.acquireEip(eip.Name, eip.Namespace, portName); err != nil {
			return err
		}
	}
	if eipV4Cidr, err = c.getEipV4Cidr(v4ip); err != nil {
		return err
	}
	if v4Gw, _, err = c.GetGwbySubnet(util.VpcExternalNet); err != nil {
		klog.Errorf("failed to get gw, err: %v", err)
		return err
	}
	// create
	if err = c.createEipInPod(eip.Spec.NatGwDp, v4Gw, eipV4Cidr); err != nil {
		klog.Errorf("failed to create eip '%s' in pod, %v", key, err)
		return err
	}
	if err = c.createOrUpdateCrdEip(key, eip.Namespace, v4ip, v6ip, mac, eip.Spec.NatGwDp); err != nil {
		klog.Errorf("failed to update eip %s, %v", key, err)
		return err
	}
	if err = c.patchEipStatus(key, v4ip, "", "", true); err != nil {
		klog.Errorf("failed to patch status for eip %s, %v", key, err)
		return err
	}
	if _, err = c.handleIptablesEipFinalizer(eip, false); err != nil {
		klog.Errorf("failed to handle finalizer for eip %s, %v", key, err)
		return err
	}
	return nil
}

func (c *Controller) handleResetIptablesEip(key string) error {
	eip, err := c.iptablesEipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	var notUse bool
	switch eip.Status.Nat {
	case "fip":
		notUse = true
	case "dnat":
		time.Sleep(10 * time.Second)
		// nat change eip not that fast
		dnats, err := c.config.KubeOvnClient.KubeovnV1().IptablesDnatRules().List(context.Background(), metav1.ListOptions{
			LabelSelector: fields.OneTermEqualSelector(util.VpcEipLabel, key).String(),
		})
		if err != nil {
			klog.Errorf("failed to get dnats, %v", err)
			return err
		}
		if len(dnats.Items) == 0 {
			notUse = true
		}
	case "snat":
		time.Sleep(10 * time.Second)
		// nat change eip not that fast
		snats, err := c.config.KubeOvnClient.KubeovnV1().IptablesSnatRules().List(context.Background(), metav1.ListOptions{
			LabelSelector: fields.OneTermEqualSelector(util.VpcEipLabel, key).String(),
		})
		if err != nil {
			klog.Errorf("failed to get snats, %v", err)
			return err
		}
		if len(snats.Items) == 0 {
			notUse = true
		}
	default:
		notUse = true
	}

	if notUse {
		if err := c.natLabelEip(key, ""); err != nil {
			klog.Errorf("failed to clean label for eip %s, %v", key, err)
			return err
		}
		if err := c.patchResetEipStatusNat(key, ""); err != nil {
			klog.Errorf("failed to clean status for eip %s, %v", key, err)
			return err
		}
	}
	return nil
}

func (c *Controller) handleUpdateIptablesEip(key string) error {
	if vpcNatEnabled != "true" {
		time.Sleep(10 * time.Second)
		if vpcNatEnabled != "true" {
			return fmt.Errorf("failed to del vpc nat eip, vpcNatEnabled='%s'", vpcNatEnabled)
		}
	}
	c.vpcNatGwKeyMutex.Lock(key)
	defer c.vpcNatGwKeyMutex.Unlock(key)

	cachedEip, err := c.iptablesEipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	eip := cachedEip.DeepCopy()
	// should delete
	if !eip.DeletionTimestamp.IsZero() {
		klog.V(3).Infof("clean eip '%s' in pod", key)
		v4Cidr, err := c.getEipV4Cidr(eip.Status.IP)
		if err != nil {
			klog.Errorf("failed to clean eip %s, %v", key, err)
			return err
		}
		if err = c.deleteEipInPod(eip.Spec.NatGwDp, v4Cidr); err != nil {
			klog.Errorf("failed to clean eip '%s' in pod, %v", key, err)
			return err
		}
		if _, err = c.handleIptablesEipFinalizer(eip, true); err != nil {
			klog.Errorf("failed to handle finalizer for eip %s, %v", key, err)
			return err
		}
		return nil
	}
	if eip.Status.IP != "" && eip.Spec.V4ip == "" {
		// eip spec V4ip is removed
		if err = c.createOrUpdateCrdEip(key, eip.Namespace, eip.Status.IP, eip.Spec.V6ip, eip.Spec.MacAddress, eip.Spec.NatGwDp); err != nil {
			klog.Errorf("failed to update eip %s, %v", key, err)
			return err
		}
	}
	// eip change ip
	if c.eipChangeIP(eip) {
		klog.V(3).Infof("eip change ip, old ip '%s', new ip '%s'", eip.Status.IP, eip.Spec.V4ip)
		var v4Cidr, v4Gw, v4ip, v6ip, mac, natType, natName string
		if v4Cidr, err = c.getEipV4Cidr(eip.Status.IP); err != nil {
			klog.Errorf("failed to get old eip cidr, %v", err)
			return err
		}
		// remove old
		if err = c.deleteEipInPod(eip.Spec.NatGwDp, v4Cidr); err != nil {
			klog.Errorf("failed to clean old eip, %v", err)
			return err
		}
		c.ipam.ReleaseAddressByPod(key)
		// create new
		portName := ovs.PodNameToPortName(eip.Name, eip.Namespace, MACVLAN_NAD_PROVIDER)
		if v4ip, v6ip, mac, err = c.acquireStaticEip(eip.Name, eip.Namespace, portName, eip.Spec.V4ip); err != nil {
			return err
		}
		if v4Cidr, err = c.getEipV4Cidr(eip.Spec.V4ip); err != nil {
			klog.Errorf("failed to clean old eip, %v", err)
			return err
		}
		if v4Gw, _, err = c.GetGwbySubnet(util.VpcExternalNet); err != nil {
			return err
		}
		if err = c.createEipInPod(eip.Spec.NatGwDp, v4Gw, v4Cidr); err != nil {
			klog.Errorf("failed to clean eip, %v", err)
			return err
		}
		if err = c.createOrUpdateCrdEip(key, eip.Namespace, v4ip, v6ip, mac, eip.Spec.NatGwDp); err != nil {
			klog.Errorf("failed to update eip %s, %v", key, err)
			return err
		}
		if err = c.patchEipStatus(key, v4ip, "", "", true); err != nil {
			klog.Errorf("failed to patch status for eip %s, %v", key, err)
			return err
		}
		// inform nat to replace eip
		if eip.Status.Nat == "" {
			klog.V(3).Infof("no nat use eip %s", key)
			return nil
		}
		switch eip.Status.Nat {
		case "fip":
			// get all dnat by eip spec
			fips, err := c.config.KubeOvnClient.KubeovnV1().IptablesFIPRules().List(context.Background(), metav1.ListOptions{
				LabelSelector: fields.OneTermEqualSelector(util.VpcEipLabel, key).String(),
			})
			if err != nil {
				klog.Errorf("failed to get fip, %v", err)
				return err
			}
			if len(fips.Items) == 0 {
				err = fmt.Errorf("no fip use eip %s", eip.Name)
				return err
			} else if len(fips.Items) != 1 {
				err = fmt.Errorf("too many fips use eip %s", eip.Name)
				return err
			}
			for _, fip := range fips.Items {
				if err = c.redoFip(fip.Name, time.Now().Format("2006-01-02T15:04:05"), true); err != nil {
					klog.Errorf("failed to notify fip '%s' to change ip, %v", natName, err)
					return err
				}
			}
		case "dnat":
			// get all dnat by eip spec
			dnats, err := c.config.KubeOvnClient.KubeovnV1().IptablesDnatRules().List(context.Background(), metav1.ListOptions{
				LabelSelector: fields.OneTermEqualSelector(util.VpcEipLabel, key).String(),
			})
			if err != nil {
				klog.Errorf("failed to get dnats, %v", err)
				return err
			}
			if len(dnats.Items) == 0 {
				err = fmt.Errorf("no dnat use eip %s", eip.Name)
				return err
			}
			for _, dnat := range dnats.Items {
				klog.V(3).Infof("redo dnat '%s' depended on eip %s", dnat.Name, eip.Name)
				if err = c.redoDnat(dnat.Name, time.Now().Format("2006-01-02T15:04:05"), true); err != nil {
					klog.Errorf("failed to notify dnat '%s' to change ip, %v", natName, err)
					return err
				}
			}
		case "snat":
			// get all snat by eip spec
			snats, err := c.config.KubeOvnClient.KubeovnV1().IptablesSnatRules().List(context.Background(), metav1.ListOptions{
				LabelSelector: fields.OneTermEqualSelector(util.VpcEipLabel, key).String(),
			})
			if err != nil {
				klog.Errorf("failed to get snats, %v", err)
				return err
			}
			if len(snats.Items) == 0 {
				err = fmt.Errorf("no snat use eip %s", eip.Name)
				return err
			}
			for _, snat := range snats.Items {
				klog.V(3).Infof("redo snat '%s' depended on eip %s", snat.Name, eip.Name)
				if err = c.redoSnat(snat.Name, time.Now().Format("2006-01-02T15:04:05"), true); err != nil {
					klog.Errorf("failed to notify snat '%s' to change ip, %v", natName, err)
					return err
				}
			}
		default:
			err = fmt.Errorf("nat type label '%s' is invalid for eip %s", natType, key)
			return err
		}
		return nil
	}

	// redo
	if eip.Status.Redo != "" &&
		eip.Status.IP != "" &&
		eip.DeletionTimestamp.IsZero() {
		eipV4Cidr, err := c.getEipV4Cidr(eip.Status.IP)
		if err != nil {
			klog.Errorf("failed to get eip or v4Cidr, %v", err)
			return err
		}
		var v4Gw string
		if v4Gw, _, err = c.GetGwbySubnet(util.VpcExternalNet); err != nil {
			klog.Errorf("failed to get gw, %v", err)
			return err
		}
		if err = c.createEipInPod(eip.Spec.NatGwDp, v4Gw, eipV4Cidr); err != nil {
			klog.Errorf("failed to create eip, %v", err)
			return err
		}
		if err = c.patchEipStatus(key, "", "", "", true); err != nil {
			klog.Errorf("failed to patch status for eip %s, %v", key, err)
			return err
		}
		return nil
	}
	if _, err = c.handleIptablesEipFinalizer(eip, false); err != nil {
		klog.Errorf("failed to handle finalizer for eip, %v", err)
		return err
	}
	return nil
}

func (c *Controller) handleDelIptablesEip(key string) error {
	c.ipam.ReleaseAddressByPod(key)
	klog.V(3).Infof("deleted vpc nat eip %s", key)
	return nil
}

func (c *Controller) GetEip(eipName string) (*kubeovnv1.IptablesEIP, error) {
	cachedEip, err := c.iptablesEipsLister.Get(eipName)
	if err != nil {
		klog.Errorf("failed to get eip %s, %v", eipName, err)
		return nil, err
	}
	if cachedEip.Spec.V4ip == "" {
		return nil, fmt.Errorf("eip '%s' is not ready, has no v4ip", eipName)
	}
	eip := cachedEip.DeepCopy()
	return eip, nil
}

func (c *Controller) createEipInPod(dp, gw, v4Cidr string) error {
	gwPod, err := c.getNatGwPod(dp)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	var addRules []string
	rule := fmt.Sprintf("%s,%s", v4Cidr, gw)
	addRules = append(addRules, rule)
	if err = c.execNatGwRules(gwPod, natGwEipAdd, addRules); err != nil {
		return err
	}
	return nil
}

func (c *Controller) deleteEipInPod(dp, v4Cidr string) error {
	gwPod, err := c.getNatGwPod(dp)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	var delRules []string
	rule := v4Cidr
	delRules = append(delRules, rule)
	if err = c.execNatGwRules(gwPod, natGwEipDel, delRules); err != nil {
		return err
	}
	return nil
}

func (c *Controller) acquireStaticEip(name, namespace, nicName, ip string) (string, string, string, error) {
	checkConflict := true
	var v4ip, v6ip, mac string
	var err error
	for _, ipStr := range strings.Split(ip, ",") {
		if net.ParseIP(ipStr) == nil {
			return "", "", "", fmt.Errorf("failed to parse eip ip %s", ipStr)
		}
	}

	if v4ip, v6ip, mac, err = c.ipam.GetStaticAddress(name, nicName, ip, mac, util.VpcExternalNet, checkConflict); err != nil {
		klog.Errorf("failed to get static ip %v, mac %v, subnet %v, err %v", ip, mac, util.VpcExternalNet, err)
		return "", "", "", err
	}
	return v4ip, v6ip, mac, nil
}

func (c *Controller) acquireEip(name, namespace, nicName string) (string, string, string, error) {
	var skippedAddrs []string
	for {
		ipv4, ipv6, mac, err := c.ipam.GetRandomAddress(name, nicName, "", util.VpcExternalNet, skippedAddrs, true)
		if err != nil {
			return "", "", "", err
		}

		ipv4OK, ipv6OK, err := c.validatePodIP(name, util.VpcExternalNet, ipv4, ipv6)
		if err != nil {
			return "", "", "", err
		}
		if ipv4OK && ipv6OK {
			return ipv4, ipv6, mac, nil
		}
		if !ipv4OK {
			skippedAddrs = append(skippedAddrs, ipv4)
		}
		if !ipv6OK {
			skippedAddrs = append(skippedAddrs, ipv6)
		}
	}
}

func (c *Controller) eipChangeIP(eip *kubeovnv1.IptablesEIP) bool {
	if eip.Status.IP == "" || eip.Spec.V4ip == "" {
		// eip created but not ready
		return false
	}
	if eip.Status.IP != eip.Spec.V4ip {
		return true
	}
	return false
}

func (c *Controller) getEipV4Cidr(v4ip string) (string, error) {
	extSubnetMask, err := c.ipam.GetSubnetV4Mask(util.VpcExternalNet)
	if err != nil {
		klog.Errorf("failed to get eip '%s' mask from subnet %s, %v", v4ip, util.VpcExternalNet, err)
		return "", err
	}
	v4IpCidr := fmt.Sprintf("%s/%s", v4ip, extSubnetMask)
	return v4IpCidr, nil
}

func (c *Controller) GetGwbySubnet(name string) (string, string, error) {
	if subnet, ok := c.ipam.Subnets[name]; ok {
		return subnet.V4Gw, subnet.V6Gw, nil
	} else {
		return "", "", fmt.Errorf("failed to get subnet %s", name)
	}
}

func (c *Controller) createOrUpdateCrdEip(key, ns, v4ip, v6ip, mac, natGwDp string) error {
	eipCr, err := c.config.KubeOvnClient.KubeovnV1().IptablesEIPs().Get(context.Background(), key, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			_, err := c.config.KubeOvnClient.KubeovnV1().IptablesEIPs().Create(context.Background(), &kubeovnv1.IptablesEIP{
				ObjectMeta: metav1.ObjectMeta{
					Name: key,
					Labels: map[string]string{
						util.SubnetNameLabel:        util.VpcExternalNet,
						util.VpcNatGatewayNameLabel: natGwDp,
					},
				},
				Spec: kubeovnv1.IptablesEipSpec{
					V4ip:       v4ip,
					V6ip:       v6ip,
					MacAddress: mac,
					NatGwDp:    natGwDp,
				},
			}, metav1.CreateOptions{})

			if err != nil {
				errMsg := fmt.Errorf("failed to create eip crd %s, %v", key, err)
				klog.Error(errMsg)
				return errMsg
			}
		} else {
			errMsg := fmt.Errorf("failed to get eip crd %s, %v", key, err)
			klog.Error(errMsg)
			return errMsg
		}
	} else {
		eip := eipCr.DeepCopy()
		if eip.Spec.MacAddress != mac || eip.Spec.V4ip != v4ip {
			eip.Spec.MacAddress = mac
			eip.Spec.V4ip = v4ip
			eip.Spec.V6ip = v6ip
			eip.Spec.NatGwDp = natGwDp
			if _, err := c.config.KubeOvnClient.KubeovnV1().IptablesEIPs().Update(context.Background(), eip, metav1.UpdateOptions{}); err != nil {
				errMsg := fmt.Errorf("failed to update eip crd %s, %v", key, err)
				klog.Error(errMsg)
				return errMsg
			}
		}
		var needUpdateLabel bool
		var op string
		if len(eip.Labels) == 0 {
			op = "add"
			eip.Labels = map[string]string{
				util.SubnetNameLabel:        util.VpcExternalNet,
				util.VpcNatGatewayNameLabel: natGwDp,
				util.VpcNatLabel:            "",
			}
			needUpdateLabel = true
		} else if eip.Labels[util.SubnetNameLabel] != util.VpcExternalNet {
			op = "replace"
			eip.Labels[util.SubnetNameLabel] = util.VpcExternalNet
			eip.Labels[util.VpcNatGatewayNameLabel] = natGwDp
			eip.Labels[util.VpcNatLabel] = ""
			needUpdateLabel = true
		}
		if needUpdateLabel {
			patchPayloadTemplate := `[{ "op": "%s", "path": "/metadata/labels", "value": %s }]`
			raw, _ := json.Marshal(eip.Labels)
			patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
			if _, err := c.config.KubeOvnClient.KubeovnV1().IptablesEIPs().Patch(context.Background(), key, types.JSONPatchType,
				[]byte(patchPayload), metav1.PatchOptions{}); err != nil {
				klog.Errorf("failed to patch label for eip %s, %v", eip.Name, err)
				return err
			}
		}
	}
	return nil
}

func (c *Controller) handleIptablesEipFinalizer(eip *kubeovnv1.IptablesEIP, justDelete bool) (bool, error) {
	if !eip.DeletionTimestamp.IsZero() && justDelete {
		if len(eip.Finalizers) == 0 {
			return true, nil
		}
		eip.Finalizers = util.RemoveString(eip.Finalizers, util.ControllerName)
		raw, _ := json.Marshal(eip.Finalizers)
		patchPayloadTemplate := `[{ "op": "remove", "path": "/metadata/finalizers", "value": %s }]`
		patchPayload := fmt.Sprintf(patchPayloadTemplate, raw)
		if _, err := c.config.KubeOvnClient.KubeovnV1().IptablesEIPs().Patch(context.Background(), eip.Name, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{}); err != nil {
			klog.Errorf("failed to remove finalizer from eip %s, %v", eip.Name, err)
			return false, err
		}
		return true, nil
	}
	if eip.DeletionTimestamp.IsZero() && !util.ContainsString(eip.Finalizers, util.ControllerName) {
		if len(eip.Finalizers) != 0 {
			return false, nil
		}
		eip.Finalizers = append(eip.Finalizers, util.ControllerName)
		raw, _ := json.Marshal(eip.Finalizers)
		patchPayloadTemplate := `[{ "op": "add", "path": "/metadata/finalizers", "value": %s }]`
		patchPayload := fmt.Sprintf(patchPayloadTemplate, raw)
		if _, err := c.config.KubeOvnClient.KubeovnV1().IptablesEIPs().Patch(context.Background(), eip.Name, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{}); err != nil {
			klog.Errorf("failed to add finalizer to eip %s, %v", eip.Name, err)
			return false, err
		}
		// wait local cache ready
		time.Sleep(2 * time.Second)
		return false, nil
	}

	if !eip.DeletionTimestamp.IsZero() && !eip.Status.Ready {
		if len(eip.Finalizers) == 0 {
			return true, nil
		}
		eip.Finalizers = util.RemoveString(eip.Finalizers, util.ControllerName)
		raw, _ := json.Marshal(eip.Finalizers)
		patchPayloadTemplate := `[{ "op": "remove", "path": "/metadata/finalizers", "value": %s }]`
		patchPayload := fmt.Sprintf(patchPayloadTemplate, raw)
		if _, err := c.config.KubeOvnClient.KubeovnV1().IptablesEIPs().Patch(context.Background(), eip.Name, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{}); err != nil {
			klog.Errorf("failed to remove finalizer from eip %s, %v", eip.Name, err)
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func (c *Controller) patchEipStatus(key, v4ip, redo, nat string, ready bool) error {
	oriEip, err := c.iptablesEipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	eip := oriEip.DeepCopy()
	var changed bool
	if eip.Status.Ready != ready {
		eip.Status.Ready = ready
		changed = true
	}

	if redo != "" && eip.Status.Redo != redo {
		eip.Status.Redo = redo
		changed = true
	}

	if ready && v4ip != "" && eip.Status.IP != v4ip {
		eip.Status.IP = v4ip
		changed = true
	}
	if ready && nat != "" && eip.Status.Nat != nat {
		eip.Status.Nat = nat
		changed = true
	}

	if changed {
		bytes, err := eip.Status.Bytes()
		if err != nil {
			return err
		}
		if _, err = c.config.KubeOvnClient.KubeovnV1().IptablesEIPs().Patch(context.Background(), key, types.MergePatchType,
			bytes, metav1.PatchOptions{}, "status"); err != nil {
			klog.Errorf("failed to patch eip %s, %v", eip.Name, err)
			return err
		}
	}
	return nil
}

func (c *Controller) patchResetEipStatusNat(key, nat string) error {
	oriEip, err := c.iptablesEipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	eip := oriEip.DeepCopy()
	if eip.Status.Nat != nat {
		eip.Status.Nat = nat
		bytes, err := eip.Status.Bytes()
		if err != nil {
			return err
		}
		if _, err = c.config.KubeOvnClient.KubeovnV1().IptablesEIPs().Patch(context.Background(), key, types.MergePatchType,
			bytes, metav1.PatchOptions{}, "status"); err != nil {
			klog.Errorf("failed to patch eip '%s' nat type, %v", eip.Name, err)
			return err
		}
	}
	return nil
}
func (c *Controller) natLabelEip(eipName, natName string) error {
	oriEip, err := c.iptablesEipsLister.Get(eipName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	eip := oriEip.DeepCopy()
	var needUpdateLabel bool
	var op string
	if len(eip.Labels) == 0 {
		op = "add"
		needUpdateLabel = true
		eip.Labels = map[string]string{
			util.SubnetNameLabel:        util.VpcExternalNet,
			util.VpcNatGatewayNameLabel: eip.Spec.NatGwDp,
			util.VpcNatLabel:            natName,
		}
	} else if eip.Labels[util.VpcNatLabel] != natName {
		op = "replace"
		needUpdateLabel = true
		eip.Labels[util.SubnetNameLabel] = util.VpcExternalNet
		eip.Labels[util.VpcNatGatewayNameLabel] = eip.Spec.NatGwDp
		eip.Labels[util.VpcNatLabel] = natName
	}

	if needUpdateLabel {
		patchPayloadTemplate := `[{ "op": "%s", "path": "/metadata/labels", "value": %s }]`
		raw, _ := json.Marshal(eip.Labels)
		patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
		if _, err := c.config.KubeOvnClient.KubeovnV1().IptablesEIPs().Patch(context.Background(), eip.Name,
			types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{}); err != nil {
			klog.Errorf("failed to patch label for eip %s, %v", eip.Name, err)
			return err
		}
	}
	return err
}
