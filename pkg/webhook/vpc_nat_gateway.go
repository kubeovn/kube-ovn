package webhook

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	cli "sigs.k8s.io/controller-runtime/pkg/client"
	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	ovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

var (
	vpcNatGatewayGVK = metav1.GroupVersionKind{Group: ovnv1.SchemeGroupVersion.Group, Version: ovnv1.SchemeGroupVersion.Version, Kind: "VpcNatGateway"}
	iptablesEIPGVK   = metav1.GroupVersionKind{Group: ovnv1.SchemeGroupVersion.Group, Version: ovnv1.SchemeGroupVersion.Version, Kind: "IptablesEIP"}
	iptablesDnatRule = metav1.GroupVersionKind{Group: ovnv1.SchemeGroupVersion.Group, Version: ovnv1.SchemeGroupVersion.Version, Kind: "IptablesDnatRule"}
	iptablesSnatRule = metav1.GroupVersionKind{Group: ovnv1.SchemeGroupVersion.Group, Version: ovnv1.SchemeGroupVersion.Version, Kind: "IptablesSnatRule"}
	iptablesFIPRule  = metav1.GroupVersionKind{Group: ovnv1.SchemeGroupVersion.Group, Version: ovnv1.SchemeGroupVersion.Version, Kind: "IptablesFIPRule"}
)

func (v *ValidatingHook) VpcNatGwCreateOrUpdateHook(ctx context.Context, req admission.Request) admission.Response {
	gw := ovnv1.VpcNatGateway{}
	if err := v.decoder.DecodeRaw(req.Object, &gw); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if err := v.ValidateVpcNatConfig(ctx); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if err := v.ValidateVpcNatGatewayConfig(ctx); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if err := v.ValidateVpcNatGW(ctx, &gw); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	return ctrlwebhook.Allowed("by pass")
}

