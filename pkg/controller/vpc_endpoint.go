package controller

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"slices"
	"sort"
	"strings"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/ovs"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

//go:embed vpc-endpoint-stitcher.sh
var vpcEndpointStitcherScriptData string

const (
	vpcEndpointStitcherContainer = "vpc-endpoint-stitcher"
	vpcEndpointStitcherScriptDir = "/kube-ovn/vpc-endpoint-stitcher"
	vpcEndpointStitcherScript    = vpcEndpointStitcherScriptDir + "/vpc-endpoint-stitcher.sh"
	vpcEndpointStitcherCMName    = "vpc-endpoint-stitcher"
	vpcEndpointStitcherScriptKey = "vpc-endpoint-stitcher.sh"
)

func (c *Controller) enqueueVpcEndpointServiceFromServiceKey(key string) {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return
	}
	c.enqueueVpcEndpointServiceForK8sService(namespace, name)
}

func vpcEndpointServiceDeployName(name string) string {
	return "vpc-eps-" + name
}

func vpcEndpointDeployName(name string) string {
	return "vpc-ep-" + name
}

func vpcEndpointTransitProvider() string {
	return fmt.Sprintf("%s.%s.ovn", util.DefaultVpcEndpointTransitSwitch, metav1.NamespaceSystem)
}

func vpcEndpointServiceAllowed(eps *kubeovnv1.VpcEndpointService, vpc string) bool {
	return len(eps.Spec.AllowedVpcs) == 0 || slices.Contains(eps.Spec.AllowedVpcs, vpc)
}

func (c *Controller) enqueueAddVpcEndpointService(obj any) {
	key := cache.MetaObjectToName(obj.(*kubeovnv1.VpcEndpointService)).String()
	klog.Infof("enqueue add VpcEndpointService %s", key)
	c.addOrUpdateVpcEndpointServiceQueue.Add(key)
}

func (c *Controller) enqueueUpdateVpcEndpointService(oldObj, newObj any) {
	oldEps := oldObj.(*kubeovnv1.VpcEndpointService)
	newEps := newObj.(*kubeovnv1.VpcEndpointService)
	if oldEps.ResourceVersion == newEps.ResourceVersion {
		return
	}
	key := cache.MetaObjectToName(newEps).String()
	klog.Infof("enqueue update VpcEndpointService %s", key)
	c.addOrUpdateVpcEndpointServiceQueue.Add(key)
}

func (c *Controller) enqueueDeleteVpcEndpointService(obj any) {
	eps, ok := obj.(*kubeovnv1.VpcEndpointService)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Warningf("unexpected object type: %T", obj)
			return
		}
		eps, ok = tombstone.Obj.(*kubeovnv1.VpcEndpointService)
		if !ok {
			klog.Warningf("unexpected object type: %T", tombstone.Obj)
			return
		}
	}
	key := cache.MetaObjectToName(eps).String()
	klog.Infof("enqueue delete VpcEndpointService %s", key)
	c.addOrUpdateVpcEndpointServiceQueue.Add(key)
}

func (c *Controller) enqueueAddVpcEndpoint(obj any) {
	key := cache.MetaObjectToName(obj.(*kubeovnv1.VpcEndpoint)).String()
	klog.Infof("enqueue add VpcEndpoint %s", key)
	c.addOrUpdateVpcEndpointQueue.Add(key)
}

func (c *Controller) enqueueUpdateVpcEndpoint(oldObj, newObj any) {
	oldEp := oldObj.(*kubeovnv1.VpcEndpoint)
	newEp := newObj.(*kubeovnv1.VpcEndpoint)
	if oldEp.ResourceVersion == newEp.ResourceVersion {
		return
	}
	key := cache.MetaObjectToName(newEp).String()
	klog.Infof("enqueue update VpcEndpoint %s", key)
	c.addOrUpdateVpcEndpointQueue.Add(key)
}

func (c *Controller) enqueueDeleteVpcEndpoint(obj any) {
	ep, ok := obj.(*kubeovnv1.VpcEndpoint)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Warningf("unexpected object type: %T", obj)
			return
		}
		ep, ok = tombstone.Obj.(*kubeovnv1.VpcEndpoint)
		if !ok {
			klog.Warningf("unexpected object type: %T", tombstone.Obj)
			return
		}
	}
	key := cache.MetaObjectToName(ep).String()
	klog.Infof("enqueue delete VpcEndpoint %s", key)
	c.addOrUpdateVpcEndpointQueue.Add(key)
}

func (c *Controller) enqueueVpcEndpointServiceForK8sService(namespace, name string) {
	if c.config == nil || !c.config.EnableVpcEndpoint || c.vpcEndpointServiceLister == nil {
		return
	}
	selector := labels.Set{
		util.VpcEndpointSvcNsLabel:   namespace,
		util.VpcEndpointSvcNameLabel: name,
	}.AsSelector()
	services, err := c.vpcEndpointServiceLister.List(selector)
	if err != nil {
		klog.Errorf("failed to list VpcEndpointServices for service %s/%s: %v", namespace, name, err)
		return
	}
	for _, eps := range services {
		c.addOrUpdateVpcEndpointServiceQueue.Add(eps.Name)
	}
}

func (c *Controller) enqueueVpcEndpointsForService(serviceName string) {
	if c.vpcEndpointLister == nil {
		return
	}
	// Prefer the service label, but also match Spec.EndpointService so VEPs that
	// are still waiting (labels not applied yet) get woken when the VES becomes ready.
	seen := map[string]struct{}{}
	selector := labels.Set{util.VpcEndpointServiceLabel: serviceName}.AsSelector()
	endpoints, err := c.vpcEndpointLister.List(selector)
	if err != nil {
		klog.Errorf("failed to list VpcEndpoints for service %s: %v", serviceName, err)
	} else {
		for _, ep := range endpoints {
			seen[ep.Name] = struct{}{}
			c.addOrUpdateVpcEndpointQueue.Add(ep.Name)
		}
	}
	all, err := c.vpcEndpointLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list VpcEndpoints while waking consumers of %s: %v", serviceName, err)
		return
	}
	for _, ep := range all {
		if ep.Spec.EndpointService != serviceName {
			continue
		}
		if _, ok := seen[ep.Name]; ok {
			continue
		}
		c.addOrUpdateVpcEndpointQueue.Add(ep.Name)
	}
}

func (c *Controller) enqueueVpcEndpointServiceByName(name string) {
	if name == "" || c.addOrUpdateVpcEndpointServiceQueue == nil {
		return
	}
	c.addOrUpdateVpcEndpointServiceQueue.Add(name)
}

func (c *Controller) initVpcEndpointTransit() error {
	if !c.config.EnableVpcEndpoint || !c.config.EnableLb {
		return nil
	}
	if c.config.VpcEndpointTransitSwitch == "" || c.config.VpcEndpointTransitCIDR == "" {
		return nil
	}
	if err := c.ensureVpcEndpointStitcherConfigMap(); err != nil {
		return err
	}
	return c.ensureVpcEndpointTransitNetwork()
}

