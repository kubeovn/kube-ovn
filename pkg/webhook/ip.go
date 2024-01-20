package webhook

import (
	"context"
	"fmt"
	"net"
	"net/http"

	ovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var ipGVK = metav1.GroupVersionKind{Group: ovnv1.SchemeGroupVersion.Group, Version: ovnv1.SchemeGroupVersion.Version, Kind: "IP"}

func (v *ValidatingHook) IPCreateHook(ctx context.Context, req admission.Request) admission.Response {
	ip := ovnv1.IP{}
	if err := v.decoder.DecodeRaw(req.Object, &ip); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if err := v.ValidateIP(ctx, &ip); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	return ctrlwebhook.Allowed("by pass")
}

func (v *ValidatingHook) IPUpdateHook(ctx context.Context, req admission.Request) admission.Response {
	ipOld := ovnv1.IP{}
	if err := v.decoder.DecodeRaw(req.OldObject, &ipOld); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	ipNew := ovnv1.IP{}
	if err := v.decoder.DecodeRaw(req.Object, &ipNew); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	// ip can not change these specs below
	if ipNew.Spec.Namespace != ipOld.Spec.Namespace {
		err := fmt.Errorf("ip %s namespace can not change", ipNew.Name)
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	if ipNew.Spec.PodName != ipOld.Spec.PodName {
		err := fmt.Errorf("ip %s podName can not change", ipNew.Name)
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	if ipNew.Spec.Subnet != ipOld.Spec.Subnet {
		err := fmt.Errorf("ip %s subnet can not change", ipNew.Name)
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	if ipNew.Spec.PodType != ipOld.Spec.PodType {
		err := fmt.Errorf("ip %s podType can not change", ipNew.Name)
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	return ctrlwebhook.Allowed("by pass")
}

func (v *ValidatingHook) ValidateIP(ctx context.Context, ip *ovnv1.IP) error {
	if ip.Spec.Subnet == "" {
		err := fmt.Errorf("subnet parameter cannot be empty")
		return err
	}

	subnet := &ovnv1.Subnet{}
	key := types.NamespacedName{Name: ip.Spec.Subnet}
	if err := v.cache.Get(ctx, key, subnet); err != nil {
		return err
	}

	if ip.Spec.V4IPAddress != "" {
		if net.ParseIP(ip.Spec.V4IPAddress) == nil {
			err := fmt.Errorf("%s is not a valid ip", ip.Spec.V4IPAddress)
			return err
		}

		if !util.CIDRContainIP(subnet.Spec.CIDRBlock, ip.Spec.V4IPAddress) {
			err := fmt.Errorf("the V4ip %s is not in the range of subnet %s, cidr %v",
				ip.Spec.V4IPAddress, subnet.Name, subnet.Spec.CIDRBlock)
			return err
		}
	}

	if ip.Spec.V6IPAddress != "" {
		if net.ParseIP(ip.Spec.V6IPAddress) == nil {
			err := fmt.Errorf("%s is not a valid ip", ip.Spec.V6IPAddress)
			return err
		}

		if !util.CIDRContainIP(subnet.Spec.CIDRBlock, ip.Spec.V6IPAddress) {
			err := fmt.Errorf("the ip %s is not in the range of subnet %s, cidr %v",
				ip.Spec.V6IPAddress, subnet.Name, subnet.Spec.CIDRBlock)
			return err
		}
	}

	if ip.Spec.NodeName != "" {
		// node ip skip later neccessary specs check
		return nil
	}

	// invalid ip who has no namespace, name, subnet, podType, no need to handle it here
	if ip.Spec.Namespace == "" || ip.Spec.PodName == "" || ip.Spec.Subnet == "" || ip.Spec.PodType == "" {
		err := fmt.Errorf("invalid ip %s, ip spec should set namespace podName subnet podType", ip.Name)
		return err
	}
	return nil
}
