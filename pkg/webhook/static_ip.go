package webhook

import (
	"context"
	"net/http"
	"strings"

	ovnv1 "github.com/alauda/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/alauda/kube-ovn/pkg/ovs"
	"github.com/alauda/kube-ovn/pkg/util"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var (
	deploymentGVK  = metav1.GroupVersionKind{Group: appsv1.SchemeGroupVersion.Group, Version: appsv1.SchemeGroupVersion.Version, Kind: "Deployment"}
	statefulSetGVK = metav1.GroupVersionKind{Group: appsv1.SchemeGroupVersion.Group, Version: appsv1.SchemeGroupVersion.Version, Kind: "StatefulSet"}
	daemonSetGVK   = metav1.GroupVersionKind{Group: appsv1.SchemeGroupVersion.Group, Version: appsv1.SchemeGroupVersion.Version, Kind: "DaemonSet"}
	podGVK         = metav1.GroupVersionKind{Group: corev1.SchemeGroupVersion.Group, Version: corev1.SchemeGroupVersion.Version, Kind: "Pod"}
)

func (v *ValidatingHook) DeploymentCreateHook(ctx context.Context, req admission.Request) admission.Response {
	o := appsv1.Deployment{}
	if err := v.decoder.Decode(req, &o); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	// Get pod template static ips
	staticIPSAnno := o.Spec.Template.GetAnnotations()[util.IpPoolAnnotation]
	klog.V(3).Infof("%s %s@%s, ip_pool: %s", o.Kind, o.GetName(), o.GetNamespace(), staticIPSAnno)
	if staticIPSAnno == "" {
		return ctrlwebhook.Allowed("by pass")
	}
	return v.podControllerCreate(ctx, staticIPSAnno, o.GetNamespace())
}

func (v *ValidatingHook) StatefulSetCreateHook(ctx context.Context, req admission.Request) admission.Response {
	o := appsv1.StatefulSet{}
	if err := v.decoder.Decode(req, &o); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	// Get pod template static ips
	staticIPSAnno := o.Spec.Template.GetAnnotations()[util.IpPoolAnnotation]
	klog.V(3).Infof("%s %s@%s, ip_pool: %s", o.Kind, o.GetName(), o.GetNamespace(), staticIPSAnno)
	if staticIPSAnno == "" {
		return ctrlwebhook.Allowed("by pass")
	}
	return v.podControllerCreate(ctx, staticIPSAnno, o.GetNamespace())
}

func (v *ValidatingHook) DaemonSetCreateHook(ctx context.Context, req admission.Request) admission.Response {
	o := appsv1.DaemonSet{}
	if err := v.decoder.Decode(req, &o); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	// Get pod template static ips
	staticIPSAnno := o.Spec.Template.GetAnnotations()[util.IpPoolAnnotation]
	klog.V(3).Infof("%s %s@%s, ip_pool: %s", o.Kind, o.GetName(), o.GetNamespace(), staticIPSAnno)
	if staticIPSAnno == "" {
		return ctrlwebhook.Allowed("by pass")
	}
	return v.podControllerCreate(ctx, staticIPSAnno, o.GetNamespace())
}

func (v *ValidatingHook) PodCreateHook(ctx context.Context, req admission.Request) admission.Response {
	o := corev1.Pod{}
	if err := v.decoder.Decode(req, &o); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	poolAnno := o.GetAnnotations()[util.IpPoolAnnotation]
	klog.V(3).Infof("%s %s@%s, ip_pool: %s", o.Kind, o.GetName(), o.GetNamespace(), poolAnno)
	if poolAnno != "" {
		return ctrlwebhook.Allowed("by pass")
	}
	staticIP := o.GetAnnotations()[util.IpAddressAnnotation]
	klog.V(3).Infof("%s %s@%s, ip_address: %s", o.Kind, o.GetName(), o.GetNamespace(), staticIP)
	if staticIP == "" {
		return ctrlwebhook.Allowed("by pass")
	}
	// Get logical switch name
	lsName := v.opt.DefaultLS
	var subnet ovnv1.Subnet
	subnetList := &ovnv1.SubnetList{}
	err := v.cache.List(ctx, subnetList)
	if err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	for _, s := range subnetList.Items {
		for _, ns := range s.Spec.Namespaces {
			if ns == o.GetNamespace() {
				lsName = s.Name
				subnet = s
				break
			}
		}
	}
	// Get all logical switch port address
	lsps, err := v.ovnClient.GetLogicalSwitchPortByLogicalSwitch(lsName)
	if err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	var usedIPs []string
	for _, lsp := range lsps {
		addr, err := v.ovnClient.GetPortAddr(lsp)
		if err != nil {
			if err == ovs.ErrNoAddr {
				continue
			}
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}
		usedIPs = append(usedIPs, addr...)
	}
	// Get logical switch exclude ips
	excludeIPs, err := v.ovnClient.GetLogicalSwitchExcludeIPS(lsName)
	if err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	parsedExcludeIPs := util.ExpandExcludeIPs(excludeIPs, subnet.Spec.CIDRBlock)
	usedIPs = append(usedIPs, parsedExcludeIPs...)
	// Check static ips overlap
	if util.IsStringsOverlap([]string{staticIP}, usedIPs) {
		return ctrlwebhook.Denied("overlap")
	}
	newExcludeIPs := append(excludeIPs, staticIP)
	// Write back to exclude ips
	if err := v.ovnClient.SetLogicalSwitchExcludeIPS(lsName, newExcludeIPs); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	return ctrlwebhook.Allowed("by pass")
}

func (v *ValidatingHook) DeploymentUpdateHook(ctx context.Context, req admission.Request) admission.Response {
	o := appsv1.Deployment{}
	n := appsv1.Deployment{}
	if err := v.decoder.DecodeRaw(req.OldObject, &o); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	if err := v.decoder.DecodeRaw(req.Object, &n); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	// Get pod template static ips
	oldStaticIPSAnno := o.Spec.Template.GetAnnotations()[util.IpPoolAnnotation]
	newStaticIPSAnno := n.Spec.Template.GetAnnotations()[util.IpPoolAnnotation]
	klog.V(3).Infof("%s %s@%s, old ip_pool: %s, new ip_pool:%s", o.Kind, o.GetName(), o.GetNamespace(), oldStaticIPSAnno, newStaticIPSAnno)
	if len(util.DiffStringSlice(strings.Split(oldStaticIPSAnno, ","), strings.Split(newStaticIPSAnno, ","))) == 0 {
		return ctrlwebhook.Allowed("by pass")
	}
	return v.podControllerUpdate(ctx, oldStaticIPSAnno, newStaticIPSAnno, o.GetNamespace())
}

func (v *ValidatingHook) StatefulSetUpdateHook(ctx context.Context, req admission.Request) admission.Response {
	o := appsv1.StatefulSet{}
	n := appsv1.StatefulSet{}
	if err := v.decoder.DecodeRaw(req.OldObject, &o); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	if err := v.decoder.DecodeRaw(req.Object, &n); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	// Get pod template static ips
	oldStaticIPSAnno := o.Spec.Template.GetAnnotations()[util.IpPoolAnnotation]
	newStaticIPSAnno := n.Spec.Template.GetAnnotations()[util.IpPoolAnnotation]
	klog.V(3).Infof("%s %s@%s, old ip_pool: %s, new ip_pool:%s", o.Kind, o.GetName(), o.GetNamespace(), oldStaticIPSAnno, newStaticIPSAnno)
	if len(util.DiffStringSlice(strings.Split(oldStaticIPSAnno, ","), strings.Split(newStaticIPSAnno, ","))) == 0 {
		return ctrlwebhook.Allowed("by pass")
	}
	return v.podControllerUpdate(ctx, oldStaticIPSAnno, newStaticIPSAnno, o.GetNamespace())
}

func (v *ValidatingHook) DaemonSetUpdateHook(ctx context.Context, req admission.Request) admission.Response {
	o := appsv1.DaemonSet{}
	n := appsv1.DaemonSet{}
	if err := v.decoder.DecodeRaw(req.OldObject, &o); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	if err := v.decoder.DecodeRaw(req.Object, &n); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	// Get pod template static ips
	oldStaticIPSAnno := o.Spec.Template.GetAnnotations()[util.IpPoolAnnotation]
	newStaticIPSAnno := n.Spec.Template.GetAnnotations()[util.IpPoolAnnotation]
	klog.V(3).Infof("%s %s@%s, old ip_pool: %s, new ip_pool:%s", o.Kind, o.GetName(), o.GetNamespace(), oldStaticIPSAnno, newStaticIPSAnno)
	if len(util.DiffStringSlice(strings.Split(oldStaticIPSAnno, ","), strings.Split(newStaticIPSAnno, ","))) == 0 {
		return ctrlwebhook.Allowed("by pass")
	}
	return v.podControllerUpdate(ctx, oldStaticIPSAnno, newStaticIPSAnno, o.GetNamespace())
}

func (v *ValidatingHook) DeploymentDeleteHook(ctx context.Context, req admission.Request) admission.Response {
	o := appsv1.Deployment{}
	if err := v.client.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: req.Name}, &o); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	staticIPSAnno := o.Spec.Template.GetAnnotations()[util.IpPoolAnnotation]
	klog.V(3).Infof("%s %s@%s, ip_pool: %s", o.Kind, o.GetName(), o.GetNamespace(), staticIPSAnno)
	if staticIPSAnno == "" {
		return ctrlwebhook.Allowed("by pass")
	}
	return v.podControllerDelete(ctx, staticIPSAnno, o.GetNamespace())
}

