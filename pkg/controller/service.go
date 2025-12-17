package controller

import (
	"context"
	"fmt"
	"maps"
	"net"
	"reflect"
	"slices"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
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

type updateSvcObject struct {
	key      string
	oldPorts []v1.ServicePort
	newPorts []v1.ServicePort
}

func (c *Controller) enqueueAddService(obj any) {
	svc := obj.(*v1.Service)
	key := cache.MetaObjectToName(svc).String()
	klog.V(3).Infof("enqueue add endpoint %s", key)
	c.addOrUpdateEndpointSliceQueue.Add(key)

	if c.config.EnableNP {
		netpols, err := c.svcMatchNetworkPolicies(svc)
		if err != nil {
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

func (c *Controller) enqueueDeleteService(obj any) {
	var svc *v1.Service
	switch t := obj.(type) {
	case *v1.Service:
		svc = t
	case cache.DeletedFinalStateUnknown:
		s, ok := t.Obj.(*v1.Service)
		if !ok {
			klog.Warningf("unexpected object type: %T", t.Obj)
			return
		}
		svc = s
	default:
		klog.Warningf("unexpected type: %T", obj)
		return
	}

	klog.Infof("enqueue delete service %s/%s", svc.Namespace, svc.Name)

	vip, ok := svc.Annotations[util.SwitchLBRuleVipsAnnotation]
	if ok || svc.Spec.ClusterIP != v1.ClusterIPNone && svc.Spec.ClusterIP != "" || svc.Annotations[util.ServiceExternalIPFromSubnetAnnotation] != "" {
		if c.config.EnableNP {
			netpols, err := c.svcMatchNetworkPolicies(svc)
			if err != nil {
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

		if svc.Annotations[util.ServiceExternalIPFromSubnetAnnotation] != "" {
			for _, ingress := range svc.Status.LoadBalancer.Ingress {
				ips = append(ips, ingress.IP)
			}
		}

		for _, port := range svc.Spec.Ports {
			vpcSvc := &vpcService{
				Protocol: port.Protocol,
				Vpc:      svc.Annotations[util.VpcAnnotation],
				Svc:      svc.DeepCopy(),
			}
			for _, ip := range ips {
				vpcSvc.Vips = append(vpcSvc.Vips, util.JoinHostPort(ip, port.Port))
			}
			klog.V(3).Infof("delete vpc service: %v", vpcSvc)
			c.deleteServiceQueue.Add(vpcSvc)
		}
	}
}

func (c *Controller) enqueueUpdateService(oldObj, newObj any) {
	oldSvc := oldObj.(*v1.Service)
	newSvc := newObj.(*v1.Service)
	if oldSvc.ResourceVersion == newSvc.ResourceVersion {
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

	key := cache.MetaObjectToName(newSvc).String()
	klog.V(3).Infof("enqueue update service %s", key)
	if len(ipsToDel) != 0 {
		ipsToDelStr := strings.Join(ipsToDel, ",")
		key = strings.Join([]string{key, ipsToDelStr}, "#")
	}

	updateSvc := &updateSvcObject{
		key:      key,
		oldPorts: oldSvc.Spec.Ports,
		newPorts: newSvc.Spec.Ports,
	}
	c.updateServiceQueue.Add(updateSvc)
}

func (c *Controller) handleDeleteService(service *vpcService) error {
	key := cache.MetaObjectToName(service.Svc).String()

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
			if c.config.EnableOVNLBPreferLocal {
				if err = c.OVNNbClient.LoadBalancerDeleteIPPortMapping(lb, vip); err != nil {
					klog.Errorf("failed to delete ip port mapping for vip %s from LB %s: %v", vip, lb, err)
					return err
				}
			}

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

func (c *Controller) handleUpdateService(svcObject *updateSvcObject) error {
	key := svcObject.key
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
		lbVIPs := maps.Clone(lb.Vips)
		klog.V(3).Infof("existing vips of LB %s: %v", lbName, lbVIPs)
		for _, vip := range svcVips {
			if err := c.OVNNbClient.LoadBalancerDeleteVip(oLbName, vip, ignoreHealthCheck); err != nil {
				klog.Errorf("failed to delete vip %s from LB %s: %v", vip, oLbName, err)
				return err
			}

			if _, ok := lbVIPs[vip]; !ok {
				klog.Infof("add vip %s to LB %s", vip, lbName)
				needUpdateEndpointQueue = true
			}
		}
		for vip := range lbVIPs {
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
		oLbVIPs := maps.Clone(oLb.Vips)
		klog.V(3).Infof("existing vips of LB %s: %v", oLbName, oLbVIPs)
		for vip := range oLbVIPs {
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

	if err := c.checkServiceLBIPBelongToSubnet(svc); err != nil {
		klog.Error(err)
		return err
	}

	if needUpdateEndpointQueue {
		c.addOrUpdateEndpointSliceQueue.Add(key)
	}
	// add the svc key which has the same vip
	vip, ok := svc.Annotations[util.SwitchLBRuleVipsAnnotation]
	if ok {
		allSlrs, err := c.switchLBRuleLister.List(labels.Everything())
		if err != nil {
			klog.Error(err)
			return err
		}
		for _, slr := range allSlrs {
			if slr.Spec.Vip == vip {
				slrKey := fmt.Sprintf("%s/slr-%s", slr.Spec.Namespace, slr.Name)
				c.addOrUpdateEndpointSliceQueue.Add(slrKey)
			}
		}
	}

	if c.config.EnableLbSvc && svc.Spec.Type == v1.ServiceTypeLoadBalancer {
		changed, err := c.checkLbSvcDeployAnnotationChanged(svc)
		if err != nil {
			klog.Errorf("failed to check annotation change for lb svc %s: %v", key, err)
			return err
		}

		// only process svc.spec.ports update
		if !changed {
			klog.Infof("update loadbalancer service %s", key)
			pod, err := c.getLbSvcPod(name, namespace)
			if err != nil {
				klog.Errorf("failed to get pod for lb svc %s: %v", key, err)
				if strings.Contains(err.Error(), "not found") {
					return nil
				}
				return err
			}

			toDel := diffSvcPorts(svcObject.oldPorts, svcObject.newPorts)
			if err := c.delDnatRules(pod, toDel, svc); err != nil {
				klog.Errorf("failed to delete dnat rules, err: %v", err)
				return err
			}
			if err = c.updatePodAttachNets(pod, svc); err != nil {
				klog.Errorf("failed to update pod attachment network for lb svc %s: %v", key, err)
				return err
			}
		}
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
	if !c.config.EnableLbSvc {
		return nil
	}

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
	if svc.Spec.Type != v1.ServiceTypeLoadBalancer {
		return nil
	}
	// Skip non kube-ovn lb-svc.
	if _, ok := svc.Annotations[util.AttachmentProvider]; !ok {
		return nil
	}

	klog.Infof("handle add loadbalancer service %s", key)

	if err = c.validateSvc(svc); err != nil {
		c.recorder.Event(svc, v1.EventTypeWarning, "ValidateSvcFailed", err.Error())
		klog.Errorf("failed to validate lb svc %s: %v", key, err)
		return err
	}

	nad, err := c.getAttachNetworkForService(svc)
	if err != nil {
		c.recorder.Event(svc, v1.EventTypeWarning, "GetNADFailed", err.Error())
		klog.Errorf("failed to check attachment network of lb svc %s: %v", key, err)
		return err
	}

	if err = c.createLbSvcPod(svc, nad); err != nil {
		klog.Errorf("failed to create lb svc pod for %s: %v", key, err)
		return err
	}

	var pod *v1.Pod
	for {
		pod, err = c.getLbSvcPod(name, namespace)
		if err != nil {
			klog.Warningf("pod for lb svc %s is not running: %v", key, err)
			time.Sleep(time.Second)
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

	svc, err = c.servicesLister.Services(namespace).Get(name)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	targetSvc := svc.DeepCopy()
	if err = c.updatePodAttachNets(pod, targetSvc); err != nil {
		klog.Errorf("failed to update pod attachment network for service %s/%s: %v", namespace, name, err)
		return err
	}

	// compatible with IPv4 and IPv6 dual stack subnet.
	var ingress []v1.LoadBalancerIngress
	for ip := range strings.SplitSeq(loadBalancerIP, ",") {
		if ip != "" && net.ParseIP(ip) != nil {
			ingress = append(ingress, v1.LoadBalancerIngress{IP: ip})
		}
	}
	targetSvc.Status.LoadBalancer.Ingress = ingress
	if !equality.Semantic.DeepEqual(svc.Status, targetSvc.Status) {
		if _, err = c.config.KubeClient.CoreV1().Services(namespace).
			UpdateStatus(context.Background(), targetSvc, metav1.UpdateOptions{}); err != nil {
			klog.Errorf("failed to update status of service %s/%s: %v", namespace, name, err)
			return err
		}
	}

	return nil
}

func getVipIps(svc *v1.Service) []string {
	var ips []string
	if vip, ok := svc.Annotations[util.SwitchLBRuleVipsAnnotation]; ok {
		ips = strings.Split(vip, ",")
	} else {
		ips = util.ServiceClusterIPs(*svc)
		if svc.Annotations[util.ServiceExternalIPFromSubnetAnnotation] != "" {
			for _, ingress := range svc.Status.LoadBalancer.Ingress {
				ips = append(ips, ingress.IP)
			}
		}
	}
	return ips
}

func diffSvcPorts(oldPorts, newPorts []v1.ServicePort) (toDel []v1.ServicePort) {
	for _, oldPort := range oldPorts {
		found := false
		for _, newPort := range newPorts {
			if reflect.DeepEqual(oldPort, newPort) {
				found = true
				break
			}
		}
		if !found {
			toDel = append(toDel, oldPort)
		}
	}

	return toDel
}

func (c *Controller) checkServiceLBIPBelongToSubnet(svc *v1.Service) error {
	svc = svc.DeepCopy()
	if svc.Annotations == nil {
		svc.Annotations = map[string]string{}
	}
	subnets, err := c.subnetsLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list subnets: %v", err)
		return err
	}

	isServiceExternalIPFromSubnet := false
	for _, subnet := range subnets {
		for _, ingress := range svc.Status.LoadBalancer.Ingress {
			if util.CIDRContainIP(subnet.Spec.CIDRBlock, ingress.IP) {
				svc.Annotations[util.ServiceExternalIPFromSubnetAnnotation] = subnet.Name
				isServiceExternalIPFromSubnet = true
				break
			}
		}
	}

	if !isServiceExternalIPFromSubnet {
		delete(svc.Annotations, util.ServiceExternalIPFromSubnetAnnotation)
	}
	klog.Infof("Service %s/%s external IP belongs to subnet: %v", svc.Namespace, svc.Name, isServiceExternalIPFromSubnet)
	if _, err = c.config.KubeClient.CoreV1().Services(svc.Namespace).Update(context.TODO(), svc, metav1.UpdateOptions{}); err != nil {
		klog.Errorf("failed to update service %s/%s: %v", svc.Namespace, svc.Name, err)
		return err
	}

	return nil
}
