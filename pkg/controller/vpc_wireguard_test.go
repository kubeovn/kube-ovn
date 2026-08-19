package controller

import (
	"testing"

	"github.com/stretchr/testify/require"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func TestVpcWireGuardAllowedIPsFromSubnets(t *testing.T) {
	gw := &kubeovnv1.VpcWireGuard{
		ObjectMeta: metav1.ObjectMeta{Name: "vpn"},
		Spec: kubeovnv1.VpcWireGuardSpec{
			Vpc: "tenant",
		},
	}
	require.Equal(t, "0.0.0.0/0", vpcWireGuardAllowedIPsFromSubnets(gw, nil))

	gw.Spec.AllowedIPs = []string{"10.0.0.0/16", "10.1.0.0/16"}
	require.Equal(t, "10.0.0.0/16, 10.1.0.0/16", vpcWireGuardAllowedIPsFromSubnets(gw, nil))

	gw.Spec.AllowedIPs = nil
	subnets := []*kubeovnv1.Subnet{
		{Spec: kubeovnv1.SubnetSpec{Vpc: "tenant", CIDRBlock: "10.0.0.0/24"}},
		{Spec: kubeovnv1.SubnetSpec{Vpc: "other", CIDRBlock: "10.9.0.0/24"}},
		{Spec: kubeovnv1.SubnetSpec{Vpc: "tenant", CIDRBlock: ""}},
	}
	require.Equal(t, "10.0.0.0/24", vpcWireGuardAllowedIPsFromSubnets(gw, subnets))
}

func TestVpcWireGuardContainerCommandWaitsForConfig(t *testing.T) {
	require.Contains(t, vpcWireGuardContainerCommand, "/etc/wireguard/wg0.conf")
	require.Contains(t, vpcWireGuardContainerCommand, "wireguard.sh init")
	require.NotContains(t, vpcWireGuardContainerCommand, "PostStart")
}

func TestVpcWireGuardRouteExternalIDs(t *testing.T) {
	ids := vpcWireGuardRouteExternalIDs("vpn")
	require.Equal(t, "kube-ovn-vpc-wireguard", ids["vendor"])
	require.Equal(t, "vpn", ids["vpc-wireguard"])
}