func (v *ValidatingHook) StatefulSetDeleteHook(ctx context.Context, req admission.Request) admission.Response {
	o := appsv1.StatefulSet{}
	if err := v.client.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: req.Name}, &o); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	staticIPSAnno := o.Spec.Template.GetAnnotations()[util.IpPoolAnnotation]
	klog.V(3).Infof("%s %s@%s, ip_pool: %s", o.Kind, o.GetName(), o.GetNamespace(), staticIPSAnno)
	if staticIPSAnno == "" {
		return ctrlwebhook.Allowed("by pass")
	}
	return v.podControllerDelete(ctx, staticIPSAnno, o.GetNamespace())
}

func (v *ValidatingHook) DaemonSetDeleteHook(ctx context.Context, req admission.Request) admission.Response {
	o := appsv1.DaemonSet{}
	if err := v.client.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: req.Name}, &o); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	staticIPSAnno := o.Spec.Template.GetAnnotations()[util.IpPoolAnnotation]
	klog.V(3).Infof("%s %s@%s, ip_pool: %s", o.Kind, o.GetName(), o.GetNamespace(), staticIPSAnno)
	if staticIPSAnno == "" {
		return ctrlwebhook.Allowed("by pass")
	}
	return v.podControllerDelete(ctx, staticIPSAnno, o.GetNamespace())
}

