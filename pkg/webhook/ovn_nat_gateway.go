package webhook

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"

	"k8s.io/klog/v2"
	cli "sigs.k8s.io/controller-runtime/pkg/client"
	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	ovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

var (
	ovnEip  = ovnv1.SchemeGroupVersion.WithKind(util.KindOvnEip)
	ovnFip  = ovnv1.SchemeGroupVersion.WithKind(util.KindOvnFip)
	ovnDnat = ovnv1.SchemeGroupVersion.WithKind(util.KindOvnDnatRule)
	ovnSnat = ovnv1.SchemeGroupVersion.WithKind(util.KindOvnSnatRule)
)

func (v *ValidatingHook) ovnEipCreateHook(ctx context.Context, req admission.Request) admission.Response {
	eip := ovnv1.OvnEip{}
	if err := v.decoder.DecodeRaw(req.Object, &eip); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if err := v.ValidateOvnEip(ctx, &eip); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	return ctrlwebhook.Allowed("bypass")
}

func (v *ValidatingHook) ovnEipUpdateHook(ctx context.Context, req admission.Request) admission.Response {
	eipNew := ovnv1.OvnEip{}
	if err := v.decoder.DecodeRaw(req.Object, &eipNew); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	eipOld := ovnv1.OvnEip{}
	if err := v.decoder.DecodeRaw(req.OldObject, &eipOld); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if eipOld.Spec != eipNew.Spec {
		if eipOld.Status.Ready {
			err := errors.New("OvnEip not support change")
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}
		if err := v.ValidateOvnEip(ctx, &eipNew); err != nil {
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}
	}
	return ctrlwebhook.Allowed("bypass")
}

func (v *ValidatingHook) isOvnEipInUse(ctx context.Context, eipV4IP, eipV6IP string) (string, error) {
	var err error
	dnatList := ovnv1.OvnDnatRuleList{}
	fipList := ovnv1.OvnFipList{}
	snatList := ovnv1.OvnSnatRuleList{}
	opts := cli.MatchingLabels{util.EipV4IpLabel: eipV4IP, util.EipV6IpLabel: eipV6IP}
	err = v.cache.List(ctx, &dnatList, opts)
	if err != nil {
		klog.Errorf("failed to list ovn dnat, %v", err)
		return "", err
	}
	if len(dnatList.Items) != 0 {
		return util.DnatUsingEip, nil
	}
	err = v.cache.List(ctx, &fipList, opts)
	if err != nil {
		klog.Errorf("failed to list ovn fip, %v", err)
		return "", err
	}
	if len(fipList.Items) != 0 {
		return util.FipUsingEip, nil
	}
	err = v.cache.List(ctx, &snatList, opts)
	if err != nil {
		klog.Errorf("failed to list ovn snat, %v", err)
		return "", err
	}
	if len(snatList.Items) != 0 {
		return util.SnatUsingEip, nil
	}
	return "", nil
}

func (v *ValidatingHook) ovnEipDeleteHook(ctx context.Context, req admission.Request) admission.Response {
	eip := ovnv1.OvnEip{}
	if err := v.decoder.DecodeRaw(req.OldObject, &eip); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if eip.Status.Ready {
		var err error
		nat, err := v.isOvnEipInUse(ctx, eip.Spec.V4Ip, eip.Spec.V6Ip)
		if nat != "" {
			err = fmt.Errorf("OvnEip %s is still used by ovn nat", eip.Name)
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}
		if err != nil {
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}
	}

	return ctrlwebhook.Allowed("bypass")
}

func (v *ValidatingHook) ovnDnatCreateHook(ctx context.Context, req admission.Request) admission.Response {
	dnat := ovnv1.OvnDnatRule{}
	if err := v.decoder.DecodeRaw(req.Object, &dnat); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if err := v.ValidateOvnDnat(ctx, &dnat); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	return ctrlwebhook.Allowed("bypass")
}

