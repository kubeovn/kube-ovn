package controller

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"k8s.io/utils/set"

	"github.com/ovn-kubernetes/libovsdb/ovsdb"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

type RouterLBRuleInfo struct {
	Name       string
	Namespace  string
	OvnEip     string
	Vpc        string
	Ports      []int32
	IsRecreate bool
}

func generateRlrSvcName(name string) string {
	return "rlr-" + name
}

func newRouterLBRuleInfo(rlr *kubeovnv1.RouterLBRule) *RouterLBRuleInfo {
	namespace := rlr.Spec.Namespace
	if namespace == "" {
		namespace = metav1.NamespaceDefault
	}

	ports := make([]int32, 0, len(rlr.Spec.Ports))
	for _, p := range rlr.Spec.Ports {
		ports = append(ports, p.Port)
	}

	return &RouterLBRuleInfo{
		Name:      rlr.Name,
		Namespace: namespace,
		OvnEip:    rlr.Spec.OvnEip,
		Vpc:       rlr.Spec.Vpc,
		Ports:     ports,
	}
}

func (c *Controller) requeueRouterLBRulesForEip(eipName string, isRecreate bool) {
	rlrs, err := c.routerLBRuleLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list RouterLBRules for EIP %s: %v", eipName, err)
		return
	}
	for _, rlr := range rlrs {
		if rlr.Spec.OvnEip != eipName {
			continue
		}
		klog.Infof("re-queuing RouterLBRule %s due to EIP %s change", rlr.Name, eipName)
		if isRecreate {
			info := newRouterLBRuleInfo(rlr)
			info.IsRecreate = true
			c.updateRouterLBRuleQueue.Add(info)
		} else {
			c.addRouterLBRuleQueue.Add(rlr.Name)
		}
	}
}

func (c *Controller) enqueueAddRouterLBRule(obj any) {
	key := cache.MetaObjectToName(obj.(*kubeovnv1.RouterLBRule)).String()
	klog.Infof("enqueue add RouterLBRule %s", key)
	c.addRouterLBRuleQueue.Add(key)
}

func (c *Controller) enqueueUpdateRouterLBRule(oldObj, newObj any) {
	var (
		oldRlr = oldObj.(*kubeovnv1.RouterLBRule)
		newRlr = newObj.(*kubeovnv1.RouterLBRule)
		info   = newRouterLBRuleInfo(oldRlr)
	)

	if oldRlr.ResourceVersion == newRlr.ResourceVersion {
		return
	}

	if oldRlr.Spec.OvnEip != newRlr.Spec.OvnEip ||
		oldRlr.Spec.Vpc != newRlr.Spec.Vpc ||
		oldRlr.Spec.Namespace != newRlr.Spec.Namespace {
		info.IsRecreate = true
	}

	c.updateRouterLBRuleQueue.Add(info)
}

func (c *Controller) enqueueDeleteRouterLBRule(obj any) {
	var rlr *kubeovnv1.RouterLBRule
	switch t := obj.(type) {
	case *kubeovnv1.RouterLBRule:
		rlr = t
	case cache.DeletedFinalStateUnknown:
		r, ok := t.Obj.(*kubeovnv1.RouterLBRule)
		if !ok {
			klog.Warningf("unexpected object type: %T", t.Obj)
			return
		}
		rlr = r
	default:
		klog.Warningf("unexpected type: %T", obj)
		return
	}

	klog.Infof("enqueue del RouterLBRule %s", rlr.Name)
	c.delRouterLBRuleQueue.Add(newRouterLBRuleInfo(rlr))
}

