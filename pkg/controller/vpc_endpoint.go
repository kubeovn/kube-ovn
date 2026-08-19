package controller

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"
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
	kubeovnscheme "github.com/kubeovn/kube-ovn/pkg/client/clientset/versioned/scheme"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (c *Controller) enqueueVpcEndpointServiceFromServiceKey(key string) {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return
	}
	c.enqueueVpcEndpointServiceForK8sService(namespace, name)
}

func vpcEndpointTransitLrpName(vpc, transitSwitch string) string {
	return fmt.Sprintf("%s-%s", vpc, transitSwitch)
}

func vpcEndpointTransitLspName(vpc, transitSwitch string) string {
	return fmt.Sprintf("%s-%s", transitSwitch, vpc)
}

func vpcEndpointServiceLBName(name, protocol string) string {
	return fmt.Sprintf("vpc-eps-%s-%s", name, strings.ToLower(protocol))
}

func vpcEndpointLBName(name, protocol string) string {
	return fmt.Sprintf("vpc-ep-%s-%s", name, strings.ToLower(protocol))
}

func vpcEndpointVipCRName(name string) string {
	return "vpc-ep-" + name
}

func vpcEndpointServiceLSPName(name string) string {
	return "vpc-eps-" + name
}

func vpcEndpointServiceIPAMName(name string) string {
	return "vpc-eps/" + name
}

func vpcEndpointSnatIPAMName(vpc string) string {
	return "vpc-ep-snat/" + vpc
}

func vpcEndpointSnatMatch(transitVIP string) string {
	if util.CheckProtocol(transitVIP) == kubeovnv1.ProtocolIPv6 {
		return fmt.Sprintf("ip6.dst == %s", transitVIP)
	}
	return fmt.Sprintf("ip4.dst == %s", transitVIP)
}

func vpcEndpointSnatLogicalIP(transitVIP string) string {
	if util.CheckProtocol(transitVIP) == kubeovnv1.ProtocolIPv6 {
		return "::/0"
	}
	return "0.0.0.0/0"
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
	if c.vpcEndpointServiceLister == nil {
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
	selector := labels.Set{util.VpcEndpointServiceLabel: serviceName}.AsSelector()
	endpoints, err := c.vpcEndpointLister.List(selector)
	if err != nil {
		klog.Errorf("failed to list VpcEndpoints for service %s: %v", serviceName, err)
		return
	}
	for _, ep := range endpoints {
		c.addOrUpdateVpcEndpointQueue.Add(ep.Name)
	}
}

func (c *Controller) initVpcEndpointTransit() error {
	if !c.config.EnableLb {
		return nil
	}
	if c.config.VpcEndpointTransitSwitch == "" || c.config.VpcEndpointTransitCIDR == "" {
		return nil
	}

	gw, err := util.GetGwByCidr(c.config.VpcEndpointTransitCIDR)
	if err != nil {
		klog.Errorf("failed to get gateway for vpc endpoint transit cidr %s: %v", c.config.VpcEndpointTransitCIDR, err)
		return err
	}
	if err := c.OVNNbClient.CreateBareLogicalSwitch(c.config.VpcEndpointTransitSwitch); err != nil {
		klog.Errorf("failed to create vpc endpoint transit switch %s: %v", c.config.VpcEndpointTransitSwitch, err)
		return err
	}
	if err := c.ipam.AddOrUpdateSubnet(c.config.VpcEndpointTransitSwitch, c.config.VpcEndpointTransitCIDR, gw, strings.Split(gw, ",")); err != nil {
		klog.Errorf("failed to init ipam for vpc endpoint transit switch: %v", err)
		return err
	}
	return c.restoreVpcEndpointTransitAddresses()
}

func (c *Controller) restoreVpcEndpointTransitAddresses() error {
	services, err := c.vpcEndpointServiceLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list VpcEndpointServices: %v", err)
		return err
	}
	for _, eps := range services {
		if eps.Status.TransitVIP == "" {
			continue
		}
		if _, _, _, err := c.ipam.GetStaticAddress(vpcEndpointServiceIPAMName(eps.Name), vpcEndpointServiceLSPName(eps.Name), eps.Status.TransitVIP, nil, c.config.VpcEndpointTransitSwitch, false); err != nil {
			klog.Errorf("failed to restore transit vip %s for %s: %v", eps.Status.TransitVIP, eps.Name, err)
			return err
		}
	}

	endpoints, err := c.vpcEndpointLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list VpcEndpoints: %v", err)
		return err
	}
	claimed := map[string]string{}
	for _, ep := range endpoints {
		if ep.Status.SnatIP == "" {
			continue
		}
		if existing, ok := claimed[ep.Spec.Vpc]; ok && existing != ep.Status.SnatIP {
			continue
		}
		claimed[ep.Spec.Vpc] = ep.Status.SnatIP
		if _, _, _, err := c.ipam.GetStaticAddress(vpcEndpointSnatIPAMName(ep.Spec.Vpc), vpcEndpointTransitLrpName(ep.Spec.Vpc, c.config.VpcEndpointTransitSwitch), ep.Status.SnatIP, nil, c.config.VpcEndpointTransitSwitch, false); err != nil {
			klog.Errorf("failed to restore snat ip %s for vpc %s: %v", ep.Status.SnatIP, ep.Spec.Vpc, err)
			return err
		}
	}
	return nil
}

