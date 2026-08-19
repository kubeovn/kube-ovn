package controller

import (
	"testing"

	"github.com/stretchr/testify/require"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

func TestVpcEndpointNaming(t *testing.T) {
	require.Equal(t, "tenant-a-vpc-endpoint-transit", vpcEndpointTransitLrpName("tenant-a", "vpc-endpoint-transit"))
	require.Equal(t, "vpc-endpoint-transit-tenant-a", vpcEndpointTransitLspName("tenant-a", "vpc-endpoint-transit"))
	require.Equal(t, "vpc-eps-db-tcp", vpcEndpointServiceLBName("db", "TCP"))
	require.Equal(t, "vpc-ep-client-udp", vpcEndpointLBName("client", "UDP"))
	require.Equal(t, "vpc-ep-client", vpcEndpointVipCRName("client"))
	require.Equal(t, "vpc-eps-db", vpcEndpointServiceLSPName("db"))
	require.Equal(t, "vpc-eps/db", vpcEndpointServiceIPAMName("db"))
	require.Equal(t, "vpc-ep-snat/tenant-a", vpcEndpointSnatIPAMName("tenant-a"))
}

func TestVpcEndpointSnatMatch(t *testing.T) {
	require.Equal(t, "ip4.dst == 100.65.1.20", vpcEndpointSnatMatch("100.65.1.20"))
	require.Equal(t, "ip6.dst == fd00:65::20", vpcEndpointSnatMatch("fd00:65::20"))
	require.Equal(t, "0.0.0.0/0", vpcEndpointSnatLogicalIP("100.65.1.20"))
	require.Equal(t, "::/0", vpcEndpointSnatLogicalIP("fd00:65::20"))
}

func TestVpcEndpointServiceAllowed(t *testing.T) {
	open := &kubeovnv1.VpcEndpointService{}
	require.True(t, vpcEndpointServiceAllowed(open, "consumer"))

	restricted := &kubeovnv1.VpcEndpointService{
		Spec: kubeovnv1.VpcEndpointServiceSpec{AllowedVpcs: []string{"a", "b"}},
	}
	require.True(t, vpcEndpointServiceAllowed(restricted, "a"))
	require.False(t, vpcEndpointServiceAllowed(restricted, "c"))
}

func TestVpcEndpointIPFromNetworks(t *testing.T) {
	require.Equal(t, "100.65.0.10", vpcEndpointIPFromNetworks([]string{"100.65.0.10/16"}))
	require.Equal(t, "100.65.0.10", vpcEndpointIPFromNetworks([]string{"100.65.0.10/16", "fd00:65::10/112"}))
	require.Empty(t, vpcEndpointIPFromNetworks(nil))
}