// checkEipPortConflict returns an error if eip:portStr is already used by another
// RouterLBRule or OvnDnatRule (excluding the named resources).
// portStr is the string representation of the port number.
func (c *Controller) checkEipPortConflict(eipName, portStr, excludeRlr, excludeDnat string) error {
	rlrs, err := c.routerLBRuleLister.List(labels.Everything())
	if err != nil {
		return err
	}
	for _, r := range rlrs {
		if r.Name == excludeRlr || r.Spec.OvnEip != eipName {
			continue
		}
		for _, p := range r.Spec.Ports {
			if strconv.Itoa(int(p.Port)) == portStr {
				return fmt.Errorf("eip %s port %s already used by RouterLBRule %s", eipName, portStr, r.Name)
			}
		}
	}

	dnats, err := c.ovnDnatRulesLister.List(labels.SelectorFromSet(labels.Set{
		util.VpcDnatEPortLabel: portStr,
	}))
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}
	for _, d := range dnats {
		if d.Name != excludeDnat && d.Spec.OvnEip == eipName {
			return fmt.Errorf("eip %s port %s already used by OvnDnatRule %s", eipName, portStr, d.Name)
		}
	}
	return nil
}

func (c *Controller) handleAddOrUpdateRouterLBRule(key string) error {
	klog.V(3).Infof("handleAddOrUpdateRouterLBRule %s", key)

	rlr, err := c.routerLBRuleLister.Get(key)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		klog.Error(err)
		return err
	}

	if rlr.Spec.OvnEip == "" {
		klog.Warningf("RouterLBRule %s has empty OvnEip, skipping", key)
		return nil
	}
	if rlr.Spec.Vpc == "" {
		err = fmt.Errorf("RouterLBRule %s has empty Vpc", key)
		klog.Error(err)
		return err
	}
	if len(rlr.Spec.Ports) == 0 {
		err = fmt.Errorf("RouterLBRule %s has no ports", key)
		klog.Error(err)
		return err
	}

	eip, err := c.GetOvnEip(rlr.Spec.OvnEip)
	if err != nil {
		klog.Errorf("failed to get OvnEip %s for RouterLBRule %s: %v", rlr.Spec.OvnEip, key, err)
		return err
	}
	if eip.Spec.Type == util.OvnEipTypeLSP {
		err = fmt.Errorf("RouterLBRule %s cannot use LSP-type EIP %s", key, eip.Name)
		klog.Error(err)
		return err
	}

	v4Ip := eip.Status.V4Ip
	v6Ip := eip.Status.V6Ip
	if v4Ip == "" && v6Ip == "" {
		err = fmt.Errorf("RouterLBRule %s: EIP %s has no IP", key, eip.Name)
		klog.Error(err)
		return err
	}
	if eip.Spec.ExternalSubnet == "" {
		err = fmt.Errorf("RouterLBRule %s: EIP %s has no external subnet", key, eip.Name)
		klog.Error(err)
		return err
	}
	// Verify the VPC router has an active LRP on the EIP's external subnet so that
	// OVN installs ARP proxy flows for the LB VIP on the external network.
	lrpEipName := fmt.Sprintf("%s-%s", rlr.Spec.Vpc, eip.Spec.ExternalSubnet)
	lrpEip, err := c.ovnEipsLister.Get(lrpEipName)
	if err != nil || !lrpEip.Status.Ready || lrpEip.Spec.Type != util.OvnEipTypeLRP {
		err = fmt.Errorf("vpc %s has no ready LRP on external subnet %s: ensure spec.extraExternalSubnets contains it",
			rlr.Spec.Vpc, eip.Spec.ExternalSubnet)
		klog.Error(err)
		return err
	}

	vipParts := make([]string, 0, 2)
	if v4Ip != "" {
		vipParts = append(vipParts, v4Ip)
	}
	if v6Ip != "" {
		vipParts = append(vipParts, v6Ip)
	}
	vip := strings.Join(vipParts, ",")

	namespace := rlr.Spec.Namespace
	if namespace == "" {
		namespace = metav1.NamespaceDefault
	}

	for _, port := range rlr.Spec.Ports {
		if err = c.checkEipPortConflict(rlr.Spec.OvnEip, strconv.Itoa(int(port.Port)), rlr.Name, ""); err != nil {
			klog.Errorf("RouterLBRule %s port conflict: %v", key, err)
			return err
		}
	}

	svcName := generateRlrSvcName(rlr.Name)

	var (
		oldSvc          *corev1.Service
		oldEps          *corev1.Endpoints
		needToCreateSvc bool
		needToCreateEps bool
	)

	if oldSvc, err = c.servicesLister.Services(namespace).Get(svcName); err != nil {
		if k8serrors.IsNotFound(err) {
			needToCreateSvc = true
			needToCreateEps = true
		} else {
			klog.Errorf("failed to get service %s: %v", svcName, err)
			return err
		}
	}

	if oldEps, err = c.config.KubeClient.CoreV1().Endpoints(namespace).Get(context.Background(), svcName, metav1.GetOptions{}); err != nil {
		if k8serrors.IsNotFound(err) {
			needToCreateEps = true
		} else {
			klog.Errorf("failed to get endpoints %s: %v", svcName, err)
			return err
		}
	}

	rlrCopy := rlr.DeepCopy()
	if len(rlrCopy.Spec.Endpoints) > 0 {
		eps := generateRlrEndpoints(rlrCopy, oldEps, svcName, namespace)
		if needToCreateEps {
			if _, err = c.config.KubeClient.CoreV1().Endpoints(namespace).Create(context.Background(), eps, metav1.CreateOptions{}); err != nil {
				err = fmt.Errorf("failed to create endpoints %s: %w", svcName, err)
				klog.Error(err)
				return err
			}
		} else {
			if _, err = c.config.KubeClient.CoreV1().Endpoints(namespace).Update(context.Background(), eps, metav1.UpdateOptions{}); err != nil {
				err = fmt.Errorf("failed to update endpoints %s: %w", svcName, err)
				klog.Error(err)
				return err
			}
		}
		rlrCopy.Spec.Selector = nil
	}

	svc := generateRlrHeadlessService(rlrCopy, oldSvc, svcName, namespace, vip)
	if needToCreateSvc {
		if _, err = c.config.KubeClient.CoreV1().Services(namespace).Create(context.Background(), svc, metav1.CreateOptions{}); err != nil {
			err = fmt.Errorf("failed to create service %s: %w", svcName, err)
			klog.Error(err)
			return err
		}
	} else {
		if _, err = c.config.KubeClient.CoreV1().Services(namespace).Update(context.Background(), svc, metav1.UpdateOptions{}); err != nil {
			err = fmt.Errorf("failed to update service %s: %w", svcName, err)
			klog.Error(err)
			return err
		}
	}

	// Attach VPC shared LBs to the router so external traffic gets LB applied at the router.
	vpc, err := c.vpcsLister.Get(rlr.Spec.Vpc)
	if err != nil {
		klog.Errorf("failed to get VPC %s: %v", rlr.Spec.Vpc, err)
		return err
	}

	// Verify the VPC router is connected to the EIP's external subnet so that
	// OVN can install ARP proxy flows for the LB VIP on the external network.
	// Without this connection nodes cannot reach the VIP.
	vpcLBs := []string{
		vpc.Status.TCPLoadBalancer, vpc.Status.TCPSessionLoadBalancer,
		vpc.Status.UDPLoadBalancer, vpc.Status.UDPSessionLoadBalancer,
		vpc.Status.SctpLoadBalancer, vpc.Status.SctpSessionLoadBalancer,
	}
	var nonEmptyVpcLBs []string
	for _, lb := range vpcLBs {
		if lb != "" {
			nonEmptyVpcLBs = append(nonEmptyVpcLBs, lb)
		}
	}
	if len(nonEmptyVpcLBs) > 0 {
		if err = c.OVNNbClient.LogicalRouterUpdateLoadBalancers(rlr.Spec.Vpc, ovsdb.MutateOperationInsert, nonEmptyVpcLBs...); err != nil {
			klog.Errorf("failed to attach LBs to router %s: %v", rlr.Spec.Vpc, err)
			return err
		}
	}

	newRlr := rlr.DeepCopy()
	newRlr.Status.Service = fmt.Sprintf("%s/%s", namespace, svcName)

	formatPorts := ""
	for _, port := range newRlr.Spec.Ports {
		protocol := port.Protocol
		if protocol == "" {
			protocol = "TCP"
		}
		formatPorts = fmt.Sprintf("%s,%d/%s", formatPorts, port.Port, protocol)
	}
	newRlr.Status.Ports = strings.TrimPrefix(formatPorts, ",")

	if _, err = c.config.KubeOvnClient.KubeovnV1().RouterLBRules().UpdateStatus(context.Background(), newRlr, metav1.UpdateOptions{}); err != nil {
		err = fmt.Errorf("failed to update RouterLBRule status: %w", err)
		klog.Error(err)
		return err
	}
	return nil
}