func (v *ValidatingHook) VpcNatGwDeleteHook(ctx context.Context, req admission.Request) admission.Response {
	eipList := ovnv1.IptablesEIPList{}
	if err := v.client.List(ctx, &eipList, cli.MatchingLabels{util.VpcNatGatewayNameLabel: req.Name}); err != nil {
		return ctrlwebhook.Errored(http.StatusInternalServerError, err)
	}
	if len(eipList.Items) != 0 {
		err := fmt.Errorf("vpc-nat-gateway \"%s\" cannot be deleted if any eip is in use", req.Name)
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	return ctrlwebhook.Allowed("by pass")
}

func (v *ValidatingHook) iptablesEIPCreateHook(ctx context.Context, req admission.Request) admission.Response {
	eip := ovnv1.IptablesEIP{}
	if err := v.decoder.DecodeRaw(req.Object, &eip); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if err := v.ValidateVpcNatConfig(ctx); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if err := v.ValidateVpcNatGatewayConfig(ctx); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if err := v.ValidateIptablesEIP(ctx, &eip); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	return ctrlwebhook.Allowed("by pass")
}

func (v *ValidatingHook) iptablesEIPUpdateHook(ctx context.Context, req admission.Request) admission.Response {
	eipNew := ovnv1.IptablesEIP{}
	if err := v.decoder.DecodeRaw(req.Object, &eipNew); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	eipOld := ovnv1.IptablesEIP{}
	if err := v.decoder.DecodeRaw(req.OldObject, &eipOld); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if eipOld.Spec != eipNew.Spec {
		if eipOld.Status.Ready && eipNew.Status.Redo == eipOld.Status.Redo {
			err := fmt.Errorf("IptablesEIP \"%s\" is ready,not support change", eipNew.Name)
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}
		if err := v.ValidateVpcNatConfig(ctx); err != nil {
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}

		if err := v.ValidateVpcNatGatewayConfig(ctx); err != nil {
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}

		if err := v.ValidateIptablesEIP(ctx, &eipNew); err != nil {
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}
	}
	return ctrlwebhook.Allowed("by pass")
}

func (v *ValidatingHook) iptablesEIPDeleteHook(ctx context.Context, req admission.Request) admission.Response {
	eip := ovnv1.IptablesEIP{}
	if err := v.decoder.DecodeRaw(req.OldObject, &eip); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if err := v.ValidateVpcNatConfig(ctx); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if err := v.ValidateVpcNatGatewayConfig(ctx); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if eip.Status.Ready {
		var err error
		fipList := ovnv1.IptablesFIPRuleList{}
		snatList := ovnv1.IptablesSnatRuleList{}
		dnatList := ovnv1.IptablesDnatRuleList{}

		for natType := range strings.SplitSeq(eip.Status.Nat, ",") {
			switch natType {
			case util.FipUsingEip:
				err = v.cache.List(ctx, &fipList, cli.MatchingLabels{util.EipV4IpLabel: eip.Status.IP})
			case util.SnatUsingEip:
				err = v.cache.List(ctx, &snatList, cli.MatchingLabels{util.EipV4IpLabel: eip.Status.IP})
			case util.DnatUsingEip:
				err = v.cache.List(ctx, &dnatList, cli.MatchingLabels{util.EipV4IpLabel: eip.Status.IP})
			}
		}

		if err != nil {
			if !k8serrors.IsNotFound(err) {
				return ctrlwebhook.Errored(http.StatusInternalServerError, err)
			}
		}

		if len(fipList.Items) != 0 || len(snatList.Items) != 0 || len(dnatList.Items) != 0 {
			err = fmt.Errorf("eip \"%s\" is still in use,you need to delete the %s of eip first", eip.Name, eip.Status.Nat)
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}
	}

	return ctrlwebhook.Allowed("by pass")
}

func (v *ValidatingHook) iptablesDnatCreateHook(ctx context.Context, req admission.Request) admission.Response {
	dnat := ovnv1.IptablesDnatRule{}
	if err := v.decoder.DecodeRaw(req.Object, &dnat); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if err := v.ValidateVpcNatConfig(ctx); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if err := v.ValidateVpcNatGatewayConfig(ctx); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if err := v.ValidateIptablesDnat(ctx, &dnat); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	return ctrlwebhook.Allowed("by pass")
}

func (v *ValidatingHook) iptablesDnatUpdateHook(ctx context.Context, req admission.Request) admission.Response {
	dnatNew := ovnv1.IptablesDnatRule{}
	if err := v.decoder.DecodeRaw(req.Object, &dnatNew); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	dnatOld := ovnv1.IptablesDnatRule{}
	if err := v.decoder.DecodeRaw(req.OldObject, &dnatOld); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if dnatOld.Spec != dnatNew.Spec {
		if dnatOld.Status.Ready && dnatOld.Status.Redo == dnatNew.Status.Redo {
			err := fmt.Errorf("IptablesDnatRule \"%s\" is ready,not support change", dnatNew.Name)
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}
		if err := v.ValidateVpcNatConfig(ctx); err != nil {
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}

		if err := v.ValidateVpcNatGatewayConfig(ctx); err != nil {
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}

		if err := v.ValidateIptablesDnat(ctx, &dnatNew); err != nil {
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}
	}

	return ctrlwebhook.Allowed("by pass")
}

func (v *ValidatingHook) iptablesSnatCreateHook(ctx context.Context, req admission.Request) admission.Response {
	snat := ovnv1.IptablesSnatRule{}
	if err := v.decoder.DecodeRaw(req.Object, &snat); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if err := v.ValidateVpcNatConfig(ctx); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if err := v.ValidateVpcNatGatewayConfig(ctx); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if err := v.ValidateIptablesSnat(ctx, &snat); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	return ctrlwebhook.Allowed("by pass")
}

func (v *ValidatingHook) iptablesSnatUpdateHook(ctx context.Context, req admission.Request) admission.Response {
	snatNew := ovnv1.IptablesSnatRule{}
	if err := v.decoder.DecodeRaw(req.Object, &snatNew); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	snatOld := ovnv1.IptablesSnatRule{}
	if err := v.decoder.DecodeRaw(req.OldObject, &snatOld); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if snatOld.Spec != snatNew.Spec {
		if snatOld.Status.Ready && snatOld.Status.Redo == snatNew.Status.Redo {
			err := fmt.Errorf("IptablesSnatRule \"%s\" is ready,not support change", snatNew.Name)
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}
		if err := v.ValidateVpcNatConfig(ctx); err != nil {
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}

		if err := v.ValidateVpcNatGatewayConfig(ctx); err != nil {
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}

		if err := v.ValidateIptablesSnat(ctx, &snatNew); err != nil {
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}
	}

	return ctrlwebhook.Allowed("by pass")
}

func (v *ValidatingHook) iptablesFipCreateHook(ctx context.Context, req admission.Request) admission.Response {
	fip := ovnv1.IptablesFIPRule{}
	if err := v.decoder.DecodeRaw(req.Object, &fip); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if err := v.ValidateVpcNatConfig(ctx); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if err := v.ValidateVpcNatGatewayConfig(ctx); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if err := v.ValidateIptablesFip(ctx, &fip); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	return ctrlwebhook.Allowed("by pass")
}

func (v *ValidatingHook) iptablesFipUpdateHook(ctx context.Context, req admission.Request) admission.Response {
	fipNew := ovnv1.IptablesFIPRule{}
	if err := v.decoder.DecodeRaw(req.Object, &fipNew); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	fipOld := ovnv1.IptablesFIPRule{}
	if err := v.decoder.DecodeRaw(req.OldObject, &fipOld); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if fipNew.Spec != fipOld.Spec {
		if fipOld.Status.Ready && fipNew.Status.Redo == fipOld.Status.Redo {
			err := fmt.Errorf("IptablesFIPRule \"%s\" is ready,not support change", fipNew.Name)
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}
		if err := v.ValidateVpcNatConfig(ctx); err != nil {
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}

		if err := v.ValidateVpcNatGatewayConfig(ctx); err != nil {
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}

		if err := v.ValidateIptablesFip(ctx, &fipNew); err != nil {
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}
	}
	return ctrlwebhook.Allowed("by pass")
}

func (v *ValidatingHook) ValidateVpcNatGW(ctx context.Context, gw *ovnv1.VpcNatGateway) error {
	if gw.Spec.Vpc == "" {
		return errors.New("parameter \"vpc\" cannot be empty")
	}
	vpc := &ovnv1.Vpc{}
	key := types.NamespacedName{Name: gw.Spec.Vpc}
	if err := v.cache.Get(ctx, key, vpc); err != nil {
		return err
	}

	if gw.Spec.Subnet == "" {
		return errors.New("parameter \"subnet\" cannot be empty")
	}

	subnet := &ovnv1.Subnet{}
	key = types.NamespacedName{Name: gw.Spec.Subnet}
	if err := v.cache.Get(ctx, key, subnet); err != nil {
		return err
	}

	if net.ParseIP(gw.Spec.LanIP) == nil {
		err := fmt.Errorf("lanIP %s is not a valid", gw.Spec.LanIP)
		return err
	}

	if !util.CIDRContainIP(subnet.Spec.CIDRBlock, gw.Spec.LanIP) {
		err := fmt.Errorf("lanIP %s is not in the range of subnet %s, cidr %v",
			gw.Spec.LanIP, subnet.Name, subnet.Spec.CIDRBlock)
		return err
	}

	for _, t := range gw.Spec.Tolerations {
		if t.Operator != corev1.TolerationOpExists &&
			t.Operator != corev1.TolerationOpEqual {
			err := fmt.Errorf("invalid taint operator: %s, supported params: \"Equal\", \"Exists\"", t.Operator)
			return err
		}

		if t.Effect != corev1.TaintEffectNoSchedule &&
			t.Effect != corev1.TaintEffectNoExecute &&
			t.Effect != corev1.TaintEffectPreferNoSchedule {
			err := fmt.Errorf("invalid taint effect: %s, supported params: \"NoSchedule\", \"PreferNoSchedule\", \"NoExecute\"", t.Effect)
			return err
		}
	}

	if gw.Spec.QoSPolicy != "" {
		qos := &ovnv1.QoSPolicy{}
		key = types.NamespacedName{Name: gw.Spec.QoSPolicy}
		if err := v.cache.Get(ctx, key, qos); err != nil {
			return err
		}
	}

	// Validate default SNAT configuration
	if gw.Spec.EnableDefaultSnat {
		if gw.Spec.DefaultSnatSubnet == "" {
			return errors.New("defaultSnatSubnet cannot be empty when enableDefaultSnat is true")
		}

		defaultSnatSubnet := &ovnv1.Subnet{}
		key = types.NamespacedName{Name: gw.Spec.DefaultSnatSubnet}
		if err := v.cache.Get(ctx, key, defaultSnatSubnet); err != nil {
			return fmt.Errorf("defaultSnatSubnet %s not found: %w", gw.Spec.DefaultSnatSubnet, err)
		}

		// Verify it's a subnet with NatOutgoing enabled for fallback SNAT to work
		if !defaultSnatSubnet.Spec.NatOutgoing {
			return fmt.Errorf("defaultSnatSubnet %s must have natOutgoing enabled for fallback SNAT to work", gw.Spec.DefaultSnatSubnet)
		}
	}

	return nil
}

func (v *ValidatingHook) ValidateVpcNatGatewayConfig(ctx context.Context) error {
	cm := &corev1.ConfigMap{}
	cmKey := types.NamespacedName{Namespace: "kube-system", Name: util.VpcNatGatewayConfig}
	if err := v.cache.Get(ctx, cmKey, cm); err != nil {
		if k8serrors.IsNotFound(err) {
			return fmt.Errorf("configMap \"%s\" not configured", util.VpcNatGatewayConfig)
		}
		return err
	}

	if cm.Data["enable-vpc-nat-gw"] != "true" {
		err := fmt.Errorf("parameter \"enable-vpc-nat-gw\" in ConfigMap \"%s\" not true", util.VpcNatGatewayConfig)
		return err
	}

	return nil
}

func (v *ValidatingHook) ValidateIptablesEIP(ctx context.Context, eip *ovnv1.IptablesEIP) error {
	if eip.Spec.NatGwDp == "" {
		return errors.New("parameter \"natGwDp\" cannot be empty")
	}

	subnet := &ovnv1.Subnet{}
	externalNetwork := util.GetExternalNetwork(eip.Spec.ExternalSubnet)
	key := types.NamespacedName{Name: externalNetwork}
	if err := v.cache.Get(ctx, key, subnet); err != nil {
		return err
	}

	if eip.Spec.V4ip != "" {
		if net.ParseIP(eip.Spec.V4ip) == nil {
			return fmt.Errorf("v4ip %s is not a valid", eip.Spec.V4ip)
		}

		if !util.CIDRContainIP(subnet.Spec.CIDRBlock, eip.Spec.V4ip) {
			err := fmt.Errorf("the vip %s is not in the range of subnet \"%s\", cidr %v",
				eip.Spec.V4ip, subnet.Name, subnet.Spec.CIDRBlock)
			return err
		}
	}

	if eip.Spec.V6ip != "" {
		// v6 ip address can not use upper case
		if util.ContainsUppercase(eip.Spec.V6ip) {
			err := fmt.Errorf("eip %s v6 ip address %s can not contain upper case", eip.Name, eip.Spec.V6ip)
			return err
		}
		if net.ParseIP(eip.Spec.V6ip) == nil {
			err := fmt.Errorf("v6ip %s is not a valid", eip.Spec.V6ip)
			return err
		}

		if !util.CIDRContainIP(subnet.Spec.CIDRBlock, eip.Spec.V6ip) {
			err := fmt.Errorf("the vip %s is not in the range of subnet \"%s\", cidr %v",
				eip.Spec.V6ip, subnet.Name, subnet.Spec.CIDRBlock)
			return err
		}
	}

	return nil
}

func (v *ValidatingHook) ValidateIptablesDnat(ctx context.Context, dnat *ovnv1.IptablesDnatRule) error {
	if dnat.Spec.EIP == "" {
		return errors.New("parameter \"eip\" cannot be empty")
	}
	eip := &ovnv1.IptablesEIP{}
	key := types.NamespacedName{Name: dnat.Spec.EIP}
	if err := v.cache.Get(ctx, key, eip); err != nil {
		return err
	}

	if dnat.Spec.ExternalPort == "" {
		return errors.New("parameter \"externalPort\" cannot be empty")
	}

	if dnat.Spec.InternalPort == "" {
		return errors.New("parameter \"internalPort\" cannot be empty")
	}

	if port, err := strconv.Atoi(dnat.Spec.ExternalPort); err != nil {
		errMsg := fmt.Errorf("failed to parse externalPort %s: %w", dnat.Spec.ExternalPort, err)
		return errMsg
	} else if port < 0 || port > 65535 {
		err := fmt.Errorf("externalPort %s is not a valid port", dnat.Spec.ExternalPort)
		return err
	}

	if port, err := strconv.Atoi(dnat.Spec.InternalPort); err != nil {
		errMsg := fmt.Errorf("failed to parse internalIP %s: %w", dnat.Spec.InternalPort, err)
		return errMsg
	} else if port < 0 || port > 65535 {
		err := fmt.Errorf("internalIP %s is not a valid port", dnat.Spec.InternalPort)
		return err
	}

	if net.ParseIP(dnat.Spec.InternalIP) == nil {
		err := fmt.Errorf("internalIP %s is not a valid ip", dnat.Spec.InternalIP)
		return err
	}

	if !strings.EqualFold(dnat.Spec.Protocol, "tcp") &&
		!strings.EqualFold(dnat.Spec.Protocol, "udp") {
		err := fmt.Errorf("invalid iptable protocol: %s,supported params: \"tcp\", \"udp\"", dnat.Spec.Protocol)
		return err
	}

	return nil
}

func (v *ValidatingHook) ValidateIptablesSnat(ctx context.Context, snat *ovnv1.IptablesSnatRule) error {
	if snat.Spec.EIP == "" {
		return errors.New("parameter \"eip\" cannot be empty")
	}
	eip := &ovnv1.IptablesEIP{}
	key := types.NamespacedName{Name: snat.Spec.EIP}
	if err := v.cache.Get(ctx, key, eip); err != nil {
		return err
	}

	if err := util.CheckCidrs(snat.Spec.InternalCIDR); err != nil {
		return fmt.Errorf("invalid cidr %s", snat.Spec.InternalCIDR)
	}

	return nil
}

func (v *ValidatingHook) ValidateIptablesFip(ctx context.Context, fip *ovnv1.IptablesFIPRule) error {
	if fip.Spec.EIP == "" {
		err := errors.New("parameter \"eip\" cannot be empty")
		return err
	}
	eip := &ovnv1.IptablesEIP{}
	key := types.NamespacedName{Name: fip.Spec.EIP}
	if err := v.cache.Get(ctx, key, eip); err != nil {
		return err
	}

	if net.ParseIP(fip.Spec.InternalIP) == nil {
		err := fmt.Errorf("internalIP %s is not a valid", fip.Spec.InternalIP)
		return err
	}

	return nil
}