func (c *Controller) ensureVpcEndpointTransitNetwork() error {
	// Subnet and VPC names must differ (kube-ovn validation).
	vpcName := c.config.VpcEndpointTransitSwitch + "-vpc"
	subnetName := c.config.VpcEndpointTransitSwitch
	provider := vpcEndpointTransitProvider()
	gw, err := util.GetGwByCidr(c.config.VpcEndpointTransitCIDR)
	if err != nil {
		return fmt.Errorf("get gateway for transit cidr %s: %w", c.config.VpcEndpointTransitCIDR, err)
	}

	if _, err := c.vpcsLister.Get(vpcName); err != nil {
		if !k8serrors.IsNotFound(err) {
			return err
		}
		vpc := &kubeovnv1.Vpc{Name: vpcName}
		if _, err := c.config.KubeOvnClient.KubeovnV1().Vpcs().Create(context.Background(), vpc, metav1.CreateOptions{}); err != nil && !k8serrors.IsAlreadyExists(err) {
			return fmt.Errorf("create transit vpc %s: %w", vpcName, err)
		}
	}

	if _, err := c.subnetsLister.Get(subnetName); err != nil {
		if !k8serrors.IsNotFound(err) {
			return err
		}
		subnet := &kubeovnv1.Subnet{
			Name: subnetName,
			Spec: kubeovnv1.SubnetSpec{
				Vpc:        vpcName,
				CIDRBlock:  c.config.VpcEndpointTransitCIDR,
				Gateway:    gw,
				Protocol:   util.CheckProtocol(c.config.VpcEndpointTransitCIDR),
				Provider:   provider,
				ExcludeIps: strings.Split(gw, ","),
			},
		}
		if _, err := c.config.KubeOvnClient.KubeovnV1().Subnets().Create(context.Background(), subnet, metav1.CreateOptions{}); err != nil && !k8serrors.IsAlreadyExists(err) {
			return fmt.Errorf("create transit subnet %s: %w", subnetName, err)
		}
	} else {
		// Repair a previously created subnet that incorrectly used the same name as its VPC.
		existing, getErr := c.subnetsLister.Get(subnetName)
		if getErr == nil && existing.Spec.Vpc != vpcName {
			updated := existing.DeepCopy()
			updated.Spec.Vpc = vpcName
			updated.Spec.Provider = provider
			if _, err := c.config.KubeOvnClient.KubeovnV1().Subnets().Update(context.Background(), updated, metav1.UpdateOptions{}); err != nil {
				return fmt.Errorf("update transit subnet vpc: %w", err)
			}
		}
	}

	nadConfig := map[string]any{
		"cniVersion":    "0.3.1",
		"name":          subnetName,
		"type":          util.CniTypeName,
		"server_socket": "/run/openvswitch/kube-ovn-daemon.sock",
		"provider":      provider,
	}
	buf, err := json.Marshal(nadConfig)
	if err != nil {
		return err
	}
	nad := &nadv1.NetworkAttachmentDefinition{
		Name:      subnetName,
		Namespace: c.config.PodNamespace,
		Spec:      nadv1.NetworkAttachmentDefinitionSpec{Config: string(buf)},
	}
	_, err = c.config.AttachNetClient.K8sCniCncfIoV1().NetworkAttachmentDefinitions(c.config.PodNamespace).Get(context.Background(), subnetName, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		if _, err = c.config.AttachNetClient.K8sCniCncfIoV1().NetworkAttachmentDefinitions(c.config.PodNamespace).Create(context.Background(), nad, metav1.CreateOptions{}); err != nil {
			if k8serrors.IsAlreadyExists(err) {
				return nil
			}
			return fmt.Errorf("create transit NAD %s/%s: %w", c.config.PodNamespace, subnetName, err)
		}
	} else if err != nil {
		return err
	}
	return nil
}

func (c *Controller) ensureVpcEndpointStitcherConfigMap() error {
	client := c.config.KubeClient.CoreV1().ConfigMaps(c.config.PodNamespace)
	desiredData := map[string]string{vpcEndpointStitcherScriptKey: vpcEndpointStitcherScriptData}
	existing, err := client.Get(context.Background(), vpcEndpointStitcherCMName, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		_, err = client.Create(context.Background(), &corev1.ConfigMap{
			Name:      vpcEndpointStitcherCMName,
			Namespace: c.config.PodNamespace,
			Data:      desiredData,
		}, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	if existing.Data[vpcEndpointStitcherScriptKey] == vpcEndpointStitcherScriptData {
		return nil
	}
	existing = existing.DeepCopy()
	existing.Data = desiredData
	_, err = client.Update(context.Background(), existing, metav1.UpdateOptions{})
	return err
}

func (c *Controller) handleAddOrUpdateVpcEndpointService(key string) error {
	c.vpcEndpointServiceKeyMutex.LockKey(key)
	defer func() { _ = c.vpcEndpointServiceKeyMutex.UnlockKey(key) }()

	cached, err := c.vpcEndpointServiceLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	eps := cached.DeepCopy()
	if !eps.DeletionTimestamp.IsZero() {
		if err := c.cleanupVpcEndpointService(eps); err != nil {
			return err
		}
		if controllerutil.ContainsFinalizer(eps, util.KubeOVNControllerFinalizer) {
			controllerutil.RemoveFinalizer(eps, util.KubeOVNControllerFinalizer)
			if _, err := c.config.KubeOvnClient.KubeovnV1().VpcEndpointServices().Update(context.Background(), eps, metav1.UpdateOptions{}); err != nil {
				return err
			}
		}
		c.enqueueVpcEndpointsForService(eps.Name)
		return nil
	}

	if !controllerutil.ContainsFinalizer(eps, util.KubeOVNControllerFinalizer) {
		controllerutil.AddFinalizer(eps, util.KubeOVNControllerFinalizer)
		if eps, err = c.config.KubeOvnClient.KubeovnV1().VpcEndpointServices().Update(context.Background(), eps, metav1.UpdateOptions{}); err != nil {
			return err
		}
	}

	if err := c.reconcileVpcEndpointService(eps); err != nil {
		klog.Errorf("failed to reconcile VpcEndpointService %s: %v", key, err)
		c.enqueueVpcEndpointsForService(eps.Name)
		_ = c.patchVpcEndpointServiceStatus(eps, false, err.Error())
		return err
	}
	c.enqueueVpcEndpointsForService(eps.Name)
	return nil
}

func (c *Controller) reconcileVpcEndpointService(eps *kubeovnv1.VpcEndpointService) error {
	if eps.Spec.Vpc == "" || eps.Spec.Namespace == "" || eps.Spec.Service == "" {
		return errors.New("vpc, namespace and service must be set")
	}
	if err := validateVpcEndpointServiceImmutability(eps); err != nil {
		return err
	}
	if _, err := c.vpcsLister.Get(eps.Spec.Vpc); err != nil {
		return fmt.Errorf("get provider vpc %s: %w", eps.Spec.Vpc, err)
	}
	if err := c.ensureVpcEndpointTransitNetwork(); err != nil {
		return err
	}
	if err := c.ensureVpcEndpointStitcherConfigMap(); err != nil {
		klog.Errorf("failed to ensure stitcher ConfigMap for VpcEndpointService %s: %v", eps.Name, err)
		return err
	}

	svc, err := c.servicesLister.Services(eps.Spec.Namespace).Get(eps.Spec.Service)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			if cleanupErr := c.deactivateVpcEndpointService(eps); cleanupErr != nil {
				klog.Errorf("failed to deactivate VpcEndpointService %s after missing Service: %v", eps.Name, cleanupErr)
				return cleanupErr
			}
		}
		return fmt.Errorf("get provider service %s/%s: %w", eps.Spec.Namespace, eps.Spec.Service, err)
	}
	if svcVpc := vpcEndpointEffectiveServiceVpc(svc, c.config.ClusterRouter); svcVpc != eps.Spec.Vpc {
		if cleanupErr := c.deactivateVpcEndpointService(eps); cleanupErr != nil {
			klog.Errorf("failed to deactivate VpcEndpointService %s after VPC mismatch: %v", eps.Name, cleanupErr)
			return cleanupErr
		}
		return fmt.Errorf("provider service %s/%s belongs to vpc %s, not %s", eps.Spec.Namespace, eps.Spec.Service, svcVpc, eps.Spec.Vpc)
	}

	providerSubnet, err := c.vpcEndpointProviderSubnet(eps.Spec.Vpc, svc)
	if err != nil {
		return err
	}

	transitVIP := eps.Status.TransitVIP
	deploy, err := c.ensureVpcEndpointServiceStitcher(eps, providerSubnet, transitVIP)
	if err != nil {
		return err
	}
	pod, err := c.waitVpcEndpointStitcherPod(deploy)
	if err != nil {
		return err
	}
	vpcIP, newTransitVIP, err := c.vpcEndpointStitcherIPs(pod, providerSubnet.Spec.Provider, vpcEndpointTransitProvider())
	if err != nil {
		return err
	}
	if vpcIP == "" || newTransitVIP == "" {
		return fmt.Errorf("waiting for provider stitcher IPs on %s/%s", pod.Namespace, pod.Name)
	}
	if util.CheckProtocol(vpcIP) != kubeovnv1.ProtocolIPv4 || util.CheckProtocol(newTransitVIP) != kubeovnv1.ProtocolIPv4 {
		return fmt.Errorf("provider stitcher allocated non-IPv4 addresses vpc=%q transit=%q", vpcIP, newTransitVIP)
	}
	transitVIP = newTransitVIP

	if err := c.initVpcEndpointStitcher(pod); err != nil {
		return err
	}
	mappings, err := c.vpcEndpointProviderMappings(svc)
	if err != nil {
		return err
	}
	if err := c.execVpcEndpointStitcher(pod, append([]string{"provider-sync", transitVIP}, mappings...)...); err != nil {
		return err
	}

	// Best-effort cleanup of legacy OVN LB/ACL datapath from previous design.
	if err := c.cleanupLegacyVpcEndpointServiceOVN(eps); err != nil {
		klog.Errorf("failed to cleanup legacy OVN resources for VpcEndpointService %s: %v", eps.Name, err)
		return err
	}

	eps.Status.TransitVIP = transitVIP
	eps.Status.Ports = vpcEndpointServicePorts(svc)
	if err := c.ensureVpcEndpointServiceLabels(eps); err != nil {
		return err
	}
	if err := c.patchVpcEndpointServiceStatus(eps, true, ""); err != nil {
		return err
	}
	return c.syncVpcEndpointServiceTransitACLs(eps)
}

