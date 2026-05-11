package webhook

import (
	"context"
	"errors"
	"net/http"

	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	ovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
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
			err := errors.New("vpc and subnet cannot have the same name")
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}
	}

	if err := util.ValidateVpc(&vpc); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	return ctrlwebhook.Allowed("bypass")
}

func (v *ValidatingHook) VpcUpdateHook(_ context.Context, req admission.Request) admission.Response {
	vpc := ovnv1.Vpc{}
	if err := v.decoder.DecodeRaw(req.Object, &vpc); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if err := util.ValidateVpc(&vpc); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	return ctrlwebhook.Allowed("bypass")
}

func (v *ValidatingHook) VpcDeleteHook(ctx context.Context, req admission.Request) admission.Response {
	vpc := ovnv1.Vpc{}
	if err := v.decoder.DecodeRaw(req.OldObject, &vpc); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	if len(vpc.Status.Subnets) != 0 {
		return ctrlwebhook.Denied("can't delete vpc when any subnet in the vpc")
	}

	dnatList := &ovnv1.OvnDnatRuleList{}
	if err := v.cache.List(ctx, dnatList); err != nil {
		return ctrlwebhook.Errored(http.StatusInternalServerError, err)
	}
	for _, item := range dnatList.Items {
		if item.Spec.Vpc == vpc.Name {
			return ctrlwebhook.Denied("can't delete vpc when OvnDnatRules still reference it, delete them first")
		}
	}

	snatList := &ovnv1.OvnSnatRuleList{}
	if err := v.cache.List(ctx, snatList); err != nil {
		return ctrlwebhook.Errored(http.StatusInternalServerError, err)
	}
	for _, item := range snatList.Items {
		if item.Spec.Vpc == vpc.Name {
			return ctrlwebhook.Denied("can't delete vpc when OvnSnatRules still reference it, delete them first")
		}
	}

	return ctrlwebhook.Allowed("bypass")
}
