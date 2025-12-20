package webhook

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	kubevirtv1 "kubevirt.io/api/core/v1"
	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	ovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

var (
	deploymentGVK  = metav1.GroupVersionKind{Group: appsv1.SchemeGroupVersion.Group, Version: appsv1.SchemeGroupVersion.Version, Kind: "Deployment"}
	statefulSetGVK = metav1.GroupVersionKind{Group: appsv1.SchemeGroupVersion.Group, Version: appsv1.SchemeGroupVersion.Version, Kind: "StatefulSet"}
	daemonSetGVK   = metav1.GroupVersionKind{Group: appsv1.SchemeGroupVersion.Group, Version: appsv1.SchemeGroupVersion.Version, Kind: "DaemonSet"}
	jobSetGVK      = metav1.GroupVersionKind{Group: batchv1.SchemeGroupVersion.Group, Version: batchv1.SchemeGroupVersion.Version, Kind: "Job"}
	cornJobSetGVK  = metav1.GroupVersionKind{Group: batchv1.SchemeGroupVersion.Group, Version: batchv1.SchemeGroupVersion.Version, Kind: "CronJob"}
	podGVK         = metav1.GroupVersionKind{Group: corev1.SchemeGroupVersion.Group, Version: corev1.SchemeGroupVersion.Version, Kind: "Pod"}
	subnetGVK      = metav1.GroupVersionKind{Group: ovnv1.SchemeGroupVersion.Group, Version: ovnv1.SchemeGroupVersion.Version, Kind: "Subnet"}
	vpcGVK         = metav1.GroupVersionKind{Group: ovnv1.SchemeGroupVersion.Group, Version: ovnv1.SchemeGroupVersion.Version, Kind: "Vpc"}
)

func (v *ValidatingHook) DeploymentCreateHook(ctx context.Context, req admission.Request) admission.Response {
	o := appsv1.Deployment{}
	if err := v.decoder.Decode(req, &o); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	// Get pod template static ips
	staticIPSAnno := o.Spec.Template.GetAnnotations()[util.IPPoolAnnotation]
	klog.V(3).Infof("%s %s@%s, ip_pool: %s", o.Kind, o.GetName(), o.GetNamespace(), staticIPSAnno)
	if staticIPSAnno == "" {
		return ctrlwebhook.Allowed("by pass")
	}
	return v.validateIP(ctx, o.Spec.Template.GetAnnotations(), o.Kind, o.GetName(), o.GetNamespace())
}

func (v *ValidatingHook) StatefulSetCreateHook(ctx context.Context, req admission.Request) admission.Response {
	o := appsv1.StatefulSet{}
	if err := v.decoder.Decode(req, &o); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	// Get pod template static ips
	staticIPSAnno := o.Spec.Template.GetAnnotations()[util.IPPoolAnnotation]
	klog.V(3).Infof("%s %s@%s, ip_pool: %s", o.Kind, o.GetName(), o.GetNamespace(), staticIPSAnno)
	if staticIPSAnno == "" {
		return ctrlwebhook.Allowed("by pass")
	}
	return v.validateIP(ctx, o.Spec.Template.GetAnnotations(), o.Kind, o.GetName(), o.GetNamespace())
}

func (v *ValidatingHook) DaemonSetCreateHook(ctx context.Context, req admission.Request) admission.Response {
	o := appsv1.DaemonSet{}
	if err := v.decoder.Decode(req, &o); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	// Get pod template static ips
	staticIPSAnno := o.Spec.Template.GetAnnotations()[util.IPPoolAnnotation]
	klog.V(3).Infof("%s %s@%s, ip_pool: %s", o.Kind, o.GetName(), o.GetNamespace(), staticIPSAnno)
	if staticIPSAnno == "" {
		return ctrlwebhook.Allowed("by pass")
	}
	return v.validateIP(ctx, o.Spec.Template.GetAnnotations(), o.Kind, o.GetName(), o.GetNamespace())
}

func (v *ValidatingHook) JobSetCreateHook(ctx context.Context, req admission.Request) admission.Response {
	o := batchv1.Job{}
	if err := v.decoder.Decode(req, &o); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	// Get pod template static ips
	staticIPSAnno := o.Spec.Template.GetAnnotations()[util.IPPoolAnnotation]
	klog.V(3).Infof("%s %s@%s, ip_pool: %s", o.Kind, o.GetName(), o.GetNamespace(), staticIPSAnno)
	if staticIPSAnno == "" {
		return ctrlwebhook.Allowed("by pass")
	}
	return v.validateIP(ctx, o.Spec.Template.GetAnnotations(), o.Kind, o.GetName(), o.GetNamespace())
}

func (v *ValidatingHook) CornJobSetCreateHook(ctx context.Context, req admission.Request) admission.Response {
	o := batchv1.CronJob{}
	if err := v.decoder.Decode(req, &o); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	// Get pod template static ips
	staticIPSAnno := o.Spec.JobTemplate.Spec.Template.GetAnnotations()[util.IPPoolAnnotation]
	klog.V(3).Infof("%s %s@%s, ip_pool: %s", o.Kind, o.GetName(), o.GetNamespace(), staticIPSAnno)
	if staticIPSAnno == "" {
		return ctrlwebhook.Allowed("by pass")
	}
	return v.validateIP(ctx, o.Spec.JobTemplate.Spec.Template.GetAnnotations(), o.Kind, o.GetName(), o.GetNamespace())
}

