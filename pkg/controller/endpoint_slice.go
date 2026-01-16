package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func findServiceKey(endpointSlice *discoveryv1.EndpointSlice) string {
	if endpointSlice != nil && endpointSlice.Labels != nil && endpointSlice.Labels[discoveryv1.LabelServiceName] != "" {
		return endpointSlice.Namespace + "/" + endpointSlice.Labels[discoveryv1.LabelServiceName]
	}
	return ""
}

func (c *Controller) enqueueAddEndpointSlice(obj any) {
	key := findServiceKey(obj.(*discoveryv1.EndpointSlice))
	if key != "" {
		klog.V(3).Infof("enqueue add endpointSlice %s", key)
		c.addOrUpdateEndpointSliceQueue.Add(key)
	}
}

func (c *Controller) enqueueUpdateEndpointSlice(oldObj, newObj any) {
	oldEndpointSlice := oldObj.(*discoveryv1.EndpointSlice)
	newEndpointSlice := newObj.(*discoveryv1.EndpointSlice)
	if oldEndpointSlice.ResourceVersion == newEndpointSlice.ResourceVersion {
		return
	}

	if len(oldEndpointSlice.Endpoints) == 0 && len(newEndpointSlice.Endpoints) == 0 {
		return
	}

	key := findServiceKey(newEndpointSlice)
	if key != "" {
		klog.V(3).Infof("enqueue update endpointSlice for service %s", key)
		c.addOrUpdateEndpointSliceQueue.Add(key)
	}
}

