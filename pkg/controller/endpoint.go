package controller

import (
	"context"
	"fmt"
	"maps"
	"net"
	"slices"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"k8s.io/utils/set"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) enqueueAddEndpoint(obj interface{}) {
	key := cache.MetaObjectToName(obj.(*v1.Endpoints)).String()
	klog.V(3).Infof("enqueue add endpoint %s", key)
	c.addOrUpdateEndpointQueue.Add(key)
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

	key := cache.MetaObjectToName(newEp).String()
	klog.V(3).Infof("enqueue update endpoint %s", key)
	c.addOrUpdateEndpointQueue.Add(key)
}

func (c *Controller) handleUpdateEndpoint(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	c.epKeyMutex.LockKey(key)
	defer func() { _ = c.epKeyMutex.UnlockKey(key) }()
	klog.Infof("handle update endpoint %s", key)

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
		vips                     []string
		vip, vpcName, subnetName string
		ok                       bool
		ignoreHealthCheck        = true
	)

	if vip, ok = svc.Annotations[util.SwitchLBRuleVipsAnnotation]; ok {
		vips = []string{vip}

		for _, subset := range ep.Subsets {
			for _, address := range subset.Addresses {
				// TODO: IPv6
				if util.CheckProtocol(vip) == kubeovnv1.ProtocolIPv4 &&
					address.TargetRef.Name != "" {
					ignoreHealthCheck = false
				}
			}
		}
	} else if vips = util.ServiceClusterIPs(*svc); len(vips) == 0 {
		return nil
	}

	if pods, err = c.podsLister.Pods(namespace).List(labels.Set(svc.Spec.Selector).AsSelector()); err != nil {
		klog.Errorf("failed to get pods for service %s in namespace %s: %v", name, namespace, err)
		return err
	}
	for i, pod := range pods {
		if pod.Status.PodIP != "" || len(pod.Status.PodIPs) != 0 {
			continue
		}

		for _, subset := range ep.Subsets {
			for _, addr := range subset.Addresses {
				if addr.TargetRef == nil || addr.TargetRef.Kind != "Pod" || addr.TargetRef.Name != pod.Name {
					continue
				}

				p, err := c.config.KubeClient.CoreV1().Pods(pod.Namespace).Get(context.Background(), pod.Name, metav1.GetOptions{})
				if err != nil {
					klog.Errorf("failed to get pod %s/%s: %v", pod.Namespace, pod.Name, err)
					return err
				}
				pods[i] = p.DeepCopy()
				break
			}
			if pods[i] != pod {
				break
			}
		}
	}

	vpcName, subnetName = c.getVpcSubnetName(pods, ep, svc)
	vpc, err := c.vpcsLister.Get(vpcName)
	if err != nil {
		klog.Errorf("failed to get vpc %s, %v", vpcName, err)
		return err
	}

	var tcpLB, udpLB, sctpLB string
	tcpLBs := set.New(vpc.Status.TCPLoadBalancer, vpc.Status.TCPSessionLoadBalancer, vpc.Status.LocalTCPLoadBalancer, vpc.Status.TCPSessionLoadBalancer)
	udpLBs := set.New(vpc.Status.UDPLoadBalancer, vpc.Status.UDPSessionLoadBalancer, vpc.Status.LocalUDPLoadBalancer, vpc.Status.UDPSessionLoadBalancer)
	sctpLBs := set.New(vpc.Status.SCTPLoadBalancer, vpc.Status.SCTPSessionLoadBalancer, vpc.Status.LocalSCTPLoadBalancer, vpc.Status.SCTPSessionLoadBalancer)
	tpLocal := svc.Spec.InternalTrafficPolicy != nil && *svc.Spec.InternalTrafficPolicy == v1.ServiceInternalTrafficPolicyLocal
	if tpLocal {
		if svc.Spec.SessionAffinity == v1.ServiceAffinityClientIP {
			tcpLB, udpLB, sctpLB = vpc.Status.LocalTCPSessionLoadBalancer, vpc.Status.LocalUDPSessionLoadBalancer, vpc.Status.LocalSCTPSessionLoadBalancer
		} else {
			tcpLB, udpLB, sctpLB = vpc.Status.LocalTCPLoadBalancer, vpc.Status.LocalUDPLoadBalancer, vpc.Status.LocalSCTPLoadBalancer
		}
	} else {
		if svc.Spec.SessionAffinity == v1.ServiceAffinityClientIP {
			tcpLB, udpLB, sctpLB = vpc.Status.TCPSessionLoadBalancer, vpc.Status.UDPSessionLoadBalancer, vpc.Status.SCTPSessionLoadBalancer
		} else {
			tcpLB, udpLB, sctpLB = vpc.Status.TCPLoadBalancer, vpc.Status.UDPLoadBalancer, vpc.Status.SCTPLoadBalancer
		}
	}

	for _, vip := range vips {
		for _, port := range svc.Spec.Ports {
			var lb string
			var lbs set.Set[string]
			switch port.Protocol {
			case v1.ProtocolTCP:
				lb, lbs = tcpLB, tcpLBs.Clone()
			case v1.ProtocolUDP:
				lb, lbs = udpLB, udpLBs.Clone()
			case v1.ProtocolSCTP:
				lb, lbs = sctpLB, sctpLBs.Clone()
			default:
				klog.Errorf("unsupported protocol %q", port.Protocol)
				continue
			}

			var (
				checkIP   string
				externals map[string]string
			)

			if !ignoreHealthCheck {
				if checkIP, err = c.getHealthCheckVip(subnetName, vip); err != nil {
					klog.Error(err)
					return err
				}
				externals = map[string]string{
					util.SwitchLBRuleSubnet: subnetName,
				}
			}

			// chassis template variable name format:
			// LB_VIP_<PROTOCOL>_<IP_HEX>_<PORT>
			// LB_BACKENDS_<PROTOCOL>_<IP_HEX>_<PORT>
			lbVIP := util.JoinHostPort(vip, port.Port)
			vipHex := util.IP2Hex(net.ParseIP(vip))
			vipVar := strings.ToUpper(fmt.Sprintf("LB_VIP_%s_%s_%d", port.Protocol, vipHex, port.Port))
			backendsVar := strings.ToUpper(fmt.Sprintf("LB_BACKENDS_%s_%s_%d", port.Protocol, vipHex, port.Port))
			ipPortMapping, backends := getIPPortMappingBackend(ep, pods, port, vip, checkIP, ignoreHealthCheck)
			if len(backends) != 0 {
				vip := lbVIP
				var lbBackends []string
				if tpLocal {
					vip = "^" + vipVar
					lbBackends = []string{"^" + backendsVar}
					// add/update template variable
					nodeValues := make(map[string]string, len(backends))
					for node, endpoints := range backends {
						nodeValues[node] = strings.Join(endpoints, ",")
					}
					if err = c.OVNNbClient.UpdateChassisTemplateVarVariables(backendsVar, nodeValues); err != nil {
						klog.Errorf("failed to update Chassis_Template_Var variable %s: %v", backendsVar, err)
						return err
					}
				} else {
					lbBackends = slices.Concat(slices.Collect(maps.Values(backends))...)
				}

				klog.Infof("add vip %s with backends %v to LB %s", vip, lbBackends, lb)
				if err = c.OVNNbClient.LoadBalancerAddVip(lb, vip, lbBackends...); err != nil {
					klog.Errorf("failed to add vip %s with backends %s to LB %s: %v", vip, lbBackends, lb, err)
					return err
				}
				if !ignoreHealthCheck && len(ipPortMapping) != 0 {
					klog.Infof("add health check ip port mapping %v to LB %s", ipPortMapping, lb)
					if err = c.OVNNbClient.LoadBalancerAddHealthCheck(lb, vip, ignoreHealthCheck, ipPortMapping, externals); err != nil {
						klog.Errorf("failed to add health check for vip %s with ip port mapping %s to LB %s: %v", vip, ipPortMapping, lb, err)
						return err
					}
				}

				// for performance reason delete lb with no backends
				lbs.Delete(lb)
			}

			for _, lb := range lbs.UnsortedList() {
				klog.V(3).Infof("delete vip %s from LB %s", lbVIP, lb)
				if err = c.OVNNbClient.LoadBalancerDeleteVip(lb, lbVIP, ignoreHealthCheck); err != nil {
					klog.Errorf("failed to delete vip %s from LB %s: %v", lbVIP, lb, err)
					return err
				}
			}

			if !tpLocal || len(backends) == 0 {
				// delete chassis template var after the lb vip is deleted to avoid ovn-controller error parsing actions
				if err = c.OVNNbClient.DeleteChassisTemplateVarVariables(backendsVar); err != nil {
					klog.Errorf("failed to delete Chassis_Template_Var variable %s: %v", backendsVar, err)
					return err
				}
			}
		}
	}

	if svc.Annotations[util.VpcAnnotation] != vpcName {
		patch := util.KVPatch{util.VpcAnnotation: vpcName}
		if err = util.PatchAnnotations(c.config.KubeClient.CoreV1().Services(namespace), svc.Name, patch); err != nil {
			klog.Errorf("failed to patch service %s: %v", key, err)
			return err
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

func getIPPortMappingBackend(endpoints *v1.Endpoints, pods []*v1.Pod, servicePort v1.ServicePort, serviceIP, checkVip string, ignoreHealthCheck bool) (map[string]string, map[string][]string) {
	var (
		ipPortMapping = map[string]string{}
		backends      = make(map[string][]string)
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
			var node string
			if !ignoreHealthCheck && address.TargetRef.Name != "" {
				ipName := fmt.Sprintf("%s.%s", address.TargetRef.Name, endpoints.Namespace)
				ipPortMapping[address.IP] = fmt.Sprintf(util.HealthCheckNamedVipTemplate, ipName, checkVip)
			}
			if address.TargetRef == nil || address.TargetRef.Kind != "Pod" {
				if util.CheckProtocol(address.IP) == protocol {
					if address.NodeName != nil {
						node = *address.NodeName
					}
					backends[node] = append(backends[node], util.JoinHostPort(address.IP, targetPort))
				}
				continue
			}
			var ip string
			for _, pod := range pods {
				if pod.Name == address.TargetRef.Name {
					for _, podIP := range util.PodIPs(*pod) {
						if util.CheckProtocol(podIP) == protocol {
							ip = podIP
							break
						}
					}
					node = pod.Spec.NodeName
					break
				}
			}
			if ip == "" && util.CheckProtocol(address.IP) == protocol {
				ip = address.IP
			}
			if ip != "" {
				backends[node] = append(backends[node], util.JoinHostPort(ip, targetPort))
			}
		}
	}

	return ipPortMapping, backends
}