func (v *ValidatingHook) PodDeleteHook(ctx context.Context, req admission.Request) admission.Response {
	o := corev1.Pod{}
	if err := v.client.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: req.Name}, &o); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	poolAnno := o.GetAnnotations()[util.IpPoolAnnotation]
	klog.V(3).Infof("%s %s@%s, ip_pool: %s", o.Kind, o.GetName(), o.GetNamespace(), poolAnno)
	if poolAnno != "" {
		return ctrlwebhook.Allowed("by pass")
	}
	staticIP := o.GetAnnotations()[util.IpAddressAnnotation]
	klog.V(3).Infof("%s %s@%s, ip_address: %s", o.Kind, o.GetName(), o.GetNamespace(), staticIP)
	if staticIP == "" {
		return ctrlwebhook.Allowed("by pass")
	}
	// Get logical switch name
	lsName := v.opt.DefaultLS
	subnetList := &ovnv1.SubnetList{}
	err := v.cache.List(ctx, subnetList)
	if err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	for _, subnet := range subnetList.Items {
		for _, ns := range subnet.Spec.Namespaces {
			if ns == o.GetNamespace() {
				lsName = subnet.Name
				break
			}
		}
	}
	// Get logical switch exclude ips
	excludeIPs, err := v.ovnClient.GetLogicalSwitchExcludeIPS(lsName)
	if err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	newExcludeIPs := []string{}
	for _, excludeIP := range excludeIPs {
		if excludeIP != staticIP {
			newExcludeIPs = append(newExcludeIPs, excludeIP)
		}
	}
	// Write back to exclude ips
	if err := v.ovnClient.SetLogicalSwitchExcludeIPS(lsName, newExcludeIPs); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	return ctrlwebhook.Allowed("by pass")
}

