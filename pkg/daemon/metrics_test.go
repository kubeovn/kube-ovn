package daemon

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestGatewayNetfilterMetrics(t *testing.T) {
	registry := prometheus.NewRegistry()
	require.NoError(t, registerGatewayNetfilterMetrics(registry))

	metricFamilies, err := registry.Gather()
	require.NoError(t, err)
	names := make(map[string]struct{}, len(metricFamilies))
	for _, family := range metricFamilies {
		names[family.GetName()] = struct{}{}
	}
	for _, name := range []string{
		"kube_ovn_gateway_netfilter_backend",
		"kube_ovn_gateway_netfilter_detect_failures_total",
		"kube_ovn_gateway_netfilter_switch_failures_total",
		"kube_ovn_gateway_nft_transactions_total",
		"kube_ovn_gateway_nft_transaction_failures_total",
		"kube_ovn_gateway_nft_transaction_duration_seconds",
		"kube_ovn_gateway_nft_repairs_total",
	} {
		_, ok := names[name]
		require.True(t, ok, name)
	}
}
