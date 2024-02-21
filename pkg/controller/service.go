package controller

import (
	"context"
	"fmt"
	"net"
	"slices"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	Svc      *v1.Service
}

func (c *Controller) enqueueAddService(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.updateEndpointQueue.Add(key)
	svc := obj.(*v1.Service)

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

	if c.config.EnableLbSvc {
		klog.V(3).Infof("enqueue add service %s", key)
		c.addServiceQueue.Add(key)
	}
}

func (c *Controller) enqueueDeleteService(obj interface{}) {
	svc := obj.(*v1.Service)
	klog.Infof("enqueue delete service %s/%s", svc.Namespace, svc.Name)

	vip, ok := svc.Annotations[util.SwitchLBRuleVipsAnnotation]
	if ok || svc.Spec.ClusterIP != v1.ClusterIPNone && svc.Spec.ClusterIP != "" {

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
		if ok {
			ips = strings.Split(vip, ",")
		}

		for _, port := range svc.Spec.Ports {
			vpcSvc := &vpcService{
				Protocol: port.Protocol,
				Vpc:      svc.Annotations[util.VpcAnnotation],
				Svc:      svc,
			}
			for _, ip := range ips {
				vpcSvc.Vips = append(vpcSvc.Vips, util.JoinHostPort(ip, port.Port))
			}
			klog.Infof("delete vpc service %v", vpcSvc)
			c.deleteServiceQueue.Add(vpcSvc)
		}
	}
}

func (c *Controller) enqueueUpdateService(oldObj, newObj interface{}) {
	oldSvc := oldObj.(*v1.Service)
	newSvc := newObj.(*v1.Service)
	if oldSvc.ResourceVersion == newSvc.ResourceVersion {
		return
	}

	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(newObj); err != nil {
		utilruntime.HandleError(err)
		return
	}

	oldClusterIps := getVipIps(oldSvc)
	newClusterIps := getVipIps(newSvc)
	var ipsToDel []string
	for _, oldClusterIP := range oldClusterIps {
		if !slices.Contains(newClusterIps, oldClusterIP) {
			ipsToDel = append(ipsToDel, oldClusterIP)
		}
	}

	klog.V(3).Infof("enqueue update service %s", key)
	if len(ipsToDel) != 0 {
		ipsToDelStr := strings.Join(ipsToDel, ",")
		key = strings.Join([]string{key, ipsToDelStr}, "#")
	}

	c.updateServiceQueue.Add(key)
}

func (c *Controller) runAddServiceWorker() {
	for c.processNextAddServiceWorkItem() {
	}
}

func (c *Controller) runDeleteServiceWorker() {
	for c.processNextDeleteServiceWorkItem() {
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
	key, err := cache.MetaNamespaceKeyFunc(service.Svc)
	if err != nil {
		klog.Error(err)
		utilruntime.HandleError(fmt.Errorf("failed to get meta namespace key of %#v: %v", service.Svc, err))
		return nil
	}

	c.svcKeyMutex.LockKey(key)
	defer func() { _ = c.svcKeyMutex.UnlockKey(key) }()
	klog.Infof("handle delete service %s", key)

	svcs, err := c.servicesLister.Services(v1.NamespaceAll).List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list svc, %v", err)
		return err
	}

	var (
		vpcLB             [2]string
		vpcLbConfig       = c.GenVpcLoadBalancer(service.Vpc)
		ignoreHealthCheck = true
	)

	switch service.Protocol {
	case v1.ProtocolTCP:
		vpcLB = [2]string{vpcLbConfig.TCPLoadBalancer, vpcLbConfig.TCPSessLoadBalancer}
	case v1.ProtocolUDP:
		vpcLB = [2]string{vpcLbConfig.UDPLoadBalancer, vpcLbConfig.UDPSessLoadBalancer}
	case v1.ProtocolSCTP:
		vpcLB = [2]string{vpcLbConfig.SctpLoadBalancer, vpcLbConfig.SctpSessLoadBalancer}
	}

	for _, vip := range service.Vips {
		var (
			ip    string
			found bool
		)
		ip = parseVipAddr(vip)

		for _, svc := range svcs {
			if slices.Contains(util.ServiceClusterIPs(*svc), ip) {
				found = true
				break
			}
		}
		if found {
			continue
		}

		for _, lb := range vpcLB {
			if err = c.OVNNbClient.LoadBalancerDeleteVip(lb, vip, ignoreHealthCheck); err != nil {
				klog.Errorf("failed to delete vip %s from LB %s: %v", vip, lb, err)
				return err
			}
		}
	}

	if service.Svc.Spec.Type == v1.ServiceTypeLoadBalancer && c.config.EnableLbSvc {
		if err := c.deleteLbSvc(service.Svc); err != nil {
			klog.Errorf("failed to delete service %s, %v", service.Svc.Name, err)
			return err
		}
	}

	return nil
}

