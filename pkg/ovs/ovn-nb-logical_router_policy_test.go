package ovs

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func newLogicalRouterPolicy(priority int, match, action string, nextHops []string, externalIDs map[string]string) *ovnnb.LogicalRouterPolicy {
	return &ovnnb.LogicalRouterPolicy{
		UUID:        ovsclient.NamedUUID(),
		Priority:    priority,
		Match:       match,
		Action:      action,
		Nexthops:    nextHops,
		ExternalIDs: externalIDs,
	}
}

func (suite *OvnClientTestSuite) testAddLogicalRouterPolicy() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lrName := "test-add-policy-lr"
	priority := 11011
	match := "ip4.src == $ovn.default.lm2_ip4"
	action := ovnnb.LogicalRouterPolicyActionAllow
	nextHops := []string{"100.64.0.2"}

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	err = nbClient.AddLogicalRouterPolicy(lrName, priority, match, action, nextHops, nil)
	require.NoError(t, err)

	lr, err := nbClient.GetLogicalRouter(lrName, false)
	require.NoError(t, err)

	policyList, err := nbClient.GetLogicalRouterPolicy(lrName, priority, match, false)
	require.NoError(t, err)
	require.Len(t, policyList, 1)
	require.Contains(t, lr.Policies, policyList[0].UUID)

	err = nbClient.AddLogicalRouterPolicy(lrName, priority, match, action, nextHops, nil)
	require.NoError(t, err)

	action = ovnnb.LogicalRouterPolicyActionDrop
	err = nbClient.AddLogicalRouterPolicy(lrName, priority, match, action, nextHops, nil)
	require.NoError(t, err)
}

func (suite *OvnClientTestSuite) testCreateLogicalRouterPolicies() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lrName := "test-create-policies-lr"
	priority := 11011
	basePort := 12300
	matchPrefix := "ip4.src == $ovn.default.lm2_ip4"
	action := ovnnb.LogicalRouterPolicyActionAllow
	policies := make([]*ovnnb.LogicalRouterPolicy, 0, 3)

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	t.Run("add policies to logical router", func(t *testing.T) {
		for i := 0; i < 3; i++ {
			match := fmt.Sprintf("%s && tcp.dst == %d", matchPrefix, basePort+i)
			policy := nbClient.newLogicalRouterPolicy(priority, match, action, nil, nil)
			policies = append(policies, policy)
		}

		err = nbClient.CreateLogicalRouterPolicies(lrName, append(policies, nil)...)
		require.NoError(t, err)

		lr, err := nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)

		for i := 0; i < 3; i++ {
			match := fmt.Sprintf("%s && tcp.dst == %d", matchPrefix, basePort+i)
			policyList, err := nbClient.GetLogicalRouterPolicy(lrName, priority, match, false)
			require.NoError(t, err)
			require.Len(t, policyList, 1)
			require.Equal(t, match, policyList[0].Match)
			require.Contains(t, lr.Policies, policyList[0].UUID)
		}
	})
}

func (suite *OvnClientTestSuite) testDeleteLogicalRouterPolicy() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lrName := "test-del-policy-lr"
	priority := 11012
	match := "ip4.src == $ovn.default.lm2_ip4"
	action := ovnnb.LogicalRouterPolicyActionAllow
	nextHops := []string{"100.64.0.2"}

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	err = nbClient.AddLogicalRouterPolicy(lrName, priority, match, action, nextHops, nil)
	require.NoError(t, err)

	t.Run("no err when delete existent logical switch port", func(t *testing.T) {
		lr, err := nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)

		policyList, err := nbClient.GetLogicalRouterPolicy(lrName, priority, match, false)
		require.NoError(t, err)
		require.Len(t, policyList, 1)
		require.Contains(t, lr.Policies, policyList[0].UUID)

		err = nbClient.DeleteLogicalRouterPolicy(lrName, priority, match)
		require.NoError(t, err)

		_, err = nbClient.GetLogicalRouterPolicy(lrName, priority, match, false)
		require.ErrorContains(t, err, "not found policy")

		lr, err = nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.NotContains(t, lr.Policies, policyList[0].UUID)
	})
}