func (c *Controller) vpcEndpointProviderSubnet(vpcName string, svc *corev1.Service) (*kubeovnv1.Subnet, error) {
	if svc.Annotations != nil {
		if subnet := svc.Annotations[util.LogicalSwitchAnnotation]; subnet != "" {
			return c.subnetsLister.Get(subnet)
		}
	}
	vpc, err := c.vpcsLister.Get(vpcName)
	if err != nil {
		return nil, err
	}
	if vpc.Status.DefaultLogicalSwitch != "" {
		return c.subnetsLister.Get(vpc.Status.DefaultLogicalSwitch)
	}
	for _, name := range vpc.Status.Subnets {
		return c.subnetsLister.Get(name)
	}
	return nil, fmt.Errorf("no subnet found for provider vpc %s", vpcName)
}

func (c *Controller) vpcEndpointProviderMappings(svc *corev1.Service) ([]string, error) {
	endpointSlices, err := c.endpointSlicesLister.EndpointSlices(svc.Namespace).List(labels.Set{
		discoveryv1.LabelServiceName: svc.Name,
	}.AsSelector())
	if err != nil {
		return nil, err
	}
	// getEndpointBackend filters addresses by protocol of serviceIP; use ClusterIP
	// (or a v4 placeholder) so empty string does not drop all backends.
	serviceIP := svc.Spec.ClusterIP
	if serviceIP == "" || serviceIP == corev1.ClusterIPNone {
		serviceIP = "0.0.0.0"
	}
	mappings := make([]string, 0, len(svc.Spec.Ports))
	for _, port := range svc.Spec.Ports {
		backends := c.getEndpointBackend(endpointSlices, port, serviceIP)
		v4Backends := make([]string, 0, len(backends))
		for _, backend := range backends {
			host := backend
			if h, _, err := net.SplitHostPort(strings.Trim(backend, "[]")); err == nil {
				host = h
			}
			if util.CheckProtocol(host) != kubeovnv1.ProtocolIPv4 {
				continue
			}
			v4Backends = append(v4Backends, backend)
		}
		mappings = append(mappings, vpcEndpointProviderPortMappings(port, v4Backends)...)
	}
	if len(mappings) == 0 {
		return nil, errors.New("no ready backends for provider service")
	}
	return mappings, nil
}

// vpcEndpointProviderPortMappings builds provider-sync args for one Service port.
// Each backend becomes proto:port:hostPort (stable-sorted) so the stitcher can
// install iptables nth DNAT rules across replicas. hostPort is ip:port or [ip]:port.
func vpcEndpointProviderPortMappings(port corev1.ServicePort, backends []string) []string {
	if len(backends) == 0 {
		return nil
	}
	sorted := append([]string(nil), backends...)
	sort.Strings(sorted)
	proto := strings.ToLower(string(port.Protocol))
	if proto == "" {
		proto = "tcp"
	}
	mappings := make([]string, 0, len(sorted))
	for _, backend := range sorted {
		mappings = append(mappings, fmt.Sprintf("%s:%d:%s", proto, port.Port, backend))
	}
	return mappings
}

func validateVpcEndpointServiceImmutability(eps *kubeovnv1.VpcEndpointService) error {
	if eps.Labels == nil {
		return nil
	}
	if vpc := eps.Labels[util.VpcEndpointVpcLabel]; vpc != "" && vpc != eps.Spec.Vpc {
		return fmt.Errorf("vpc is immutable after creation (was %s)", vpc)
	}
	if ns := eps.Labels[util.VpcEndpointSvcNsLabel]; ns != "" && ns != eps.Spec.Namespace {
		return fmt.Errorf("namespace is immutable after creation (was %s)", ns)
	}
	if name := eps.Labels[util.VpcEndpointSvcNameLabel]; name != "" && name != eps.Spec.Service {
		return fmt.Errorf("service is immutable after creation (was %s)", name)
	}
	return nil
}

func vpcEndpointEffectiveServiceVpc(svc *corev1.Service, clusterRouter string) string {
	if svc.Annotations != nil {
		if vpc := svc.Annotations[util.VpcAnnotation]; vpc != "" {
			return vpc
		}
		if vpc := svc.Annotations[util.LogicalRouterAnnotation]; vpc != "" {
			return vpc
		}
	}
	return clusterRouter
}

