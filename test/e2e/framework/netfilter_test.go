package framework

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGatewayBackendIsNFTables(t *testing.T) {
	metrics := `kube_ovn_gateway_netfilter_backend{backend="iptables"} 0
kube_ovn_gateway_netfilter_backend{backend="nftables"} 1
`

	require.True(t, gatewayBackendIsNFTables(metrics))
	require.False(t, gatewayBackendIsNFTables(`kube_ovn_gateway_netfilter_backend{backend="nftables"} 0`))
}