func (c *Controller) handleUpdateEndpointSlice(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	c.epKeyMutex.LockKey(key)
	defer func() { _ = c.epKeyMutex.UnlockKey(key) }()
	klog.Infof("handle update endpointSlice for service %s", key)

	endpointSlices, err := c.endpointSlicesLister.EndpointSlices(namespace).List(labels.Set(map[string]string{discoveryv1.LabelServiceName: name}).AsSelector())
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
		isPreferLocalBackend     = false
	)

	if vip, ok = svc.Annotations[util.SwitchLBRuleVipsAnnotation]; ok {
		lbVips = []string{vip}

		for _, endpointSlice := range endpointSlices {
			for _, endpoint := range endpointSlice.Endpoints {
				if util.CheckProtocol(vip) == kubeovnv1.ProtocolIPv4 &&
					endpoint.TargetRef.Name != "" {
					ignoreHealthCheck = false
				}
			}
		}
	} else if lbVips = util.ServiceClusterIPs(*svc); len(lbVips) == 0 {
		return nil
	}

	if c.config.EnableLb && c.config.EnableOVNLBPreferLocal {
		if svc.Spec.Type == v1.ServiceTypeLoadBalancer && svc.Spec.ExternalTrafficPolicy == v1.ServiceExternalTrafficPolicyTypeLocal {
			if len(svc.Status.LoadBalancer.Ingress) > 0 {
				for _, ingress := range svc.Status.LoadBalancer.Ingress {
					if ingress.IP != "" {
						lbVips = append(lbVips, ingress.IP)
					}
				}
			}
			isPreferLocalBackend = true
		} else if svc.Spec.Type == v1.ServiceTypeClusterIP && svc.Spec.InternalTrafficPolicy != nil && *svc.Spec.InternalTrafficPolicy == v1.ServiceInternalTrafficPolicyLocal {
			isPreferLocalBackend = true
		}
	}

	if pods, err = c.podsLister.Pods(namespace).List(labels.Set(svc.Spec.Selector).AsSelector()); err != nil {
		klog.Errorf("failed to get pods for service %s in namespace %s: %v", name, namespace, err)
		return err
	}
	vpcName, subnetName = c.getVpcSubnetName(pods, endpointSlices, svc)

	var (
		vpc    *kubeovnv1.Vpc
		svcVpc string
	)

	if vpc, err = c.vpcsLister.Get(vpcName); err != nil {
		klog.Errorf("failed to get vpc %s, %v", vpcName, err)
		return err
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

			if !ignoreHealthCheck {
				if checkIP, err = c.getHealthCheckVip(subnetName, lbVip); err != nil {
					klog.Error(err)
					return err
				}
				externals = map[string]string{
					util.SwitchLBRuleSubnet: subnetName,
				}
			}

			if isPreferLocalBackend {
				// only use the ipportmapping's lsp to ip map when the backend is local
				checkIP = util.MasqueradeCheckIP
			}
			isGenIPPortMapping := !ignoreHealthCheck || isPreferLocalBackend
			ipPortMapping, backends = getIPPortMappingBackend(endpointSlices, port, lbVip, checkIP, isGenIPPortMapping)
			// for performance reason delete lb with no backends
			if len(backends) != 0 {
				vip = util.JoinHostPort(lbVip, port.Port)
				klog.Infof("add vip endpoint %s, backends %v to LB %s", vip, backends, lb)
				if err = c.OVNNbClient.LoadBalancerAddVip(lb, vip, backends...); err != nil {
					klog.Errorf("failed to add vip %s with backends %s to LB %s: %v", lbVip, backends, lb, err)
					return err
				}

				if isPreferLocalBackend && len(ipPortMapping) != 0 {
					if err = c.OVNNbClient.LoadBalancerUpdateIPPortMapping(lb, vip, ipPortMapping); err != nil {
						klog.Errorf("failed to update ip port mapping %s for vip %s to LB %s: %v", ipPortMapping, vip, lb, err)
						return err
					}
				}

				if !ignoreHealthCheck && len(ipPortMapping) != 0 {
					klog.Infof("add health check ip port mapping %v to LB %s", ipPortMapping, lb)
					if err = c.OVNNbClient.LoadBalancerAddHealthCheck(lb, vip, ignoreHealthCheck, ipPortMapping, externals); err != nil {
						klog.Errorf("failed to add health check for vip %s with ip port mapping %s to LB %s: %v", lbVip, ipPortMapping, lb, err)
						return err
					}
				}
			} else {
				vip = util.JoinHostPort(lbVip, port.Port)
				klog.V(3).Infof("delete vip endpoint %s from LB %s", vip, lb)
				if err = c.OVNNbClient.LoadBalancerDeleteVip(lb, vip, true); err != nil {
					klog.Errorf("failed to delete vip endpoint %s from LB %s: %v", vip, lb, err)
					return err
				}

				klog.V(3).Infof("delete vip endpoint %s from old LB %s", vip, oldLb)
				if err = c.OVNNbClient.LoadBalancerDeleteVip(oldLb, vip, true); err != nil {
					klog.Errorf("failed to delete vip %s from LB %s: %v", vip, oldLb, err)
					return err
				}

				if c.config.EnableOVNLBPreferLocal {
					if err := c.OVNNbClient.LoadBalancerDeleteIPPortMapping(lb, vip); err != nil {
						klog.Errorf("failed to delete ip port mapping for vip %s from LB %s: %v", vip, lb, err)
						return err
					}
					if err := c.OVNNbClient.LoadBalancerDeleteIPPortMapping(oldLb, vip); err != nil {
						klog.Errorf("failed to delete ip port mapping for vip %s from LB %s: %v", vip, lb, err)
						return err
					}
				}
			}
		}
	}

	if svcVpc = svc.Annotations[util.VpcAnnotation]; svcVpc != vpcName {
		patch := util.KVPatch{util.VpcAnnotation: vpcName}
		if err = util.PatchAnnotations(c.config.KubeClient.CoreV1().Services(namespace), svc.Name, patch); err != nil {
			klog.Errorf("failed to patch service %s: %v", key, err)
			return err
		}
	}

	return nil
}

