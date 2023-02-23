package ovs

import (
	"fmt"
	"testing"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
	"github.com/stretchr/testify/require"
)

func newLogicalRouterPolicy(lrName string, priority int, match, action string, nextHops []string, externalIDs map[string]string) *ovnnb.LogicalRouterPolicy {
	policy := &ovnnb.LogicalRouterPolicy{
		UUID:     ovsclient.NamedUUID(),
		Priority: priority,
		Match:    match,
		Action:   action,
		Nexthops: nextHops,
		ExternalIDs: map[string]string{
			logicalRouterKey: lrName,
		},
	}

	for k, v := range externalIDs {
		policy.ExternalIDs[k] = v
	}

	return policy
}

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

	err = ovnClient.AddLogicalRouterPolicy(lrName, priority, match, action, nextHops, nil)
	require.NoError(t, err)
}

func (suite *OvnClientTestSuite) testCreateLogicalRouterPolicies() {
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

		err = ovnClient.CreateLogicalRouterPolicies(lrName, append(policies, nil)...)
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

func (suite *OvnClientTestSuite) testDeleteLogicalRouterPolicy() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lrName := "test-del-policy-lr"
	priority := 11012
	match := "ip4.src == $ovn.default.lm2_ip4"
	action := ovnnb.LogicalRouterPolicyActionAllow
	nextHops := []string{"100.64.0.2"}

	err := ovnClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	err = ovnClient.AddLogicalRouterPolicy(lrName, priority, match, action, nextHops, nil)
	require.NoError(t, err)

	t.Run("no err when delete existent logical switch port", func(t *testing.T) {
		lr, err := ovnClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)

		policy, err := ovnClient.GetLogicalRouterPolicy(lrName, priority, match, false)
		require.NoError(t, err)
		require.Contains(t, lr.Policies, policy.UUID)

		err = ovnClient.DeleteLogicalRouterPolicy(lrName, priority, match)
		require.NoError(t, err)

		_, err = ovnClient.GetLogicalRouterPolicy(lrName, priority, match, false)
		require.ErrorContains(t, err, "not found policy")

		lr, err = ovnClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.NotContains(t, lr.Policies, policy.UUID)
	})
}

func (suite *OvnClientTestSuite) testDeleteLogicalRouterPolicies() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lrName := "test-clear-policies-lr"
	basePriority := 12100
	match := "ip4.src == $ovn.default.lm2_ip4"
	action := ovnnb.LogicalRouterPolicyActionAllow
	nextHops := []string{"100.64.0.2"}

	externalIDs := map[string]string{
		"vendor":           util.CniTypeName,
		"subnet":           "test-subnet",
		"isU2ORoutePolicy": "true",
	}

	err := ovnClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	prepare := func() {
		for i := 0; i < 3; i++ {
			priority := basePriority + i
			err = ovnClient.AddLogicalRouterPolicy(lrName, priority, match, action, nextHops, externalIDs)
			require.NoError(t, err)
		}

		lr, err := ovnClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Len(t, lr.Policies, 3)

		policies, err := ovnClient.ListLogicalRouterPolicies(-1, externalIDs)
		require.NoError(t, err)
		require.Len(t, policies, 3)
	}

	t.Run("delete some policies with different priority", func(t *testing.T) {
		prepare()

		err = ovnClient.DeleteLogicalRouterPolicies(lrName, -1, externalIDs)
		require.NoError(t, err)

		lr, err := ovnClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Empty(t, lr.Policies)

		policies, err := ovnClient.ListLogicalRouterPolicies(-1, externalIDs)
		require.NoError(t, err)
		require.Empty(t, policies)
	})

	t.Run("delete same priority", func(t *testing.T) {
		prepare()

		err = ovnClient.DeleteLogicalRouterPolicies(lrName, basePriority, externalIDs)
		require.NoError(t, err)

		lr, err := ovnClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Len(t, lr.Policies, 2)

		// no basePriority policy
		policies, err := ovnClient.ListLogicalRouterPolicies(-1, externalIDs)
		require.NoError(t, err)
		require.Len(t, policies, 2)
	})
}

