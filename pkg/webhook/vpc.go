package webhook

import (
	"context"
	"fmt"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"net/http"

	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	ovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func (v *ValidatingHook) VpcCreateHook(ctx context.Context, req admission.Request) admission.Response {
	vpc := ovnv1.Vpc{}
	if err := v.decoder.DecodeRaw(req.Object, &vpc); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	subnetList := &ovnv1.SubnetList{}
	if err := v.cache.List(ctx, subnetList); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	for _, item := range subnetList.Items {
		if item.Name == vpc.Name {
			err := fmt.Errorf("vpc and subnet cannot have the same name")
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}
	}

	if err := util.ValidateVpc(&vpc); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	return ctrlwebhook.Allowed("by pass")
}

func (v *ValidatingHook) VpcUpdateHook(ctx context.Context, req admission.Request) admission.Response {
	vpc := ovnv1.Vpc{}
	if err := v.decoder.DecodeRaw(req.Object, &vpc); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if err := util.ValidateVpc(&vpc); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	return ctrlwebhook.Allowed("by pass")
}

func (v *ValidatingHook) VpcDeleteHook(ctx context.Context, req admission.Request) admission.Response {
	vpc := ovnv1.Vpc{}
	if err := v.decoder.DecodeRaw(req.OldObject, &vpc); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	if 0 != len(vpc.Status.Subnets) {
		err := fmt.Errorf("can't delete vpc when any subnet in the vpc")
		return ctrlwebhook.Denied(err.Error())
	}
	return ctrlwebhook.Allowed("by pass")
}