func (v *ValidatingHook) PodCreateHook(ctx context.Context, req admission.Request) admission.Response {
	o := corev1.Pod{}
	if err := v.decoder.Decode(req, &o); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	poolAnno := o.GetAnnotations()[util.IPPoolAnnotation]
	klog.V(3).Infof("%s %s@%s, ip_pool: %s", o.Kind, o.GetName(), o.GetNamespace(), poolAnno)

	staticIP := o.GetAnnotations()[util.IPAddressAnnotation]
	klog.V(3).Infof("%s %s@%s, ip_address: %s", o.Kind, o.GetName(), o.GetNamespace(), staticIP)
	if staticIP == "" && poolAnno == "" {
		return ctrlwebhook.Allowed("by pass")
	}
	if v.allowLiveMigration(o.GetAnnotations()) {
		return ctrlwebhook.Allowed("by pass")
	}
	name := o.GetName()
	// If the pod is created by a VM, we need to get the VM name from owner references
	for _, owner := range o.GetOwnerReferences() {
		if owner.Kind == util.KindVirtualMachineInstance &&
			strings.HasPrefix(owner.APIVersion, kubevirtv1.SchemeGroupVersion.Group+"/") {
			name = owner.Name
			klog.V(3).Infof("pod %s is created by vm %s", o.GetName(), name)
			break
		}
	}
	return v.validateIP(ctx, o.GetAnnotations(), o.Kind, name, o.GetNamespace())
}

func (v *ValidatingHook) allowLiveMigration(annotations map[string]string) bool {
	if _, ok := annotations[kubevirtv1.MigrationJobNameAnnotation]; ok {
		return true
	}
	return false
}

func (v *ValidatingHook) validateIP(ctx context.Context, annotations map[string]string, kind, name, namespace string) admission.Response {
	if err := util.ValidatePodNetwork(annotations); err != nil {
		klog.Errorf("validate %s %s/%s failed: %v", kind, namespace, name, err)
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	ipList := &ovnv1.IPList{}
	if err := v.cache.List(ctx, ipList); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	if err := v.validateIPConflict(ctx, annotations, name, ipList.Items); err != nil {
		return ctrlwebhook.Denied(err.Error())
	}

	return ctrlwebhook.Allowed("by pass")
}

func (v *ValidatingHook) validateIPConflict(ctx context.Context, annotations map[string]string, name string, ipList []ovnv1.IP) error {
	annoSubnet := annotations[util.LogicalSwitchAnnotation]
	if annotations[util.LogicalSwitchAnnotation] == "" {
		annoSubnet = util.DefaultSubnet
	}

	if ipAddress := annotations[util.IPAddressAnnotation]; ipAddress != "" {
		if err := v.checkIPConflict(ipAddress, annoSubnet, name, ipList); err != nil {
			return err
		}
	}

	ipPool := annotations[util.IPPoolAnnotation]
	if ipPool != "" {
		if !strings.ContainsRune(ipPool, ',') && net.ParseIP(ipPool) == nil {
			pool := &ovnv1.IPPool{}
			if err := v.cache.Get(ctx, types.NamespacedName{Name: ipPool}, pool); err != nil {
				return fmt.Errorf("ippool %q not found", ipPool)
			}
		} else if err := v.checkIPConflict(ipPool, annoSubnet, name, ipList); err != nil {
			return err
		}
	}
	return nil
}

func (v *ValidatingHook) checkIPConflict(ipAddress, annoSubnet, name string, ipList []ovnv1.IP) error {
	var ipAddr net.IP
	for ip := range strings.SplitSeq(ipAddress, ",") {
		if strings.Contains(ip, "/") {
			ipAddr, _, _ = net.ParseCIDR(ip)
		} else {
			ipAddr = net.ParseIP(strings.TrimSpace(ip))
		}
		if ipAddr == nil {
			return fmt.Errorf("invalid static ip/ippool annotation value: %s", ipAddress)
		}

		for _, ipCR := range ipList {
			if annoSubnet != "" && ipCR.Spec.Subnet != annoSubnet {
				continue
			}

			v4IP, v6IP := util.SplitStringIP(ipCR.Spec.IPAddress)
			// v6 ip address can not use upper case
			if util.ContainsUppercase(v6IP) {
				err := fmt.Errorf("v6 ip address %s can not contain upper case", v6IP)
				klog.Error(err)
				return err
			}
			if ipAddr.String() == v4IP || ipAddr.String() == v6IP {
				// The IP's spec podName does not equal the Pod name in the request;
				// The two names have a containment relationship.
				if name == ipCR.Spec.PodName {
					klog.Infof("get same ip crd for %s", name)
				} else {
					err := fmt.Errorf("annotation static-ip %s is conflict with ip crd %s, ip %s", ipAddr.String(), ipCR.Name, ipCR.Spec.IPAddress)
					return err
				}
			}
		}
	}

	return nil
}
