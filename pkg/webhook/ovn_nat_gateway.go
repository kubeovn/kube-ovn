package webhook

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	cli "sigs.k8s.io/controller-runtime/pkg/client"
	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	ovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

var (
	ovnEip  = metav1.GroupVersionKind{Group: ovnv1.SchemeGroupVersion.Group, Version: ovnv1.SchemeGroupVersion.Version, Kind: "OvnEip"}
	ovnFip  = metav1.GroupVersionKind{Group: ovnv1.SchemeGroupVersion.Group, Version: ovnv1.SchemeGroupVersion.Version, Kind: "OvnFip"}
	ovnDnat = metav1.GroupVersionKind{Group: ovnv1.SchemeGroupVersion.Group, Version: ovnv1.SchemeGroupVersion.Version, Kind: "OvnDnatRule"}
	ovnSnat = metav1.GroupVersionKind{Group: ovnv1.SchemeGroupVersion.Group, Version: ovnv1.SchemeGroupVersion.Version, Kind: "OvnSnatRule"}
)

func (v *ValidatingHook) ovnEipCreateHook(ctx context.Context, req admission.Request) admission.Response {
	eip := ovnv1.OvnEip{}
	if err := v.decoder.DecodeRaw(req.Object, &eip); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if err := v.ValidateOvnEip(ctx, &eip); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	return ctrlwebhook.Allowed("by pass")
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
			err := fmt.Errorf("ovnEip \"%s\" is ready, not support change", eipNew.Name)
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}
		if err := v.ValidateOvnEip(ctx, &eipNew); err != nil {
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}
	}
	return ctrlwebhook.Allowed("by pass")
}

func (v *ValidatingHook) isOvnEipInUse(ctx context.Context, eipV4IP string) (string, error) {
	var err error
	dnatList := ovnv1.OvnDnatRuleList{}
	fipList := ovnv1.OvnFipList{}
	snatList := ovnv1.OvnSnatRuleList{}
	opts := cli.MatchingLabels{util.EipV4IpLabel: eipV4IP}
	err = v.cache.List(ctx, &dnatList, opts)
	if err != nil {
		klog.Errorf("failed to get ovn dnats, %v", err)
		return "", err
	}
	if len(dnatList.Items) != 0 {
		return "dnat", nil
	}
	err = v.cache.List(ctx, &fipList, opts)
	if err != nil {
		klog.Errorf("failed to get ovn fip, %v", err)
		return "", err
	}
	if len(fipList.Items) != 0 {
		return "fip", nil
	}
	err = v.cache.List(ctx, &snatList, opts)
	if err != nil {
		klog.Errorf("failed to get ovn dnats, %v", err)
		return "", err
	}
	if len(snatList.Items) != 0 {
		return "snat", nil
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
		nat, err := v.isOvnEipInUse(ctx, eip.Spec.V4Ip)
		if nat != "" {
			err = fmt.Errorf("eip \"%s\" is still using by ovn nat", eip.Name)
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}
		if err != nil {
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}
	}

	return ctrlwebhook.Allowed("by pass")
}

func (v *ValidatingHook) ovnDnatCreateHook(ctx context.Context, req admission.Request) admission.Response {
	dnat := ovnv1.OvnDnatRule{}
	if err := v.decoder.DecodeRaw(req.Object, &dnat); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if err := v.ValidateOvnDnat(ctx, &dnat); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	return ctrlwebhook.Allowed("by pass")
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
			err := fmt.Errorf("OvnDnatRule \"%s\" is ready, not support change", dnatNew.Name)
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}
		if err := v.ValidateOvnDnat(ctx, &dnatNew); err != nil {
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}
	}

	return ctrlwebhook.Allowed("by pass")
}

func (v *ValidatingHook) ovnSnatCreateHook(ctx context.Context, req admission.Request) admission.Response {
	snat := ovnv1.OvnSnatRule{}
	if err := v.decoder.DecodeRaw(req.Object, &snat); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if err := v.ValidateOvnSnat(ctx, &snat); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	return ctrlwebhook.Allowed("by pass")
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
			err := fmt.Errorf("OvnSnatRule \"%s\" is ready, not support change", snatNew.Name)
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}
		if err := v.ValidateOvnSnat(ctx, &snatNew); err != nil {
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}
	}

	return ctrlwebhook.Allowed("by pass")
}

func (v *ValidatingHook) ovnFipCreateHook(ctx context.Context, req admission.Request) admission.Response {
	fip := ovnv1.OvnFip{}
	if err := v.decoder.DecodeRaw(req.Object, &fip); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if err := v.ValidateOvnFip(ctx, &fip); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	return ctrlwebhook.Allowed("by pass")
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
			err := fmt.Errorf("OvnFIPRule \"%s\" is ready, not support change", fipNew.Name)
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}
		if err := v.ValidateOvnFip(ctx, &fipNew); err != nil {
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}
	}
	return ctrlwebhook.Allowed("by pass")
}

