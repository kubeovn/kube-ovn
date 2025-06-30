package controller

import (
	"context"
	"fmt"
	"slices"
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

	vpcName, subnetName, err = c.getVpcAndSubnetForEndpoints(endpointSlices, svc)
	if err != nil {
		return err
	}

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
			ipPortMapping, backends = c.getIPPortMappingBackend(endpointSlices, port, lbVip, checkIP, isGenIPPortMapping)
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

// serviceHasSelector returns if a service has selectors
func serviceHasSelector(service *v1.Service) bool {
	return len(service.Spec.Selector) > 0
}

// getVpcAndSubnetForEndpoints returns the name of the VPC/Subnet for EndpointSlices
func (c *Controller) getVpcAndSubnetForEndpoints(endpointSlices []*discoveryv1.EndpointSlice, service *v1.Service) (vpcName, subnetName string, err error) {
	// Let the user self-determine what VPC and subnet to use if they provided annotations
	if service.Annotations != nil {
		if vpc := service.Annotations[util.LogicalRouterAnnotation]; vpc != "" {
			vpcName = vpc
		}
		if subnet := service.Annotations[util.LogicalSwitchAnnotation]; subnet != "" {
			subnetName = subnet
		}

		if vpcName != "" && subnetName != "" {
			return vpcName, subnetName, nil
		}
	}

	// Choose the most optimized and straightforward way to retrieve the name of the VPC and subnet
	if serviceHasSelector(service) {
		// The service has a selector, which means that the EndpointSlices should have targets.
		// We can use those targets instead of looking at every pod in the namespace.
		vpcName, subnetName = c.findVpcAndSubnetWithTargets(endpointSlices)
	} else {
		// The service has no selectors, we must find which pods in the namespace of the service
		// are targeted by the endpoint by only looking at the IPs.
		pods, err := c.podsLister.Pods(service.Namespace).List(labels.Everything())
		if err != nil {
			err := fmt.Errorf("failed to get pods for service %s in namespace %s: %w", service.Name, service.Namespace, err)
			klog.Error(err)
			return "", "", err
		}

		vpcName, subnetName = c.findVpcAndSubnetWithNoTargets(endpointSlices, pods)
	}

	if vpcName == "" { // Default to what's on the service or to the default VPC
		if vpcName = service.Annotations[util.VpcAnnotation]; vpcName == "" {
			vpcName = c.config.ClusterRouter
		}
	}

	if subnetName == "" { // Use the default subnet
		subnetName = util.DefaultSubnet
	}

	return vpcName, subnetName, nil
}

// findVpcAndSubnetWithTargets returns the name of the VPC and Subnet for endpoints with targets
func (c *Controller) findVpcAndSubnetWithTargets(endpointSlices []*discoveryv1.EndpointSlice) (vpcName, subnetName string) {
	for _, slice := range endpointSlices {
		for _, endpoint := range slice.Endpoints {
			if endpoint.TargetRef == nil {
				continue
			}

			namespace, name := endpoint.TargetRef.Namespace, endpoint.TargetRef.Name
			if name == "" || namespace == "" {
				continue
			}

			pod, err := c.podsLister.Pods(namespace).Get(name)
			if err != nil {
				err := fmt.Errorf("couldn't retrieve pod %s/%s: %w", namespace, name, err)
				klog.Error(err)
				continue
			}

			vpc, subnet, err := c.getEndpointVpcAndSubnet(pod, endpoint.Addresses)
			if err != nil {
				err := fmt.Errorf("couldn't retrieve get subnet/vpc for pod %s/%s: %w", namespace, name, err)
				klog.Error(err)
				continue
			}

			if vpcName == "" {
				vpcName = vpc
			}

			if subnetName == "" {
				subnetName = subnet
			}

			if vpcName != "" && subnetName != "" {
				return vpcName, subnetName
			}
		}
	}

	return vpcName, subnetName
}

// findVpcAndSubnetWithNoTargets returns the name of the VPC and Subnet for endpoints with no targets
func (c *Controller) findVpcAndSubnetWithNoTargets(endpointSlices []*discoveryv1.EndpointSlice, pods []*v1.Pod) (vpcName, subnetName string) {
	for _, slice := range endpointSlices {
		for _, endpoint := range slice.Endpoints {
			for _, pod := range pods {
				vpc, subnet, err := c.getEndpointVpcAndSubnet(pod, endpoint.Addresses)
				if err != nil {
					err := fmt.Errorf("couldn't retrieve subnet/vpc for pod %s/%s: %w", pod.Namespace, pod.Name, err)
					klog.Error(err)
					continue
				}

				if vpcName == "" {
					vpcName = vpc
				}

				if subnetName == "" {
					subnetName = subnet
				}

				if vpcName != "" && subnetName != "" {
					return vpcName, subnetName
				}
			}
		}
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

func (c *Controller) getIPPortMappingBackend(endpointSlices []*discoveryv1.EndpointSlice, servicePort v1.ServicePort, serviceIP, checkVip string, isGenIPPortMapping bool) (map[string]string, []string) {
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
				pod, err := c.podsLister.Pods(endpoint.TargetRef.Namespace).Get(endpoint.TargetRef.Name)
				if err != nil {
					err := fmt.Errorf("failed to get pod %s/%s: %w", endpoint.TargetRef.Namespace, endpoint.TargetRef.Name, err)
					klog.Error(err)
					continue
				}

				lspName, err := c.getEndpointTargetLSPName(pod, endpoint.Addresses)
				if err != nil {
					err := fmt.Errorf("couldn't get LSP for the endpoint's target: %w", err)
					klog.Error(err)
					continue
				}

				for _, address := range endpoint.Addresses {
					ipPortMapping[address] = fmt.Sprintf(util.HealthCheckNamedVipTemplate, lspName, checkVip)
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

// getPodProviders returns all the providers available on a pod
func (c *Controller) getPodProviders(pod *v1.Pod) ([]string, error) {
	// Get all the networks to which the pod is attached
	podNetworks, err := c.getPodKubeovnNets(pod)
	if err != nil {
		return nil, fmt.Errorf("failed to get pod networks: %w", err)
	}

	// Retrieve all the providers
	var providers []string
	for _, podNetwork := range podNetworks {
		providers = append(providers, podNetwork.ProviderName)
	}

	return providers, nil
}

// getMatchingProviderForAddress returns the provider linked to a subnet in which a particular address is present
func getMatchingProviderForAddress(pod *v1.Pod, providers []string, address string) string {
	if pod.Annotations == nil {
		return ""
	}

	// Find which provider is linked to this address
	for _, provider := range providers {
		ipsForProvider, exists := pod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, provider)]
		if !exists {
			continue
		}

		ips := strings.Split(ipsForProvider, ",")
		if slices.Contains(ips, address) {
			return provider
		}
	}

	return ""
}

// getEndpointProvider returns the provider linked to the addresses of an endpoint
func (c *Controller) getEndpointProvider(pod *v1.Pod, addresses []string) (string, error) {
	// Retrieve all the providers of the pod
	providers, err := c.getPodProviders(pod)
	if err != nil {
		return "", err
	}

	// Get the first matching provider for any of the address in the endpoint
	var provider string
	for _, address := range addresses {
		if provider = getMatchingProviderForAddress(pod, providers, address); provider != "" {
			return provider, nil
		}
	}

	return "", nil
}

// getEndpointTargetLSPNameFromProvider returns the name of the LSP for a pod targeted by an endpoint.
// A custom provider can be specified if the LSP is within a subnet that doesn't use
// the default "ovn" provider.
func getEndpointTargetLSPNameFromProvider(pod *v1.Pod, provider string) string {
	// If no provider is specified, use the default one
	if provider == "" {
		provider = util.OvnProvider
	}

	target := pod.Name

	// If this pod is a VM launcher pod, we need to retrieve the name of the VM. This is necessary
	// because we do not use the same syntax for the LSP of normal pods and for VM pods
	if vmName, exists := pod.Annotations[fmt.Sprintf(util.VMAnnotationTemplate, provider)]; exists {
		target = vmName
	}

	return ovs.PodNameToPortName(target, pod.Namespace, provider)
}

// getEndpointTargetLSP returns the name of the LSP on which addresses are attached for a specific pod
func (c *Controller) getEndpointTargetLSPName(pod *v1.Pod, addresses []string) (string, error) {
	// Retrieve the provider for those addresses
	provider, err := c.getEndpointProvider(pod, addresses)
	if err != nil {
		return "", err
	}

	return getEndpointTargetLSPNameFromProvider(pod, provider), nil
}

// getSubnetByProvider returns the subnet linked to a provider on a pod
func (c *Controller) getSubnetByProvider(pod *v1.Pod, provider string) (string, error) {
	subnetName, exists := pod.Annotations[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, provider)]
	if !exists {
		return "", fmt.Errorf("couldn't find subnet linked to provider %s", provider)
	}

	return subnetName, nil
}

// getVpcByProvider returns the VPC linked to a provider on a pod
func (c *Controller) getVpcByProvider(pod *v1.Pod, provider string) (string, error) {
	vpcName, exists := pod.Annotations[fmt.Sprintf(util.LogicalRouterAnnotationTemplate, provider)]
	if !exists {
		return "", fmt.Errorf("couldn't find vpc linked to provider %s", provider)
	}

	return vpcName, nil
}

// getEndpointVpcAndSubnet returns the VPC/subnet for a pod and a set of addresses attached to it
func (c *Controller) getEndpointVpcAndSubnet(pod *v1.Pod, addresses []string) (string, string, error) {
	// Retrieve the provider for those addresses
	provider, err := c.getEndpointProvider(pod, addresses)
	if err != nil {
		return "", "", err
	}

	if provider == "" {
		return "", "", nil
	}

	// Retrieve the subnet
	subnet, err := c.getSubnetByProvider(pod, provider)
	if err != nil {
		return "", "", err
	}

	// Retrieve the VPC
	vpc, err := c.getVpcByProvider(pod, provider)
	if err != nil {
		return "", "", err
	}

	return vpc, subnet, nil
}