func (c *Controller) handleAddOrUpdateVpcEndpointService(key string) error {
	c.vpcEndpointServiceKeyMutex.LockKey(key)
	defer func() { _ = c.vpcEndpointServiceKeyMutex.UnlockKey(key) }()

	cached, err := c.vpcEndpointServiceLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
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
				klog.Errorf("failed to remove finalizer from VpcEndpointService %s: %v", key, err)
				return err
			}
		}
		c.enqueueVpcEndpointsForService(eps.Name)
		return nil
	}

	if !controllerutil.ContainsFinalizer(eps, util.KubeOVNControllerFinalizer) {
		controllerutil.AddFinalizer(eps, util.KubeOVNControllerFinalizer)
		if eps, err = c.config.KubeOvnClient.KubeovnV1().VpcEndpointServices().Update(context.Background(), eps, metav1.UpdateOptions{}); err != nil {
			klog.Errorf("failed to add finalizer to VpcEndpointService %s: %v", key, err)
			return err
		}
	}

	if err := c.reconcileVpcEndpointService(eps); err != nil {
		klog.Errorf("failed to reconcile VpcEndpointService %s: %v", key, err)
		return c.patchVpcEndpointServiceStatus(eps, false, err.Error())
	}
	c.enqueueVpcEndpointsForService(eps.Name)
	return nil
}

func (c *Controller) reconcileVpcEndpointService(eps *kubeovnv1.VpcEndpointService) error {
	if eps.Spec.Vpc == "" || eps.Spec.Namespace == "" || eps.Spec.Service == "" {
		return errors.New("vpc, namespace and service must be set")
	}
	if _, err := c.vpcsLister.Get(eps.Spec.Vpc); err != nil {
		return fmt.Errorf("get provider vpc %s: %w", eps.Spec.Vpc, err)
	}
	svc, err := c.servicesLister.Services(eps.Spec.Namespace).Get(eps.Spec.Service)
	if err != nil {
		return fmt.Errorf("get provider service %s/%s: %w", eps.Spec.Namespace, eps.Spec.Service, err)
	}

	snatIP, err := c.ensureVpcTransitAttachment(eps.Spec.Vpc)
	if err != nil {
		return err
	}
	klog.V(5).Infof("provider vpc %s attached to transit with snat ip %s", eps.Spec.Vpc, snatIP)

	transitVIP, mac, err := c.ensureVpcEndpointServiceVIP(eps)
	if err != nil {
		return err
	}
	if err := c.ensureVpcEndpointServiceLSP(eps.Name, transitVIP, mac); err != nil {
		return err
	}
	if err := c.ensureVpcEndpointServiceLoadBalancers(eps, svc, transitVIP); err != nil {
		return err
	}

	eps.Status.TransitVIP = transitVIP
	eps.Status.Mac = mac
	eps.Status.Ports = vpcEndpointServicePorts(svc)
	if err := c.ensureVpcEndpointServiceLabels(eps); err != nil {
		return err
	}
	return c.patchVpcEndpointServiceStatus(eps, true, "")
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
	if _, err := c.config.KubeOvnClient.KubeovnV1().VpcEndpointServices().Update(context.Background(), eps, metav1.UpdateOptions{}); err != nil {
		klog.Errorf("failed to update labels on VpcEndpointService %s: %v", eps.Name, err)
		return err
	}
	return nil
}

