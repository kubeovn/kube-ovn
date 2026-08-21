package webhook

import (
	"testing"

	"github.com/stretchr/testify/require"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func TestValidateVpcWireGuardSpec(t *testing.T) {
	gw := &ovnv1.VpcWireGuard{
		ObjectMeta: metav1.ObjectMeta{Name: "vpn"},
		Spec: ovnv1.VpcWireGuardSpec{
			Vpc:               "tenant",
			Subnet:            "lan",
			ClientSubnet:      "vpn-pool",
			GenerateServerKey: true,
			Exposure: ovnv1.VpcWireGuardExposure{
				Type: ovnv1.VpcWireGuardExposureDualNIC,
			},
		},
	}
	require.NoError(t, validateVpcWireGuardSpec(gw))

	bad := gw.DeepCopy()
	bad.Spec.ClientSubnet = "lan"
	require.Error(t, validateVpcWireGuardSpec(bad))

	bad = gw.DeepCopy()
	bad.Spec.Exposure.Type = ovnv1.VpcWireGuardExposureDNAT
	require.Error(t, validateVpcWireGuardSpec(bad))
	bad.Spec.Exposure.EIP = "eip"
	bad.Spec.Exposure.NatGateway = "nat"
	require.NoError(t, validateVpcWireGuardSpec(bad))

	bad = gw.DeepCopy()
	bad.Spec.GenerateServerKey = false
	require.Error(t, validateVpcWireGuardSpec(bad))

	fip := gw.DeepCopy()
	fip.Spec.Exposure.Type = ovnv1.VpcWireGuardExposureFIP
	require.Error(t, validateVpcWireGuardSpec(fip))
	fip.Spec.Exposure.EIP = "eip"
	fip.Spec.Exposure.NatGateway = "nat"
	require.NoError(t, validateVpcWireGuardSpec(fip))
}

func TestValidateVpcWireGuardPeerSpec(t *testing.T) {
	peer := &ovnv1.VpcWireGuardPeer{
		ObjectMeta: metav1.ObjectMeta{Name: "alice"},
		Spec: ovnv1.VpcWireGuardPeerSpec{
			WireGuard:   "vpn",
			GenerateKey: true,
		},
	}
	require.NoError(t, validateVpcWireGuardPeerSpec(peer))

	peer.Spec.WireGuard = ""
	require.Error(t, validateVpcWireGuardPeerSpec(peer))

	peer.Spec.WireGuard = "vpn"
	peer.Spec.GenerateKey = false
	peer.Spec.PublicKey = "not-valid"
	require.Error(t, validateVpcWireGuardPeerSpec(peer))

	peer.Spec.GenerateKey = true
	peer.Spec.PublicKey = "should-be-empty"
	require.Error(t, validateVpcWireGuardPeerSpec(peer))
}

func TestValidateIPInCIDR(t *testing.T) {
	require.NoError(t, validateIPInCIDR("10.255.0.4", "10.255.0.0/24"))
	require.Error(t, validateIPInCIDR("10.0.0.4", "10.255.0.0/24"))
	require.Error(t, validateIPInCIDR("bad", "10.255.0.0/24"))
	require.NoError(t, validateIPInCIDR("10.255.0.4", "10.0.0.0/8,10.255.0.0/24"))
	require.Error(t, validateIPInCIDR("10.255.0.4", "not-a-cidr,also-bad"))
}

func TestValidateVpcWireGuardSpecKeyModes(t *testing.T) {
	gw := &ovnv1.VpcWireGuard{
		ObjectMeta: metav1.ObjectMeta{Name: "vpn"},
		Spec: ovnv1.VpcWireGuardSpec{
			Vpc:          "tenant",
			Subnet:       "lan",
			ClientSubnet: "vpn-pool",
			Exposure: ovnv1.VpcWireGuardExposure{
				Type: ovnv1.VpcWireGuardExposureDualNIC,
			},
		},
	}
	gw.Spec.GenerateServerKey = true
	gw.Spec.PublicKey = "x"
	require.Error(t, validateVpcWireGuardSpec(gw))

	gw.Spec.PublicKey = ""
	gw.Spec.GenerateServerKey = false
	require.Error(t, validateVpcWireGuardSpec(gw))

	gw.Spec.Vpc = ""
	require.Error(t, validateVpcWireGuardSpec(gw))

	gw.Spec.Vpc = "tenant"
	gw.Spec.Exposure.Type = "Nope"
	require.Error(t, validateVpcWireGuardSpec(gw))
}
