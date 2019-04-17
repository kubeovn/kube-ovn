package controller

import (
	"fmt"
	"strings"

	"github.com/alauda/kube-ovn/pkg/util"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
)

func (c *Controller) enqueueAddService(obj interface{}) {
	if !c.isLeader.Load().(bool) {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.addServiceQueue.AddRateLimited(key)
}

func (c *Controller) enqueueUpdateService(old, new interface{}) {
	if !c.isLeader.Load().(bool) {
		return
	}
	oldSvc := old.(*v1.Service)
	newSvc := new.(*v1.Service)
	if oldSvc.ResourceVersion == newSvc.ResourceVersion {
		return
	}

	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(new); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.updateServiceQueue.AddRateLimited(key)
}

func (c *Controller) runAddServiceWorker() {
	for c.processNextAddServiceWorkItem() {
	}
}

func (c *Controller) runUpdateServiceWorker() {
	for c.processNextUpdateServiceWorkItem() {
	}
}

func (c *Controller) processNextAddServiceWorkItem() bool {
	obj, shutdown := c.addServiceQueue.Get()

	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.addServiceQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.addServiceQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleAddService(key); err != nil {
			c.addServiceQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.addServiceQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextUpdateServiceWorkItem() bool {
	obj, shutdown := c.updateServiceQueue.Get()

	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.updateServiceQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.updateServiceQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleUpdateService(key); err != nil {
			c.updateServiceQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.updateServiceQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) handleAddService(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}
	svc, err := c.servicesLister.Services(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}

	if !containsString(svc.Finalizers, util.ServiceAnnotation) {
		svc.SetFinalizers(append(svc.Finalizers, util.ServiceAnnotation))
		_, err = c.config.KubeClient.CoreV1().Services(namespace).Update(svc)
		if err != nil {
			return err
		}
	}

	ip := svc.Spec.ClusterIP
	if ip == "" || ip == v1.ClusterIPNone {
		return nil
	}

	if !svc.DeletionTimestamp.IsZero() {
		if containsString(svc.Finalizers, util.ServiceAnnotation) {
			svc.SetFinalizers(removeString(svc.Finalizers, util.ServiceAnnotation))
			_, err = c.config.KubeClient.CoreV1().Services(namespace).Update(svc)
		}
	}

	return err
}

func (c *Controller) handleUpdateService(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}
	klog.Infof("update svc %s/%s", namespace, name)
	svc, err := c.servicesLister.Services(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}

	if !svc.DeletionTimestamp.IsZero() {
		if containsString(svc.Finalizers, util.ServiceAnnotation) {
			svc.SetFinalizers(removeString(svc.Finalizers, util.ServiceAnnotation))
			_, err = c.config.KubeClient.CoreV1().Services(namespace).Update(svc)
			if err != nil {
				return err
			}
		}
	}

	ip := svc.Spec.ClusterIP
	if ip == "" || ip == v1.ClusterIPNone {
		return nil
	}

	tcpVips := []string{}
	udpVips := []string{}

	for _, port := range svc.Spec.Ports {
		if port.Protocol == v1.ProtocolTCP {
			tcpVips = append(tcpVips, fmt.Sprintf("%s:%d", ip, port.Port))
		} else if port.Protocol == v1.ProtocolUDP {
			udpVips = append(udpVips, fmt.Sprintf("%s:%d", ip, port.Port))
		}
	}

	if !svc.DeletionTimestamp.IsZero() {
		// for service deletion
		klog.Infof("delete service %s/%s", namespace, name)
		for _, vip := range tcpVips {
			err := c.ovnClient.DeleteLoadBalancerVip(vip, c.config.ClusterTcpLoadBalancer)
			if err != nil {
				klog.Errorf("failed to delete vip %s from tcp lb, %v", vip, err)
				return err
			}
		}

		for _, vip := range udpVips {
			err := c.ovnClient.DeleteLoadBalancerVip(vip, c.config.ClusterUdpLoadBalancer)
			if err != nil {
				klog.Errorf("failed to delete vip %s from udp lb, %v", vip, err)
				return err
			}
		}
	} else {
		// for service update
		klog.Infof("update service %s/%s", namespace, name)
		lbUuid, err := c.ovnClient.FindLoadbalancer(c.config.ClusterTcpLoadBalancer)
		if err != nil {
			klog.Errorf("failed to get lb %v", err)
		}
		vips, err := c.ovnClient.GetLoadBalancerVips(lbUuid)
		if err != nil {
			klog.Errorf("failed to get tcp lb vips %v", err)
			return err
		}
		klog.Infof("exist tcp vips are %v", vips)
		for _, vip := range tcpVips {
			if _, ok := vips[vip]; !ok {
				klog.Infof("add vip %s to tcp lb", vip)
				c.updateEndpointQueue.AddRateLimited(key)
				break
			}
		}

		for vip := range vips {
			if strings.HasPrefix(vip, ip) && !containsString(tcpVips, vip) {
				klog.Infof("remove stall vip %s", vip)
				err := c.ovnClient.DeleteLoadBalancerVip(vip, c.config.ClusterTcpLoadBalancer)
				if err != nil {
					klog.Errorf("failed to delete vip %s from tcp lb %v", vip, err)
					return err
				}
			}
		}

		lbUuid, err = c.ovnClient.FindLoadbalancer(c.config.ClusterUdpLoadBalancer)
		if err != nil {
			klog.Errorf("failed to get lb %v", err)
		}
		vips, err = c.ovnClient.GetLoadBalancerVips(lbUuid)
		if err != nil {
			klog.Errorf("failed to get udp lb vips %v", err)
			return err
		}
		klog.Infof("exist udp vips are %v", vips)
		for _, vip := range udpVips {
			if _, ok := vips[vip]; !ok {
				klog.Infof("add vip %s to udp lb", vip)
				c.updateEndpointQueue.AddRateLimited(key)
				break
			}
		}

		for vip := range vips {
			if strings.HasPrefix(vip, ip) && !containsString(udpVips, vip) {
				klog.Infof("remove stall vip %s", vip)
				err := c.ovnClient.DeleteLoadBalancerVip(vip, c.config.ClusterUdpLoadBalancer)
				if err != nil {
					klog.Errorf("failed to delete vip %s from udp lb %v", vip, err)
					return err
				}
			}
		}
	}

	return nil
}

//
// Helper functions to check and remove string from a slice of strings.
//
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func removeString(slice []string, s string) (result []string) {
	for _, item := range slice {
		if item == s {
			continue
		}
		result = append(result, item)
	}
	return
}