func (c *Controller) ensureVpcEndpointServiceVIP(eps *kubeovnv1.VpcEndpointService) (string, string, error) {
	ipamName := vpcEndpointServiceIPAMName(eps.Name)
	lspName := vpcEndpointServiceLSPName(eps.Name)
	if eps.Status.TransitVIP != "" {
		v4, v6, mac, err := c.ipam.GetStaticAddress(ipamName, lspName, eps.Status.TransitVIP, ptr.To(eps.Status.Mac), c.config.VpcEndpointTransitSwitch, false)
		if err != nil {
			return "", "", err
		}
		return vpcEndpointPreferIP(v4, v6), mac, nil
	}
	v4, v6, mac, err := c.ipam.GetRandomAddress(ipamName, lspName, nil, c.config.VpcEndpointTransitSwitch, "", nil, true)
	if err != nil {
		return "", "", err
	}
	return vpcEndpointPreferIP(v4, v6), mac, nil
}

func (c *Controller) ensureVpcEndpointServiceLSP(name, ip, mac string) error {
	lspName := vpcEndpointServiceLSPName(name)
	if err := c.OVNNbClient.CreateLogicalSwitchPort(c.config.VpcEndpointTransitSwitch, lspName, ip, mac, name, metav1.NamespaceSystem, false, "", "", false, nil, ""); err != nil {
		klog.Errorf("failed to create transit lsp %s: %v", lspName, err)
		return err
	}
	if err := c.OVNNbClient.SetLogicalSwitchPortArpProxy(lspName, true); err != nil {
		klog.Errorf("failed to enable arp proxy on transit lsp %s: %v", lspName, err)
		return err
	}
	return nil
}

func (c *Controller) ensureVpcEndpointServiceLoadBalancers(eps *kubeovnv1.VpcEndpointService, svc *corev1.Service, transitVIP string) error {
	endpointSlices, err := c.endpointSlicesLister.EndpointSlices(svc.Namespace).List(labels.Set{
		discoveryv1.LabelServiceName: svc.Name,
	}.AsSelector())
	if err != nil {
		return err
	}

	protocols := map[corev1.Protocol]struct{}{}
	for _, port := range svc.Spec.Ports {
		protocols[port.Protocol] = struct{}{}
		lbName := vpcEndpointServiceLBName(eps.Name, string(port.Protocol))
		if err := c.OVNNbClient.CreateLoadBalancer(lbName, strings.ToLower(string(port.Protocol))); err != nil {
			return err
		}
		if err := c.OVNNbClient.LogicalRouterUpdateLoadBalancers(eps.Spec.Vpc, ovsdb.MutateOperationInsert, lbName); err != nil {
			return err
		}
		vip := util.JoinHostPort(transitVIP, port.Port)
		backends := c.getEndpointBackend(endpointSlices, port, transitVIP)
		if len(backends) == 0 {
			if err := c.OVNNbClient.LoadBalancerDeleteVip(lbName, vip, true); err != nil {
				return err
			}
			continue
		}
		if err := c.OVNNbClient.LoadBalancerAddVip(lbName, vip, backends...); err != nil {
			return err
		}
	}
	return c.deleteUnusedProtocolLBs(eps.Name, eps.Spec.Vpc, vpcEndpointServiceLBName, protocols)
}