func (c *Controller) handleDelRouterLBRule(info *RouterLBRuleInfo) error {
	klog.V(3).Infof("handleDelRouterLBRule %s", info.Name)

	svcName := generateRlrSvcName(info.Name)

	var vips []string
	vpcForRlr := ""
	if svc, e := c.servicesLister.Services(info.Namespace).Get(svcName); e == nil {
		// Build ip:port vips for LBHC cleanup from the service annotation IPs + known ports.
		if vipAnnotation := svc.Annotations[util.RouterLBRuleVipsAnnotation]; vipAnnotation != "" {
			for ip := range strings.SplitSeq(vipAnnotation, ",") {
				ip = strings.TrimSpace(ip)
				if ip == "" {
					continue
				}
				for _, port := range info.Ports {
					vips = append(vips, util.JoinHostPort(ip, port))
				}
			}
		}
		if vpcForRlr = svc.Annotations[util.VpcAnnotation]; vpcForRlr == "" {
			vpcForRlr = svc.Annotations[util.LogicalRouterAnnotation]
		}
	}

	if err := c.config.KubeClient.CoreV1().Services(info.Namespace).Delete(context.Background(), svcName, metav1.DeleteOptions{}); err != nil {
		if !k8serrors.IsNotFound(err) {
			klog.Errorf("failed to delete service %s: %v", svcName, err)
			return err
		}
	}

	if len(vips) == 0 {
		return nil
	}

	var vpcLBNames set.Set[string]
	if vpcForRlr != "" {
		vpc, e := c.vpcsLister.Get(vpcForRlr)
		switch {
		case e == nil:
			vpcLBNames = set.New(
				vpc.Status.TCPLoadBalancer, vpc.Status.UDPLoadBalancer,
				vpc.Status.SctpLoadBalancer, vpc.Status.TCPSessionLoadBalancer,
				vpc.Status.UDPSessionLoadBalancer, vpc.Status.SctpSessionLoadBalancer,
			)
			vpcLBNames.Delete("")
		case k8serrors.IsNotFound(e):
			klog.Warningf("VPC %s not found for RLR %s, falling back to unscoped deletion", vpcForRlr, info.Name)
		default:
			klog.Errorf("failed to get VPC %s for RLR %s: %v", vpcForRlr, info.Name, e)
			return e
		}
	}

	// Explicitly remove VIP entries from the VPC shared load balancers.
	// The service-delete queue only handles cluster-IP services; for
	// RouterLBRule headless services the VIP must be removed here.
	if vpcLBNames != nil {
		for _, lbName := range vpcLBNames.UnsortedList() {
			for _, vip := range vips {
				if e := c.OVNNbClient.LoadBalancerDeleteVip(lbName, vip, true); e != nil && !k8serrors.IsNotFound(e) {
					klog.Errorf("failed to delete vip %s from LB %s for RLR %s: %v", vip, lbName, info.Name, e)
					return e
				}
			}
		}
	}

	lbhcs, err := c.OVNNbClient.ListLoadBalancerHealthChecks(
		func(lbhc *ovnnb.LoadBalancerHealthCheck) bool {
			return slices.Contains(vips, lbhc.Vip)
		},
	)
	if err != nil && !k8serrors.IsNotFound(err) {
		klog.Errorf("failed to list LBHC for vips %v: %v", vips, err)
		return err
	}

	vipSubnets := make(map[string]struct{})
	lbhcUUIDsToDelete := set.New[string]()
	for _, lbhc := range lbhcs {
		lbs, e := c.OVNNbClient.ListLoadBalancers(
			func(lb *ovnnb.LoadBalancer) bool {
				return slices.Contains(lb.HealthCheck, lbhc.UUID)
			},
		)
		if e != nil && !k8serrors.IsNotFound(e) {
			klog.Errorf("failed to list LBs for LBHC %s: %v", lbhc.Vip, e)
			return e
		}

		belongsToThisVpc := false
		referencedByOtherVpc := false
		for _, lb := range lbs {
			if vpcLBNames != nil && !vpcLBNames.Has(lb.Name) {
				referencedByOtherVpc = true
				continue
			}
			belongsToThisVpc = true

			if e = c.OVNNbClient.LoadBalancerDeleteHealthCheck(lb.Name, lbhc.UUID); e != nil && !k8serrors.IsNotFound(e) {
				klog.Errorf("failed to delete LBHC %s from LB %s: %v", lbhc.Vip, lb.Name, e)
				return e
			}
			if e = c.OVNNbClient.LoadBalancerDeleteIPPortMapping(lb.Name, lbhc.Vip); e != nil && !k8serrors.IsNotFound(e) {
				klog.Errorf("failed to delete IP port mapping %s from LB %s: %v", lbhc.Vip, lb.Name, e)
				return e
			}
		}

		if (belongsToThisVpc || vpcLBNames == nil) && !referencedByOtherVpc {
			lbhcUUIDsToDelete.Insert(lbhc.UUID)
		}
		if belongsToThisVpc || vpcLBNames == nil {
			if vip, ex := lbhc.ExternalIDs[util.SwitchLBRuleSubnet]; ex && vip != "" {
				vipSubnets[vip] = struct{}{}
			}
		}
	}

	if lbhcUUIDsToDelete.Len() > 0 {
		if err = c.OVNNbClient.DeleteLoadBalancerHealthChecks(
			func(lbhc *ovnnb.LoadBalancerHealthCheck) bool {
				return lbhcUUIDsToDelete.Has(lbhc.UUID)
			},
		); err != nil && !k8serrors.IsNotFound(err) {
			klog.Errorf("failed to delete LBHCs for RLR %s: %v", info.Name, err)
			return err
		}
	}

	for vip := range vipSubnets {
		remaining, e := c.OVNNbClient.ListLoadBalancerHealthChecks(
			func(lbhc *ovnnb.LoadBalancerHealthCheck) bool {
				return lbhc.ExternalIDs[util.SwitchLBRuleSubnet] == vip
			},
		)
		if e != nil && !k8serrors.IsNotFound(e) {
			klog.Errorf("failed to list remaining LBHCs for health-check vip %s: %v", vip, e)
			continue
		}
		if len(remaining) == 0 {
			if e = c.config.KubeOvnClient.KubeovnV1().Vips().Delete(context.Background(), vip, metav1.DeleteOptions{}); e != nil && !k8serrors.IsNotFound(e) {
				klog.Errorf("failed to delete health-check vip %s for RLR %s: %v", vip, info.Name, e)
			}
		}
	}

	return nil
}