func (suite *OvnClientTestSuite) testClearLogicalRouterPolicy() {
	t := suite.T()
	t.Parallel()

	ovnClient := suite.ovnClient
	lrName := "test-clear-policy-lr"
	basePriority := 11012
	match := "ip4.src == $ovn.default.lm2_ip4"
	action := ovnnb.LogicalRouterPolicyActionAllow
	nextHops := []string{"100.64.0.2"}

	err := ovnClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		priority := basePriority + i
		err = ovnClient.AddLogicalRouterPolicy(lrName, priority, match, action, nextHops, nil)
		require.NoError(t, err)
	}

	lr, err := ovnClient.GetLogicalRouter(lrName, false)
	require.NoError(t, err)
	require.Len(t, lr.Policies, 3)

	for i := 0; i < 3; i++ {
		priority := basePriority + i
		_, err = ovnClient.GetLogicalRouterPolicy(lrName, priority, match, false)
		require.NoError(t, err)
	}

	err = ovnClient.ClearLogicalRouterPolicy(lrName)
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		priority := basePriority + i
		_, err = ovnClient.GetLogicalRouterPolicy(lrName, priority, match, false)
		require.ErrorContains(t, err, "not found policy")
	}

	lr, err = ovnClient.GetLogicalRouter(lrName, false)
	require.NoError(t, err)
	require.Empty(t, lr.Policies)
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

func (suite *OvnClientTestSuite) test_newLogicalRouterPolicy() {
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

func (suite *OvnClientTestSuite) test_policyFilter() {
	t := suite.T()
	t.Parallel()

	lrName := "test-filter-policy-lr"
	basePriority := 10000
	match := "ip4.src == $ovn.default.lm2_ip4"
	nextHops := []string{"100.64.0.2"}
	action := ovnnb.LogicalRouterPolicyActionAllow
	policies := make([]*ovnnb.LogicalRouterPolicy, 0)

	// create three polices
	for i := 0; i < 3; i++ {
		priority := basePriority + i
		policy := newLogicalRouterPolicy(lrName, priority, match, action, nextHops, nil)
		policies = append(policies, policy)
	}

	// create two polices with other logical router key
	for i := 0; i < 2; i++ {
		priority := basePriority + i
		policy := newLogicalRouterPolicy(lrName, priority, match, action, nextHops, nil)
		policy.ExternalIDs[logicalRouterKey] = lrName + "-test"
		policies = append(policies, policy)
	}

	t.Run("include all policies", func(t *testing.T) {
		filterFunc := policyFilter(-1, nil)
		count := 0
		for _, policy := range policies {
			if filterFunc(policy) {
				count++
			}
		}
		require.Equal(t, count, 5)
	})

	t.Run("include all policies with external ids", func(t *testing.T) {
		filterFunc := policyFilter(-1, map[string]string{logicalRouterKey: lrName})
		count := 0
		for _, policy := range policies {
			if filterFunc(policy) {
				count++
			}
		}
		require.Equal(t, count, 3)
	})

	t.Run("include all policies with same priority", func(t *testing.T) {
		filterFunc := policyFilter(10000, map[string]string{logicalRouterKey: lrName})
		count := 0
		for _, policy := range policies {
			if filterFunc(policy) {
				count++
			}
		}
		require.Equal(t, count, 1)
	})

	t.Run("result should exclude policies when externalIDs's length is not equal", func(t *testing.T) {
		t.Parallel()

		policy := newLogicalRouterPolicy(lrName, basePriority+10, match, action, nextHops, nil)
		filterFunc := policyFilter(-1, map[string]string{
			logicalRouterKey: lrName,
			"key":            "value",
		})

		require.False(t, filterFunc(policy))
	})
}