func (v *ValidatingHook) ovnDnatUpdateHook(ctx context.Context, req admission.Request) admission.Response {
	dnatNew := ovnv1.OvnDnatRule{}
	if err := v.decoder.DecodeRaw(req.Object, &dnatNew); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	dnatOld := ovnv1.OvnDnatRule{}
	if err := v.decoder.DecodeRaw(req.OldObject, &dnatOld); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if dnatOld.Spec != dnatNew.Spec {
		if dnatOld.Status.Ready {
			err := errors.New("OvnDnat not support change")
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}
		if err := v.ValidateOvnDnat(ctx, &dnatNew); err != nil {
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}
	}

	return ctrlwebhook.Allowed("bypass")
}

func (v *ValidatingHook) ovnSnatCreateHook(ctx context.Context, req admission.Request) admission.Response {
	snat := ovnv1.OvnSnatRule{}
	if err := v.decoder.DecodeRaw(req.Object, &snat); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if err := v.ValidateOvnSnat(ctx, &snat); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	return ctrlwebhook.Allowed("bypass")
}

func (v *ValidatingHook) ovnSnatUpdateHook(ctx context.Context, req admission.Request) admission.Response {
	snatNew := ovnv1.OvnSnatRule{}
	if err := v.decoder.DecodeRaw(req.Object, &snatNew); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	snatOld := ovnv1.OvnSnatRule{}
	if err := v.decoder.DecodeRaw(req.OldObject, &snatOld); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if snatOld.Spec != snatNew.Spec {
		if snatOld.Status.Ready {
			err := errors.New("OvnSnat not support change")
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}
		if err := v.ValidateOvnSnat(ctx, &snatNew); err != nil {
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}
	}

	return ctrlwebhook.Allowed("bypass")
}

func (v *ValidatingHook) ovnFipCreateHook(ctx context.Context, req admission.Request) admission.Response {
	fip := ovnv1.OvnFip{}
	if err := v.decoder.DecodeRaw(req.Object, &fip); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if err := v.ValidateOvnFip(ctx, &fip); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	return ctrlwebhook.Allowed("bypass")
}

func (v *ValidatingHook) ovnFipUpdateHook(ctx context.Context, req admission.Request) admission.Response {
	fipNew := ovnv1.OvnFip{}
	if err := v.decoder.DecodeRaw(req.Object, &fipNew); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	fipOld := ovnv1.OvnFip{}
	if err := v.decoder.DecodeRaw(req.OldObject, &fipOld); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if fipNew.Spec != fipOld.Spec {
		if fipOld.Status.Ready {
			err := errors.New("OvnFip not support change")
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}
		if err := v.ValidateOvnFip(ctx, &fipNew); err != nil {
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}
	}
	return ctrlwebhook.Allowed("bypass")
}

func (v *ValidatingHook) ValidateOvnEip(ctx context.Context, eip *ovnv1.OvnEip) error {
	subnet := &ovnv1.Subnet{}
	externalNetwork := util.GetExternalNetwork(eip.Spec.ExternalSubnet)
	key := cli.ObjectKey{Name: externalNetwork}
	if err := v.cache.Get(ctx, key, subnet); err != nil {
		return err
	}

	if eip.Spec.V4Ip != "" {
		if net.ParseIP(eip.Spec.V4Ip) == nil {
			err := fmt.Errorf("spec v4Ip %s is not a valid", eip.Spec.V4Ip)
			return err
		}

		if !util.CIDRContainIP(subnet.Spec.CIDRBlock, eip.Spec.V4Ip) {
			err := fmt.Errorf("the vip %s is not in the range of subnet %s, cidr %v",
				eip.Spec.V4Ip, subnet.Name, subnet.Spec.CIDRBlock)
			return err
		}
	}

	if eip.Spec.V6Ip != "" {
		// v6 ip address can not use upper case
		if util.ContainsUppercase(eip.Spec.V6Ip) {
			err := fmt.Errorf("eip %s v6 ip address %s can not contain upper case", eip.Name, eip.Spec.V6Ip)
			klog.Error(err)
			return err
		}
		if net.ParseIP(eip.Spec.V6Ip) == nil {
			err := fmt.Errorf("spec v6ip %s is not a valid", eip.Spec.V6Ip)
			return err
		}

		if !util.CIDRContainIP(subnet.Spec.CIDRBlock, eip.Spec.V6Ip) {
			err := fmt.Errorf("the vip %s is not in the range of subnet %s, cidr %v",
				eip.Spec.V6Ip, subnet.Name, subnet.Spec.CIDRBlock)
			return err
		}
	}

	return nil
}