func vpcEndpointServicePorts(svc *corev1.Service) string {
	parts := make([]string, 0, len(svc.Spec.Ports))
	for _, port := range svc.Spec.Ports {
		parts = append(parts, fmt.Sprintf("%s/%d", strings.ToLower(string(port.Protocol)), port.Port))
	}
	return strings.Join(parts, ",")
}

func (c *Controller) ensureVpcEndpointServiceLabels(eps *kubeovnv1.VpcEndpointService) error {
	if eps.Labels == nil {
		eps.Labels = map[string]string{}
	}
	if eps.Labels[util.VpcEndpointVpcLabel] == eps.Spec.Vpc &&
		eps.Labels[util.VpcEndpointSvcNsLabel] == eps.Spec.Namespace &&
		eps.Labels[util.VpcEndpointSvcNameLabel] == eps.Spec.Service {
		return nil
	}
	eps.Labels[util.VpcEndpointVpcLabel] = eps.Spec.Vpc
	eps.Labels[util.VpcEndpointSvcNsLabel] = eps.Spec.Namespace
	eps.Labels[util.VpcEndpointSvcNameLabel] = eps.Spec.Service
	_, err := c.config.KubeOvnClient.KubeovnV1().VpcEndpointServices().Update(context.Background(), eps, metav1.UpdateOptions{})
	return err
}

func (c *Controller) ensureVpcEndpointServiceStitcher(eps *kubeovnv1.VpcEndpointService, providerSubnet *kubeovnv1.Subnet, transitVIP string) (*appsv1.Deployment, error) {
	name := vpcEndpointServiceDeployName(eps.Name)
	labels := map[string]string{
		"app":                         name,
		util.VpcEndpointStitcherLabel: "provider",
		util.VpcEndpointOwnerLabel:    eps.Name,
		util.VpcEndpointVpcLabel:      eps.Spec.Vpc,
		util.VpcEndpointSvcNsLabel:    eps.Spec.Namespace,
		util.VpcEndpointSvcNameLabel:  eps.Spec.Service,
	}
	annotations := map[string]string{
		util.VpcAnnotation:           eps.Spec.Vpc,
		util.LogicalSwitchAnnotation: providerSubnet.Name,
		nadv1.NetworkAttachmentAnnot: fmt.Sprintf("%s/%s", c.config.PodNamespace, c.config.VpcEndpointTransitSwitch),
		fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, vpcEndpointTransitProvider()): c.config.VpcEndpointTransitSwitch,
	}
	if transitVIP != "" {
		annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, vpcEndpointTransitProvider())] = transitVIP
	}

	if err := c.ensureVpcEndpointStitcherConfigMapIn(eps.Spec.Namespace, eps); err != nil {
		return nil, err
	}
	deploy := c.genVpcEndpointStitcherDeployment(name, eps.Spec.Namespace, labels, annotations)
	if err := util.SetOwnerReference(eps, deploy); err != nil {
		return nil, err
	}
	return c.createOrUpdateVpcEndpointDeployment(eps.Spec.Namespace, deploy)
}

func (c *Controller) genVpcEndpointStitcherDeployment(name, namespace string, labels, annotations map[string]string) *appsv1.Deployment {
	privileged := true
	image := vpcNatImage
	if image == "" {
		c.resyncVpcNatConfig()
		image = vpcNatImage
	}
	if image == "" {
		image = c.config.Image
	}
	return &appsv1.Deployment{
		Name:      name,
		Namespace: namespace,
		Labels:    labels,
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To[int32](1),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
			Strategy: appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
			Template: corev1.PodTemplateSpec{
				Labels:      labels,
				Annotations: annotations,
				Spec: corev1.PodSpec{
					TerminationGracePeriodSeconds: ptr.To[int64](0),
					Containers: []corev1.Container{{
						Name:            vpcEndpointStitcherContainer,
						Image:           image,
						Command:         []string{"sleep", "infinity"},
						ImagePullPolicy: corev1.PullIfNotPresent,
						Lifecycle: &corev1.Lifecycle{
							PostStart: &corev1.LifecycleHandler{
								Exec: &corev1.ExecAction{
									Command: []string{"sh", "-c", "sysctl -w net.ipv4.ip_forward=1"},
								},
							},
						},
						SecurityContext: &corev1.SecurityContext{
							Privileged:               &privileged,
							AllowPrivilegeEscalation: &privileged,
						},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "stitcher-script",
							MountPath: vpcEndpointStitcherScriptDir,
						}},
					}},
					Volumes: []corev1.Volume{{
						Name: "stitcher-script",
						ConfigMap: &corev1.ConfigMapVolumeSource{
							Name:        vpcEndpointStitcherCMName,
							DefaultMode: ptr.To[int32](0o755),
						},
					}},
				},
			},
		},
	}
}

func (c *Controller) createOrUpdateVpcEndpointDeployment(namespace string, desired *appsv1.Deployment) (*appsv1.Deployment, error) {
	client := c.config.KubeClient.AppsV1().Deployments(namespace)
	existing, err := client.Get(context.Background(), desired.Name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		return client.Create(context.Background(), desired, metav1.CreateOptions{})
	}
	if err != nil {
		return nil, err
	}
	existing.Labels = desired.Labels
	existing.OwnerReferences = desired.OwnerReferences
	existing.Spec = desired.Spec
	// Recreate strategy cannot retain a rollingUpdate block from a prior RollingUpdate deploy.
	if existing.Spec.Strategy.Type == appsv1.RecreateDeploymentStrategyType {
		existing.Spec.Strategy.RollingUpdate = nil
	}
	return client.Update(context.Background(), existing, metav1.UpdateOptions{})
}

func (c *Controller) waitVpcEndpointStitcherPod(deploy *appsv1.Deployment) (*corev1.Pod, error) {
	selector := labels.Set{"app": deploy.Name}.AsSelector()
	pods, err := c.podsLister.Pods(deploy.Namespace).List(selector)
	if err != nil {
		return nil, err
	}
	for _, pod := range pods {
		if pod.DeletionTimestamp != nil {
			continue
		}
		if pod.Status.Phase == corev1.PodRunning && vpcEndpointPodReady(pod) {
			return pod, nil
		}
	}
	return nil, fmt.Errorf("waiting for stitcher pod of deployment %s", deploy.Name)
}

func vpcEndpointPodReady(pod *corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func (c *Controller) vpcEndpointStitcherIPs(pod *corev1.Pod, vpcProvider, transitProvider string) (vpcIP, transitIP string, err error) {
	if pod.Annotations == nil {
		return "", "", nil
	}
	vpcIP = pod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, vpcProvider)]
	if vpcIP == "" && vpcProvider == util.OvnProvider {
		vpcIP = pod.Annotations[util.IPAddressAnnotation]
	}
	if vpcIP == "" {
		vpcIP = pod.Status.PodIP
	}
	transitIP = pod.Annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, transitProvider)]
	// Multus may only expose network-status; parse as fallback.
	if transitIP == "" {
		if status := pod.Annotations[nadv1.NetworkStatusAnnot]; status != "" {
			transitIP = firstIPFromNetworkStatus(status, c.config.VpcEndpointTransitSwitch)
		}
	}
	return vpcEndpointSelectIPv4(vpcIP), vpcEndpointSelectIPv4(transitIP), nil
}

