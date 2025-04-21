package controller

import (
	"testing"

	v1alpha1 "sigs.k8s.io/network-policy-api/apis/v1alpha1"

	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
)

func TestBanpACLAction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		arg  v1alpha1.BaselineAdminNetworkPolicyRuleAction
		ret  ovnnb.ACLAction
	}{
		{
			"allow",
			v1alpha1.BaselineAdminNetworkPolicyRuleActionAllow,
			ovnnb.ACLActionAllowRelated,
		},
		{
			"deny",
			v1alpha1.BaselineAdminNetworkPolicyRuleActionDeny,
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
			ret := banpACLAction(tt.arg)
			require.Equal(t, tt.ret, ret)
		})
	}
}
