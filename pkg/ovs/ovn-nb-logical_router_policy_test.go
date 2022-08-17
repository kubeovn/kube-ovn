package ovs

import (
	"fmt"
	"testing"

	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/stretchr/testify/require"
)

func (suite *OvnClientTestSuite) testAddLogicalRouterPolicy() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lrName := "test-add-policy-lr"
	priority := 11011
	match := "ip4.src == $ovn.default.lm2_ip4"
	action := ovnnb.LogicalRouterPolicyActionAllow
	nextHops := []string{"100.64.0.2"}

	err := ovnClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	err = ovnClient.AddLogicalRouterPolicy(lrName, priority, match, action, nextHops, nil)
	require.NoError(t, err)

	lr, err := ovnClient.GetLogicalRouter(lrName, false)
	require.NoError(t, err)

	policy, err := ovnClient.GetLogicalRouterPolicy(lrName, priority, match, false)
	require.NoError(t, err)
	require.Contains(t, lr.Policies, policy.UUID)
}

func (suite *OvnClientTestSuite) testCreateLogicalRouterPolicys() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lrName := "test-create-policies-lr"
	priority := 11011
	basePort := 12300
	matchPrefix := "ip4.src == $ovn.default.lm2_ip4"
	action := ovnnb.LogicalRouterPolicyActionAllow
	policies := make([]*ovnnb.LogicalRouterPolicy, 0, 3)

	err := ovnClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	t.Run("add policies to logical router", func(t *testing.T) {
		for i := 0; i < 3; i++ {
			match := fmt.Sprintf("%s && tcp.dst == %d", matchPrefix, basePort+i)
			policy, err := ovnClient.newLogicalRouterPolicy(lrName, priority, match, action, nil, nil)
			require.NoError(t, err)
			policies = append(policies, policy)
		}

		err = ovnClient.CreateLogicalRouterPolicys(lrName, append(policies, nil)...)
		require.NoError(t, err)

		lr, err := ovnClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)

		for i := 0; i < 3; i++ {
			match := fmt.Sprintf("%s && tcp.dst == %d", matchPrefix, basePort+i)
			policy, err := ovnClient.GetLogicalRouterPolicy(lrName, priority, match, false)
			require.NoError(t, err)
			require.Equal(t, match, policy.Match)

			require.Contains(t, lr.Policies, policy.UUID)
		}
	})
}

func (suite *OvnClientTestSuite) testGetLogicalRouterPolicy() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lrName := "test_get_policy_lr"
	priority := 11000
	match := "ip4.src == $ovn.default.lm2_ip4"

	err := ovnClient.CreateBareLogicalRouterPolicy(lrName, priority, match, ovnnb.LogicalRouterPolicyActionAllow, nil)
	require.NoError(t, err)

	t.Run("priority and match are same", func(t *testing.T) {
		t.Parallel()
		policy, err := ovnClient.GetLogicalRouterPolicy(lrName, priority, match, false)
		require.NoError(t, err)
		require.Equal(t, priority, policy.Priority)
		require.Equal(t, match, policy.Match)
		require.Equal(t, ovnnb.LogicalRouterPolicyActionAllow, policy.Action)
	})

	t.Run("priority and match are not all same", func(t *testing.T) {
		t.Parallel()

		_, err = ovnClient.GetLogicalRouterPolicy(lrName, 10010, match, false)
		require.ErrorContains(t, err, "not found policy")

		_, err = ovnClient.GetLogicalRouterPolicy(lrName, priority, match+" && tcp", false)
		require.ErrorContains(t, err, "not found policy")
	})

	t.Run("should no err when priority and match are not all same but ignoreNotFound=true", func(t *testing.T) {
		t.Parallel()

		_, err := ovnClient.GetLogicalRouterPolicy(lrName, priority, match, true)
		require.NoError(t, err)
	})

	t.Run("no acl belongs to parent exist", func(t *testing.T) {
		t.Parallel()

		_, err := ovnClient.GetLogicalRouterPolicy(lrName+"_1", priority, match, false)
		require.ErrorContains(t, err, "not found policy")
	})
}

func (suite *OvnClientTestSuite) testnewLogicalRouterPolicy() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lrName := "test-new-policy-lr"
	priority := 10000
	match := "ip4.src == $ovn.default.lm2_ip4"
	nextHops := []string{"100.64.0.2"}
	action := ovnnb.LogicalRouterPolicyActionAllow

	expect := &ovnnb.LogicalRouterPolicy{
		Priority: priority,
		Match:    match,
		Action:   action,
		Nexthops: nextHops,
		ExternalIDs: map[string]string{
			logicalRouterKey: lrName,
			"key":            "value",
		},
	}

	policy, err := ovnClient.newLogicalRouterPolicy(lrName, priority, match, action, nextHops, map[string]string{"key": "value"})
	require.NoError(t, err)
	expect.UUID = policy.UUID
	require.Equal(t, expect, policy)
}
