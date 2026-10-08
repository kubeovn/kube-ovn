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
		switch {
		case vipOld.Status.Mac == "":
			if err := v.ValidateVip(ctx, &vipNew); err != nil {
				return ctrlwebhook.Errored(http.StatusBadRequest, err)
			}
		case v.isLegacySwitchLBVipMacRepair(ctx, req, &vipOld, &vipNew):
			// allow: the controller is renewing the mac of a switch_lb_rule vip created
			// before the own-mac fix, whose mac was forced to the subnet gateway mac
		default:
			err := errors.New("vip has been assigned, does not support change")
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}
	}
	return ctrlwebhook.Allowed("bypass")
}

// isLegacySwitchLBVipMacRepair reports whether req is the kube-ovn controller performing
// the one-time repair of a switch_lb_rule vip whose mac was forced to the subnet gateway
// mac by a historical bug (see handleAddVirtualIP/needsSwitchLBRuleMacRepair). Three
// independent conditions must all hold, so the normal immutability contract still applies
// to every other caller, field, vip type, and once a vip has been repaired:
//   - the request is authenticated as the controller's own service account (not any of
//     the other kube-ovn components that share identities with it);
//   - MacAddress is the sole spec field being changed;
//   - the vip's old mac actually is the subnet's gateway mac, i.e. it genuinely is a
//     pre-fix collision and not an arbitrary mac change. Subnet.Status.GatewayMAC no
//     longer equals vip.Status.Mac once the repair lands, which makes the exemption
//     self-expiring without any separate one-time marker.
func (v *ValidatingHook) isLegacySwitchLBVipMacRepair(ctx context.Context, req admission.Request, vipOld, vipNew *ovnv1.Vip) bool {
	if v.controllerUserName == "" || req.UserInfo.Username != v.controllerUserName {
		return false
	}
	if vipOld.Spec.Type != util.SwitchLBRuleVip || vipNew.Spec.Type != util.SwitchLBRuleVip {
		return false
	}
	specWithRenewedMac := vipOld.Spec.DeepCopy()
	specWithRenewedMac.MacAddress = vipNew.Spec.MacAddress
	if !reflect.DeepEqual(*specWithRenewedMac, vipNew.Spec) {
		return false
	}
	if vipNew.Spec.MacAddress == "" || vipNew.Spec.MacAddress == vipOld.Spec.MacAddress {
		return false
	}

	subnet := &ovnv1.Subnet{}
	if err := v.cache.Get(ctx, client.ObjectKey{Name: vipOld.Spec.Subnet}, subnet); err != nil {
		return false
	}
	return subnet.Status.GatewayMAC != "" && subnet.Status.GatewayMAC == vipOld.Status.Mac
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