func (v *ValidatingHook) podControllerCreate(ctx context.Context, staticIPSAnno, namespace string) admission.Response {
	staticIPs := strings.Fields(strings.ReplaceAll(staticIPSAnno, ",", " "))
	// Get logical switch name
	lsName := v.opt.DefaultLS
	subnetList := &ovnv1.SubnetList{}
	var subnet ovnv1.Subnet
	err := v.cache.List(ctx, subnetList)
	if err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	for _, s := range subnetList.Items {
		for _, ns := range s.Spec.Namespaces {
			if ns == namespace {
				lsName = s.Name
				subnet = s
				break
			}
		}
	}
	// Get all logical switch port address
	lsps, err := v.ovnClient.GetLogicalSwitchPortByLogicalSwitch(lsName)
	if err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	var usedIPs []string
	for _, lsp := range lsps {
		addr, err := v.ovnClient.GetPortAddr(lsp)
		if err != nil {
			if err == ovs.ErrNoAddr {
				continue
			}
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}
		usedIPs = append(usedIPs, addr...)
	}
	// Get logical switch exclude ips
	excludeIPs, err := v.ovnClient.GetLogicalSwitchExcludeIPS(lsName)
	if err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	parsedExcludeIPs := util.ExpandExcludeIPs(excludeIPs, subnet.Spec.CIDRBlock)
	usedIPs = append(usedIPs, parsedExcludeIPs...)
	// Check static ips overlap
	if util.IsStringsOverlap(staticIPs, usedIPs) {
		return ctrlwebhook.Denied("overlap")
	}
	newExcludeIPs := append(excludeIPs, staticIPs...)
	// Write back to exclude ips
	if err := v.ovnClient.SetLogicalSwitchExcludeIPS(lsName, newExcludeIPs); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	return ctrlwebhook.Allowed("by pass")
}

func (v *ValidatingHook) podControllerUpdate(ctx context.Context, oldStaticIPSAnno, newStaticIPSAnno, namespace string) admission.Response {
	oldStaticIPs := strings.Fields(strings.ReplaceAll(oldStaticIPSAnno, ",", " "))
	newStaticIPs := strings.Fields(strings.ReplaceAll(newStaticIPSAnno, ",", " "))
	toDel := []string{}
	for _, oIP := range oldStaticIPs {
		found := false
		for _, nIP := range newStaticIPs {
			if oIP == nIP {
				found = true
				break
			}
		}
		if !found {
			toDel = append(toDel, oIP)
		}
	}
	toAdd := []string{}
	for _, nIP := range newStaticIPs {
		found := false
		for _, oIP := range oldStaticIPs {
			if nIP == oIP {
				found = true
				break
			}
		}
		if !found {
			toAdd = append(toAdd, nIP)
		}
	}
	// Get logical switch name
	lsName := v.opt.DefaultLS
	var subnet ovnv1.Subnet
	subnetList := &ovnv1.SubnetList{}
	err := v.cache.List(ctx, subnetList)
	if err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	for _, s := range subnetList.Items {
		for _, ns := range s.Spec.Namespaces {
			if ns == namespace {
				lsName = s.Name
				subnet = s
				break
			}
		}
	}
	// Get logical switch exclude ips
	excludeIPs, err := v.ovnClient.GetLogicalSwitchExcludeIPS(lsName)
	if err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	parsedExcludeIPs := util.ExpandExcludeIPs(excludeIPs, subnet.Spec.CIDRBlock)
	// Check static ips overlap
	if util.IsStringsOverlap(toAdd, parsedExcludeIPs) {
		return ctrlwebhook.Denied("overlap")
	}
	newExcludeIPs := []string{}
	for _, excludeIP := range excludeIPs {
		found := false
		for _, dIP := range toDel {
			if dIP == excludeIP {
				found = true
				break
			}
		}
		if !found {
			newExcludeIPs = append(newExcludeIPs, excludeIP)
		}
	}
	newExcludeIPs = append(newExcludeIPs, toAdd...)
	// Write back to exclude ips
	if err := v.ovnClient.SetLogicalSwitchExcludeIPS(lsName, newExcludeIPs); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	return ctrlwebhook.Allowed("by pass")
}

func (v *ValidatingHook) podControllerDelete(ctx context.Context, staticIPSAnno, namespace string) admission.Response {
	staticIPs := strings.Fields(strings.ReplaceAll(staticIPSAnno, ",", " "))
	// Get logical switch name
	lsName := v.opt.DefaultLS
	subnetList := &ovnv1.SubnetList{}
	err := v.cache.List(ctx, subnetList)
	if err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	for _, subnet := range subnetList.Items {
		for _, ns := range subnet.Spec.Namespaces {
			if ns == namespace {
				lsName = subnet.Name
				break
			}
		}
	}
	// Get logical switch exclude ips
	excludeIPs, err := v.ovnClient.GetLogicalSwitchExcludeIPS(lsName)
	if err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	newExcludeIPs := []string{}
	for _, excludeIP := range excludeIPs {
		found := false
		for _, staticIP := range staticIPs {
			if excludeIP == staticIP {
				found = true
				break
			}
		}
		if !found {
			newExcludeIPs = append(newExcludeIPs, excludeIP)
		}
	}
	// Write back to exclude ips
	if err := v.ovnClient.SetLogicalSwitchExcludeIPS(lsName, newExcludeIPs); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	return ctrlwebhook.Allowed("by pass")
}