func (c *Controller) handleUpdateRouterLBRule(info *RouterLBRuleInfo) error {
	klog.V(3).Infof("handleUpdateRouterLBRule %s", info.Name)
	if info.IsRecreate {
		if err := c.handleDelRouterLBRule(info); err != nil {
			klog.Errorf("failed to delete RouterLBRule %s during update: %v", info.Name, err)
			return err
		}
	}
	if err := c.handleAddOrUpdateRouterLBRule(info.Name); err != nil {
		klog.Errorf("failed to add/update RouterLBRule %s: %v", info.Name, err)
		return err
	}
	return nil
}

func generateRlrHeadlessService(rlr *kubeovnv1.RouterLBRule, oldSvc *corev1.Service, svcName, namespace, vip string) *corev1.Service {
	selectors := make(map[string]string)
	for _, s := range rlr.Spec.Selector {
		kv := strings.Split(strings.TrimSpace(s), ":")
		if len(kv) != 2 {
			continue
		}
		selectors[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
	}

	var ports []corev1.ServicePort
	for _, port := range rlr.Spec.Ports {
		ports = append(ports, corev1.ServicePort{
			Name:     port.Name,
			Protocol: corev1.Protocol(port.Protocol),
			Port:     port.Port,
			TargetPort: intstr.IntOrString{
				Type:   intstr.Int,
				IntVal: port.TargetPort,
			},
		})
	}

	families, policy := getIPFamilies(vip)

	annotations := map[string]string{
		util.RouterLBRuleVipsAnnotation: vip,
		util.LogicalRouterAnnotation:    rlr.Spec.Vpc,
	}
	if rlr.Annotations != nil {
		if hc := rlr.Annotations[util.ServiceHealthCheck]; hc != "" {
			annotations[util.ServiceHealthCheck] = hc
		}
	}

	var svc *corev1.Service
	if oldSvc != nil {
		svc = oldSvc.DeepCopy()
		if svc.Annotations == nil {
			svc.Annotations = map[string]string{}
		}
		maps.Copy(svc.Annotations, annotations)
		svc.Spec.Ports = ports
		svc.Spec.Selector = selectors
		svc.Spec.SessionAffinity = corev1.ServiceAffinity(rlr.Spec.SessionAffinity)
		svc.Spec.IPFamilies = families
		svc.Spec.IPFamilyPolicy = &policy
	} else {
		svc = &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:        svcName,
				Namespace:   namespace,
				Annotations: annotations,
			},
			Spec: corev1.ServiceSpec{
				Ports:           ports,
				Selector:        selectors,
				ClusterIP:       corev1.ClusterIPNone,
				Type:            corev1.ServiceTypeClusterIP,
				SessionAffinity: corev1.ServiceAffinity(rlr.Spec.SessionAffinity),
				IPFamilies:      families,
				IPFamilyPolicy:  &policy,
			},
		}
	}
	return svc
}

func generateRlrEndpoints(rlr *kubeovnv1.RouterLBRule, oldEps *corev1.Endpoints, svcName, namespace string) *corev1.Endpoints {
	var ports []corev1.EndpointPort
	for _, port := range rlr.Spec.Ports {
		ports = append(ports, corev1.EndpointPort{
			Name:     port.Name,
			Protocol: corev1.Protocol(port.Protocol),
			Port:     port.TargetPort,
		})
	}

	var addrs []corev1.EndpointAddress
	for _, endpoint := range rlr.Spec.Endpoints {
		addrs = append(addrs, corev1.EndpointAddress{
			IP: endpoint,
			TargetRef: &corev1.ObjectReference{
				Namespace: namespace,
			},
		})
	}

	subsets := []corev1.EndpointSubset{{Addresses: addrs, Ports: ports}}

	if oldEps != nil {
		eps := oldEps.DeepCopy()
		eps.Name = svcName
		eps.Namespace = namespace
		eps.Subsets = subsets
		return eps
	}
	return &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: namespace,
		},
		Subsets: subsets,
	}
}
