package webhook

import (
	"context"
	"errors"
	"fmt"
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

	return ctrlwebhook.Allowed("by pass")
}

func (v *ValidatingHook) VpcUpdateHook(_ context.Context, req admission.Request) admission.Response {
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
	if len(vpc.Status.Subnets) != 0 {
		return ctrlwebhook.Denied("can't delete vpc when any subnet in the vpc")
	}

	dnatList := &ovnv1.OvnDnatRuleList{}
	if err := v.cache.List(ctx, dnatList); err != nil {
		return ctrlwebhook.Errored(http.StatusInternalServerError, err)
	}
	// Use Status.Vpc first as vpc may be inferred from spec.ipName if not in spec.
	for _, item := range dnatList.Items {
		referencedVpc := item.Status.Vpc
		if referencedVpc == "" {
			referencedVpc = item.Spec.Vpc
		}
		if referencedVpc == vpc.Name {
			return ctrlwebhook.Denied(fmt.Sprintf("can't delete vpc %q: still referenced by OvnDnatRule %q", vpc.Name, item.Name))
		}
	}

	snatList := &ovnv1.OvnSnatRuleList{}
	if err := v.cache.List(ctx, snatList); err != nil {
		return ctrlwebhook.Errored(http.StatusInternalServerError, err)
	}
	// Use Status.Vpc first as vpc may be inferred from spec.ipName if not in spec.
	for _, item := range snatList.Items {
		referencedVpc := item.Status.Vpc
		if referencedVpc == "" {
			referencedVpc = item.Spec.Vpc
		}
		if referencedVpc == vpc.Name {
			return ctrlwebhook.Denied(fmt.Sprintf("can't delete vpc %q: still referenced by OvnSnatRule %q", vpc.Name, item.Name))
		}
	}

	fipList := &ovnv1.OvnFipList{}
	if err := v.cache.List(ctx, fipList); err != nil {
		return ctrlwebhook.Errored(http.StatusInternalServerError, err)
	}
	// Use Status.Vpc first as vpc may be inferred from spec.ipName if not in spec.
	for _, item := range fipList.Items {
		referencedVpc := item.Status.Vpc
		if referencedVpc == "" {
			referencedVpc = item.Spec.Vpc
		}
		if referencedVpc == vpc.Name {
			return ctrlwebhook.Denied(fmt.Sprintf("can't delete vpc %q: still referenced by OvnFip %q", vpc.Name, item.Name))
		}
	}

	return ctrlwebhook.Allowed("by pass")
}
