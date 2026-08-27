package webhook

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	ovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

var vtepBindingGVK = ovnv1.SchemeGroupVersion.WithKind(util.KindVtepBinding)

func (v *ValidatingHook) VtepBindingCreateHook(ctx context.Context, req admission.Request) admission.Response {
	binding := ovnv1.VtepBinding{}
	if err := v.decoder.DecodeRaw(req.Object, &binding); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	if err := v.ValidateVtepBinding(ctx, &binding, nil); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	return ctrlwebhook.Allowed("bypass")
}

func (v *ValidatingHook) VtepBindingUpdateHook(ctx context.Context, req admission.Request) admission.Response {
	oldBinding := ovnv1.VtepBinding{}
	if err := v.decoder.DecodeRaw(req.OldObject, &oldBinding); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	newBinding := ovnv1.VtepBinding{}
	if err := v.decoder.DecodeRaw(req.Object, &newBinding); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	if err := v.ValidateVtepBinding(ctx, &newBinding, &oldBinding); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	return ctrlwebhook.Allowed("bypass")
}

func (v *ValidatingHook) ValidateVtepBinding(ctx context.Context, binding, oldBinding *ovnv1.VtepBinding) error {
	if binding.Spec.Subnet == "" {
		return errors.New("subnet parameter cannot be empty")
	}
	if binding.Spec.PhysicalSwitch == "" {
		return errors.New("physicalSwitch parameter cannot be empty")
	}
	if binding.Spec.PhysicalPort == "" {
		return errors.New("physicalPort parameter cannot be empty")
	}
	if binding.Spec.VlanID < 0 || binding.Spec.VlanID > 4095 {
		return fmt.Errorf("vlanID %d is invalid, must be between 0 and 4095", binding.Spec.VlanID)
	}

	if oldBinding != nil {
		if binding.Spec.Subnet != oldBinding.Spec.Subnet {
			return errors.New("subnet is immutable")
		}
		if binding.Spec.PhysicalSwitch != oldBinding.Spec.PhysicalSwitch {
			return errors.New("physicalSwitch is immutable")
		}
		if binding.Spec.VtepLogicalSwitch != oldBinding.Spec.VtepLogicalSwitch {
			return errors.New("vtepLogicalSwitch is immutable")
		}
		if binding.Spec.PhysicalPort != oldBinding.Spec.PhysicalPort {
			return errors.New("physicalPort is immutable")
		}
		if binding.Spec.VlanID != oldBinding.Spec.VlanID {
			return errors.New("vlanID is immutable")
		}
	}

	subnet := &ovnv1.Subnet{}
	if err := v.cache.Get(ctx, client.ObjectKey{Name: binding.Spec.Subnet}, subnet); err != nil {
		return fmt.Errorf("failed to get subnet %s: %w", binding.Spec.Subnet, err)
	}

	bindingList := &ovnv1.VtepBindingList{}
	if err := v.cache.List(ctx, bindingList, &client.ListOptions{LabelSelector: labels.Everything()}); err != nil {
		return fmt.Errorf("failed to list vtep bindings: %w", err)
	}
	for i := range bindingList.Items {
		if err := ovnv1.VtepBindingConflict(binding, &bindingList.Items[i]); err != nil {
			return err
		}
	}
	return nil
}