func (v *ValidatingHook) ValidateOvnDnat(ctx context.Context, dnat *ovnv1.OvnDnatRule) error {
	if dnat.Spec.OvnEip == "" {
		err := errors.New("should set spec ovnEip")
		return err
	}
	if dnat.Spec.IPName == "" && dnat.Spec.V4Ip == "" && dnat.Spec.V6Ip == "" {
		err := errors.New("should set spec ipName or v4 or v6 Ip")
		return err
	}
	eip := &ovnv1.OvnEip{}
	key := cli.ObjectKey{Name: dnat.Spec.OvnEip}
	if err := v.cache.Get(ctx, key, eip); err != nil {
		return err
	}

	if dnat.Spec.ExternalPort == "" {
		err := errors.New("should set spec externalPort")
		return err
	}

	if dnat.Spec.InternalPort == "" {
		err := errors.New("should set spec internalPort")
		return err
	}

	if port, err := strconv.Atoi(dnat.Spec.ExternalPort); err != nil {
		errMsg := fmt.Errorf("failed to parse spec externalPort %s: %w", dnat.Spec.ExternalPort, err)
		return errMsg
	} else if port < 0 || port > 65535 {
		err := fmt.Errorf("spec externalPort %s is not a valid port", dnat.Spec.ExternalPort)
		return err
	}

	if port, err := strconv.Atoi(dnat.Spec.InternalPort); err != nil {
		errMsg := fmt.Errorf("failed to parse spec internalPort %s: %w", dnat.Spec.InternalPort, err)
		return errMsg
	} else if port < 0 || port > 65535 {
		err := fmt.Errorf("spec internalPort %s is not a valid port", dnat.Spec.InternalPort)
		return err
	}

	if !strings.EqualFold(dnat.Spec.Protocol, "tcp") &&
		!strings.EqualFold(dnat.Spec.Protocol, "udp") {
		err := fmt.Errorf("invalid dnat protocol: %s, support tcp or udp", dnat.Spec.Protocol)
		return err
	}

	return nil
}

func (v *ValidatingHook) ValidateOvnSnat(ctx context.Context, snat *ovnv1.OvnSnatRule) error {
	if snat.Spec.OvnEip == "" {
		return errors.New("should set spec OvnEip")
	}

	if snat.Spec.VpcSubnet != "" && snat.Spec.IPName != "" {
		return errors.New("should not set spec vpcSubnet and ipName at the same time")
	}

	if snat.Spec.Vpc != "" && snat.Spec.V4IpCidr == "" && snat.Spec.V6IpCidr == "" {
		return errors.New("should set spec v4 or v6 IpCidr (subnet cidr or ip address) when spec vpc is set")
	}

	if snat.Spec.Vpc == "" && snat.Spec.V4IpCidr != "" && snat.Spec.V6IpCidr != "" {
		return errors.New("should set spec vpc while spec v4 or v6 IpCidr is not set")
	}

	if snat.Spec.VpcSubnet == "" && snat.Spec.IPName == "" && snat.Spec.Vpc == "" && snat.Spec.V4IpCidr == "" && snat.Spec.V6IpCidr == "" {
		return errors.New("should set spec vpcSubnet or ipName or vpc and v4 and v6 IpCidr at least")
	}

	eip := &ovnv1.OvnEip{}
	key := cli.ObjectKey{Name: snat.Spec.OvnEip}
	return v.cache.Get(ctx, key, eip)
}

func (v *ValidatingHook) ValidateOvnFip(ctx context.Context, fip *ovnv1.OvnFip) error {
	if fip.Spec.OvnEip == "" {
		err := errors.New("should set spec ovnEip")
		return err
	}
	if fip.Spec.IPName == "" && fip.Spec.V4Ip == "" && fip.Spec.V6Ip == "" {
		err := errors.New("should set spec ipName or ip")
		return err
	}
	eip := &ovnv1.OvnEip{}
	key := cli.ObjectKey{Name: fip.Spec.OvnEip}
	return v.cache.Get(ctx, key, eip)
}
