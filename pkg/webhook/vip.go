package webhook

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"reflect"

	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	ovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

var vipGVK = ovnv1.SchemeGroupVersion.WithKind(util.KindVip)

func (v *ValidatingHook) VipCreateHook(ctx context.Context, req admission.Request) admission.Response {
	vip := ovnv1.Vip{}
	if err := v.decoder.DecodeRaw(req.Object, &vip); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if err := v.ValidateVip(ctx, &vip); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	return ctrlwebhook.Allowed("bypass")
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
			err := errors.New("vip has been assigned, does not support change")
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}
	}
	return ctrlwebhook.Allowed("bypass")
}

func (v *ValidatingHook) ValidateVip(ctx context.Context, vip *ovnv1.Vip) error {
	if vip.Spec.Subnet == "" {
		return errors.New("subnet parameter cannot be empty")
	}

	subnet := &ovnv1.Subnet{}
	key := client.ObjectKey{Name: vip.Spec.Subnet}
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
		// v6 ip address can not use upper case
		if util.ContainsUppercase(vip.Spec.V6ip) {
			err := fmt.Errorf("vip %s v6 ip address %s can not contain upper case", vip.Name, vip.Spec.V6ip)
			return err
		}
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
