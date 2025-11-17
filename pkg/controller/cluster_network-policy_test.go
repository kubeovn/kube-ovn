package controller

import (
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/network-policy-api/apis/v1alpha2"
	"testing"
)

func TestValidateCnpConfig(t *testing.T) {
	t.Parallel()

	var tooBigIngressList []v1alpha2.ClusterNetworkPolicyIngressRule
	for i := 0; i < util.CnpMaxRules+1; i++ {
		tooBigIngressList = append(tooBigIngressList, v1alpha2.ClusterNetworkPolicyIngressRule{Name: "test"})
	}

	var tooBigEgressList []v1alpha2.ClusterNetworkPolicyEgressRule
	for i := 0; i < util.CnpMaxRules+1; i++ {
		tooBigEgressList = append(tooBigEgressList, v1alpha2.ClusterNetworkPolicyEgressRule{Name: "test"})
	}

	tests := []struct {
		name        string
		priorityMap map[int32]string
		cnp         *v1alpha2.ClusterNetworkPolicy
		error       bool
	}{
		{
			name:        "no egress rule",
			priorityMap: map[int32]string{10: "test"},
			cnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Priority: 99,
					Ingress: []v1alpha2.ClusterNetworkPolicyIngressRule{
						{
							Name: "test",
						},
					},
				},
			},
			error: false,
		},
		{
			name:        "no ingress rule",
			priorityMap: map[int32]string{10: "test"},
			cnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Priority: 99,
					Egress: []v1alpha2.ClusterNetworkPolicyEgressRule{
						{
							Name: "test",
						},
					},
				},
			},
			error: false,
		},
		{
			name:        "no ingress or egressrule",
			priorityMap: map[int32]string{10: "test"},
			cnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Priority: 99,
				},
			},
			error: false,
		},
		{
			name:        "too many ingress rules",
			priorityMap: map[int32]string{10: "test"},
			cnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Priority: 99,
					Ingress:  tooBigIngressList,
				},
			},
			error: true,
		},
		{
			name:        "too many egress rules",
			priorityMap: map[int32]string{10: "test"},
			cnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Priority: 99,
					Egress:   tooBigEgressList,
				},
			},
			error: true,
		},
		{
			name:        "too many egress/ingress rules",
			priorityMap: map[int32]string{10: "test"},
			cnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Priority: 99,
					Ingress:  tooBigIngressList,
					Egress:   tooBigEgressList,
				},
			},
			error: true,
		},
		{
			name:        "just enough egress/ingress rules",
			priorityMap: map[int32]string{10: "test"},
			cnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{
					Priority: 99,
					Ingress:  tooBigIngressList[:util.CnpMaxRules], // We have one too much, remove it
					Egress:   tooBigEgressList[:util.CnpMaxRules],  // We have one too much, remove it
				},
			},
			error: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCnpConfig(tt.priorityMap, tt.cnp)
			if tt.error {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCheckCnpPriorities(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		priorityMap map[int32]string
		cnp         *v1alpha2.ClusterNetworkPolicy
		error       bool
	}{
		{
			name:  "no priorityMap",
			cnp:   &v1alpha2.ClusterNetworkPolicy{},
			error: true,
		},
		{
			name:        "no cnp",
			priorityMap: make(map[int32]string),
			error:       true,
		},
		{
			name:  "no priorityMap and cnp",
			error: true,
		},
		{
			name:        "cnp priority conflict",
			priorityMap: map[int32]string{10: "test"},
			cnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{Priority: 10},
			},
			error: true,
		},
		{
			name:        "cnp no priority conflict",
			priorityMap: map[int32]string{10: "test"},
			cnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{Priority: 99},
			},
			error: false,
		},
		{
			name:        "cnp priority too high",
			priorityMap: map[int32]string{10: "test"},
			cnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{Priority: 100},
			},
			error: true,
		},
		{
			name:        "cnp priority map empty",
			priorityMap: map[int32]string{},
			cnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{Priority: 99},
			},
			error: false,
		},
		{
			name:        "cnp priority negative",
			priorityMap: map[int32]string{10: "test"},
			cnp: &v1alpha2.ClusterNetworkPolicy{
				Spec: v1alpha2.ClusterNetworkPolicySpec{Priority: -1},
			},
			error: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkCnpPriorities(tt.priorityMap, tt.cnp)
			if tt.error {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestFetchCnpCIDRAddresses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		networks []v1alpha2.CIDR
		v4Addrs  []string
		v6Addrs  []string
	}{
		{
			name: "nil",
		},
		{
			name:     "empty",
			networks: []v1alpha2.CIDR{},
		},
		{
			name:     "ipv4 only",
			networks: []v1alpha2.CIDR{"1.1.1.0/24"},
			v4Addrs:  []string{"1.1.1.0/24"},
		},
		{
			name:     "ipv6 only",
			networks: []v1alpha2.CIDR{"fd00::/64"},
			v6Addrs:  []string{"fd00::/64"},
		},
		{
			name:     "mixed",
			networks: []v1alpha2.CIDR{"1.1.1.0/24", "fd00::/64"},
			v4Addrs:  []string{"1.1.1.0/24"},
			v6Addrs:  []string{"fd00::/64"},
		},
		{
			name:     "invalid ipv4 cidr",
			networks: []v1alpha2.CIDR{"1.1.1.0/33", "fd00::/64"},
			v6Addrs:  []string{"fd00::/64"},
		},
		{
			name:     "invalid ipv6 cidr",
			networks: []v1alpha2.CIDR{"1.1.1.0/24", "fd00::/129"},
			v4Addrs:  []string{"1.1.1.0/24"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v4Addrs, v6Addrs := fetchCnpCIDRAddresses(tt.networks)
			require.Equal(t, tt.v4Addrs, v4Addrs)
			require.Equal(t, tt.v6Addrs, v6Addrs)
		})
	}
}

func TestGetCnpName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		arg  string
		ret  string
	}{
		{
			"normal",
			"foo",
			"foo",
		},
		{
			"start with digital",
			"123",
			"cnp123",
		},
		{
			"start with hyphen",
			"-foo",
			"cnp-foo",
		},
		{
			"start with underscore",
			"_foo",
			"cnp_foo",
		},
		{
			"start with dot",
			".foo",
			"cnp.foo",
		},
		{
			"start with slash",
			"/foo",
			"cnp/foo",
		},
		{
			"start with colon",
			":foo",
			"cnp:foo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ret := getCnpName(tt.arg)
			require.Equal(t, tt.ret, ret)
		})
	}
}

func TestGetCnpACLAction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		arg  v1alpha2.ClusterNetworkPolicyRuleAction
		ret  ovnnb.ACLAction
	}{
		{
			"allow",
			v1alpha2.ClusterNetworkPolicyRuleActionAccept,
			ovnnb.ACLActionAllowRelated,
		},
		{
			"pass",
			v1alpha2.ClusterNetworkPolicyRuleActionPass,
			ovnnb.ACLActionPass,
		},
		{
			"deny",
			v1alpha2.ClusterNetworkPolicyRuleActionDeny,
			ovnnb.ACLActionDrop,
		},
		{
			"unknown",
			"foo",
			ovnnb.ACLActionDrop,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ret := getCnpACLAction(tt.arg)
			require.Equal(t, tt.ret, ret)
		})
	}
}
