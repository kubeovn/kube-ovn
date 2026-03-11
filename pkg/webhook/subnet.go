package webhook

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"

	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	ovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func (v *ValidatingHook) SubnetCreateHook(ctx context.Context, req admission.Request) admission.Response {
	o := ovnv1.Subnet{}
	if err := v.decoder.Decode(req, &o); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if err := util.ValidateSubnet(o); err != nil {
		return ctrlwebhook.Denied(err.Error())
	}

	subnetList := &ovnv1.SubnetList{}
	if err := v.cache.List(ctx, subnetList); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	if err := util.ValidateCidrConflict(o, subnetList.Items); err != nil {
		return admission.Errored(http.StatusConflict, err)
	}

	vpcList := &ovnv1.VpcList{}
	if err := v.cache.List(ctx, vpcList); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	for _, item := range vpcList.Items {
		if item.Name == o.Name {
			err := errors.New("vpc and subnet cannot have the same name")
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}

		if o.Spec.Vpc == item.Name && item.Status.Standby && !item.Status.Default {
			for _, ns := range o.Spec.Namespaces {
				if !slices.Contains(item.Spec.Namespaces, ns) {
					err := fmt.Errorf("namespace '%s' is out of range to custom vpc '%s'", ns, item.Name)
					return ctrlwebhook.Errored(http.StatusBadRequest, err)
				}
			}
		}
	}

	return ctrlwebhook.Allowed("bypass")
}

func (v *ValidatingHook) SubnetUpdateHook(ctx context.Context, req admission.Request) admission.Response {
	o := ovnv1.Subnet{}
	if err := v.decoder.Decode(req, &o); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	oldSubnet := ovnv1.Subnet{}
	if err := v.decoder.DecodeRaw(req.OldObject, &oldSubnet); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	if (o.Spec.Gateway != oldSubnet.Spec.Gateway) && (o.Status.V4UsingIPs != 0 || o.Status.V6UsingIPs != 0) {
		return ctrlwebhook.Denied("can't update gateway of cidr when any IPs in Using")
	}

	if o.Spec.Vpc != oldSubnet.Spec.Vpc {
		if oldSubnet.Spec.Vpc != "" || o.Spec.Vpc != util.DefaultVpc {
			return ctrlwebhook.Denied("vpc can only be changed from empty to ovn-cluster")
		}
	}

	if err := util.ValidateSubnet(o); err != nil {
		return ctrlwebhook.Denied(err.Error())
	}

	subnetList := &ovnv1.SubnetList{}
	if err := v.cache.List(ctx, subnetList); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	if err := util.ValidateCidrConflict(o, subnetList.Items); err != nil {
		return admission.Errored(http.StatusConflict, err)
	}

	return ctrlwebhook.Allowed("bypass")
}

func (v *ValidatingHook) SubnetDeleteHook(_ context.Context, req admission.Request) admission.Response {
	subnet := ovnv1.Subnet{}
	if err := v.decoder.DecodeRaw(req.OldObject, &subnet); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	if subnet.Status.V4UsingIPs != 0 || subnet.Status.V6UsingIPs != 0 {
		return ctrlwebhook.Denied("can't delete subnet when any IPs in Using")
	}
	return ctrlwebhook.Allowed("bypass")
}
