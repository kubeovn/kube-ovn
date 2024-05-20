package controller

import (
	"fmt"
	"net"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"github.com/kubeovn/kube-ovn/pkg/util"
)

type vpcService struct {
	Vips     []string
	Vpc      string
	Protocol v1.Protocol
}

func (c *Controller) enqueueAddService(obj interface{}) {
	if !c.isLeader() {
		return
	}
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.updateEndpointQueue.Add(key)
	svc := obj.(*v1.Service)
	klog.V(3).Infof("enqueue add service %s", key)

	if c.config.EnableNP {
		var netpols []string
		if netpols, err = c.svcMatchNetworkPolicies(svc); err != nil {
			utilruntime.HandleError(err)
			return
		}

		for _, np := range netpols {
			c.updateNpQueue.Add(np)
		}
	}
}

func (c *Controller) enqueueDeleteService(obj interface{}) {
	if !c.isLeader() {
		return
	}
	svc := obj.(*v1.Service)
	//klog.V(3).Infof("enqueue delete service %s/%s", svc.Namespace, svc.Name)
	klog.Infof("enqueue delete service %s/%s", svc.Namespace, svc.Name)
	if svc.Spec.ClusterIP != v1.ClusterIPNone && svc.Spec.ClusterIP != "" {

		if c.config.EnableNP {
			var netpols []string
			var err error
			if netpols, err = c.svcMatchNetworkPolicies(svc); err != nil {
				utilruntime.HandleError(err)
				return
			}

			for _, np := range netpols {
				c.updateNpQueue.Add(np)
			}
		}

		ips := util.ServiceClusterIPs(*svc)
		for _, port := range svc.Spec.Ports {
			vpcSvc := &vpcService{
				Protocol: port.Protocol,
				Vpc:      svc.Annotations[util.VpcAnnotation],
			}
			for _, ip := range ips {
				vpcSvc.Vips = append(vpcSvc.Vips, util.JoinHostPort(ip, port.Port))
			}
			klog.Infof("delete vpc service %v", vpcSvc)
			c.deleteServiceQueue.Add(vpcSvc)
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
	c.updateServiceQueue.Add(key)
}

func (c *Controller) runDeleteServiceWorker() {
	for c.processNextDeleteServiceWorkItem() {
	}
}

func (c *Controller) runUpdateServiceWorker() {
	for c.processNextUpdateServiceWorkItem() {
	}
}

func (c *Controller) processNextDeleteServiceWorkItem() bool {
	obj, shutdown := c.deleteServiceQueue.Get()

	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.deleteServiceQueue.Done(obj)
		var vpcSvc *vpcService
		var ok bool
		if vpcSvc, ok = obj.(*vpcService); !ok {
			c.deleteServiceQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected vpcService in workqueue but got %#v", obj))
			return nil
		}

		if err := c.handleDeleteService(vpcSvc); err != nil {
			c.deleteServiceQueue.AddRateLimited(obj)
			return fmt.Errorf("error syncing '%v': %s, requeuing", vpcSvc.Vips, err.Error())
		}
		c.deleteServiceQueue.Forget(obj)
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

func (c *Controller) handleDeleteService(service *vpcService) error {
	svcs, err := c.servicesLister.Services(v1.NamespaceAll).List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list svc, %v", err)
		return err
	}

	vpcLbConfig := c.GenVpcLoadBalancer(service.Vpc)
	var vpcLB [2]string
	switch service.Protocol {
	case v1.ProtocolTCP:
		vpcLB = [2]string{vpcLbConfig.TcpLoadBalancer, vpcLbConfig.TcpSessLoadBalancer}
	case v1.ProtocolUDP:
		vpcLB = [2]string{vpcLbConfig.UdpLoadBalancer, vpcLbConfig.UdpSessLoadBalancer}
	}

	for _, vip := range service.Vips {
		var found bool
		ip := parseVipAddr(vip)
		for _, svc := range svcs {
			if util.ContainsString(util.ServiceClusterIPs(*svc), ip) {
				found = true
				break
			}
		}
		if found {
			continue
		}

		for _, lb := range vpcLB {
			if err := c.ovnLegacyClient.DeleteLoadBalancerVip(vip, lb); err != nil {
				klog.Errorf("failed to delete vip %s from LB %s: %v", vip, lb, err)
				return err
			}
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

	vpcName := svc.Annotations[util.VpcAnnotation]
	if vpcName == "" {
		vpcName = util.DefaultVpc
	}
	vpc, err := c.vpcsLister.Get(vpcName)
	if err != nil {
		klog.Errorf("failed to get vpc %s of lb, %v", vpcName, err)
		return err
	}

	tcpLb, udpLb := vpc.Status.TcpLoadBalancer, vpc.Status.UdpLoadBalancer
	oTcpLb, oUdpLb := vpc.Status.TcpSessionLoadBalancer, vpc.Status.UdpSessionLoadBalancer
	if svc.Spec.SessionAffinity == v1.ServiceAffinityClientIP {
		tcpLb, udpLb = vpc.Status.TcpSessionLoadBalancer, vpc.Status.UdpSessionLoadBalancer
		oTcpLb, oUdpLb = vpc.Status.TcpLoadBalancer, vpc.Status.UdpLoadBalancer
	}

	var tcpVips, udpVips []string
	ips := util.ServiceClusterIPs(*svc)
	for _, port := range svc.Spec.Ports {
		for _, ip := range ips {
			switch port.Protocol {
			case v1.ProtocolTCP:
				tcpVips = append(tcpVips, util.JoinHostPort(ip, port.Port))
			case v1.ProtocolUDP:
				udpVips = append(udpVips, util.JoinHostPort(ip, port.Port))
			}
		}
	}

	// for service update
	updateVip := func(lb, oLb string, svcVips []string) error {
		vips, err := c.ovnLegacyClient.GetLoadBalancerVips(lb)
		if err != nil {
			klog.Errorf("failed to get vips of LB %s: %v", lb, err)
			return err
		}
		klog.V(3).Infof("existing vips of LB %s: %v", lb, vips)
		for _, vip := range svcVips {
			if err = c.ovnLegacyClient.DeleteLoadBalancerVip(vip, oLb); err != nil {
				klog.Errorf("failed to delete vip %s from LB %s: %v", vip, oLb, err)
				return err
			}
			if _, ok := vips[vip]; !ok {
				klog.Infof("add vip %s to LB %s", vip, lb)
				c.updateEndpointQueue.Add(key)
				break
			}
		}
		for vip := range vips {
			if ip := parseVipAddr(vip); util.ContainsString(ips, ip) && !util.IsStringIn(vip, svcVips) {
				klog.Infof("remove stale vip %s from LB %s", vip, lb)
				if err = c.ovnLegacyClient.DeleteLoadBalancerVip(vip, lb); err != nil {
					klog.Errorf("failed to delete vip %s from LB %s: %v", vip, lb, err)
					return err
				}
			}
		}
		if vips, err = c.ovnLegacyClient.GetLoadBalancerVips(oLb); err != nil {
			klog.Errorf("failed to get vips of LB %s: %v", oLb, err)
			return err
		}
		klog.V(3).Infof("existing vips of LB %s: %v", oLb, vips)
		for vip := range vips {
			if ip := parseVipAddr(vip); util.ContainsString(ips, ip) {
				klog.Infof("remove stale vip %s from LB %s", vip, oLb)
				if err = c.ovnLegacyClient.DeleteLoadBalancerVip(vip, oLb); err != nil {
					klog.Errorf("failed to delete vip %s from LB %s: %v", vip, oLb, err)
					return err
				}
			}
		}
		return nil
	}

	if err = updateVip(tcpLb, oTcpLb, tcpVips); err != nil {
		return err
	}
	if err = updateVip(udpLb, oUdpLb, udpVips); err != nil {
		return err
	}

	return nil
}

// Parse key of map, [fd00:10:96::11c9]:10665 for example
func parseVipAddr(vip string) string {
	host, _, err := net.SplitHostPort(vip)
	if err != nil {
		klog.Errorf("failed to parse vip %q: %v", vip, err)
		return ""
	}
	return host
}
