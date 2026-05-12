package controller

import (
	"encoding/json"
	"testing"

	jsonpatch "github.com/evanphx/json-patch/v5"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestHasAllocatedAnnotation(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		expected    bool
	}{
		{
			name:        "nil annotations",
			annotations: nil,
			expected:    false,
		},
		{
			name:        "empty annotations",
			annotations: map[string]string{},
			expected:    false,
		},
		{
			name: "default provider allocated",
			annotations: map[string]string{
				"ovn.kubernetes.io/allocated": "true",
			},
			expected: true,
		},
		{
			name: "custom provider allocated",
			annotations: map[string]string{
				"my-provider.kubernetes.io/allocated": "true",
			},
			expected: true,
		},
		{
			name: "allocated is false",
			annotations: map[string]string{
				"ovn.kubernetes.io/allocated": "false",
			},
			expected: false,
		},
		{
			name: "unrelated annotations only",
			annotations: map[string]string{
				"app":                          "test",
				"ovn.kubernetes.io/ip_address": "10.0.0.1",
			},
			expected: false,
		},
		{
			name: "multiple providers with one allocated",
			annotations: map[string]string{
				"ovn.kubernetes.io/allocated":         "false",
				"my-provider.kubernetes.io/allocated": "true",
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: tt.annotations,
				},
			}
			require.Equal(t, tt.expected, hasAllocatedAnnotation(pod))
		})
	}
}

// TestBuildVpcLBStatusPatch_PreservesInitDefaultVpcFields is the regression
// guard for the bootstrap deadlock fix: initLoadBalancer's status patch must
// only set the six LB-name fields, and must never carry Standby/Default/
// Router/DefaultLogicalSwitch. If someone reintroduces a whole-VpcStatus
// serialization here, the merge-patch body would overwrite fields that
// InitDefaultVpc wrote moments earlier, reproducing the race that bricked
// fresh-install HA E2E runs.
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

	// The patch body must not reference any non-LB field. A regression that
	// reintroduces vpc.Status.Bytes() would immediately trip these asserts
	// because VpcStatus has non-omitempty booleans/strings that always get
	// serialized.
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

	// Simulate the etcd state right after InitDefaultVpc UpdateStatus and
	// verify the merge patch keeps Standby/Default/Router/DefaultLogicalSwitch
	// intact while writing the six LB fields.
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
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// TestInitLB_SkipsCtFlushForSessionAffinityUDP is the regression guard for a
// cross-namespace UDP session-affinity breakage. ct_flush wipes all conntrack
// entries on the LB's datapath whenever any vip is mutated. Since the session
// UDP LB (UDPSessLoadBalancer) is shared by every service in the VPC, enabling
// ct_flush on it lets an unrelated service's backend change invalidate the
// affinity state of any concurrently running session-affinity service, landing
// subsequent packets on a different backend. initLB must therefore only set
// ct_flush=true on the non-session UDP LB.
func TestInitLB_SkipsCtFlushForSessionAffinityUDP(t *testing.T) {
	tests := []struct {
		name              string
		protocol          string
		sessionAffinity   bool
		expectCtFlushCall bool
	}{
		{
			name:              "udp non-session LB enables ct_flush",
			protocol:          "udp",
			sessionAffinity:   false,
			expectCtFlushCall: true,
		},
		{
			name:              "udp session-affinity LB skips ct_flush",
			protocol:          "udp",
			sessionAffinity:   true,
			expectCtFlushCall: false,
		},
		{
			name:              "tcp non-session LB skips ct_flush",
			protocol:          "tcp",
			sessionAffinity:   false,
			expectCtFlushCall: false,
		},
		{
			name:              "tcp session-affinity LB skips ct_flush",
			protocol:          "tcp",
			sessionAffinity:   true,
			expectCtFlushCall: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeCtrl, err := newFakeControllerWithOptions(t, nil)
			require.NoError(t, err)
			ctrl := fakeCtrl.fakeController
			mockOvnClient := fakeCtrl.mockOvnClient

			lbName := "cluster-" + tt.protocol + "-test-loadbalancer"

			if tt.sessionAffinity {
				mockOvnClient.EXPECT().
					CreateLoadBalancer(
						lbName, tt.protocol,
						ovnnb.LoadBalancerSelectionFieldsIPSrc,
						ovnnb.LoadBalancerSelectionFieldsIpv6Src,
					).
					Return(nil)
				mockOvnClient.EXPECT().
					SetLoadBalancerAffinityTimeout(lbName, util.DefaultServiceSessionStickinessTimeout).
					Return(nil)
			} else {
				mockOvnClient.EXPECT().
					CreateLoadBalancer(lbName, tt.protocol).
					Return(nil)
			}
			mockOvnClient.EXPECT().
				SetLoadBalancerPreferLocalBackend(lbName, gomock.Any()).
				Return(nil)

			if tt.expectCtFlushCall {
				mockOvnClient.EXPECT().
					SetLoadBalancerCtFlush(lbName, true).
					Return(nil)
			} else {
				mockOvnClient.EXPECT().
					SetLoadBalancerCtFlush(gomock.Any(), gomock.Any()).
					Times(0)
			}

			require.NoError(t, ctrl.initLB(lbName, tt.protocol, tt.sessionAffinity))
		})
	}
}