func (c *Controller) deleteUnusedProtocolLBs(name, vpc string, lbNameFn func(string, string) string, keep map[corev1.Protocol]struct{}) error {
	for _, protocol := range []corev1.Protocol{corev1.ProtocolTCP, corev1.ProtocolUDP, corev1.ProtocolSCTP} {
		if _, ok := keep[protocol]; ok {
			continue
		}
		lbName := lbNameFn(name, string(protocol))
		if err := c.OVNNbClient.LogicalRouterUpdateLoadBalancers(vpc, ovsdb.MutateOperationDelete, lbName); err != nil {
			klog.Errorf("failed to detach lb %s from vpc %s: %v", lbName, vpc, err)
			return err
		}
		if err := c.OVNNbClient.DeleteLoadBalancers(func(lb *ovnnb.LoadBalancer) bool { return lb.Name == lbName }); err != nil {
			klog.Errorf("failed to delete lb %s: %v", lbName, err)
			return err
		}
	}
	return nil
}

func (c *Controller) cleanupVpcEndpointService(eps *kubeovnv1.VpcEndpointService) error {
	for _, protocol := range []string{"tcp", "udp", "sctp"} {
		lbName := vpcEndpointServiceLBName(eps.Name, protocol)
		if err := c.OVNNbClient.LogicalRouterUpdateLoadBalancers(eps.Spec.Vpc, ovsdb.MutateOperationDelete, lbName); err != nil {
			klog.Errorf("failed to detach lb %s: %v", lbName, err)
			return err
		}
		if err := c.OVNNbClient.DeleteLoadBalancers(func(lb *ovnnb.LoadBalancer) bool { return lb.Name == lbName }); err != nil {
			klog.Errorf("failed to delete lb %s: %v", lbName, err)
			return err
		}
	}
	if err := c.OVNNbClient.DeleteLogicalSwitchPort(vpcEndpointServiceLSPName(eps.Name)); err != nil {
		klog.Errorf("failed to delete transit lsp for %s: %v", eps.Name, err)
		return err
	}
	c.ipam.ReleaseAddressByPod(vpcEndpointServiceIPAMName(eps.Name), c.config.VpcEndpointTransitSwitch)
	return c.maybeDetachVpcFromTransit(eps.Spec.Vpc)
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
	if _, err = c.config.KubeOvnClient.KubeovnV1().VpcEndpointServices().Patch(context.Background(), eps.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status"); err != nil {
		klog.Errorf("failed to patch VpcEndpointService %s status: %v", eps.Name, err)
		return err
	}
	return nil
}

func (c *Controller) handleAddOrUpdateVpcEndpoint(key string) error {
	c.vpcEndpointKeyMutex.LockKey(key)
	defer func() { _ = c.vpcEndpointKeyMutex.UnlockKey(key) }()

	cached, err := c.vpcEndpointLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}
	ep := cached.DeepCopy()
	if !ep.DeletionTimestamp.IsZero() {
		if err := c.cleanupVpcEndpoint(ep); err != nil {
			return err
		}
		if controllerutil.ContainsFinalizer(ep, util.KubeOVNControllerFinalizer) {
			controllerutil.RemoveFinalizer(ep, util.KubeOVNControllerFinalizer)
			if _, err := c.config.KubeOvnClient.KubeovnV1().VpcEndpoints().Update(context.Background(), ep, metav1.UpdateOptions{}); err != nil {
				klog.Errorf("failed to remove finalizer from VpcEndpoint %s: %v", key, err)
				return err
			}
		}
		return nil
	}

	if !controllerutil.ContainsFinalizer(ep, util.KubeOVNControllerFinalizer) {
		controllerutil.AddFinalizer(ep, util.KubeOVNControllerFinalizer)
		if ep, err = c.config.KubeOvnClient.KubeovnV1().VpcEndpoints().Update(context.Background(), ep, metav1.UpdateOptions{}); err != nil {
			klog.Errorf("failed to add finalizer to VpcEndpoint %s: %v", key, err)
			return err
		}
	}

	if err := c.reconcileVpcEndpoint(ep); err != nil {
		klog.Errorf("failed to reconcile VpcEndpoint %s: %v", key, err)
		return c.patchVpcEndpointStatus(ep, false, err.Error())
	}
	return nil
}

