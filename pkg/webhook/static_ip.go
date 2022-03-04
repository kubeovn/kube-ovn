package webhook

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	ovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

var (
	deploymentGVK  = metav1.GroupVersionKind{Group: appsv1.SchemeGroupVersion.Group, Version: appsv1.SchemeGroupVersion.Version, Kind: "Deployment"}
	statefulSetGVK = metav1.GroupVersionKind{Group: appsv1.SchemeGroupVersion.Group, Version: appsv1.SchemeGroupVersion.Version, Kind: "StatefulSet"}
	daemonSetGVK   = metav1.GroupVersionKind{Group: appsv1.SchemeGroupVersion.Group, Version: appsv1.SchemeGroupVersion.Version, Kind: "DaemonSet"}
	podGVK         = metav1.GroupVersionKind{Group: corev1.SchemeGroupVersion.Group, Version: corev1.SchemeGroupVersion.Version, Kind: "Pod"}
	subnetGVK      = metav1.GroupVersionKind{Group: ovnv1.SchemeGroupVersion.Group, Version: ovnv1.SchemeGroupVersion.Version, Kind: "Subnet"}
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
	return v.validateIp(ctx, o.Spec.Template.GetAnnotations(), o.Kind, o.GetName(), o.GetNamespace())
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
	return v.validateIp(ctx, o.Spec.Template.GetAnnotations(), o.Kind, o.GetName(), o.GetNamespace())
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
	return v.validateIp(ctx, o.Spec.Template.GetAnnotations(), o.Kind, o.GetName(), o.GetNamespace())
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
	return v.validateIp(ctx, o.GetAnnotations(), o.Kind, o.GetName(), o.GetNamespace())
}

func (v *ValidatingHook) validateIp(ctx context.Context, annotations map[string]string, kind, name, namespace string) admission.Response {
	if err := util.ValidatePodNetwork(annotations); err != nil {
		klog.Errorf("validate %s %s/%s failed: %v", kind, namespace, name, err)
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	ipList := &ovnv1.IPList{}
	if err := v.cache.List(ctx, ipList); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	if err := v.validateIPConflict(annotations, ipList.Items); err != nil {
		return ctrlwebhook.Denied(err.Error())
	}

	return ctrlwebhook.Allowed("by pass")
}

func (v *ValidatingHook) validateIPConflict(annotations map[string]string, ipList []ovnv1.IP) error {
	annoSubnet := annotations[util.LogicalSwitchAnnotation]
	if annotations[util.LogicalSwitchAnnotation] == "" {
		annoSubnet = util.DefaultSubnet
	}

	if ipAddress := annotations[util.IpAddressAnnotation]; ipAddress != "" {
		if err := v.checkIPConflict(ipAddress, annoSubnet, ipList); err != nil {
			return err
		}
	}

	ipPool := annotations[util.IpPoolAnnotation]
	if ipPool != "" {
		if err := v.checkIPConflict(ipPool, annoSubnet, ipList); err != nil {
			return err
		}
	}
	return nil
}

func (v *ValidatingHook) checkIPConflict(ipAddress, annoSubnet string, ipList []ovnv1.IP) error {
	var ipAddr net.IP
	for _, ip := range strings.Split(ipAddress, ",") {
		if strings.Contains(ip, "/") {
			ipAddr, _, _ = net.ParseCIDR(ip)
		} else {
			ipAddr = net.ParseIP(strings.TrimSpace(ip))
		}

		for _, ipCr := range ipList {
			if annoSubnet != "" && ipCr.Spec.Subnet != annoSubnet {
				continue
			}

			v4IP, v6IP := util.SplitStringIP(ipCr.Spec.IPAddress)
			if ipAddr.String() == v4IP || ipAddr.String() == v6IP {
				err := fmt.Errorf("annotation static-ip %s is conflict with ip crd %s, ip %s", ipAddr.String(), ipCr.Name, ipCr.Spec.IPAddress)
				return err
			}
		}
	}

	return nil
}