// vpcEndpointSelectIPv4 returns the first IPv4 address from a kube-ovn IP annotation
// value, which may be dual-stack ("v4,v6") and/or include CIDR masks. Empty if none.
func vpcEndpointSelectIPv4(ipStr string) string {
	if ipStr == "" {
		return ""
	}
	for _, part := range strings.Split(ipStr, ",") {
		ip := strings.Split(strings.TrimSpace(part), "/")[0]
		if util.CheckProtocol(ip) == kubeovnv1.ProtocolIPv4 {
			return ip
		}
	}
	return ""
}

func firstIPFromNetworkStatus(statusJSON, nadName string) string {
	var statuses []struct {
		Name string   `json:"name"`
		IPs  []string `json:"ips"`
	}
	if err := json.Unmarshal([]byte(statusJSON), &statuses); err != nil {
		return ""
	}
	for _, st := range statuses {
		if !strings.Contains(st.Name, nadName) || len(st.IPs) == 0 {
			continue
		}
		if v4 := vpcEndpointSelectIPv4(strings.Join(st.IPs, ",")); v4 != "" {
			return v4
		}
		return st.IPs[0]
	}
	return ""
}

func (c *Controller) initVpcEndpointStitcher(pod *corev1.Pod) error {
	return c.execVpcEndpointStitcher(pod, "init", "eth0,net1")
}

func (c *Controller) execVpcEndpointStitcher(pod *corev1.Pod, args ...string) error {
	cmd := vpcEndpointStitcherScript + " " + strings.Join(args, " ")
	stdOut, errOut, err := util.ExecuteCommandInContainer(c.config.KubeClient, c.config.KubeRestConfig, pod.Namespace, pod.Name, vpcEndpointStitcherContainer, []string{"/bin/bash", "-c", cmd}...)
	if err != nil {
		return fmt.Errorf("exec stitcher on %s/%s: %w, stdout=%s stderr=%s", pod.Namespace, pod.Name, err, stdOut, errOut)
	}
	klog.V(3).Infof("stitcher %s/%s: %s %s", pod.Namespace, pod.Name, stdOut, errOut)
	return nil
}

