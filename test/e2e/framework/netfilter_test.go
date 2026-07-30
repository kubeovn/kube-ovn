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

func TestGatewayMetricsURL(t *testing.T) {
	require.Equal(t, "http://172.18.0.2:10665/metrics", GatewayMetricsURL("172.18.0.2"))
	require.Equal(t, "http://[fc00:f853:ccd:e793::2]:10665/metrics", GatewayMetricsURL("fc00:f853:ccd:e793::2"))
}
