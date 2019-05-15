package controller

import (
	"fmt"
	"k8s.io/apimachinery/pkg/labels"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
)

func (c *Controller) enqueueDeleteService(obj interface{}) {
	if !c.isLeader() {
		return
	}
	svc := obj.(*v1.Service)
	klog.V(3).Infof("enqueue delete service %s/%s", svc.Namespace, svc.Name)
	if svc.Spec.ClusterIP != v1.ClusterIPNone && svc.Spec.ClusterIP != "" {
		for _, port := range svc.Spec.Ports {
			if port.Protocol == v1.ProtocolTCP {
				c.deleteTcpServiceQueue.AddRateLimited(fmt.Sprintf("%s:%d", svc.Spec.ClusterIP, port.Port))
			} else if port.Protocol == v1.ProtocolUDP {
				c.deleteUdpServiceQueue.AddRateLimited(fmt.Sprintf("%s:%d", svc.Spec.ClusterIP, port.Port))
			}
		}
	}
}

func (c *Controller) enqueueUpdateService(old, new interface{}) {
	if !c.isLeader() {
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
	klog.V(3).Infof("enqueue update service %s", key)
	c.updateServiceQueue.AddRateLimited(key)
}

func (c *Controller) runDeleteTcpServiceWorker() {
	for c.processNextDeleteTcpServiceWorkItem() {
	}
}

func (c *Controller) runDeleteUdpServiceWorker() {
	for c.processNextDeleteUdpServiceWorkItem() {
	}
}

func (c *Controller) runUpdateServiceWorker() {
	for c.processNextUpdateServiceWorkItem() {
	}
}

func (c *Controller) processNextDeleteTcpServiceWorkItem() bool {
	obj, shutdown := c.deleteTcpServiceQueue.Get()

	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.deleteTcpServiceQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.deleteTcpServiceQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleDeleteService(key, v1.ProtocolTCP); err != nil {
			c.deleteTcpServiceQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.deleteTcpServiceQueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) processNextDeleteUdpServiceWorkItem() bool {
	obj, shutdown := c.deleteUdpServiceQueue.Get()

	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.deleteUdpServiceQueue.Done(obj)
		var key string
		var ok bool
		if key, ok = obj.(string); !ok {
			c.deleteUdpServiceQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.handleDeleteService(key, v1.ProtocolUDP); err != nil {
			c.deleteUdpServiceQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.deleteUdpServiceQueue.Forget(obj)
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

func (c *Controller) handleDeleteService(vip string, protocol v1.Protocol) error {
	svcs, err := c.servicesLister.Services(v1.NamespaceAll).List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list svc, %v", err)
		return err
	}
	for _, svc := range svcs {
		if svc.Spec.ClusterIP == strings.Split(vip, ":")[0] {
			return nil
		}
	}

	if protocol == v1.ProtocolTCP {
		err := c.ovnClient.DeleteLoadBalancerVip(vip, c.config.ClusterTcpLoadBalancer)
		if err != nil {
			klog.Errorf("failed to delete vip %s from tcp lb, %v", vip, err)
			return err
		}
	} else {
		err := c.ovnClient.DeleteLoadBalancerVip(vip, c.config.ClusterUdpLoadBalancer)
		if err != nil {
			klog.Errorf("failed to delete vip %s from udp lb, %v", vip, err)
			return err
		}
	}

	return nil
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

	return nil
}

//
// Helper functions to check string from a slice of strings.
//
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}