func (suite *OvnClientTestSuite) testDeleteLogicalRouterPolicies() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
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

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	prepare := func() {
		for i := 0; i < 3; i++ {
			priority := basePriority + i
			err = nbClient.AddLogicalRouterPolicy(lrName, priority, match, action, nextHops, externalIDs)
			require.NoError(t, err)
		}

		lr, err := nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Len(t, lr.Policies, 3)

		policies, err := nbClient.ListLogicalRouterPolicies(lrName, -1, externalIDs, true)
		require.NoError(t, err)
		require.Len(t, policies, 3)
	}

	t.Run("delete some policies with different priority", func(t *testing.T) {
		prepare()

		err = nbClient.DeleteLogicalRouterPolicies(lrName, -1, externalIDs)
		require.NoError(t, err)

		lr, err := nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Empty(t, lr.Policies)

		policies, err := nbClient.ListLogicalRouterPolicies(lrName, -1, externalIDs, true)
		require.NoError(t, err)
		require.Empty(t, policies)
	})

	t.Run("delete same priority", func(t *testing.T) {
		prepare()

		err = nbClient.DeleteLogicalRouterPolicies(lrName, basePriority, externalIDs)
		require.NoError(t, err)

		lr, err := nbClient.GetLogicalRouter(lrName, false)
		require.NoError(t, err)
		require.Len(t, lr.Policies, 2)

		// no basePriority policy
		policies, err := nbClient.ListLogicalRouterPolicies(lrName, -1, externalIDs, true)
		require.NoError(t, err)
		require.Len(t, policies, 2)
	})
}

func (suite *OvnClientTestSuite) testClearLogicalRouterPolicy() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lrName := "test-clear-policy-lr"
	basePriority := 11012
	match := "ip4.src == $ovn.default.lm2_ip4"
	action := ovnnb.LogicalRouterPolicyActionAllow
	nextHops := []string{"100.64.0.2"}

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		priority := basePriority + i
		err = nbClient.AddLogicalRouterPolicy(lrName, priority, match, action, nextHops, nil)
		require.NoError(t, err)
	}

	lr, err := nbClient.GetLogicalRouter(lrName, false)
	require.NoError(t, err)
	require.Len(t, lr.Policies, 3)

	for i := 0; i < 3; i++ {
		priority := basePriority + i
		_, err = nbClient.GetLogicalRouterPolicy(lrName, priority, match, false)
		require.NoError(t, err)
	}

	err = nbClient.ClearLogicalRouterPolicy(lrName)
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		priority := basePriority + i
		_, err = nbClient.GetLogicalRouterPolicy(lrName, priority, match, false)
		require.ErrorContains(t, err, "not found policy")
	}

	lr, err = nbClient.GetLogicalRouter(lrName, false)
	require.NoError(t, err)
	require.Empty(t, lr.Policies)
}

func (suite *OvnClientTestSuite) testGetLogicalRouterPolicy() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lrName := "test_get_policy_lr"
	priority := 11000
	match := "ip4.src == $ovn.default.lm2_ip4"
	fakeMatch := "ip4.dst == $ovn.default.lm2_ip4"

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	err = nbClient.AddLogicalRouterPolicy(lrName, priority, match, ovnnb.LogicalRouterPolicyActionAllow, nil, nil)
	require.NoError(t, err)

	t.Run("priority and match are same", func(t *testing.T) {
		t.Parallel()
		policyList, err := nbClient.GetLogicalRouterPolicy(lrName, priority, match, false)
		require.NoError(t, err)
		require.Len(t, policyList, 1)
		require.Equal(t, priority, policyList[0].Priority)
		require.Equal(t, match, policyList[0].Match)
		require.Equal(t, ovnnb.LogicalRouterPolicyActionAllow, policyList[0].Action)
	})

	t.Run("priority and match are not all same", func(t *testing.T) {
		t.Parallel()

		_, err = nbClient.GetLogicalRouterPolicy(lrName, 10010, match, false)
		require.ErrorContains(t, err, "not found policy")

		_, err = nbClient.GetLogicalRouterPolicy(lrName, priority, match+" && tcp", false)
		require.ErrorContains(t, err, "not found policy")
	})

	t.Run("should no err when priority and match are not all same but ignoreNotFound=true", func(t *testing.T) {
		t.Parallel()

		_, err := nbClient.GetLogicalRouterPolicy(lrName, priority, match, true)
		require.NoError(t, err)
	})

	t.Run("should no err and no policy for ignoreNotFound=true", func(t *testing.T) {
		t.Parallel()

		plcList, err := nbClient.GetLogicalRouterPolicy(lrName, priority, fakeMatch, true)
		require.Nil(t, plcList)
		require.NoError(t, err)
	})

	t.Run("no policy belongs to parent exist", func(t *testing.T) {
		t.Parallel()

		_, err := nbClient.GetLogicalRouterPolicy(lrName+"_1", priority, match, false)
		require.ErrorContains(t, err, "not found logical router")
	})
}