func (c *Controller) handleUpdateService(key string) error {
	keys := strings.Split(key, "#")
	key = keys[0]
	var ipsToDel []string
	if len(keys) == 2 {
		ipsToDelStr := keys[1]
		ipsToDel = strings.Split(ipsToDelStr, ",")
	}

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		klog.Error(err)
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	c.svcKeyMutex.LockKey(key)
	defer func() { _ = c.svcKeyMutex.UnlockKey(key) }()
	klog.Infof("handle update service %s", key)

	svc, err := c.servicesLister.Services(namespace).Get(name)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	ips := getVipIps(svc)

	vpcName := svc.Annotations[util.VpcAnnotation]
	if vpcName == "" {
		vpcName = c.config.ClusterRouter
	}
	vpc, err := c.vpcsLister.Get(vpcName)
	if err != nil {
		klog.Errorf("failed to get vpc %s of lb, %v", vpcName, err)
		return err
	}

	tcpLb, udpLb, sctpLb := vpc.Status.TCPLoadBalancer, vpc.Status.UDPLoadBalancer, vpc.Status.SctpLoadBalancer
	oTCPLb, oUDPLb, oSctpLb := vpc.Status.TCPSessionLoadBalancer, vpc.Status.UDPSessionLoadBalancer, vpc.Status.SctpSessionLoadBalancer
	if svc.Spec.SessionAffinity == v1.ServiceAffinityClientIP {
		tcpLb, udpLb, sctpLb, oTCPLb, oUDPLb, oSctpLb = oTCPLb, oUDPLb, oSctpLb, tcpLb, udpLb, sctpLb
	}

	var tcpVips, udpVips, sctpVips []string
	for _, port := range svc.Spec.Ports {
		for _, ip := range ips {
			switch port.Protocol {
			case v1.ProtocolTCP:
				tcpVips = append(tcpVips, util.JoinHostPort(ip, port.Port))
			case v1.ProtocolUDP:
				udpVips = append(udpVips, util.JoinHostPort(ip, port.Port))
			case v1.ProtocolSCTP:
				sctpVips = append(sctpVips, util.JoinHostPort(ip, port.Port))
			}
		}
	}

	var (
		needUpdateEndpointQueue = false
		ignoreHealthCheck       = true
	)

	// for service update
	updateVip := func(lbName, oLbName string, svcVips []string) error {
		if len(lbName) == 0 {
			return nil
		}

		lb, err := c.OVNNbClient.GetLoadBalancer(lbName, false)
		if err != nil {
			klog.Errorf("failed to get LB %s: %v", lbName, err)
			return err
		}
		klog.V(3).Infof("existing vips of LB %s: %v", lbName, lb.Vips)
		for _, vip := range svcVips {
			if err := c.OVNNbClient.LoadBalancerDeleteVip(oLbName, vip, ignoreHealthCheck); err != nil {
				klog.Errorf("failed to delete vip %s from LB %s: %v", vip, oLbName, err)
				return err
			}

			if !needUpdateEndpointQueue {
				if _, ok := lb.Vips[vip]; !ok {
					klog.Infof("add vip %s to LB %s", vip, lbName)
					needUpdateEndpointQueue = true
				}
			}
		}
		for vip := range lb.Vips {
			if ip := parseVipAddr(vip); (slices.Contains(ips, ip) && !slices.Contains(svcVips, vip)) || slices.Contains(ipsToDel, ip) {
				klog.Infof("remove stale vip %s from LB %s", vip, lbName)
				if err := c.OVNNbClient.LoadBalancerDeleteVip(lbName, vip, ignoreHealthCheck); err != nil {
					klog.Errorf("failed to delete vip %s from LB %s: %v", vip, lbName, err)
					return err
				}
			}
		}

		if len(oLbName) == 0 {
			return nil
		}

		oLb, err := c.OVNNbClient.GetLoadBalancer(oLbName, false)
		if err != nil {
			klog.Errorf("failed to get LB %s: %v", oLbName, err)
			return err
		}
		klog.V(3).Infof("existing vips of LB %s: %v", oLbName, lb.Vips)
		for vip := range oLb.Vips {
			if ip := parseVipAddr(vip); slices.Contains(ips, ip) || slices.Contains(ipsToDel, ip) {
				klog.Infof("remove stale vip %s from LB %s", vip, oLbName)
				if err = c.OVNNbClient.LoadBalancerDeleteVip(oLbName, vip, ignoreHealthCheck); err != nil {
					klog.Errorf("failed to delete vip %s from LB %s: %v", vip, oLbName, err)
					return err
				}
			}
		}
		return nil
	}

	if err = updateVip(tcpLb, oTCPLb, tcpVips); err != nil {
		klog.Error(err)
		return err
	}
	if err = updateVip(udpLb, oUDPLb, udpVips); err != nil {
		klog.Error(err)
		return err
	}
	if err = updateVip(sctpLb, oSctpLb, sctpVips); err != nil {
		klog.Error(err)
		return err
	}

	if needUpdateEndpointQueue {
		c.updateEndpointQueue.Add(key)
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

func (c *Controller) handleAddService(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		klog.Error(err)
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	c.svcKeyMutex.LockKey(key)
	defer func() { _ = c.svcKeyMutex.UnlockKey(key) }()
	klog.Infof("handle add service %s", key)

	svc, err := c.servicesLister.Services(namespace).Get(name)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	if svc.Spec.Type != v1.ServiceTypeLoadBalancer || !c.config.EnableLbSvc {
		return nil
	}
	klog.Infof("add svc %s/%s", namespace, name)

	if err = c.validateSvc(svc); err != nil {
		klog.Errorf("failed to validate lb svc, %v", err)
		return err
	}

	if err = c.checkAttachNetwork(svc); err != nil {
		klog.Errorf("failed to check attachment network, %v", err)
		return err
	}

	if err = c.createLbSvcPod(svc); err != nil {
		klog.Errorf("failed to create lb svc pod, %v", err)
		return err
	}

	var pod *v1.Pod
	for {
		pod, err = c.getLbSvcPod(name, namespace)
		if err != nil {
			klog.Errorf("wait lb svc pod to running, %v", err)
			time.Sleep(1 * time.Second)
		}
		if pod != nil {
			break
		}

		// It's important here to check existing of svc, used to break the loop.
		_, err = c.servicesLister.Services(namespace).Get(name)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				return nil
			}
			klog.Error(err)
			return err
		}
	}

	loadBalancerIP, err := c.getPodAttachIP(pod, svc)
	if err != nil {
		klog.Errorf("failed to get loadBalancerIP: %v", err)
		return err
	}

	newSvc, err := c.servicesLister.Services(namespace).Get(name)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	var ingress v1.LoadBalancerIngress
	ingress.IP = loadBalancerIP
	newSvc.Status.LoadBalancer.Ingress = []v1.LoadBalancerIngress{ingress}

	var updateSvc *v1.Service
	if updateSvc, err = c.config.KubeClient.CoreV1().Services(namespace).UpdateStatus(context.Background(), newSvc, metav1.UpdateOptions{}); err != nil {
		klog.Errorf("update service %s/%s status failed: %v", namespace, name, err)
		return err
	}

	if err := c.updatePodAttachNets(pod, updateSvc); err != nil {
		klog.Errorf("update service %s/%s attachment network failed: %v", namespace, name, err)
		return err
	}

	return nil
}

func getVipIps(svc *v1.Service) []string {
	var ips []string
	if vip, ok := svc.Annotations[util.SwitchLBRuleVipsAnnotation]; ok {
		ips = strings.Split(vip, ",")
	} else {
		ips = util.ServiceClusterIPs(*svc)
	}
	return ips
}