func (c *Controller) reconcileVpcEndpoint(ep *kubeovnv1.VpcEndpoint) error {
	if ep.Spec.Vpc == "" || ep.Spec.Subnet == "" || ep.Spec.EndpointService == "" {
		return errors.New("vpc, subnet and endpointService must be set")
	}
	subnet, err := c.subnetsLister.Get(ep.Spec.Subnet)
	if err != nil {
		return fmt.Errorf("get subnet %s: %w", ep.Spec.Subnet, err)
	}
	if subnet.Spec.Vpc != ep.Spec.Vpc {
		return fmt.Errorf("subnet %s belongs to vpc %s, not %s", ep.Spec.Subnet, subnet.Spec.Vpc, ep.Spec.Vpc)
	}
	eps, err := c.vpcEndpointServiceLister.Get(ep.Spec.EndpointService)
	if err != nil {
		return fmt.Errorf("get VpcEndpointService %s: %w", ep.Spec.EndpointService, err)
	}
	if !vpcEndpointServiceAllowed(eps, ep.Spec.Vpc) {
		return fmt.Errorf("vpc %s is not allowed to consume endpoint service %s", ep.Spec.Vpc, eps.Name)
	}
	if !eps.Status.Ready || eps.Status.TransitVIP == "" {
		return fmt.Errorf("endpoint service %s is not ready", eps.Name)
	}
	svc, err := c.servicesLister.Services(eps.Spec.Namespace).Get(eps.Spec.Service)
	if err != nil {
		return fmt.Errorf("get provider service %s/%s: %w", eps.Spec.Namespace, eps.Spec.Service, err)
	}

	snatIP, err := c.ensureVpcTransitAttachment(ep.Spec.Vpc)
	if err != nil {
		return err
	}
	vip, err := c.ensureVpcEndpointVip(ep)
	if err != nil {
		return err
	}
	if vpcEndpointPreferIP(vip.Status.V4ip, vip.Status.V6ip) == "" {
		return fmt.Errorf("waiting for local vip %s allocation", vip.Name)
	}
	localVIP := vpcEndpointPreferIP(vip.Status.V4ip, vip.Status.V6ip)
	if err := c.ensureVpcEndpointLoadBalancers(ep, svc, localVIP, eps.Status.TransitVIP); err != nil {
		return err
	}
	if err := c.OVNNbClient.AddSnatWithMatch(ep.Spec.Vpc, snatIP, vpcEndpointSnatLogicalIP(eps.Status.TransitVIP), vpcEndpointSnatMatch(eps.Status.TransitVIP)); err != nil {
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

func (c *Controller) ensureVpcEndpointLabels(ep *kubeovnv1.VpcEndpoint) error {
	if ep.Labels == nil {
		ep.Labels = map[string]string{}
	}
	if ep.Labels[util.VpcEndpointServiceLabel] == ep.Spec.EndpointService &&
		ep.Labels[util.VpcEndpointVpcLabel] == ep.Spec.Vpc {
		return nil
	}
	ep.Labels[util.VpcEndpointServiceLabel] = ep.Spec.EndpointService
	ep.Labels[util.VpcEndpointVpcLabel] = ep.Spec.Vpc
	if _, err := c.config.KubeOvnClient.KubeovnV1().VpcEndpoints().Update(context.Background(), ep, metav1.UpdateOptions{}); err != nil {
		klog.Errorf("failed to update labels on VpcEndpoint %s: %v", ep.Name, err)
		return err
	}
	return nil
}

func (c *Controller) ensureVpcEndpointVip(ep *kubeovnv1.VpcEndpoint) (*kubeovnv1.Vip, error) {
	name := vpcEndpointVipCRName(ep.Name)
	existing, err := c.virtualIpsLister.Get(name)
	if err != nil && !k8serrors.IsNotFound(err) {
		return nil, err
	}
	if k8serrors.IsNotFound(err) {
		vip := &kubeovnv1.Vip{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: kubeovnv1.VipSpec{
				Namespace: metav1.NamespaceSystem,
				Subnet:    ep.Spec.Subnet,
				Type:      util.SwitchLBRuleVip,
				V4ip:      ep.Spec.IP,
			},
		}
		if err := controllerutil.SetControllerReference(ep, vip, kubeovnscheme.Scheme); err != nil {
			return nil, err
		}
		created, err := c.config.KubeOvnClient.KubeovnV1().Vips().Create(context.Background(), vip, metav1.CreateOptions{})
		if err != nil {
			if !k8serrors.IsAlreadyExists(err) {
				return nil, err
			}
			return c.config.KubeOvnClient.KubeovnV1().Vips().Get(context.Background(), name, metav1.GetOptions{})
		}
		return created, nil
	}
	return existing, nil
}

func (c *Controller) ensureVpcEndpointLoadBalancers(ep *kubeovnv1.VpcEndpoint, svc *corev1.Service, localVIP, transitVIP string) error {
	protocols := map[corev1.Protocol]struct{}{}
	for _, port := range svc.Spec.Ports {
		protocols[port.Protocol] = struct{}{}
		lbName := vpcEndpointLBName(ep.Name, string(port.Protocol))
		if err := c.OVNNbClient.CreateLoadBalancer(lbName, strings.ToLower(string(port.Protocol))); err != nil {
			return err
		}
		if err := c.OVNNbClient.LogicalRouterUpdateLoadBalancers(ep.Spec.Vpc, ovsdb.MutateOperationInsert, lbName); err != nil {
			return err
		}
		vip := util.JoinHostPort(localVIP, port.Port)
		backend := util.JoinHostPort(transitVIP, port.Port)
		if err := c.OVNNbClient.LoadBalancerAddVip(lbName, vip, backend); err != nil {
			return err
		}
	}
	return c.deleteUnusedProtocolLBs(ep.Name, ep.Spec.Vpc, vpcEndpointLBName, protocols)
}

func (c *Controller) cleanupVpcEndpoint(ep *kubeovnv1.VpcEndpoint) error {
	for _, protocol := range []string{"tcp", "udp", "sctp"} {
		lbName := vpcEndpointLBName(ep.Name, protocol)
		if err := c.OVNNbClient.LogicalRouterUpdateLoadBalancers(ep.Spec.Vpc, ovsdb.MutateOperationDelete, lbName); err != nil {
			return err
		}
		if err := c.OVNNbClient.DeleteLoadBalancers(func(lb *ovnnb.LoadBalancer) bool { return lb.Name == lbName }); err != nil {
			return err
		}
	}
	if ep.Status.TransitVIP != "" && ep.Status.SnatIP != "" {
		if err := c.OVNNbClient.DeleteSnatWithMatch(ep.Spec.Vpc, ep.Status.SnatIP, vpcEndpointSnatLogicalIP(ep.Status.TransitVIP), vpcEndpointSnatMatch(ep.Status.TransitVIP)); err != nil {
			return err
		}
	}
	vipName := vpcEndpointVipCRName(ep.Name)
	if err := c.config.KubeOvnClient.KubeovnV1().Vips().Delete(context.Background(), vipName, metav1.DeleteOptions{}); err != nil && !k8serrors.IsNotFound(err) {
		return err
	}
	return c.maybeDetachVpcFromTransit(ep.Spec.Vpc)
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
	if _, err = c.config.KubeOvnClient.KubeovnV1().VpcEndpoints().Patch(context.Background(), ep.Name, types.MergePatchType, bytes, metav1.PatchOptions{}, "status"); err != nil {
		klog.Errorf("failed to patch VpcEndpoint %s status: %v", ep.Name, err)
		return err
	}
	return nil
}

func (c *Controller) ensureVpcTransitAttachment(vpcName string) (string, error) {
	lrpName := vpcEndpointTransitLrpName(vpcName, c.config.VpcEndpointTransitSwitch)
	lspName := vpcEndpointTransitLspName(vpcName, c.config.VpcEndpointTransitSwitch)
	exist, err := c.OVNNbClient.LogicalRouterPortExists(lrpName)
	if err != nil {
		return "", err
	}
	if exist {
		lrp, err := c.OVNNbClient.GetLogicalRouterPort(lrpName, false)
		if err != nil {
			return "", err
		}
		return vpcEndpointIPFromNetworks(lrp.Networks), nil
	}

	v4, v6, _, err := c.ipam.GetRandomAddress(vpcEndpointSnatIPAMName(vpcName), lrpName, nil, c.config.VpcEndpointTransitSwitch, "", nil, true)
	if err != nil {
		return "", err
	}
	ip := util.GetStringIP(v4, v6)
	networks, err := util.GetIPAddrWithMask(ip, c.config.VpcEndpointTransitCIDR)
	if err != nil {
		c.ipam.ReleaseAddressByPod(vpcEndpointSnatIPAMName(vpcName), c.config.VpcEndpointTransitSwitch)
		return "", err
	}
	if err := c.OVNNbClient.CreateLogicalPatchPort(c.config.VpcEndpointTransitSwitch, vpcName, lspName, lrpName, networks, ""); err != nil {
		c.ipam.ReleaseAddressByPod(vpcEndpointSnatIPAMName(vpcName), c.config.VpcEndpointTransitSwitch)
		return "", err
	}
	return vpcEndpointPreferIP(v4, v6), nil
}

func vpcEndpointPreferIP(v4, v6 string) string {
	if v4 != "" {
		return v4
	}
	return v6
}

func vpcEndpointIPFromNetworks(networks []string) string {
	for _, network := range networks {
		ip, _, err := net.ParseCIDR(network)
		if err != nil {
			continue
		}
		if ip.To4() != nil {
			return ip.String()
		}
	}
	if len(networks) == 0 {
		return ""
	}
	ip, _, err := net.ParseCIDR(networks[0])
	if err != nil {
		return strings.Split(networks[0], "/")[0]
	}
	return ip.String()
}

func (c *Controller) maybeDetachVpcFromTransit(vpcName string) error {
	inUse, err := c.vpcUsesTransit(vpcName)
	if err != nil {
		return err
	}
	if inUse {
		return nil
	}
	lrpName := vpcEndpointTransitLrpName(vpcName, c.config.VpcEndpointTransitSwitch)
	lspName := vpcEndpointTransitLspName(vpcName, c.config.VpcEndpointTransitSwitch)
	if err := c.OVNNbClient.RemoveLogicalPatchPort(lspName, lrpName); err != nil {
		return err
	}
	c.ipam.ReleaseAddressByPod(vpcEndpointSnatIPAMName(vpcName), c.config.VpcEndpointTransitSwitch)
	return nil
}

func (c *Controller) vpcUsesTransit(vpcName string) (bool, error) {
	services, err := c.vpcEndpointServiceLister.List(labels.Everything())
	if err != nil {
		return false, err
	}
	for _, eps := range services {
		if eps.Spec.Vpc == vpcName && eps.DeletionTimestamp.IsZero() {
			return true, nil
		}
	}
	endpoints, err := c.vpcEndpointLister.List(labels.Everything())
	if err != nil {
		return false, err
	}
	for _, ep := range endpoints {
		if ep.Spec.Vpc == vpcName && ep.DeletionTimestamp.IsZero() {
			return true, nil
		}
	}
	return false, nil
}
