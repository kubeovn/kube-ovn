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

type IPPortMapping map[string]string

// getServiceForEndpointSlice returns the service linked to an EndpointSlice
func getServiceForEndpointSlice(endpointSlice *discoveryv1.EndpointSlice) string {
	if endpointSlice != nil && endpointSlice.Labels != nil {
		return endpointSlice.Labels[discoveryv1.LabelServiceName]
	}

	return ""
}

func findServiceKey(endpointSlice *discoveryv1.EndpointSlice) string {
	service := getServiceForEndpointSlice(endpointSlice)
	if service == "" {
		return ""
	}

	return endpointSlice.Namespace + "/" + service
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

	endpointSlices, err := c.endpointSlicesLister.EndpointSlices(namespace).List(labels.Set{discoveryv1.LabelServiceName: name}.AsSelector())
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

		// Health checks can only run against IPv4 endpoints and if the service doesn't specify they must be disabled
		if util.CheckProtocol(vip) == kubeovnv1.ProtocolIPv4 && !serviceHealthChecksDisabled(svc) {
			ignoreHealthCheck = false
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

	// If Kube-OVN is running in secondary CNI mode, the endpoint IPs should be derived from the network attachment definitions
	// This overwrite can be removed if endpoint construction accounts for network attachment IP address
	// TODO: Identify how endpoints are constructed, by default, endpoints has IP address of eth0 interface
	if c.config.EnableNonPrimaryCNI && serviceHasSelector(svc) {
		var pods []*v1.Pod
		if pods, err = c.podsLister.Pods(namespace).List(labels.Set(svc.Spec.Selector).AsSelector()); err != nil {
			klog.Errorf("failed to get pods for service %s in namespace %s: %v", name, namespace, err)
			return err
		}
		err = c.replaceEndpointAddressesWithSecondaryIPs(endpointSlices, pods)
		if err != nil {
			klog.Errorf("failed to update endpointSlice: %v", err)
			return err
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

			backends = c.getEndpointBackend(endpointSlices, port, lbVip)

			if !ignoreHealthCheck || isPreferLocalBackend {
				ipPortMapping, err = c.getIPPortMapping(endpointSlices, svc, checkIP)
				if err != nil {
					err := fmt.Errorf("couldn't get ip port mapping for svc %s/%s: %w", svc.Namespace, svc.Name, err)
					return err
				}
			}

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

				if !ignoreHealthCheck {
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

// Update the endpoint IP address with the secondary IP address of the pod using the network attachment definition annotation
// This is a temporary fix to allow consumers to use the secondary IP address of the pod
// TODO: Remove this function and update the endpoint construction to use the secondary IP address of the pod
func (c *Controller) replaceEndpointAddressesWithSecondaryIPs(endpointSlices []*discoveryv1.EndpointSlice, pods []*v1.Pod) error {
	// Track which pods have been processed
	processedPods := make(map[string]bool)
	// Store pod information in a map
	podMap := make(map[string]*v1.Pod, len(pods))
	for i := range pods {
		podMap[pods[i].Name] = pods[i]
	}
	// Pre-compute secondary IPs for all pods to avoid repeated annotation lookups
	secondaryIPs := make(map[string]string, len(pods))
	for _, pod := range pods {
		providers, err := c.getPodProviders(pod)
		if err != nil {
			return err
		}
		if len(providers) > 0 {
			ipAddress := pod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, providers[0])]
			if ipAddress != "" {
				secondaryIPs[pod.Name] = ipAddress
			}
		}
	}
	// Process each endpoint slice
	for i, endpoint := range endpointSlices {
		var copiedSlice *discoveryv1.EndpointSlice
		needsUpdate := false
		// Check if any endpoints need updating first
		for j, ep := range endpoint.Endpoints {
			if ep.TargetRef != nil && ep.TargetRef.Kind == "Pod" {
				podName := ep.TargetRef.Name
				// Skip if already processed this pod
				// Include slice index to handle pod in multiple slices
				podKey := fmt.Sprintf("%s/%d", podName, i)
				if processedPods[podKey] {
					continue
				}
				if secondaryIP, hasSecondaryIP := secondaryIPs[podName]; hasSecondaryIP {
					if pod, ok := podMap[podName]; ok {
						// Check if any address needs replacement
						for k, address := range ep.Addresses {
							// Only replace if it's the primary IP
							if address == pod.Status.PodIP {
								// Lazy deep copy
								if !needsUpdate {
									copiedSlice = endpoint.DeepCopy()
									needsUpdate = true
								}
								klog.Infof("updating pod %s/%s ip address %s to %s",
									pod.Namespace, pod.Name, pod.Status.PodIP, secondaryIP)
								copiedSlice.Endpoints[j].Addresses[k] = secondaryIP
								processedPods[podKey] = true
								// Only one primary IP per endpoint
								break
							} else if address == secondaryIP {
								// Already has secondary IP, mark as processed
								processedPods[podKey] = true
								break
							}
						}
					}
				}
			}
		}
		// Replace the slice if we made changes
		if needsUpdate {
			endpointSlices[i] = copiedSlice
		}
	}

	return nil
}

// enqueueStaticEndpointUpdateInNamespace enqueues updates for every statically generated EndpointSlice in a namespace.
// Statically generated EndpointSlices are not generated by the selectors of their parent service.
func (c *Controller) enqueueStaticEndpointUpdateInNamespace(namespace string) {
	// Find all the statically generated EndpointSlices in the namespace
	endpointSlices, err := c.findStaticEndpointSlicesInNamespace(namespace)
	if err != nil {
		err := fmt.Errorf("couldn't find static endpointslices in namespace %s: %w", namespace, err)
		klog.Error(err)
	}

	// Enqueue updates for all the EndpointSlices
	for _, slice := range endpointSlices {
		c.enqueueAddEndpointSlice(slice)
	}
}

// serviceHealthChecksDisabled returns whether health checks must be omitted for a particular service
func serviceHealthChecksDisabled(service *v1.Service) bool {
	// Service must not have disabled health checks
	if service.Annotations != nil && service.Annotations[util.ServiceHealthCheck] == "false" {
		return true
	}

	// If nothing is specified, checks are enabled by default
	return false
}

// findStaticEndpointSlicesInNamespace finds all the EndpointSlices in a namespace that are statically generated.
// Statically generated EndpointSlices are not generated by the selectors of their parent service.
func (c *Controller) findStaticEndpointSlicesInNamespace(namespace string) ([]*discoveryv1.EndpointSlice, error) {
	// Retrieve all the services in the namespace
	services, err := c.servicesLister.Services(namespace).List(labels.Everything())
	if err != nil {
		err := fmt.Errorf("couldn't list services in namespace %s: %w", namespace, err)
		klog.Error(err)
		return nil, err
	}

	// Only handle services that have static endpoints provided, and not selectors
	var filteredServices []*v1.Service
	for _, service := range services {
		if serviceHasSelector(service) {
			continue
		}

		filteredServices = append(filteredServices, service)
	}

	// Find the EndpointSlices linked to those services
	endpointSlices, err := c.findEndpointSlicesForServices(namespace, filteredServices)
	if err != nil {
		return nil, err
	}

	return endpointSlices, nil
}

// findEndpointSlicesForServices returns all the EndpointSlices that are linked to services in the same namespace.
// Parameter "namespace" is the namespace in which all the services are located.
// Parameter "services" is a list of all the services for which we want to find the EndpointSlices.
func (c *Controller) findEndpointSlicesForServices(namespace string, services []*v1.Service) ([]*discoveryv1.EndpointSlice, error) {
	var endpointSlices []*discoveryv1.EndpointSlice

	// Retrieve all the endpointSlices in the namespace of the services
	eps, err := c.endpointSlicesLister.EndpointSlices(namespace).List(labels.Everything())
	if err != nil {
		err := fmt.Errorf("couldn't list endpointslices in namespace %s: %w", namespace, err)
		klog.Error(err)
		return nil, err
	}

	// Find the EndpointSlices part of each service
	for _, service := range services {
		for _, endpointSlice := range eps {
			if getServiceForEndpointSlice(endpointSlice) == service.Name {
				endpointSlices = append(endpointSlices, endpointSlice)
			}
		}
	}

	return endpointSlices, nil
}

// serviceHasSelector returns if a service has selectors
func serviceHasSelector(service *v1.Service) bool {
	return len(service.Spec.Selector) > 0
}

// getCustomServiceVpcAndSubnet returns the custom VPC/Subnet defined on a service
func getCustomServiceVpcAndSubnet(service *v1.Service) (vpcName, subnetName string) {
	if service.Annotations != nil {
		vpcName = service.Annotations[util.LogicalRouterAnnotation]
		subnetName = service.Annotations[util.LogicalSwitchAnnotation]
	}

	return vpcName, subnetName
}

// getDefaultVpcAndSubnet returns the default VPC/Subnet to apply to a LoadBalancer if nothing was found
// during automatic discovery. If both parameters are non-empty, they are returned as is.
func (c *Controller) getDefaultVpcAndSubnet(service *v1.Service, vpcName, subnetName string) (string, string) {
	// Default to what's on the service or to the default VPC
	if vpcName == "" {
		if vpcName = service.Annotations[util.VpcAnnotation]; vpcName == "" {
			vpcName = c.config.ClusterRouter
		}
	}

	// Use the default subnet if it wasn't found
	if subnetName == "" {
		subnetName = util.DefaultSubnet
	}

	return vpcName, subnetName
}

// getVpcAndSubnetForEndpoints returns the name of the VPC/Subnet for EndpointSlices
func (c *Controller) getVpcAndSubnetForEndpoints(endpointSlices []*discoveryv1.EndpointSlice, service *v1.Service) (vpcName, subnetName string, err error) {
	// Let the user self-determine what VPC and subnet to use if they provided annotations on the service
	// Both the VPC and Subnet must be provided
	vpcName, subnetName = getCustomServiceVpcAndSubnet(service)
	if vpcName != "" && subnetName != "" {
		return vpcName, subnetName, nil
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

	vpcName, subnetName = c.getDefaultVpcAndSubnet(service, vpcName, subnetName)
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
		// TODO: WATCH VIP
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

// getEndpointBackend returns the LB backend for a service
func (c *Controller) getEndpointBackend(endpointSlices []*discoveryv1.EndpointSlice, servicePort v1.ServicePort, serviceIP string) (backends []string) {
	protocol := util.CheckProtocol(serviceIP)

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

	return backends
}

// endpointReady returns whether an endpoint can receive traffic
func endpointReady(endpoint discoveryv1.Endpoint) bool {
	return endpoint.Conditions.Ready == nil || *endpoint.Conditions.Ready
}

// addIPPortMappingEntry adds a new entry to an IPPortMapping for a given target, the addresses on that target and the
// VIP used to run the health checks
func (c *Controller) addIPPortMappingEntry(pod *v1.Pod, addresses []string, checkVip string, mapping IPPortMapping) error {
	// Abort if the pod is getting deleted
	if !pod.DeletionTimestamp.IsZero() {
		return nil
	}

	// Compute the name of the LSP for that endpoint target
	lspName, err := c.getEndpointTargetLSPName(pod, addresses)
	if err != nil {
		return fmt.Errorf("couldn't get LSP for the endpoint's target: %w", err)
	}

	for _, address := range addresses {
		key := address
		if util.CheckProtocol(address) == kubeovnv1.ProtocolIPv6 {
			key = fmt.Sprintf("[%s]", address)
		}
		mapping[key] = fmt.Sprintf(util.HealthCheckNamedVipTemplate, lspName, checkVip)
	}

	return nil
}

// getIPPortMapping returns the mapping between each endpoint, LSP and health check VIP
func (c *Controller) getIPPortMapping(endpointSlices []*discoveryv1.EndpointSlice, service *v1.Service, checkVip string) (IPPortMapping, error) {
	// Choose the most optimized and straightforward way to compute the IPPortMapping
	if serviceHasSelector(service) {
		// The service has a selector, which means that the EndpointSlices should have targets.
		// We can use those targets instead of looking at every pod in the namespace.
		return c.getIPPortMappingWithTargets(endpointSlices, checkVip), nil
	}

	// The service has no selectors, we must find which pods in the namespace of the service
	// are targeted by the endpoint by only looking at the IPs.
	pods, err := c.podsLister.Pods(service.Namespace).List(labels.Everything())
	if err != nil {
		err := fmt.Errorf("failed to get pods for service %s in namespace %s: %w", service.Name, service.Namespace, err)
		klog.Error(err)
		return nil, err
	}

	return c.getIPPortMappingWithNoTargets(endpointSlices, pods, checkVip), nil
}

// getIPPortMappingWithTargets returns the IPPortMapping for endpoints with targets
func (c *Controller) getIPPortMappingWithTargets(endpointSlices []*discoveryv1.EndpointSlice, checkVip string) IPPortMapping {
	mapping := make(IPPortMapping)

	for _, slice := range endpointSlices {
		for _, endpoint := range slice.Endpoints {
			if endpoint.TargetRef == nil {
				continue
			}

			namespace, name := endpoint.TargetRef.Namespace, endpoint.TargetRef.Name
			if name == "" || namespace == "" {
				continue
			}

			// Retrieve the pod for that endpoint target
			pod, err := c.podsLister.Pods(namespace).Get(name)
			if err != nil {
				err := fmt.Errorf("couldn't retrieve pod %s/%s: %w", namespace, name, err)
				klog.Error(err)
				continue
			}

			// Compute the IPPortMapping for that endpoint target
			if err := c.addIPPortMappingEntry(pod, endpoint.Addresses, checkVip, mapping); err != nil {
				err := fmt.Errorf("couldn't compute ip port mapping for pod %s/%s: %w", namespace, name, err)
				klog.Error(err)
				continue
			}
		}
	}

	return mapping
}

// getIPPortMappingWithNoTargets returns the IPPortMapping for endpoints with no targets
func (c *Controller) getIPPortMappingWithNoTargets(endpointSlices []*discoveryv1.EndpointSlice, pods []*v1.Pod, checkVip string) IPPortMapping {
	mapping := make(IPPortMapping)

	for _, slice := range endpointSlices {
		for _, endpoint := range slice.Endpoints {
			for _, pod := range pods {
				// Try to find a matching provider for the addresses
				provider, err := c.getEndpointProvider(pod, endpoint.Addresses)
				if err != nil {
					err := fmt.Errorf("couldn't get provider for pod %s/%s: %w", pod.Namespace, pod.Name, err)
					klog.Error(err)
					continue
				}

				// If the pod has a provider that matches that set of addresses, it is an endpoint target.
				// Otherwise, it isn't targeted by the EndpointSlice and can be dismissed.
				if provider == "" {
					continue
				}

				// Compute the IPPortMapping for that endpoint target
				if err := c.addIPPortMappingEntry(pod, endpoint.Addresses, checkVip, mapping); err != nil {
					err := fmt.Errorf("couldn't compute ip port mapping for pod %s/%s: %w", pod.Namespace, pod.Name, err)
					klog.Error(err)
					continue
				}
			}
		}
	}

	return mapping
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
func getSubnetByProvider(pod *v1.Pod, provider string) (string, error) {
	subnetName, exists := pod.Annotations[fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, provider)]
	if !exists {
		return "", fmt.Errorf("couldn't find subnet linked to provider %s", provider)
	}

	return subnetName, nil
}

// getVpcByProvider returns the VPC linked to a provider on a pod
func getVpcByProvider(pod *v1.Pod, provider string) (string, error) {
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
	subnet, err := getSubnetByProvider(pod, provider)
	if err != nil {
		return "", "", err
	}

	// Retrieve the VPC
	vpc, err := getVpcByProvider(pod, provider)
	if err != nil {
		return "", "", err
	}

	return vpc, subnet, nil
}
