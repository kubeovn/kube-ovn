package webhook

import (
	"context"
	"fmt"
	"net"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	cli "sigs.k8s.io/controller-runtime/pkg/client"
	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	ovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

var vpcIPsecGatewayGVK = ovnv1.SchemeGroupVersion.WithKind(util.KindVpcIPsecGateway)

func (v *ValidatingHook) VpcIPsecGwCreateOrUpdateHook(ctx context.Context, req admission.Request) admission.Response {
	gw := ovnv1.VpcIPsecGateway{}
	if err := v.decoder.DecodeRaw(req.Object, &gw); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}

	if len(req.OldObject.Raw) > 0 {
		gwOld := ovnv1.VpcIPsecGateway{}
		if err := v.decoder.DecodeRaw(req.OldObject, &gwOld); err != nil {
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}
		if gwOld.Spec.Namespace != gw.Spec.Namespace {
			err := fmt.Errorf("VpcIPsecGateway %q: spec.namespace is immutable", gw.Name)
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}
		if gwOld.Spec.Vpc != gw.Spec.Vpc || gwOld.Spec.Subnet != gw.Spec.Subnet || gwOld.Spec.LanIP != gw.Spec.LanIP {
			err := fmt.Errorf("VpcIPsecGateway %q: spec.vpc/subnet/lanIp are immutable", gw.Name)
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}
	}

	if err := v.ValidateVpcIPsecConfig(ctx); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	if err := v.ValidateVpcIPsecGatewayConfig(ctx); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	if err := v.ValidateVpcIPsecGW(ctx, &gw); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	return ctrlwebhook.Allowed("bypass")
}

func (v *ValidatingHook) VpcIPsecGwDeleteHook(_ context.Context, _ admission.Request) admission.Response {
	return ctrlwebhook.Allowed("bypass")
}

func (v *ValidatingHook) ValidateVpcIPsecConfig(ctx context.Context) error {
	cm := &corev1.ConfigMap{}
	cmKey := cli.ObjectKey{Namespace: metav1.NamespaceSystem, Name: util.VpcIPsecConfig}
	if err := v.cache.Get(ctx, cmKey, cm); err != nil {
		if k8serrors.IsNotFound(err) {
			return fmt.Errorf("configMap %q not configured", util.VpcIPsecConfig)
		}
		return err
	}
	if cm.Data["image"] == "" {
		return fmt.Errorf("parameter \"image\" in ConfigMap %q cannot be empty", util.VpcIPsecConfig)
	}
	return nil
}

func (v *ValidatingHook) ValidateVpcIPsecGatewayConfig(ctx context.Context) error {
	cm := &corev1.ConfigMap{}
	cmKey := cli.ObjectKey{Namespace: metav1.NamespaceSystem, Name: util.VpcIPsecGatewayConfig}
	if err := v.cache.Get(ctx, cmKey, cm); err != nil {
		if k8serrors.IsNotFound(err) {
			return fmt.Errorf("configMap %q not configured", util.VpcIPsecGatewayConfig)
		}
		return err
	}
	if cm.Data["enable-vpc-ipsec-gw"] != "true" {
		return fmt.Errorf("vpc ipsec gateway feature is disabled")
	}
	return nil
}

func (v *ValidatingHook) ValidateVpcIPsecGW(ctx context.Context, gw *ovnv1.VpcIPsecGateway) error {
	if err := util.ValidateIPsecGwStatefulSetNameLength(gw.Name); err != nil {
		return err
	}
	if gw.Spec.Vpc == "" || gw.Spec.Subnet == "" || gw.Spec.ExternalSubnet == "" {
		return fmt.Errorf("vpc, subnet and externalSubnet are required")
	}
	if gw.Spec.LanIP == "" || net.ParseIP(gw.Spec.LanIP) == nil {
		return fmt.Errorf("lanIp %q is invalid", gw.Spec.LanIP)
	}
	if gw.Spec.RemoteEndpoint == "" {
		return fmt.Errorf("remoteEndpoint is required")
	}
	if len(gw.Spec.RemoteCIDRs) == 0 {
		return fmt.Errorf("remoteCIDRs is required")
	}
	for _, cidr := range gw.Spec.RemoteCIDRs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return fmt.Errorf("invalid remoteCIDR %q: %w", cidr, err)
		}
	}
	for _, cidr := range gw.Spec.LocalCIDRs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return fmt.Errorf("invalid localCIDR %q: %w", cidr, err)
		}
	}
	if gw.Spec.PSKSecretRef.Name == "" {
		return fmt.Errorf("pskSecretRef.name is required")
	}

	vpc := &ovnv1.Vpc{}
	if err := v.cache.Get(ctx, cli.ObjectKey{Name: gw.Spec.Vpc}, vpc); err != nil {
		return fmt.Errorf("failed to get vpc %s: %w", gw.Spec.Vpc, err)
	}
	subnet := &ovnv1.Subnet{}
	if err := v.cache.Get(ctx, cli.ObjectKey{Name: gw.Spec.Subnet}, subnet); err != nil {
		return fmt.Errorf("failed to get subnet %s: %w", gw.Spec.Subnet, err)
	}
	if subnet.Spec.Vpc != gw.Spec.Vpc {
		return fmt.Errorf("subnet %s does not belong to vpc %s", gw.Spec.Subnet, gw.Spec.Vpc)
	}
	if !util.CIDRContainIP(subnet.Spec.CIDRBlock, gw.Spec.LanIP) {
		return fmt.Errorf("lanIP %s is not in the range of subnet %s", gw.Spec.LanIP, subnet.Name)
	}
	ext := &ovnv1.Subnet{}
	if err := v.cache.Get(ctx, cli.ObjectKey{Name: gw.Spec.ExternalSubnet}, ext); err != nil {
		return fmt.Errorf("failed to get externalSubnet %s: %w", gw.Spec.ExternalSubnet, err)
	}

	ns := gw.Spec.Namespace
	if ns == "" {
		ns = metav1.NamespaceSystem
	}
	pskNs, pskKey := util.ResolveIPsecPSKSecretRef(gw.Spec.PSKSecretRef, ns)
	if pskNs != ns {
		return fmt.Errorf("psk secret namespace %q must match gateway workload namespace %q", pskNs, ns)
	}
	secret := &corev1.Secret{}
	if err := v.cache.Get(ctx, cli.ObjectKey{Namespace: pskNs, Name: gw.Spec.PSKSecretRef.Name}, secret); err != nil {
		return fmt.Errorf("failed to get psk secret %s/%s: %w", pskNs, gw.Spec.PSKSecretRef.Name, err)
	}
	if len(secret.Data[pskKey]) == 0 {
		return fmt.Errorf("psk secret %s/%s missing key %q", pskNs, gw.Spec.PSKSecretRef.Name, pskKey)
	}
	return nil
}