func (suite *OvnClientTestSuite) testGetLogicalRouterPolicyByUUID() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lrName := "test-get-policy-by-uuid-lr"
	priority := 11011
	match := "ip4.src == $ovn.default.lm2_ip4"
	action := ovnnb.LogicalRouterPolicyActionAllow
	nextHops := []string{"100.64.0.2"}

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	err = nbClient.AddLogicalRouterPolicy(lrName, priority, match, action, nextHops, nil)
	require.NoError(t, err)

	lr, err := nbClient.GetLogicalRouter(lrName, false)
	require.NoError(t, err)

	policyList, err := nbClient.GetLogicalRouterPolicy(lrName, priority, match, false)
	require.NoError(t, err)
	require.Len(t, policyList, 1)
	require.Contains(t, lr.Policies, policyList[0].UUID)

	t.Run("get lrp with right uuid", func(t *testing.T) {
		t.Parallel()

		_, err = nbClient.GetLogicalRouterPolicyByUUID(policyList[0].UUID)
		require.NoError(t, err)
	})

	t.Run("get lrp with wrong uuid", func(t *testing.T) {
		t.Parallel()

		policy, err := nbClient.GetLogicalRouterPolicyByUUID("1234334")
		require.Nil(t, policy)
		require.NotNil(t, err)
	})
}

func (suite *OvnClientTestSuite) testGetLogicalRouterPolicyByExtID() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lrName := "test-get-policy-by-extid-lr"
	priority := 11011
	match := "ip4.src == $ovn.default.lm2_ip4"
	action := ovnnb.LogicalRouterPolicyActionAllow
	nextHops := []string{"100.64.0.2"}
	extID := map[string]string{
		"vendor": "kube-ovn",
	}

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	err = nbClient.AddLogicalRouterPolicy(lrName, priority, match, action, nextHops, extID)
	require.NoError(t, err)

	lr, err := nbClient.GetLogicalRouter(lrName, false)
	require.NoError(t, err)

	policyList, err := nbClient.GetLogicalRouterPolicy(lrName, priority, match, false)
	require.NoError(t, err)
	require.Len(t, policyList, 1)
	require.Contains(t, lr.Policies, policyList[0].UUID)

	t.Run("get lrp with right extID", func(t *testing.T) {
		t.Parallel()

		pList, err := nbClient.GetLogicalRouterPoliciesByExtID(lrName, "vendor", "kube-ovn")
		require.NoError(t, err)
		require.Len(t, pList, 1)
	})

	t.Run("get lrp with wrong extID", func(t *testing.T) {
		t.Parallel()

		pList, err := nbClient.GetLogicalRouterPoliciesByExtID(lrName, "vendor", "other")
		require.NoError(t, err)
		require.Len(t, pList, 0)
	})
}

func (suite *OvnClientTestSuite) testNewLogicalRouterPolicy() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lrName := "test-new-policy-lr"
	priority := 10000
	match := "ip4.src == $ovn.default.lm2_ip4"
	nextHops := []string{"100.64.0.2"}
	action := ovnnb.LogicalRouterPolicyActionAllow

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	expect := &ovnnb.LogicalRouterPolicy{
		Priority:    priority,
		Match:       match,
		Action:      action,
		Nexthops:    nextHops,
		ExternalIDs: map[string]string{"key": "value"},
	}

	policy := nbClient.newLogicalRouterPolicy(priority, match, action, nextHops, map[string]string{"key": "value"})
	expect.UUID = policy.UUID
	require.Equal(t, expect, policy)
}

