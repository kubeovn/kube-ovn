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
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

func (c *Controller) enqueueAddVirtualIp(obj interface{}) {
	if !c.isLeader() {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.addVirtualIpQueue.Add(key)
}

func (c *Controller) enqueueDelVirtualIp(obj interface{}) {
	if !c.isLeader() {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.delVirtualIpQueue.Add(key)
	vipObj := obj.(*kubeovnv1.VirtualIP)
	c.updateSubnetStatusQueue.Add(vipObj.Spec.Subnet)
}

func (c *Controller) runAddVirtualIpWorker() {
	for c.processNextAddVirtualIpWorkItem() {
	}
}

func (c *Controller) runDelVirtualIpWorker() {
	for c.processNextDeleteVirtualIpWorkItem() {
	}
}

func (c *Controller) processNextAddVirtualIpWorkItem() bool {
	obj, shutdown := c.addVirtualIpQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.addVirtualIpQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.addVirtualIpQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleAddVirtualIp(key); err != nil {
			c.addVirtualIpQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.addVirtualIpQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextDeleteVirtualIpWorkItem() bool {
	obj, shutdown := c.delVirtualIpQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.delVirtualIpQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.delVirtualIpQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleDelVirtualIp(key); err != nil {
			c.delVirtualIpQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.delVirtualIpQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) handleAddVirtualIp(key string) error {
	cachedVip, err := c.virtualIpsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if cachedVip.Spec.MacAddress != "" {
		// already ok
		return nil
	}
	vip := cachedVip.DeepCopy()
	klog.Infof("handle add vip [%s]", vip.Name)
	var sourceV4Ip, v4ip, v6ip, mac, nicName, subnetName, parentV4ip, parentV6ip, parentMac string
	subnetName = vip.Spec.Subnet
	if subnetName == "" {
		return fmt.Errorf("failed to create vip [%s] without subnet", key)
	}
	subnet, err := c.subnetsLister.Get(subnetName)
	if err != nil {
		return err
	}
	nicName = ovs.PodNameToPortName(vip.Name, vip.Namespace, subnet.Spec.Provider)
	sourceV4Ip = vip.Spec.V4ip
	if sourceV4Ip != "" {
		v4ip, v6ip, mac, err = c.acquireStaticVirtualAddress(subnet.Name, vip.Name, vip.Namespace, nicName, sourceV4Ip)
	} else {
		// Random allocate
		v4ip, v6ip, mac, err = c.acquireVirtualAddress(subnet.Name, vip.Name, vip.Namespace, nicName)
	}
	if err != nil {
		return err
	}
	if vip.Spec.ParentMac != "" {
		parentV4ip = vip.Spec.ParentV4ip
		parentV6ip = vip.Spec.ParentV6ip
		parentMac = vip.Spec.ParentMac
	}
	if err = c.createOrUpdateCrdVip(key, vip.Namespace, subnet.Name, v4ip, v6ip, mac, parentV4ip, parentV6ip, parentMac); err != nil {
		klog.Errorf("failed to create or update vip [%s], %v", vip.Name, err)
		time.Sleep(2 * time.Second)
		return err
	}
	if err = c.subnetCountVip(subnet); err != nil {
		klog.Errorf("failed to count virtual ip [%s] in subnet, %v", vip.Name, err)
		time.Sleep(2 * time.Second)
		return err
	}
	return nil
}

func (c *Controller) handleDelVirtualIp(key string) error {
	klog.Infof("l2 pod release virtual ip [%s]", key)
	c.ipam.ReleaseAddressByPod(key)
	return nil
}

func (c *Controller) acquireStaticVirtualAddress(subnetName, name, namespace, nicName, ip string) (string, string, string, error) {
	liveMigration := true
	var v4ip, v6ip, mac string
	var err error
	for _, ipStr := range strings.Split(ip, ",") {
		if net.ParseIP(ipStr) == nil {
			return "", "", "", fmt.Errorf("failed to parse virtual IP %s", ipStr)
		}
	}

	if v4ip, v6ip, mac, err = c.ipam.GetStaticAddress(name, nicName, ip, mac, subnetName, !liveMigration); err != nil {
		klog.Errorf("failed to get static virtual ip %v, mac %v, subnet %v, err %v", ip, mac, subnetName, err)
		return "", "", "", err
	}
	return v4ip, v6ip, mac, nil
}

func (c *Controller) acquireVirtualAddress(subnetName, name, namespace, nicName string) (string, string, string, error) {
	var skippedAddrs []string
	for {
		ipv4, ipv6, mac, err := c.ipam.GetRandomAddress(name, nicName, subnetName, skippedAddrs)
		if err != nil {
			return "", "", "", err
		}

		ipv4OK, ipv6OK, err := c.validatePodIP(name, subnetName, ipv4, ipv6)
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

func (c *Controller) subnetCountVip(subnet *kubeovnv1.Subnet) error {
	var err error
	if util.CheckProtocol(subnet.Spec.CIDRBlock) == kubeovnv1.ProtocolDual {
		err = calcDualSubnetStatusIP(subnet, c)
	} else {
		err = calcSubnetStatusIP(subnet, c)
	}
	return err
}

func (c *Controller) createOrUpdateCrdVip(key, ns, subnet, v4ip, v6ip, mac, pV4ip, pV6ip, pmac string) error {
	vipCr, err := c.config.KubeOvnClient.KubeovnV1().VirtualIPs().Get(context.Background(), key, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			_, err := c.config.KubeOvnClient.KubeovnV1().VirtualIPs().Create(context.Background(), &kubeovnv1.VirtualIP{
				ObjectMeta: metav1.ObjectMeta{
					Name: key,
					Labels: map[string]string{
						util.SubnetNameLabel: subnet,
					},
					Namespace: ns,
				},
				Spec: kubeovnv1.VirtualIPSpec{
					Namespace:  ns,
					Subnet:     subnet,
					V4ip:       v4ip,
					V6ip:       v6ip,
					MacAddress: mac,
				},
			}, metav1.CreateOptions{})

			if err != nil {
				errMsg := fmt.Errorf("failed to create vip crd for [%s], %v", key, err)
				klog.Error(errMsg)
				return errMsg
			}
		} else {
			errMsg := fmt.Errorf("failed to get vip crd for [%s], %v", key, err)
			klog.Error(errMsg)
			return errMsg
		}
	} else {
		vip := vipCr.DeepCopy()
		if vip.Spec.MacAddress == "" && mac != "" {
			// vip not support to update, just delete and create
			vip.ObjectMeta.Namespace = ns
			vip.Spec.Namespace = ns
			vip.Spec.Subnet = subnet
			vip.Spec.V4ip = v4ip
			vip.Spec.V6ip = v6ip
			vip.Spec.MacAddress = mac
			vip.Spec.ParentV4ip = pV4ip
			vip.Spec.ParentV6ip = pV6ip
			vip.Spec.ParentMac = pmac
			_, err := c.config.KubeOvnClient.KubeovnV1().VirtualIPs().Update(context.Background(), vip, metav1.UpdateOptions{})
			if err != nil {
				errMsg := fmt.Errorf("failed to update vip crd for [%s], %v", key, err)
				klog.Error(errMsg)
				return errMsg
			}
		}
		var needUpdateLabel bool
		var op string
		if len(vip.Labels) == 0 {
			op = "add"
			vip.Labels = map[string]string{util.SubnetNameLabel: subnet}
			needUpdateLabel = true
		}
		if vip.Labels[util.SubnetNameLabel] != subnet {
			op = "replace"
			vip.Labels[util.SubnetNameLabel] = subnet
			needUpdateLabel = true
		}
		if needUpdateLabel {
			patchPayloadTemplate := `[{ "op": "%s", "path": "/metadata/labels", "value": %s }]`
			raw, _ := json.Marshal(vip.Labels)
			patchPayload := fmt.Sprintf(patchPayloadTemplate, op, raw)
			_, err := c.config.KubeOvnClient.KubeovnV1().VirtualIPs().Patch(context.Background(), vip.Name, types.JSONPatchType, []byte(patchPayload), metav1.PatchOptions{})
			if err != nil {
				klog.Errorf("failed to patch vip label %s: %v", vip.Name, err)
				return err
			}
		}
	}
	return nil
}
