package controller

import (
	"context"
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) enqueueAddEndpoint(obj interface{}) {
	var (
		key string
		err error
	)

	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue add endpoint %s", key)
	c.updateEndpointQueue.Add(key)
}

func (c *Controller) enqueueUpdateEndpoint(oldObj, newObj interface{}) {
	oldEp := oldObj.(*v1.Endpoints)
	newEp := newObj.(*v1.Endpoints)
	if oldEp.ResourceVersion == newEp.ResourceVersion {
		return
	}

	if len(oldEp.Subsets) == 0 && len(newEp.Subsets) == 0 {
		return
	}

	var (
		key string
		err error
	)

	if key, err = cache.MetaNamespaceKeyFunc(newObj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).Infof("enqueue update endpoint %s", key)
	c.updateEndpointQueue.Add(key)
}

func (c *Controller) runUpdateEndpointWorker() {
	for c.processNextUpdateEndpointWorkItem() {
	}
}

func (c *Controller) processNextUpdateEndpointWorkItem() bool {
	obj, shutdown := c.updateEndpointQueue.Get()

	if shutdown {
		return false
	}

	if err := func(obj interface{}) error {
		defer c.updateEndpointQueue.Done(obj)

		var (
			key string
			ok  bool
			err error
		)

		if key, ok = obj.(string); !ok {
			c.updateEndpointQueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}

		if err = c.handleUpdateEndpoint(key); err != nil {
			c.updateEndpointQueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		c.updateEndpointQueue.Forget(obj)
		return nil
	}(obj); err != nil {
		utilruntime.HandleError(err)
		return true
	}
	return true
}

func (c *Controller) handleUpdateEndpoint(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	c.epKeyMutex.LockKey(key)
	defer func() { _ = c.epKeyMutex.UnlockKey(key) }()
	klog.Infof("update add/update endpoint %s/%s", namespace, name)

	ep, err := c.endpointsLister.Endpoints(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	cachedService, err := c.servicesLister.Services(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	svc := cachedService.DeepCopy()

	var (
		pods                     []*v1.Pod
		lbVips                   []string
		vip, vpcName, subnetName string
		ok                       bool
		ignoreHealthCheck        = true
	)

	if vip, ok = svc.Annotations[util.SwitchLBRuleVipsAnnotation]; ok {
		lbVips = []string{vip}

		for _, subset := range ep.Subsets {
			for _, address := range subset.Addresses {
				// TODO: IPv6
				if util.CheckProtocol(vip) == kubeovnv1.ProtocolIPv4 &&
					address.TargetRef.Name != "" {
					ignoreHealthCheck = false
				}
			}
		}
	} else if lbVips = util.ServiceClusterIPs(*svc); len(lbVips) == 0 {
		return nil
	}

	if pods, err = c.podsLister.Pods(namespace).List(labels.Set(svc.Spec.Selector).AsSelector()); err != nil {
		klog.Errorf("failed to get pods for service %s in namespace %s: %v", name, namespace, err)
		return err
	}

	vpcName, subnetName = c.getVpcSubnetName(pods, ep, svc)

	var (
		vpc    *kubeovnv1.Vpc
		svcVpc string
	)

	if vpc, err = c.vpcsLister.Get(vpcName); err != nil {
		klog.Errorf("failed to get vpc %s of lb, %v", vpcName, err)
		return err
	}

	if svcVpc = svc.Annotations[util.VpcAnnotation]; svcVpc != vpcName {
		if svc.Annotations == nil {
			svc.Annotations = make(map[string]string, 1)
		}
		svc.Annotations[util.VpcAnnotation] = vpcName
		if _, err = c.config.KubeClient.CoreV1().Services(namespace).Update(context.Background(), svc, metav1.UpdateOptions{}); err != nil {
			klog.Errorf("failed to update service %s/%s: %v", namespace, svc.Name, err)
			return err
		}
	}

	tcpLb, udpLb, sctpLb := vpc.Status.TCPLoadBalancer, vpc.Status.UDPLoadBalancer, vpc.Status.SctpLoadBalancer
	oldTCPLb, oldUDPLb, oldSctpLb := vpc.Status.TCPSessionLoadBalancer, vpc.Status.UDPSessionLoadBalancer, vpc.Status.SctpSessionLoadBalancer
	if svc.Spec.SessionAffinity == v1.ServiceAffinityClientIP {
		tcpLb, udpLb, sctpLb, oldTCPLb, oldUDPLb, oldSctpLb = oldTCPLb, oldUDPLb, oldSctpLb, tcpLb, udpLb, sctpLb
	}

	for _, lbVip := range lbVips {
		for _, port := range svc.Spec.Ports {
			var lb, oldLb string
			switch port.Protocol {
			case v1.ProtocolTCP:
				lb, oldLb = tcpLb, oldTCPLb
			case v1.ProtocolUDP:
				lb, oldLb = udpLb, oldUDPLb
			case v1.ProtocolSCTP:
				lb, oldLb = sctpLb, oldSctpLb
			}

			var (
				vip, checkIP             string
				backends                 []string
				ipPortMapping, externals map[string]string
			)
			vip = util.JoinHostPort(lbVip, port.Port)

			if !ignoreHealthCheck {
				if checkIP, err = c.getHealthCheckVip(subnetName, lbVip); err != nil {
					klog.Error(err)
					return err
				}

				externals = map[string]string{
					util.SwitchLBRuleSubnet: subnetName,
				}
			}

			ipPortMapping, backends = getIPPortMappingBackend(ep, pods, port, lbVip, checkIP, ignoreHealthCheck)

			// for performance reason delete lb with no backends
			if len(backends) != 0 {
				klog.Infof("add vip endpoint %s, backends %v to LB %s", vip, backends, lb)
				if err = c.OVNNbClient.LoadBalancerAddVip(lb, vip, backends...); err != nil {
					klog.Errorf("failed to add vip %s with backends %s to LB %s: %v", lbVip, backends, lb, err)
					return err
				}
				if !ignoreHealthCheck && len(ipPortMapping) != 0 {
					klog.Infof("add health check ip port mapping %v to LB %s", ipPortMapping, lb)
					if err = c.OVNNbClient.LoadBalancerAddHealthCheck(lb, vip, ignoreHealthCheck, ipPortMapping, externals); err != nil {
						klog.Errorf("failed to add health check for vip %s with ip port mapping %s to LB %s: %v", lbVip, ipPortMapping, lb, err)
						return err
					}
				}
			} else {
				klog.V(3).Infof("delete vip endpoint %s from LB %s", vip, lb)
				if err = c.OVNNbClient.LoadBalancerDeleteVip(lb, vip, ignoreHealthCheck); err != nil {
					klog.Errorf("failed to delete vip endpoint %s from LB %s: %v", vip, lb, err)
					return err
				}

				klog.V(3).Infof("delete vip endpoint %s from old LB %s", vip, oldLb)
				if err = c.OVNNbClient.LoadBalancerDeleteVip(oldLb, vip, ignoreHealthCheck); err != nil {
					klog.Errorf("failed to delete vip %s from LB %s: %v", vip, oldLb, err)
					return err
				}
			}
		}
	}
	return nil
}

func (c *Controller) getVpcSubnetName(pods []*v1.Pod, endpoints *v1.Endpoints, service *v1.Service) (string, string) {
	var (
		vpcName    string
		subnetName string
	)

	for _, pod := range pods {
		if len(pod.Annotations) == 0 {
			continue
		}

		if subnetName == "" {
			subnetName = pod.Annotations[util.LogicalSwitchAnnotation]
		}

	LOOP:
		for _, subset := range endpoints.Subsets {
			for _, addr := range subset.Addresses {
				if addr.IP == pod.Status.PodIP {
					if vpcName == "" {
						vpcName = pod.Annotations[util.LogicalRouterAnnotation]
					}

					if vpcName != "" {
						break LOOP
					}
				}
			}
		}

		if vpcName != "" && subnetName != "" {
			break
		}
	}

	if vpcName == "" {
		if vpcName = service.Annotations[util.VpcAnnotation]; vpcName == "" {
			vpcName = c.config.ClusterRouter
		}
	}

	if subnetName == "" {
		subnetName = util.DefaultSubnet
	}

	return vpcName, subnetName
}

func (c *Controller) getHealthCheckVip(subnetName, lbVip string) (string, error) {
	var (
		needCreateHealthCheckVip bool
		checkVip                 *kubeovnv1.Vip
		checkIP                  string
		err                      error
	)

	checkVip, err = c.virtualIpsLister.Get(subnetName)
	if err != nil {
		if errors.IsNotFound(err) {
			needCreateHealthCheckVip = true
		} else {
			klog.Errorf("failed to get health check vip %s, %v", subnetName, err)
			return "", err
		}
	}
	if needCreateHealthCheckVip {
		vip := &kubeovnv1.Vip{
			ObjectMeta: metav1.ObjectMeta{
				Name: subnetName,
			},
			Spec: kubeovnv1.VipSpec{
				Subnet: subnetName,
			},
		}
		if _, err = c.config.KubeOvnClient.KubeovnV1().Vips().Create(context.Background(), vip, metav1.CreateOptions{}); err != nil {
			klog.Errorf("failed to create health check vip %s, %v", subnetName, err)
			return "", err
		}

		// wait for vip created
		time.Sleep(1 * time.Second)
		checkVip, err = c.virtualIpsLister.Get(subnetName)
		if err != nil {
			klog.Errorf("failed to get health check vip %s, %v", subnetName, err)
			return "", err
		}
	}

	if checkVip.Status.V4ip == "" && checkVip.Status.V6ip == "" {
		err = fmt.Errorf("failed to get health check vip %s address", subnetName)
		klog.Error(err)
		return "", err
	}

	switch util.CheckProtocol(lbVip) {
	case kubeovnv1.ProtocolIPv4:
		checkIP = checkVip.Status.V4ip
	case kubeovnv1.ProtocolIPv6:
		checkIP = checkVip.Status.V6ip
	}
	if checkIP == "" {
		err = fmt.Errorf("failed to get health check vip subnet %s", subnetName)
		klog.Error(err)
		return "", err
	}

	return checkIP, nil
}

func getIPPortMappingBackend(endpoints *v1.Endpoints, pods []*v1.Pod, servicePort v1.ServicePort, serviceIP, checkVip string, ignoreHealthCheck bool) (map[string]string, []string) {
	var (
		ipPortMapping = map[string]string{}
		backends      = []string{}
		protocol      = util.CheckProtocol(serviceIP)
	)

	for _, subset := range endpoints.Subsets {
		var targetPort int32
		for _, port := range subset.Ports {
			if port.Name == servicePort.Name {
				targetPort = port.Port
				break
			}
		}
		if targetPort == 0 {
			continue
		}

		for _, address := range subset.Addresses {
			if !ignoreHealthCheck && address.TargetRef.Name != "" {
				ipName := fmt.Sprintf("%s.%s", address.TargetRef.Name, endpoints.Namespace)
				ipPortMapping[address.IP] = fmt.Sprintf(util.HealthCheckNamedVipTemplate, ipName, checkVip)
			}
			if address.TargetRef == nil || address.TargetRef.Kind != "Pod" {
				if util.CheckProtocol(address.IP) == protocol {
					backends = append(backends, util.JoinHostPort(address.IP, targetPort))
				}
				continue
			}
			var ip string
			for _, pod := range pods {
				if pod.Name == address.TargetRef.Name {
					podIPs := pod.Status.PodIPs
					if len(podIPs) == 0 && pod.Status.PodIP != "" {
						podIPs = []v1.PodIP{{IP: pod.Status.PodIP}}
					}
					for _, podIP := range podIPs {
						if util.CheckProtocol(podIP.IP) == protocol {
							ip = podIP.IP
							break
						}
					}
					break
				}
			}
			if ip == "" && util.CheckProtocol(address.IP) == protocol {
				ip = address.IP
			}
			if ip != "" {
				backends = append(backends, util.JoinHostPort(ip, targetPort))
			}
		}
	}

	return ipPortMapping, backends
}
