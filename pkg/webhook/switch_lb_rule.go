package webhook

import (
	"context"
	"fmt"
	ovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"net/http"
	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func (v *ValidatingHook) switchLBRuleCreateHook(ctx context.Context, req admission.Request) admission.Response {
	o := ovnv1.SwitchLBRule{}
	if err := v.decoder.Decode(req, &o); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if ok := util.IsValidIP(o.Spec.Vip); !ok {
		return ctrlwebhook.Denied(fmt.Sprintf("%s is an invalid ip", o.Spec.Vip))
	}

	if len(o.Spec.Selector) == 0 {
		return ctrlwebhook.Denied(fmt.Sprintf("don't empty the spec.selector"))
	}

	if len(o.Spec.Ports) == 0 {
		return ctrlwebhook.Denied(fmt.Sprintf("don't empty the spec.Ports"))
	}

	list := &ovnv1.SwitchLBRuleList{}
	if err := v.cache.List(ctx, list); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	for _, item := range list.Items {
		if item.Spec.Namespace == o.Spec.Namespace &&
			item.Spec.Vip == o.Spec.Vip {
			return ctrlwebhook.Denied(fmt.Sprintf("vip:%s in the ns:%s is already in use", o.Spec.Vip, o.Spec.Namespace))
		}
	}

	return ctrlwebhook.Allowed("by pass")
}

func (v *ValidatingHook) switchLBRuleUpdateHook(ctx context.Context, req admission.Request) admission.Response {
	o := ovnv1.SwitchLBRule{}
	if err := v.decoder.Decode(req, &o); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if len(o.Spec.Selector) == 0 {
		return ctrlwebhook.Denied(fmt.Sprintf("don't empty the spec.selector"))
	}

	if len(o.Spec.Ports) == 0 {
		return ctrlwebhook.Denied(fmt.Sprintf("don't empty the spec.Ports"))
	}

	oldSlr := ovnv1.SwitchLBRule{}
	if err := v.decoder.DecodeRaw(req.OldObject, &oldSlr); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if oldSlr.Spec.Vip != o.Spec.Vip {
		return ctrlwebhook.Denied(fmt.Sprintf("invalid value: vip:%s, may not change once set", o.Spec.Vip))
	}

	if oldSlr.Spec.Namespace != o.Spec.Namespace {
		return ctrlwebhook.Denied(fmt.Sprintf("invalid value: namespace:%s, may not change once set", o.Spec.Namespace))
	}

	return ctrlwebhook.Allowed("by pass")
}