func (c *Controller) cleanupLegacyVpcEndpointServiceOVN(eps *kubeovnv1.VpcEndpointService) error {
	var errs []error
	for _, protocol := range []string{"tcp", "udp", "sctp"} {
		lbName := vpcEndpointServiceLBName(eps.Name, protocol)
		if err := c.OVNNbClient.LogicalRouterUpdateLoadBalancers(eps.Spec.Vpc, ovsdb.MutateOperationDelete, lbName); err != nil {
			klog.Errorf("failed to detach legacy load balancer %s from vpc %s: %v", lbName, eps.Spec.Vpc, err)
			errs = append(errs, err)
		}
		if err := c.OVNNbClient.DeleteLoadBalancers(func(lb *ovnnb.LoadBalancer) bool { return lb.Name == lbName }); err != nil {
			klog.Errorf("failed to delete legacy load balancer %s: %v", lbName, err)
			errs = append(errs, err)
		}
	}
	if err := c.OVNNbClient.DeleteLogicalSwitchPort("vpc-eps-" + eps.Name); err != nil {
		klog.Errorf("failed to delete legacy transit LSP vpc-eps-%s: %v", eps.Name, err)
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// syncVpcEndpointServiceTransitACLs enforces AllowedVpcs on the shared transit
// switch. Open services (empty allow-list) leave the VIP reachable from any
// transit port; restricted services drop traffic to TransitVIP except from
// authorized consumer stitcher LSPs.
func (c *Controller) syncVpcEndpointServiceTransitACLs(eps *kubeovnv1.VpcEndpointService) error {
	if c.config.VpcEndpointTransitSwitch == "" {
		return nil
	}
	if len(eps.Spec.AllowedVpcs) == 0 || !eps.Status.Ready || eps.Status.TransitVIP == "" {
		return c.OVNNbClient.UpdateVpcEndpointServiceACLs(c.config.VpcEndpointTransitSwitch, eps.Name, "", nil)
	}
	return c.OVNNbClient.UpdateVpcEndpointServiceACLs(
		c.config.VpcEndpointTransitSwitch,
		eps.Name,
		eps.Status.TransitVIP,
		c.vpcEndpointAllowedConsumerLSPs(eps),
	)
}

func (c *Controller) vpcEndpointAllowedConsumerLSPs(eps *kubeovnv1.VpcEndpointService) []string {
	if c.vpcEndpointLister == nil || c.podsLister == nil {
		return nil
	}
	selector := labels.Set{util.VpcEndpointServiceLabel: eps.Name}.AsSelector()
	endpoints, err := c.vpcEndpointLister.List(selector)
	if err != nil {
		klog.Errorf("failed to list VpcEndpoints for transit ACL sync of %s: %v", eps.Name, err)
		return nil
	}
	seen := map[string]struct{}{}
	allowed := make([]string, 0)
	for _, ep := range endpoints {
		if !vpcEndpointServiceAllowed(eps, ep.Spec.Vpc) {
			continue
		}
		ns, err := c.vpcEndpointConsumerNamespace(ep.Spec.Vpc)
		if err != nil {
			continue
		}
		pods, err := c.podsLister.Pods(ns).List(labels.Set{"app": vpcEndpointDeployName(ep.Name)}.AsSelector())
		if err != nil {
			klog.Errorf("failed to list consumer stitcher pods for %s: %v", ep.Name, err)
			continue
		}
		for _, pod := range pods {
			if pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning {
				continue
			}
			lsp := ovs.PodNameToPortName(pod.Name, pod.Namespace, vpcEndpointTransitProvider())
			if _, ok := seen[lsp]; ok {
				continue
			}
			seen[lsp] = struct{}{}
			allowed = append(allowed, lsp)
		}
	}
	sort.Strings(allowed)
	return allowed
}

func (c *Controller) deactivateVpcEndpointService(eps *kubeovnv1.VpcEndpointService) error {
	name := vpcEndpointServiceDeployName(eps.Name)
	err := c.config.KubeClient.AppsV1().Deployments(eps.Spec.Namespace).Delete(context.Background(), name, metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}
	if err := c.config.KubeClient.AppsV1().Deployments(c.config.PodNamespace).Delete(context.Background(), name, metav1.DeleteOptions{}); err != nil && !k8serrors.IsNotFound(err) {
		klog.Warningf("delete provider stitcher deploy in %s: %v", c.config.PodNamespace, err)
	}
	if c.config.VpcEndpointTransitSwitch != "" {
		if err := c.OVNNbClient.UpdateVpcEndpointServiceACLs(c.config.VpcEndpointTransitSwitch, eps.Name, "", nil); err != nil {
			return fmt.Errorf("clear transit ACLs for %s: %w", eps.Name, err)
		}
	}
	if err := c.deleteVpcEndpointStitcherConfigMapIfUnused(eps.Spec.Namespace); err != nil {
		return err
	}
	eps.Status.TransitVIP = ""
	eps.Status.Mac = ""
	eps.Status.Ports = ""
	eps.Status.Ready = false
	return nil
}

func (c *Controller) cleanupVpcEndpointService(eps *kubeovnv1.VpcEndpointService) error {
	return c.deactivateVpcEndpointService(eps)
}

func (c *Controller) patchVpcEndpointServiceStatus(eps *kubeovnv1.VpcEndpointService, ready bool, errMsg string) error {
	eps.Status.Ready = ready && errMsg == ""
	if errMsg != "" {
		klog.Warningf("VpcEndpointService %s not ready: %s", eps.Name, errMsg)
	}
	bytes, err := eps.Status.Bytes()
	if err != nil {
		return err
	}
	_, err = c.config.KubeOvnClient.KubeovnV1().VpcEndpointServices().Patch(context.Background(), eps.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status")
	return err
}

func (c *Controller) handleAddOrUpdateVpcEndpoint(key string) error {
	c.vpcEndpointKeyMutex.LockKey(key)
	defer func() { _ = c.vpcEndpointKeyMutex.UnlockKey(key) }()

	cached, err := c.vpcEndpointLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	ep := cached.DeepCopy()
	if !ep.DeletionTimestamp.IsZero() {
		svcName := ep.Spec.EndpointService
		if err := c.cleanupVpcEndpoint(ep); err != nil {
			return err
		}
		if controllerutil.ContainsFinalizer(ep, util.KubeOVNControllerFinalizer) {
			controllerutil.RemoveFinalizer(ep, util.KubeOVNControllerFinalizer)
			if _, err := c.config.KubeOvnClient.KubeovnV1().VpcEndpoints().Update(context.Background(), ep, metav1.UpdateOptions{}); err != nil {
				return err
			}
		}
		c.enqueueVpcEndpointServiceByName(svcName)
		return nil
	}

	if !controllerutil.ContainsFinalizer(ep, util.KubeOVNControllerFinalizer) {
		controllerutil.AddFinalizer(ep, util.KubeOVNControllerFinalizer)
		if ep, err = c.config.KubeOvnClient.KubeovnV1().VpcEndpoints().Update(context.Background(), ep, metav1.UpdateOptions{}); err != nil {
			return err
		}
	}

	if err := c.reconcileVpcEndpoint(ep); err != nil {
		klog.Errorf("failed to reconcile VpcEndpoint %s: %v", key, err)
		_ = c.patchVpcEndpointStatus(ep, false, err.Error())
		c.enqueueVpcEndpointServiceByName(ep.Spec.EndpointService)
		// Return the reconcile error so the workqueue rate-limits and retries
		// (e.g. while waiting for the provider VES to become ready).
		return err
	}
	c.enqueueVpcEndpointServiceByName(ep.Spec.EndpointService)
	return nil
}

func (c *Controller) reconcileVpcEndpoint(ep *kubeovnv1.VpcEndpoint) error {
	if ep.Spec.Vpc == "" || ep.Spec.Subnet == "" || ep.Spec.EndpointService == "" {
		return errors.New("vpc, subnet and endpointService must be set")
	}
	if err := validateVpcEndpointImmutability(ep); err != nil {
		return err
	}
	// Label early so VES readiness can enqueue this VEP before it itself is Ready.
	if err := c.ensureVpcEndpointLabels(ep); err != nil {
		return err
	}
	subnet, err := c.subnetsLister.Get(ep.Spec.Subnet)
	if err != nil {
		return fmt.Errorf("get subnet %s: %w", ep.Spec.Subnet, err)
	}
	if subnet.Spec.Vpc != ep.Spec.Vpc {
		return fmt.Errorf("subnet %s belongs to vpc %s, not %s", ep.Spec.Subnet, subnet.Spec.Vpc, ep.Spec.Vpc)
	}
	if err := validateVpcEndpointIPv4(ep, subnet); err != nil {
		return err
	}
	eps, svc, err := c.vpcEndpointReadyProvider(ep)
	if err != nil {
		return err
	}
	return c.syncVpcEndpointConsumerStitcher(ep, subnet, eps, svc)
}

// validateVpcEndpointIPv4 rejects IPv6-only consumer subnets and IPv6 Spec.IP.
// Dual-stack subnets are allowed; the stitcher selects the IPv4 address.
func validateVpcEndpointIPv4(ep *kubeovnv1.VpcEndpoint, subnet *kubeovnv1.Subnet) error {
	proto := subnet.Spec.Protocol
	if proto == "" {
		proto = util.CheckProtocol(subnet.Spec.CIDRBlock)
	}
	if proto == kubeovnv1.ProtocolIPv6 {
		return fmt.Errorf("VpcEndpoint is IPv4-only; subnet %s is IPv6", subnet.Name)
	}
	if ep.Spec.IP != "" && util.CheckProtocol(ep.Spec.IP) != kubeovnv1.ProtocolIPv4 {
		return fmt.Errorf("VpcEndpoint.spec.ip must be IPv4 (got %q)", ep.Spec.IP)
	}
	return nil
}

func (c *Controller) vpcEndpointReadyProvider(ep *kubeovnv1.VpcEndpoint) (*kubeovnv1.VpcEndpointService, *corev1.Service, error) {
	eps, err := c.vpcEndpointServiceLister.Get(ep.Spec.EndpointService)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			if cleanupErr := c.cleanupVpcEndpoint(ep); cleanupErr != nil {
				klog.Errorf("failed to cleanup VpcEndpoint %s after missing VES: %v", ep.Name, cleanupErr)
				return nil, nil, cleanupErr
			}
		}
		return nil, nil, fmt.Errorf("get VpcEndpointService %s: %w", ep.Spec.EndpointService, err)
	}
	if !vpcEndpointServiceAllowed(eps, ep.Spec.Vpc) {
		if cleanupErr := c.cleanupVpcEndpoint(ep); cleanupErr != nil {
			klog.Errorf("failed to cleanup VpcEndpoint %s after allowedVpcs deny: %v", ep.Name, cleanupErr)
			return nil, nil, cleanupErr
		}
		return nil, nil, fmt.Errorf("vpc %s is not allowed to consume endpoint service %s", ep.Spec.Vpc, eps.Name)
	}
	if !eps.Status.Ready || eps.Status.TransitVIP == "" {
		if ep.Status.Ready || ep.Status.LocalVIP != "" || ep.Status.SnatIP != "" {
			if cleanupErr := c.cleanupVpcEndpoint(ep); cleanupErr != nil {
				klog.Errorf("failed to cleanup VpcEndpoint %s while waiting for VES: %v", ep.Name, cleanupErr)
				return nil, nil, cleanupErr
			}
		}
		return nil, nil, fmt.Errorf("endpoint service %s is not ready", eps.Name)
	}
	svc, err := c.servicesLister.Services(eps.Spec.Namespace).Get(eps.Spec.Service)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			if cleanupErr := c.cleanupVpcEndpoint(ep); cleanupErr != nil {
				klog.Errorf("failed to cleanup VpcEndpoint %s after missing Service: %v", ep.Name, cleanupErr)
				return nil, nil, cleanupErr
			}
		}
		return nil, nil, fmt.Errorf("get provider service %s/%s: %w", eps.Spec.Namespace, eps.Spec.Service, err)
	}
	return eps, svc, nil
}

