package webhook

import (
	"context"
	"fmt"
	ovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"net"
	"net/http"
	"reflect"
	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var vipGVK = metav1.GroupVersionKind{Group: ovnv1.SchemeGroupVersion.Group, Version: ovnv1.SchemeGroupVersion.Version, Kind: "Vip"}

func (v *ValidatingHook) VipCreateHook(ctx context.Context, req admission.Request) admission.Response {
	vip := ovnv1.Vip{}
	if err := v.decoder.DecodeRaw(req.Object, &vip); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if err := v.ValidateVip(ctx, &vip); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	return ctrlwebhook.Allowed("by pass")
}

func (v *ValidatingHook) VipUpdateHook(ctx context.Context, req admission.Request) admission.Response {
	vipOld := ovnv1.Vip{}
	if err := v.decoder.DecodeRaw(req.OldObject, &vipOld); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	vipNew := ovnv1.Vip{}
	if err := v.decoder.DecodeRaw(req.Object, &vipNew); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if !reflect.DeepEqual(vipNew.Spec, vipOld.Spec) {
		if vipOld.Status.Mac == "" {
			if err := v.ValidateVip(ctx, &vipNew); err != nil {
				return ctrlwebhook.Errored(http.StatusBadRequest, err)
			}
		} else {
			err := fmt.Errorf("vip has been assigned,not support change")
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}
	}
	return ctrlwebhook.Allowed("by pass")
}

func (v *ValidatingHook) ValidateVip(ctx context.Context, vip *ovnv1.Vip) error {
	if vip.Spec.Subnet == "" {
		err := fmt.Errorf("subnet parameter cannot be empty")
		return err
	}

	subnet := &ovnv1.Subnet{}
	key := types.NamespacedName{Name: vip.Spec.Subnet}
	if err := v.cache.Get(ctx, key, subnet); err != nil {
		return err
	}

	if vip.Spec.V4ip != "" {
		if net.ParseIP(vip.Spec.V4ip) == nil {
			err := fmt.Errorf("%s is not a valid ip", vip.Spec.V4ip)
			return err
		}

		if !util.CIDRContainIP(subnet.Spec.CIDRBlock, vip.Spec.V4ip) {
			err := fmt.Errorf("the V4ip %s is not in the range of subnet %s, cidr %v",
				vip.Spec.V4ip, subnet.Name, subnet.Spec.CIDRBlock)
			return err
		}
	}

	if vip.Spec.V6ip != "" {
		if net.ParseIP(vip.Spec.V6ip) == nil {
			err := fmt.Errorf("%s is not a valid ip", vip.Spec.V6ip)
			return err
		}

		if !util.CIDRContainIP(subnet.Spec.CIDRBlock, vip.Spec.V6ip) {
			err := fmt.Errorf("the vip %s is not in the range of subnet %s, cidr %v",
				vip.Spec.V6ip, subnet.Name, subnet.Spec.CIDRBlock)
			return err
		}
	}
	return nil
}
