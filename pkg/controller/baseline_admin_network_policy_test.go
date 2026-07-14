package controller

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func prepareBanpQueueTestController(t *testing.T) *Controller {
	t.Helper()

	fakeCtrl, err := newFakeControllerWithOptions(t, nil)
	require.NoError(t, err)

	ctrl := fakeCtrl.fakeController
	ctrl.addBanpQueue = newTypedRateLimitingQueue[string]("AddBaseAdminNetworkPolicy", nil)
	ctrl.updateBanpQueue = newTypedRateLimitingQueue[*AdminNetworkPolicyChangedDelta]("UpdateBaseAdminNetworkPolicy", nil)
	return ctrl
}

func newTestBanp(egressRuleName string, peerLabels map[string]string) *v1alpha1.BaselineAdminNetworkPolicy {
	return &v1alpha1.BaselineAdminNetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "default"},
		Spec: v1alpha1.BaselineAdminNetworkPolicySpec{
			Subject: v1alpha1.AdminNetworkPolicySubject{Namespaces: &metav1.LabelSelector{}},
			Egress: []v1alpha1.BaselineAdminNetworkPolicyEgressRule{{
				Name:   egressRuleName,
				Action: v1alpha1.BaselineAdminNetworkPolicyRuleActionDeny,
				To: []v1alpha1.BaselineAdminNetworkPolicyEgressPeer{{
					Namespaces: &metav1.LabelSelector{MatchLabels: peerLabels},
				}},
			}},
		},
	}
}

// A renamed rule must be handled by the add queue: the rule name is part of the acl name and of the
// address set name referenced by the acl match, so only recreating the acls keeps them consistent.
func TestEnqueueUpdateBanpRuleRename(t *testing.T) {
	t.Parallel()

	t.Run("renamed rule recreates acls via add queue", func(t *testing.T) {
		ctrl := prepareBanpQueueTestController(t)
		oldBanp := newTestBanp("old-rule", map[string]string{"app": "a"})
		newBanp := newTestBanp("new-rule", map[string]string{"app": "a"})

		ctrl.enqueueUpdateBanp(oldBanp, newBanp)

		require.Equal(t, 1, ctrl.addBanpQueue.Len())
		require.Zero(t, ctrl.updateBanpQueue.Len())
	})

	t.Run("renamed rule with peer change recreates acls via add queue", func(t *testing.T) {
		ctrl := prepareBanpQueueTestController(t)
		oldBanp := newTestBanp("old-rule", map[string]string{"app": "a"})
		newBanp := newTestBanp("new-rule", map[string]string{"app": "b"})

		ctrl.enqueueUpdateBanp(oldBanp, newBanp)

		require.Equal(t, 1, ctrl.addBanpQueue.Len())
		require.Zero(t, ctrl.updateBanpQueue.Len())
	})

	t.Run("peer change only goes through update queue", func(t *testing.T) {
		ctrl := prepareBanpQueueTestController(t)
		oldBanp := newTestBanp("rule", map[string]string{"app": "a"})
		newBanp := newTestBanp("rule", map[string]string{"app": "b"})

		ctrl.enqueueUpdateBanp(oldBanp, newBanp)

		require.Zero(t, ctrl.addBanpQueue.Len())
		require.Equal(t, 1, ctrl.updateBanpQueue.Len())
	})
}
