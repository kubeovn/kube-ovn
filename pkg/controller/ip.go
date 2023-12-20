package controller

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) enqueueAddIP(obj interface{}) {
	ipObj := obj.(*kubeovnv1.IP)
	if strings.HasPrefix(ipObj.Name, util.U2OInterconnName[0:19]) {
		return
	}
	klog.V(3).Infof("enqueue update status subnet %s", ipObj.Spec.Subnet)
	c.updateSubnetStatusQueue.Add(ipObj.Spec.Subnet)
	for _, as := range ipObj.Spec.AttachSubnets {
		klog.V(3).Infof("enqueue update attach status for subnet %s", as)
		c.updateSubnetStatusQueue.Add(as)
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue add ip %s", key)
	c.addIPQueue.Add(key)
}

func (c *Controller) enqueueUpdateIP(oldObj, newObj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(newObj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	oldIP := oldObj.(*kubeovnv1.IP)
	newIP := newObj.(*kubeovnv1.IP)
	if !newIP.DeletionTimestamp.IsZero() {
		klog.V(3).Infof("enqueue update ip %s", key)
		c.updateIPQueue.Add(key)
		return
	}
	if !reflect.DeepEqual(oldIP.Spec.AttachSubnets, newIP.Spec.AttachSubnets) {
		klog.V(3).Infof("enqueue update status subnet %s", newIP.Spec.Subnet)
		for _, as := range newIP.Spec.AttachSubnets {
			klog.V(3).Infof("enqueue update status for attach subnet %s", as)
			c.updateSubnetStatusQueue.Add(as)
		}
	}
}

func (c *Controller) enqueueDelIP(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	ipObj := obj.(*kubeovnv1.IP)
	if strings.HasPrefix(ipObj.Name, util.U2OInterconnName[0:19]) {
		return
	}
	klog.V(3).Infof("enqueue del ip %s", key)
	c.delIPQueue.Add(ipObj)
}

func (c *Controller) runAddIPWorker() {
	for c.processNextAddIPWorkItem() {
	}
}

func (c *Controller) runUpdateIPWorker() {
	for c.processNextUpdateIPWorkItem() {
	}
}

func (c *Controller) runDelIPWorker() {
	for c.processNextDeleteIPWorkItem() {
	}
}

func (c *Controller) processNextAddIPWorkItem() bool {
	obj, shutdown := c.addIPQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.addIPQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.addIPQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleAddIP(key); err != nil {
			c.addIPQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.addIPQueue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextUpdateIPWorkItem() bool {
	obj, shutdown := c.updateIPQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.updateIPQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.updateIPQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleUpdateIP(key); err != nil {
			c.updateIPQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.updateIPQueue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextDeleteIPWorkItem() bool {
	obj, shutdown := c.delIPQueue.Get()
	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.delIPQueue.Done(obj)
		var ip *kubeovnv1.IP
		var ok bool
		if ip, ok = obj.(*kubeovnv1.IP); !ok {
			c.delIPQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected ip in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleDelIP(ip); err != nil {
			c.delIPQueue.AddRateLimited(obj)
			return fmt.Errorf("error syncing '%s': %s, requeuing", ip.Name, err.Error())
		}
		c.delIPQueue.Forget(obj)
		return nil
	}(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) handleAddIP(key string) error {
	cachedIP, err := c.ipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	klog.V(3).Infof("handle add ip %s", cachedIP.Name)
	if err := c.handleAddIPFinalizer(cachedIP, util.ControllerName); err != nil {
		klog.Errorf("failed to handle add ip finalizer %v", err)
		return err
	}
	return nil
}

func (c *Controller) handleUpdateIP(key string) error {
	cachedIP, err := c.ipsLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	if !cachedIP.DeletionTimestamp.IsZero() {
		subnet, err := c.subnetsLister.Get(cachedIP.Spec.Subnet)
		if err != nil {
			klog.Errorf("failed to get subnet %s: %v", cachedIP.Spec.Subnet, err)
			return err
		}
		cleanIPAM := true
		if isOvnSubnet(subnet) {
			portName := cachedIP.Name
			port, err := c.ovnClient.GetLogicalSwitchPort(portName, true)
			if err != nil {
				klog.Errorf("failed to get logical switch port %s: %v", portName, err)
				return err
			}
			if port != nil {
				if slices.Contains(port.Addresses, cachedIP.Spec.V4IPAddress) || slices.Contains(port.Addresses, cachedIP.Spec.V6IPAddress) {
					klog.Infof("delete ip cr lsp %s from switch %s", portName, subnet.Name)
					if err := c.ovnLegacyClient.DeleteLogicalSwitchPort(portName); err != nil {
						klog.Errorf("delete ip cr lsp %s from switch %s: %v", portName, subnet.Name, err)
						return err
					}
					klog.V(3).Infof("sync sg for deleted port %s", portName)
					sgList, err := c.getPortSg(port)
					if err != nil {
						klog.Errorf("get port sg failed, %v", err)
						return err
					}
					for _, sgName := range sgList {
						if sgName != "" {
							c.syncSgPortsQueue.Add(sgName)
						}
					}
				} else {
					// ip subnet changed in pod handle add or update pod process
					klog.Infof("lsp %s ip changed, only delete old ip cr %s", portName, key)
					cleanIPAM = false
				}
			}
		}
		if cleanIPAM {
			klog.V(3).Infof("release ipam for deleted ip %s from subnet %s", cachedIP.Name, cachedIP.Spec.Subnet)
			c.ipam.ReleaseAddressByPod(cachedIP.Name, cachedIP.Spec.Subnet)
		}
		if err = c.handleDelIPFinalizer(cachedIP, util.ControllerName); err != nil {
			klog.Errorf("failed to handle del ip finalizer %v", err)
			return err
		}
	}
	return nil
}

func (c *Controller) handleDelIP(ip *kubeovnv1.IP) error {
	klog.V(3).Infof("handle delete ip %s", ip.Name)
	klog.V(3).Infof("enqueue update status subnet %s", ip.Spec.Subnet)
	c.updateSubnetStatusQueue.Add(ip.Spec.Subnet)
	for _, as := range ip.Spec.AttachSubnets {
		klog.V(3).Infof("enqueue update attach status for subnet %s", as)
		c.updateSubnetStatusQueue.Add(as)
	}
	return nil
}

func (c *Controller) handleAddIPFinalizer(cachedIP *kubeovnv1.IP, finalizer string) error {
	if cachedIP.DeletionTimestamp.IsZero() {
		if util.ContainsString(cachedIP.Finalizers, finalizer) {
			return nil
		}
	}
	newIP := cachedIP.DeepCopy()
	controllerutil.AddFinalizer(newIP, finalizer)
	patch, err := util.GenerateMergePatchPayload(cachedIP, newIP)
	if err != nil {
		klog.Errorf("failed to generate patch payload for ip %s, %v", cachedIP.Name, err)
		return err
	}
	if _, err := c.config.KubeOvnClient.KubeovnV1().IPs().Patch(context.Background(), cachedIP.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to add finalizer for ip %s, %v", cachedIP.Name, err)
		return err
	}
	return nil
}

func (c *Controller) handleDelIPFinalizer(cachedIP *kubeovnv1.IP, finalizer string) error {
	if len(cachedIP.Finalizers) == 0 {
		return nil
	}
	newIP := cachedIP.DeepCopy()
	controllerutil.RemoveFinalizer(newIP, finalizer)
	patch, err := util.GenerateMergePatchPayload(cachedIP, newIP)
	if err != nil {
		klog.Errorf("failed to generate patch payload for ip %s, %v", cachedIP.Name, err)
		return err
	}
	if _, err := c.config.KubeOvnClient.KubeovnV1().IPs().Patch(context.Background(), cachedIP.Name,
		types.MergePatchType, patch, metav1.PatchOptions{}, ""); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("failed to remove finalizer from ip %s, %v", cachedIP.Name, err)
		return err
	}
	return nil
}
