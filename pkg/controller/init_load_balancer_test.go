package controller

import (
	"encoding/json"
	"testing"

	jsonpatch "github.com/evanphx/json-patch/v5"
	"github.com/stretchr/testify/require"
)

// TestBuildVpcLBStatusPatch_PreservesInitDefaultVpcFields is the regression
// guard for the bootstrap race: initLoadBalancer's status patch must only set
// the six LB-name fields and preserve fields owned by InitDefaultVpc.
func TestBuildVpcLBStatusPatch_PreservesInitDefaultVpcFields(t *testing.T) {
	t.Parallel()

	vpcLb := &VpcLoadBalancer{
		TCPLoadBalancer:      "cluster-tcp-loadbalancer",
		TCPSessLoadBalancer:  "cluster-tcp-session-loadbalancer",
		UDPLoadBalancer:      "cluster-udp-loadbalancer",
		UDPSessLoadBalancer:  "cluster-udp-session-loadbalancer",
		SctpLoadBalancer:     "cluster-sctp-loadbalancer",
		SctpSessLoadBalancer: "cluster-sctp-session-loadbalancer",
	}

	body, err := buildVpcLBStatusPatch(vpcLb)
	require.NoError(t, err)

	var raw struct {
		Status map[string]json.RawMessage `json:"status"`
	}
	require.NoError(t, json.Unmarshal(body, &raw))
	require.ElementsMatch(t,
		[]string{
			"tcpLoadBalancer", "tcpSessionLoadBalancer",
			"udpLoadBalancer", "udpSessionLoadBalancer",
			"sctpLoadBalancer", "sctpSessionLoadBalancer",
		},
		keysOf(raw.Status),
	)

	target, err := json.Marshal(map[string]any{
		"metadata": map[string]any{"name": "ovn-cluster"},
		"status": map[string]any{
			"standby":              true,
			"default":              true,
			"router":               "ovn-cluster",
			"defaultLogicalSwitch": "ovn-default",
		},
	})
	require.NoError(t, err)

	merged, err := jsonpatch.MergePatch(target, body)
	require.NoError(t, err)

	var got struct {
		Status map[string]any `json:"status"`
	}
	require.NoError(t, json.Unmarshal(merged, &got))

	require.Equal(t, true, got.Status["standby"], "Standby must survive the LB patch")
	require.Equal(t, true, got.Status["default"], "Default must survive the LB patch")
	require.Equal(t, "ovn-cluster", got.Status["router"], "Router must survive the LB patch")
	require.Equal(t, "ovn-default", got.Status["defaultLogicalSwitch"], "DefaultLogicalSwitch must survive the LB patch")

	require.Equal(t, vpcLb.TCPLoadBalancer, got.Status["tcpLoadBalancer"])
	require.Equal(t, vpcLb.TCPSessLoadBalancer, got.Status["tcpSessionLoadBalancer"])
	require.Equal(t, vpcLb.UDPLoadBalancer, got.Status["udpLoadBalancer"])
	require.Equal(t, vpcLb.UDPSessLoadBalancer, got.Status["udpSessionLoadBalancer"])
	require.Equal(t, vpcLb.SctpLoadBalancer, got.Status["sctpLoadBalancer"])
	require.Equal(t, vpcLb.SctpSessLoadBalancer, got.Status["sctpSessionLoadBalancer"])
}

func keysOf(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}