func (c *Controller) syncVpcEndpointConsumerStitcher(ep *kubeovnv1.VpcEndpoint, subnet *kubeovnv1.Subnet, eps *kubeovnv1.VpcEndpointService, svc *corev1.Service) error {
	if err := c.ensureVpcEndpointStitcherConfigMap(); err != nil {
		klog.Errorf("failed to ensure stitcher ConfigMap for VpcEndpoint %s: %v", ep.Name, err)
		return err
	}

	localVIP := ep.Spec.IP
	if localVIP == "" {
		localVIP = ep.Status.LocalVIP
	}
	snatIP := ep.Status.SnatIP
	deploy, err := c.ensureVpcEndpointStitcher(ep, subnet, localVIP, snatIP)
	if err != nil {
		return err
	}
	pod, err := c.waitVpcEndpointStitcherPod(deploy)
	if err != nil {
		return err
	}
	vpcProvider := subnet.Spec.Provider
	if vpcProvider == "" {
		vpcProvider = util.OvnProvider
	}
	gotLocal, gotSnat, err := c.vpcEndpointStitcherIPs(pod, vpcProvider, vpcEndpointTransitProvider())
	if err != nil {
		return err
	}
	if gotLocal == "" || gotSnat == "" {
		return fmt.Errorf("waiting for consumer stitcher IPs on %s/%s", pod.Namespace, pod.Name)
	}
	localVIP, snatIP = gotLocal, gotSnat
	if util.CheckProtocol(localVIP) != kubeovnv1.ProtocolIPv4 || util.CheckProtocol(snatIP) != kubeovnv1.ProtocolIPv4 {
		return fmt.Errorf("consumer stitcher allocated non-IPv4 addresses local=%q snat=%q", localVIP, snatIP)
	}

	if err := c.initVpcEndpointStitcher(pod); err != nil {
		return err
	}
	portArgs := make([]string, 0, len(svc.Spec.Ports))
	for _, port := range svc.Spec.Ports {
		portArgs = append(portArgs, fmt.Sprintf("%s:%d", strings.ToLower(string(port.Protocol)), port.Port))
	}
	args := append([]string{"consumer-sync", localVIP, eps.Status.TransitVIP, snatIP}, portArgs...)
	if err := c.execVpcEndpointStitcher(pod, args...); err != nil {
		return err
	}

	if err := c.cleanupLegacyVpcEndpointOVN(ep); err != nil {
		klog.Errorf("failed to cleanup legacy OVN resources for VpcEndpoint %s: %v", ep.Name, err)
		return err
	}

	ep.Status.LocalVIP = localVIP
	ep.Status.TransitVIP = eps.Status.TransitVIP
	ep.Status.SnatIP = snatIP
	if err := c.ensureVpcEndpointLabels(ep); err != nil {
		return err
	}
	return c.patchVpcEndpointStatus(ep, true, "")
}

func validateVpcEndpointImmutability(ep *kubeovnv1.VpcEndpoint) error {
	if ep.Labels == nil {
		return nil
	}
	if vpc := ep.Labels[util.VpcEndpointVpcLabel]; vpc != "" && vpc != ep.Spec.Vpc {
		return fmt.Errorf("vpc is immutable after creation (was %s)", vpc)
	}
	if subnet := ep.Labels[util.VpcEndpointSubnetLabel]; subnet != "" && subnet != ep.Spec.Subnet {
		return fmt.Errorf("subnet is immutable after creation (was %s)", subnet)
	}
	if svc := ep.Labels[util.VpcEndpointServiceLabel]; svc != "" && svc != ep.Spec.EndpointService {
		return fmt.Errorf("endpointService is immutable after creation (was %s)", svc)
	}
	if ip := ep.Labels[util.VpcEndpointIPLabel]; ip != "" && ip != ep.Spec.IP {
		return fmt.Errorf("ip is immutable after creation (was %s)", ip)
	}
	return nil
}

func (c *Controller) ensureVpcEndpointLabels(ep *kubeovnv1.VpcEndpoint) error {
	if ep.Labels == nil {
		ep.Labels = map[string]string{}
	}
	desiredIP := ep.Spec.IP
	if ep.Labels[util.VpcEndpointServiceLabel] == ep.Spec.EndpointService &&
		ep.Labels[util.VpcEndpointVpcLabel] == ep.Spec.Vpc &&
		ep.Labels[util.VpcEndpointSubnetLabel] == ep.Spec.Subnet &&
		ep.Labels[util.VpcEndpointIPLabel] == desiredIP {
		return nil
	}
	ep.Labels[util.VpcEndpointServiceLabel] = ep.Spec.EndpointService
	ep.Labels[util.VpcEndpointVpcLabel] = ep.Spec.Vpc
	ep.Labels[util.VpcEndpointSubnetLabel] = ep.Spec.Subnet
	if desiredIP == "" {
		delete(ep.Labels, util.VpcEndpointIPLabel)
	} else {
		ep.Labels[util.VpcEndpointIPLabel] = desiredIP
	}
	_, err := c.config.KubeOvnClient.KubeovnV1().VpcEndpoints().Update(context.Background(), ep, metav1.UpdateOptions{})
	return err
}

func (c *Controller) ensureVpcEndpointStitcher(ep *kubeovnv1.VpcEndpoint, subnet *kubeovnv1.Subnet, localVIP, snatIP string) (*appsv1.Deployment, error) {
	name := vpcEndpointDeployName(ep.Name)
	ns, err := c.vpcEndpointConsumerNamespace(ep.Spec.Vpc)
	if err != nil {
		return nil, err
	}
	labels := map[string]string{
		"app":                         name,
		util.VpcEndpointStitcherLabel: "consumer",
		util.VpcEndpointOwnerLabel:    ep.Name,
		util.VpcEndpointServiceLabel:  ep.Spec.EndpointService,
		util.VpcEndpointVpcLabel:      ep.Spec.Vpc,
	}
	annotations := map[string]string{
		util.VpcAnnotation:           ep.Spec.Vpc,
		util.LogicalSwitchAnnotation: subnet.Name,
		nadv1.NetworkAttachmentAnnot: fmt.Sprintf("%s/%s", c.config.PodNamespace, c.config.VpcEndpointTransitSwitch),
		fmt.Sprintf(util.LogicalSwitchAnnotationTemplate, vpcEndpointTransitProvider()): c.config.VpcEndpointTransitSwitch,
	}
	if localVIP != "" {
		annotations[util.IPAddressAnnotation] = localVIP
	}
	if snatIP != "" {
		annotations[fmt.Sprintf(util.IPAddressAnnotationTemplate, vpcEndpointTransitProvider())] = snatIP
	}
	if err := c.ensureVpcEndpointStitcherConfigMapIn(ns, ep); err != nil {
		return nil, err
	}
	deploy := c.genVpcEndpointStitcherDeployment(name, ns, labels, annotations)
	if err := util.SetOwnerReference(ep, deploy); err != nil {
		return nil, err
	}
	return c.createOrUpdateVpcEndpointDeployment(ns, deploy)
}

func (c *Controller) vpcEndpointConsumerNamespace(vpcName string) (string, error) {
	vpc, err := c.vpcsLister.Get(vpcName)
	if err != nil {
		return "", err
	}
	if len(vpc.Spec.Namespaces) > 0 {
		return vpc.Spec.Namespaces[0], nil
	}
	return "", fmt.Errorf("vpc %s has no namespaces for stitcher deployment", vpcName)
}

