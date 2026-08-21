package webhook

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	ovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

var (
	vpcWireGuardGVK     = ovnv1.SchemeGroupVersion.WithKind(util.KindVpcWireGuard)
	vpcWireGuardPeerGVK = ovnv1.SchemeGroupVersion.WithKind(util.KindVpcWireGuardPeer)
)

func (v *ValidatingHook) VpcWireGuardCreateOrUpdateHook(ctx context.Context, req admission.Request) admission.Response {
	gw := ovnv1.VpcWireGuard{}
	if err := v.decoder.DecodeRaw(req.Object, &gw); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	if err := v.ValidateVpcNatConfig(ctx); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	if err := validateVpcWireGuardSpec(&gw); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	if err := v.validateVpcWireGuardRefs(ctx, &gw); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	return ctrlwebhook.Allowed("validated")
}

func validateVpcWireGuardSpec(gw *ovnv1.VpcWireGuard) error {
	if gw.Spec.Vpc == "" || gw.Spec.Subnet == "" || gw.Spec.ClientSubnet == "" {
		return errors.New("vpc, subnet and clientSubnet are required")
	}
	if gw.Spec.Subnet == gw.Spec.ClientSubnet {
		return errors.New("clientSubnet must be different from subnet")
	}
	if err := util.ValidateVpcWireGuardStatefulSetNameLength(gw.Name); err != nil {
		return err
	}
	switch gw.Spec.Exposure.Type {
	case ovnv1.VpcWireGuardExposureDualNIC:
	case ovnv1.VpcWireGuardExposureDNAT, ovnv1.VpcWireGuardExposureFIP:
		if gw.Spec.Exposure.EIP == "" || gw.Spec.Exposure.NatGateway == "" {
			return fmt.Errorf("exposure.eip and exposure.natGateway are required for %s", gw.Spec.Exposure.Type)
		}
	default:
		return fmt.Errorf("invalid exposure type %q", gw.Spec.Exposure.Type)
	}
	if gw.Spec.GenerateServerKey {
		if gw.Spec.PublicKey != "" || gw.Spec.PrivateKeySecretRef != nil {
			return errors.New("publicKey/privateKeySecretRef must be empty when generateServerKey is true")
		}
	} else {
		if gw.Spec.PublicKey == "" || gw.Spec.PrivateKeySecretRef == nil {
			return errors.New("publicKey and privateKeySecretRef are required when generateServerKey is false")
		}
		if err := util.ParseWireGuardPublicKey(gw.Spec.PublicKey); err != nil {
			return err
		}
	}
	return nil
}

func (v *ValidatingHook) validateVpcWireGuardRefs(ctx context.Context, gw *ovnv1.VpcWireGuard) error {
	vpc := &ovnv1.Vpc{}
	if err := v.cache.Get(ctx, client.ObjectKey{Name: gw.Spec.Vpc}, vpc); err != nil {
		return fmt.Errorf("failed to get vpc %s: %w", gw.Spec.Vpc, err)
	}
	lan := &ovnv1.Subnet{}
	if err := v.cache.Get(ctx, client.ObjectKey{Name: gw.Spec.Subnet}, lan); err != nil {
		return fmt.Errorf("failed to get subnet %s: %w", gw.Spec.Subnet, err)
	}
	if lan.Spec.Vpc != "" && lan.Spec.Vpc != gw.Spec.Vpc {
		return fmt.Errorf("subnet %s belongs to vpc %s, not %s", gw.Spec.Subnet, lan.Spec.Vpc, gw.Spec.Vpc)
	}
	clientSubnet := &ovnv1.Subnet{}
	if err := v.cache.Get(ctx, client.ObjectKey{Name: gw.Spec.ClientSubnet}, clientSubnet); err != nil {
		return fmt.Errorf("failed to get clientSubnet %s: %w", gw.Spec.ClientSubnet, err)
	}
	if clientSubnet.Spec.Vpc != "" && clientSubnet.Spec.Vpc != gw.Spec.Vpc {
		return fmt.Errorf("clientSubnet %s belongs to vpc %s, not %s", gw.Spec.ClientSubnet, clientSubnet.Spec.Vpc, gw.Spec.Vpc)
	}
	return nil
}

func (v *ValidatingHook) VpcWireGuardPeerCreateOrUpdateHook(ctx context.Context, req admission.Request) admission.Response {
	peer := ovnv1.VpcWireGuardPeer{}
	if err := v.decoder.DecodeRaw(req.Object, &peer); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	if err := validateVpcWireGuardPeerSpec(&peer); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	gw := &ovnv1.VpcWireGuard{}
	if err := v.cache.Get(ctx, client.ObjectKey{Name: peer.Spec.WireGuard}, gw); err != nil {
		if k8serrors.IsNotFound(err) {
			return ctrlwebhook.Errored(http.StatusBadRequest, fmt.Errorf("vpc wireguard %s not found", peer.Spec.WireGuard))
		}
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	if peer.Spec.ClientIP != "" {
		clientSubnet := &ovnv1.Subnet{}
		if err := v.cache.Get(ctx, client.ObjectKey{Name: gw.Spec.ClientSubnet}, clientSubnet); err != nil {
			return ctrlwebhook.Errored(http.StatusBadRequest, fmt.Errorf("failed to get clientSubnet %s: %w", gw.Spec.ClientSubnet, err))
		}
		if err := validateIPInCIDR(peer.Spec.ClientIP, clientSubnet.Spec.CIDRBlock); err != nil {
			return ctrlwebhook.Errored(http.StatusBadRequest, err)
		}
	}
	return ctrlwebhook.Allowed("validated")
}

func (v *ValidatingHook) VpcWireGuardDeleteHook(ctx context.Context, req admission.Request) admission.Response {
	gw := ovnv1.VpcWireGuard{}
	if err := v.decoder.DecodeRaw(req.OldObject, &gw); err != nil {
		return ctrlwebhook.Errored(http.StatusBadRequest, err)
	}
	peers := &ovnv1.VpcWireGuardPeerList{}
	if err := v.cache.List(ctx, peers); err != nil {
		return ctrlwebhook.Errored(http.StatusInternalServerError, err)
	}
	for _, peer := range peers.Items {
		if peer.Spec.WireGuard == gw.Name {
			return ctrlwebhook.Denied(fmt.Sprintf("can't delete vpc wireguard %q: still referenced by VpcWireGuardPeer %q", gw.Name, peer.Name))
		}
	}
	return ctrlwebhook.Allowed("validated")
}

func validateVpcWireGuardPeerSpec(peer *ovnv1.VpcWireGuardPeer) error {
	if peer.Spec.WireGuard == "" {
		return errors.New("wireGuard is required")
	}
	if peer.Spec.GenerateKey {
		if peer.Spec.PublicKey != "" {
			return errors.New("publicKey must be empty when generateKey is true")
		}
	} else if err := util.ParseWireGuardPublicKey(peer.Spec.PublicKey); err != nil {
		return err
	}
	return nil
}

func validateIPInCIDR(ip, cidr string) error {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return fmt.Errorf("invalid client IP %s", ip)
	}
	for part := range strings.SplitSeq(cidr, ",") {
		_, ipNet, err := net.ParseCIDR(strings.TrimSpace(part))
		if err != nil {
			continue
		}
		if ipNet.Contains(parsed) {
			return nil
		}
	}
	return fmt.Errorf("client IP %s is not in subnet CIDR %s", ip, cidr)
}