func (v *ValidatingHook) ValidateOvnEip(ctx context.Context, eip *ovnv1.OvnEip) error {
	subnet := &ovnv1.Subnet{}
	externalNetwork := util.GetExternalNetwork(eip.Spec.ExternalSubnet)
	key := types.NamespacedName{Name: externalNetwork}
	if err := v.cache.Get(ctx, key, subnet); err != nil {
		return err
	}

	if eip.Spec.V4Ip != "" {
		if net.ParseIP(eip.Spec.V4Ip) == nil {
			err := fmt.Errorf("v4ip %s is not a valid", eip.Spec.V4Ip)
			return err
		}

		if !util.CIDRContainIP(subnet.Spec.CIDRBlock, eip.Spec.V4Ip) {
			err := fmt.Errorf("the vip %s is not in the range of subnet \"%s\", cidr %v",
				eip.Spec.V4Ip, subnet.Name, subnet.Spec.CIDRBlock)
			return err
		}
	}

	if eip.Spec.V6Ip != "" {
		if net.ParseIP(eip.Spec.V6Ip) == nil {
			err := fmt.Errorf("v6ip %s is not a valid", eip.Spec.V6Ip)
			return err
		}

		if !util.CIDRContainIP(subnet.Spec.CIDRBlock, eip.Spec.V6Ip) {
			err := fmt.Errorf("the vip %s is not in the range of subnet \"%s\", cidr %v",
				eip.Spec.V6Ip, subnet.Name, subnet.Spec.CIDRBlock)
			return err
		}
	}

	return nil
}

func (v *ValidatingHook) ValidateOvnDnat(ctx context.Context, dnat *ovnv1.OvnDnatRule) error {
	if dnat.Spec.OvnEip == "" {
		err := fmt.Errorf("parameter \"OvnEip\" cannot be empty")
		return err
	}
	if dnat.Spec.IPName == "" {
		err := fmt.Errorf("parameter \"IPName\" cannot be empty")
		return err
	}
	eip := &ovnv1.OvnEip{}
	key := types.NamespacedName{Name: dnat.Spec.OvnEip}
	if err := v.cache.Get(ctx, key, eip); err != nil {
		return err
	}

	if dnat.Spec.ExternalPort == "" {
		err := fmt.Errorf("parameter \"externalPort\" cannot be empty")
		return err
	}

	if dnat.Spec.InternalPort == "" {
		err := fmt.Errorf("parameter \"internalPort\" cannot be empty")
		return err
	}

	if port, err := strconv.Atoi(dnat.Spec.ExternalPort); err != nil {
		errMsg := fmt.Errorf("failed to parse externalPort %s: %v", dnat.Spec.ExternalPort, err)
		return errMsg
	} else if port < 0 || port > 65535 {
		err := fmt.Errorf("externalPort %s is not a valid port", dnat.Spec.ExternalPort)
		return err
	}

	if port, err := strconv.Atoi(dnat.Spec.InternalPort); err != nil {
		errMsg := fmt.Errorf("failed to parse internalIP %s: %v", dnat.Spec.InternalPort, err)
		return errMsg
	} else if port < 0 || port > 65535 {
		err := fmt.Errorf("internalIP %s is not a valid port", dnat.Spec.InternalPort)
		return err
	}

	if !strings.EqualFold(dnat.Spec.Protocol, "tcp") &&
		!strings.EqualFold(dnat.Spec.Protocol, "udp") {
		err := fmt.Errorf("invaild iptable protocol: %s,supported params: \"tcp\", \"udp\"", dnat.Spec.Protocol)
		return err
	}

	return nil
}

func (v *ValidatingHook) ValidateOvnSnat(ctx context.Context, snat *ovnv1.OvnSnatRule) error {
	if snat.Spec.OvnEip == "" {
		err := fmt.Errorf("parameter \"eip\" cannot be empty")
		return err
	}
	if snat.Spec.VpcSubnet == "" && snat.Spec.IPName == "" {
		err := fmt.Errorf("should set parameter \"VpcSubnet\" or \"IPName\" at least")
		return err
	}
	eip := &ovnv1.OvnEip{}
	key := types.NamespacedName{Name: snat.Spec.OvnEip}
	return v.cache.Get(ctx, key, eip)
}

func (v *ValidatingHook) ValidateOvnFip(ctx context.Context, fip *ovnv1.OvnFip) error {
	if fip.Spec.OvnEip == "" {
		err := fmt.Errorf("parameter \"OvnEip\" cannot be empty")
		return err
	}
	if fip.Spec.IPName == "" {
		err := fmt.Errorf("parameter \"IPName\" cannot be empty")
		return err
	}
	eip := &ovnv1.OvnEip{}
	key := types.NamespacedName{Name: fip.Spec.OvnEip}
	return v.cache.Get(ctx, key, eip)
}