func (c *Controller) getVpcSubnetName(pods []*v1.Pod, endpointSlices []*discoveryv1.EndpointSlice, service *v1.Service) (string, string) {
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
		for _, endpointSlice := range endpointSlices {
			for _, endpoint := range endpointSlice.Endpoints {
				for _, addr := range endpoint.Addresses {
					for _, podIP := range pod.Status.PodIPs {
						if addr == podIP.IP {
							if vpcName == "" {
								vpcName = pod.Annotations[util.LogicalRouterAnnotation]
							}
							if vpcName != "" {
								break LOOP
							}
						}
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

// getHealthCheckVip get health check vip for load balancer, the vip name is the subnet name
// the vip is used to check the health of the backend pod
func (c *Controller) getHealthCheckVip(subnetName, lbVip string) (string, error) {
	var (
		needCreateHealthCheckVip bool
		checkVip                 *kubeovnv1.Vip
		checkIP                  string
		err                      error
	)
	vipName := subnetName
	checkVip, err = c.virtualIpsLister.Get(vipName)
	if err != nil {
		if errors.IsNotFound(err) {
			needCreateHealthCheckVip = true
		} else {
			klog.Errorf("failed to get health check vip %s, %v", vipName, err)
			return "", err
		}
	}
	if needCreateHealthCheckVip {
		vip := &kubeovnv1.Vip{
			ObjectMeta: metav1.ObjectMeta{
				Name: vipName,
			},
			Spec: kubeovnv1.VipSpec{
				Subnet: subnetName,
			},
		}
		if _, err = c.config.KubeOvnClient.KubeovnV1().Vips().Create(context.Background(), vip, metav1.CreateOptions{}); err != nil {
			klog.Errorf("failed to create health check vip %s, %v", vipName, err)
			return "", err
		}

		// wait for vip created
		time.Sleep(1 * time.Second)
		checkVip, err = c.virtualIpsLister.Get(vipName)
		if err != nil {
			klog.Errorf("failed to get health check vip %s, %v", vipName, err)
			return "", err
		}
	}

	if checkVip.Status.V4ip == "" && checkVip.Status.V6ip == "" {
		err = fmt.Errorf("vip %s is not ready", vipName)
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
		err = fmt.Errorf("failed to get health check vip subnet %s", vipName)
		klog.Error(err)
		return "", err
	}

	return checkIP, nil
}

func getIPPortMappingBackend(endpointSlices []*discoveryv1.EndpointSlice, servicePort v1.ServicePort, serviceIP, checkVip string, isGenIPPortMapping bool) (map[string]string, []string) {
	var (
		ipPortMapping = map[string]string{}
		backends      = []string{}
		protocol      = util.CheckProtocol(serviceIP)
	)

	for _, endpointSlice := range endpointSlices {
		var targetPort int32
		for _, port := range endpointSlice.Ports {
			if port.Name != nil && *port.Name == servicePort.Name {
				targetPort = *port.Port
				break
			}
		}
		if targetPort == 0 {
			continue
		}

		for _, endpoint := range endpointSlice.Endpoints {
			if isGenIPPortMapping && endpoint.TargetRef.Name != "" {
				lspName := getEndpointTargetLSP(endpoint.TargetRef.Name, endpoint.TargetRef.Namespace, util.OvnProvider)
				for _, address := range endpoint.Addresses {
					key := address
					if util.CheckProtocol(address) == kubeovnv1.ProtocolIPv6 {
						key = fmt.Sprintf("[%s]", address)
					}
					ipPortMapping[key] = fmt.Sprintf(util.HealthCheckNamedVipTemplate, lspName, checkVip)
				}
			}
		}

		for _, endpoint := range endpointSlice.Endpoints {
			if !endpointReady(endpoint) {
				continue
			}

			for _, address := range endpoint.Addresses {
				if util.CheckProtocol(address) == protocol {
					backends = append(backends, util.JoinHostPort(address, targetPort))
				}
			}
		}
	}

	return ipPortMapping, backends
}

func endpointReady(endpoint discoveryv1.Endpoint) bool {
	return endpoint.Conditions.Ready == nil || *endpoint.Conditions.Ready
}

// getEndpointTargetLSP returns the name of the LSP for a given target/namespace.
// A custom provider can be specified if the LSP is within a subnet that doesn't use
// the default "ovn" provider.
func getEndpointTargetLSP(target, namespace, provider string) string {
	// This pod seems to be a VM launcher pod, but we do not use the same syntax for the LSP
	// of normal pods and for VM pods. We need to retrieve the real name of the VM from
	// the pod's name to compute the LSP.
	if strings.HasPrefix(target, util.VMLauncherPrefix) {
		target = getVMNameFromLauncherPod(target)
	}

	return ovs.PodNameToPortName(target, namespace, provider)
}

// getVMNameFromLauncherPod returns the name of a VirtualMachine from the name of its launcher pod (virt-launcher)
func getVMNameFromLauncherPod(podName string) string {
	// Remove the VM launcher pod prefix
	vmName := strings.TrimPrefix(podName, util.VMLauncherPrefix)

	// Remove the ID of the pod
	slice := strings.Split(vmName, "-")
	if len(slice) > 0 {
		vmName = strings.Join(slice[:len(slice)-1], "-")
	}

	return vmName
}