func (c *Controller) ensureVpcEndpointStitcherConfigMapIn(namespace string, _ metav1.Object) error {
	if namespace == c.config.PodNamespace {
		return c.ensureVpcEndpointStitcherConfigMap()
	}
	if err := c.ensureVpcEndpointStitcherConfigMap(); err != nil {
		return err
	}
	src, err := c.config.KubeClient.CoreV1().ConfigMaps(c.config.PodNamespace).Get(context.Background(), vpcEndpointStitcherCMName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	client := c.config.KubeClient.CoreV1().ConfigMaps(namespace)
	existing, err := client.Get(context.Background(), vpcEndpointStitcherCMName, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		cm := &corev1.ConfigMap{
			Name:      vpcEndpointStitcherCMName,
			Namespace: namespace,
			Data:      src.Data,
		}
		// Do not set ownerRef to the cluster-scoped VES/VEP: Kubernetes GC does
		// not honor namespaced dependents owned by cluster-scoped owners.
		_, err = client.Create(context.Background(), cm, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	if existing.Data[vpcEndpointStitcherScriptKey] == src.Data[vpcEndpointStitcherScriptKey] {
		return nil
	}
	existing = existing.DeepCopy()
	existing.Data = src.Data
	_, err = client.Update(context.Background(), existing, metav1.UpdateOptions{})
	return err
}

// deleteVpcEndpointStitcherConfigMapIfUnused removes a tenant-namespace stitcher
// ConfigMap once no stitcher Deployments remain in that namespace. The
// controller-namespace ConfigMap is kept as the shared source of truth.
func (c *Controller) deleteVpcEndpointStitcherConfigMapIfUnused(namespace string) error {
	if namespace == "" || namespace == c.config.PodNamespace {
		return nil
	}
	if c.deploymentsLister != nil {
		deps, err := c.deploymentsLister.Deployments(namespace).List(labels.Everything())
		if err != nil {
			return err
		}
		for _, dep := range deps {
			if dep.DeletionTimestamp != nil {
				continue
			}
			role := dep.Labels[util.VpcEndpointStitcherLabel]
			if role == "provider" || role == "consumer" {
				return nil
			}
		}
	}
	err := c.config.KubeClient.CoreV1().ConfigMaps(namespace).Delete(context.Background(), vpcEndpointStitcherCMName, metav1.DeleteOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}
	return nil
}

func (c *Controller) cleanupLegacyVpcEndpointOVN(ep *kubeovnv1.VpcEndpoint) error {
	var errs []error
	for _, protocol := range []string{"tcp", "udp", "sctp"} {
		lbName := vpcEndpointLBName(ep.Name, protocol)
		if err := c.OVNNbClient.LogicalSwitchUpdateLoadBalancers(ep.Spec.Subnet, ovsdb.MutateOperationDelete, lbName); err != nil {
			klog.Errorf("failed to detach legacy load balancer %s from subnet %s: %v", lbName, ep.Spec.Subnet, err)
			errs = append(errs, err)
		}
		if err := c.OVNNbClient.LogicalRouterUpdateLoadBalancers(ep.Spec.Vpc, ovsdb.MutateOperationDelete, lbName); err != nil {
			klog.Errorf("failed to detach legacy load balancer %s from vpc %s: %v", lbName, ep.Spec.Vpc, err)
			errs = append(errs, err)
		}
		if err := c.OVNNbClient.DeleteLoadBalancers(func(lb *ovnnb.LoadBalancer) bool { return lb.Name == lbName }); err != nil {
			klog.Errorf("failed to delete legacy load balancer %s: %v", lbName, err)
			errs = append(errs, err)
		}
	}
	if err := c.config.KubeOvnClient.KubeovnV1().Vips().Delete(context.Background(), vpcEndpointVipCRName(ep.Name), metav1.DeleteOptions{}); err != nil && !k8serrors.IsNotFound(err) {
		klog.Errorf("failed to delete legacy Vip %s: %v", vpcEndpointVipCRName(ep.Name), err)
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (c *Controller) cleanupVpcEndpoint(ep *kubeovnv1.VpcEndpoint) error {
	name := vpcEndpointDeployName(ep.Name)
	var consumerNS string
	if ns, err := c.vpcEndpointConsumerNamespace(ep.Spec.Vpc); err == nil {
		consumerNS = ns
		if err := c.config.KubeClient.AppsV1().Deployments(ns).Delete(context.Background(), name, metav1.DeleteOptions{}); err != nil && !k8serrors.IsNotFound(err) {
			return err
		}
	}
	if err := c.config.KubeClient.AppsV1().Deployments(c.config.PodNamespace).Delete(context.Background(), name, metav1.DeleteOptions{}); err != nil && !k8serrors.IsNotFound(err) {
		klog.Warningf("delete consumer stitcher deploy in %s: %v", c.config.PodNamespace, err)
	}
	if err := c.config.KubeOvnClient.KubeovnV1().Vips().Delete(context.Background(), "vpc-ep-"+ep.Name, metav1.DeleteOptions{}); err != nil && !k8serrors.IsNotFound(err) {
		return err
	}
	if consumerNS != "" {
		if err := c.deleteVpcEndpointStitcherConfigMapIfUnused(consumerNS); err != nil {
			return err
		}
	}
	ep.Status.LocalVIP = ""
	ep.Status.TransitVIP = ""
	ep.Status.SnatIP = ""
	ep.Status.Ready = false
	return nil
}

func (c *Controller) patchVpcEndpointStatus(ep *kubeovnv1.VpcEndpoint, ready bool, errMsg string) error {
	ep.Status.Ready = ready && errMsg == ""
	if errMsg != "" {
		klog.Warningf("VpcEndpoint %s not ready: %s", ep.Name, errMsg)
	}
	bytes, err := ep.Status.Bytes()
	if err != nil {
		return err
	}
	_, err = c.config.KubeOvnClient.KubeovnV1().VpcEndpoints().Patch(context.Background(), ep.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status")
	return err
}

// Keep compile-friendly stubs referenced by older tests / GC until fully migrated.
func vpcEndpointServiceLBName(name, protocol string) string {
	return fmt.Sprintf("vpc-eps-%s-%s", name, strings.ToLower(protocol))
}

func vpcEndpointLBName(name, protocol string) string {
	return fmt.Sprintf("vpc-ep-%s-%s", name, strings.ToLower(protocol))
}

func vpcEndpointVipCRName(name string) string {
	return "vpc-ep-" + name
}

func vpcEndpointSnatMatch(transitVIP string) string {
	if util.CheckProtocol(transitVIP) == kubeovnv1.ProtocolIPv6 {
		return fmt.Sprintf("ip6.dst == %s", transitVIP)
	}
	return fmt.Sprintf("ip4.dst == %s", transitVIP)
}

func vpcEndpointPreferIP(v4, v6 string) string {
	if v4 != "" {
		return v4
	}
	return v6
}

func vpcEndpointServiceIPAMName(name string) string {
	return "vpc-eps/" + name
}

func vpcEndpointSnatIPAMName(vpc string) string {
	return "vpc-ep-snat/" + vpc
}