func (suite *OvnClientTestSuite) testPolicyFilter() {
	t := suite.T()
	t.Parallel()

	basePriority := 10000
	match := "ip4.src == $ovn.default.lm2_ip4"
	nextHops := []string{"100.64.0.2"}
	action := ovnnb.LogicalRouterPolicyActionAllow
	policies := make([]*ovnnb.LogicalRouterPolicy, 0)

	// create three polices
	for i := 0; i < 3; i++ {
		priority := basePriority + i
		policy := newLogicalRouterPolicy(priority, match, action, nextHops, map[string]string{"k1": "v1"})
		policies = append(policies, policy)
	}

	// create two polices with different external-ids
	for i := 0; i < 2; i++ {
		priority := basePriority + i
		policy := newLogicalRouterPolicy(priority, match, action, nextHops, map[string]string{"k1": "v2"})
		policies = append(policies, policy)
	}

	t.Run("include all policies", func(t *testing.T) {
		filterFunc := policyFilter(-1, nil, true)
		count := 0
		for _, policy := range policies {
			if filterFunc(policy) {
				count++
			}
		}
		require.Equal(t, 5, count)
	})

	t.Run("include all policies with external ids", func(t *testing.T) {
		filterFunc := policyFilter(-1, map[string]string{"k1": "v1"}, true)
		count := 0
		for _, policy := range policies {
			if filterFunc(policy) {
				count++
			}
		}
		require.Equal(t, 3, count)
	})

	t.Run("include all policies with same priority", func(t *testing.T) {
		filterFunc := policyFilter(10000, map[string]string{"k1": "v1"}, true)
		count := 0
		for _, policy := range policies {
			if filterFunc(policy) {
				count++
			}
		}
		require.Equal(t, 1, count)
	})

	t.Run("result should exclude policies when externalIDs's length is not equal", func(t *testing.T) {
		t.Parallel()

		policy := newLogicalRouterPolicy(basePriority+10, match, action, nextHops, map[string]string{"k1": "v1"})
		filterFunc := policyFilter(-1, map[string]string{
			"k1":  "v1",
			"key": "value",
		}, true)
		require.False(t, filterFunc(policy))
	})
}

func (suite *OvnClientTestSuite) testDeleteRouterPolicy() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lrName := "test-delete-policy-lr"
	priority := 11011
	match := "ip4.src == $ovn.default.lm2_ip4"
	action := ovnnb.LogicalRouterPolicyActionAllow
	nextHops := []string{"100.64.0.2"}

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	err = nbClient.AddLogicalRouterPolicy(lrName, priority, match, action, nextHops, nil)
	require.NoError(t, err)

	lr, err := nbClient.GetLogicalRouter(lrName, false)
	require.NoError(t, err)

	policyList, err := nbClient.GetLogicalRouterPolicy(lrName, priority, match, false)
	require.NoError(t, err)
	require.Len(t, policyList, 1)
	require.Contains(t, lr.Policies, policyList[0].UUID)

	err = nbClient.DeleteRouterPolicy(lr, policyList[0].UUID)
	require.NoError(t, err)
}

func (suite *OvnClientTestSuite) testDeleteLogicalRouterPolicyByNexthop() {
	t := suite.T()
	t.Parallel()

	nbClient := suite.ovnNBClient
	lrName := "test-delete-policy-by-next-hop-lr"
	priority := 11011
	match := "ip4.src == $ovn.default.lm2_ip4"
	action := ovnnb.LogicalRouterPolicyActionAllow
	nextHops := []string{"100.64.0.2"}

	err := nbClient.CreateLogicalRouter(lrName)
	require.NoError(t, err)

	err = nbClient.AddLogicalRouterPolicy(lrName, priority, match, action, nextHops, nil)
	require.NoError(t, err)

	lr, err := nbClient.GetLogicalRouter(lrName, false)
	require.NoError(t, err)

	policyList, err := nbClient.GetLogicalRouterPolicy(lrName, priority, match, false)
	require.NoError(t, err)
	require.Len(t, policyList, 1)
	require.Contains(t, lr.Policies, policyList[0].UUID)

	err = nbClient.DeleteLogicalRouterPolicyByNexthop(lrName, priority, nextHops[0])
	require.NoError(t, err)

	err = nbClient.DeleteLogicalRouterPolicyByNexthop(lrName, priority+1, nextHops[0])
	require.NoError(t, err)
}
