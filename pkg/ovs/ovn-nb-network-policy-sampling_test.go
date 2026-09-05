package ovs

import (
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/aclsampling"
	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestApplyNetworkPolicyACLSamplingSelectsEligibleACLs(t *testing.T) {
	client := newACLSamplingTestClient(t, "network-policy-attachment")
	const pgName = "sample-policy.default"
	require.NoError(t, client.CreatePortGroup(pgName, nil))
	waitForPortGroup(t, client, pgName)

	eligibleAllow := newNetworkPolicySamplingTestACL(t, client, pgName, ovnnb.ACLDirectionToLport, util.IngressAllowPriority, ovnnb.ACLActionAllowRelated, "np/sample-policy.default/ingress/IPv4/0")
	eligibleIPBlock := newNetworkPolicySamplingTestACL(t, client, pgName, ovnnb.ACLDirectionFromLport, util.EgressAllowPriority, ovnnb.ACLActionAllowRelated, "np/sample-policy.default/egress/IPv6/1/ipBlock")
	eligibleDefaultDeny := newNetworkPolicySamplingTestACL(t, client, pgName, ovnnb.ACLDirectionToLport, util.IngressDefaultDrop, ovnnb.ACLActionDrop, "default/sample-policy")
	unmarkedAllow := newNetworkPolicySamplingTestACL(t, client, pgName, ovnnb.ACLDirectionToLport, util.IngressAllowPriority, ovnnb.ACLActionAllowRelated, "np/sample-policy.default/ingress/IPv4/2")
	delete(unmarkedAllow.ExternalIDs, networkPolicyACLNameExternalID)
	unmarkedAllow.ExternalIDs[ExternalIDVendor] = "external-observer"
	syntheticAll := newNetworkPolicySamplingTestACL(t, client, pgName, ovnnb.ACLDirectionToLport, util.IngressAllowPriority, ovnnb.ACLActionAllowRelated, "np/sample-policy.default/ingress/IPv4/all")
	dhcpException := newNetworkPolicySamplingTestACL(t, client, pgName, ovnnb.ACLDirectionToLport, util.IngressAllowPriority, ovnnb.ACLActionAllowRelated, "default/sample-policy")
	gateway := newNetworkPolicySamplingTestACL(t, client, pgName, ovnnb.ACLDirectionToLport, util.IngressAllowPriority, ovnnb.ACLActionAllowStateless, "np/sample-policy.default/ingress/IPv6/0")
	require.NoError(t, client.CreateAcls(pgName, portGroupKey, eligibleAllow, eligibleIPBlock, eligibleDefaultDeny, unmarkedAllow, syntheticAll, dhcpException, gateway))
	waitForACLCount(t, client, pgName, 7)

	request, err := client.PrepareNetworkPolicyACLSampling(pgName, "default", "sample-policy", "11111111-2222-3333-4444-555555555555")
	require.NoError(t, err)
	config := validACLSamplingConfig()
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		require.NoError(collect, client.ApplyNetworkPolicyACLSampling(config, request))
	}, 3*time.Second, 20*time.Millisecond)

	var actual []ovnnb.ACL
	require.Eventually(t, func() bool {
		var listErr error
		actual, listErr = client.ListAcls("", map[string]string{aclParentKey: pgName})
		if listErr != nil {
			return false
		}
		return countSampledACLs(actual) == 3
	}, 3*time.Second, 20*time.Millisecond)

	allow := aclByName(t, actual, "np/sample-policy.default/ingress/IPv4/0")
	require.NotNil(t, allow.SampleNew)
	require.NotNil(t, allow.SampleEst)
	require.Equal(t, *allow.SampleNew, *allow.SampleEst)
	require.Equal(t, "0", allow.ExternalIDs[policyRuleIndexExternalID])
	require.Equal(t, "allow", allow.ExternalIDs[policyVerdictExternalID])
	require.Equal(t, string(aclsampling.RoleRuleAllow), allow.ExternalIDs[sampleRoleExternalID])
	require.Equal(t, string(aclsampling.DirectionIngress), allow.ExternalIDs[policyDirectionExternalID])
	require.Equal(t, "sample-policy", allow.ExternalIDs[policyNameExternalID])
	require.Len(t, allow.ExternalIDs[aclMatchHashExternalID], 64)

	deny := aclByNameAndAction(t, actual, "default/sample-policy", ovnnb.ACLActionDrop)
	require.NotNil(t, deny.SampleNew)
	require.Nil(t, deny.SampleEst)
	require.NotContains(t, deny.ExternalIDs, policyRuleIndexExternalID)
	require.Equal(t, string(aclsampling.RoleDefaultDeny), deny.ExternalIDs[policyVerdictExternalID])

	unmarked := aclByName(t, actual, "np/sample-policy.default/ingress/IPv4/2")
	require.Nil(t, unmarked.SampleNew)
	require.Nil(t, unmarked.SampleEst)
	require.NotContains(t, unmarked.ExternalIDs, sampleFeatureExternalID)

	for _, name := range []string{
		"np/sample-policy.default/ingress/IPv4/all",
		"np/sample-policy.default/ingress/IPv6/0",
	} {
		acl := aclByName(t, actual, name)
		require.Nil(t, acl.SampleNew)
		require.Nil(t, acl.SampleEst)
		require.NotContains(t, acl.ExternalIDs, sampleFeatureExternalID)
	}
	for _, acl := range actual {
		if acl.Name != nil && *acl.Name == "default/sample-policy" && acl.Action == ovnnb.ACLActionAllowRelated {
			require.Nil(t, acl.SampleNew)
			require.NotContains(t, acl.ExternalIDs, sampleFeatureExternalID)
		}
	}

	config.AllowProbabilityPercent = 0
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		require.NoError(collect, client.ApplyNetworkPolicyACLSampling(config, request))
	}, 3*time.Second, 20*time.Millisecond)
	require.Eventually(t, func() bool {
		acls, listErr := client.ListAcls("", map[string]string{aclParentKey: pgName})
		if listErr != nil {
			return false
		}
		allow = aclByName(t, acls, "np/sample-policy.default/ingress/IPv4/0")
		deny = aclByNameAndAction(t, acls, "default/sample-policy", ovnnb.ACLActionDrop)
		return allow.SampleNew == nil && allow.SampleEst == nil &&
			allow.ExternalIDs[sampleFeatureExternalID] == "" && deny.SampleNew != nil
	}, 3*time.Second, 20*time.Millisecond)
}

func TestNetworkPolicyACLSamplingReusesSnapshotMetadata(t *testing.T) {
	client := newACLSamplingTestClient(t, "network-policy-prior-metadata")
	const pgName = "collision-policy.default"
	const policyUID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	require.NoError(t, client.CreatePortGroup(pgName, nil))
	waitForPortGroup(t, client, pgName)

	oldACL := newNetworkPolicySamplingTestACL(t, client, pgName, ovnnb.ACLDirectionToLport, util.IngressAllowPriority, ovnnb.ACLActionAllowRelated, "np/collision-policy.default/ingress/IPv4/0")
	allocation, err := aclsampling.NewAllocator(nil)
	require.NoError(t, err)
	expected, err := allocation.Allocate(aclsampling.SampleKey{
		SchemaVersion: aclsampling.SchemaVersionV1,
		PolicyUID:     policyUID,
		Direction:     aclsampling.DirectionIngress,
		RuleIndex:     new(0),
		Role:          aclsampling.RoleRuleAllow,
		Protocol:      "IPv4",
		ACLMatchHash:  aclsampling.HashACLMatch(oldACL.Match),
		OVNAction:     aclsampling.ActionAllowRelated,
	})
	require.NoError(t, err)
	oldACL.ExternalIDs[sampleFeatureExternalID] = networkPolicySampleFeature
	oldACL.ExternalIDs[sampleSchemaVersionExternalID] = aclsampling.SchemaVersionV1
	oldACL.ExternalIDs[sampleKeyHashExternalID] = expected.KeyHash
	oldACL.ExternalIDs[sampleMetadataExternalID] = "42"
	require.NoError(t, client.CreateAcls(pgName, portGroupKey, oldACL))
	waitForACLCount(t, client, pgName, 1)

	request, err := client.PrepareNetworkPolicyACLSampling(pgName, "default", "collision-policy", policyUID)
	require.NoError(t, err)
	require.NoError(t, client.DeleteAcls(pgName, portGroupKey, "", nil))
	waitForACLCount(t, client, pgName, 0)
	replacement := newNetworkPolicySamplingTestACL(t, client, pgName, ovnnb.ACLDirectionToLport, util.IngressAllowPriority, ovnnb.ACLActionAllowRelated, "np/collision-policy.default/ingress/IPv4/0")
	require.NoError(t, client.CreateAcls(pgName, portGroupKey, replacement))
	waitForACLCount(t, client, pgName, 1)

	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		require.NoError(collect, client.ApplyNetworkPolicyACLSampling(validACLSamplingConfig(), request))
	}, 3*time.Second, 20*time.Millisecond)
	require.Eventually(t, func() bool {
		acls, listErr := client.ListAcls("", map[string]string{aclParentKey: pgName})
		return listErr == nil && len(acls) == 1 && acls[0].ExternalIDs[sampleMetadataExternalID] == "42"
	}, 3*time.Second, 20*time.Millisecond)
}

func TestNetworkPolicyACLSamplingReusesSampleCreatedInSameRun(t *testing.T) {
	client := newACLSamplingTestClient(t, "network-policy-created-sample-reuse")
	const pgName = "duplicate-rule.default"
	require.NoError(t, client.CreatePortGroup(pgName, nil))
	waitForPortGroup(t, client, pgName)

	const aclName = "np/duplicate-rule.default/ingress/IPv4/0"
	first := newNetworkPolicySamplingTestACL(t, client, pgName, ovnnb.ACLDirectionToLport, util.IngressAllowPriority, ovnnb.ACLActionAllowRelated, aclName)
	second := newNetworkPolicySamplingTestACL(t, client, pgName, ovnnb.ACLDirectionToLport, util.IngressAllowPriority, ovnnb.ACLActionAllowRelated, aclName)
	require.NoError(t, client.CreateAcls(pgName, portGroupKey, first, second))
	waitForACLCount(t, client, pgName, 2)

	request, err := client.PrepareNetworkPolicyACLSampling(pgName, "default", "duplicate-rule", "bbbbbbbb-cccc-dddd-eeee-ffffffffffff")
	require.NoError(t, err)
	require.NoError(t, client.ApplyNetworkPolicyACLSampling(validACLSamplingConfig(), request))
	require.Eventually(t, func() bool {
		acls, listErr := client.ListAcls("", map[string]string{aclParentKey: pgName})
		if listErr != nil || len(acls) != 2 || acls[0].SampleNew == nil || acls[1].SampleNew == nil {
			return false
		}
		return *acls[0].SampleNew == *acls[1].SampleNew
	}, time.Second, 10*time.Millisecond)
}

func TestNetworkPolicyACLSamplingPreservesUnownedReferences(t *testing.T) {
	client := newACLSamplingTestClient(t, "network-policy-unowned-reference")
	const pgName = "external-sample.default"
	require.NoError(t, client.CreatePortGroup(pgName, nil))
	waitForPortGroup(t, client, pgName)

	acl := newNetworkPolicySamplingTestACL(t, client, pgName, ovnnb.ACLDirectionToLport, util.IngressAllowPriority, ovnnb.ACLActionAllowRelated, "np/external-sample.default/ingress/IPv4/0")
	independent := newNetworkPolicySamplingTestACL(t, client, pgName, ovnnb.ACLDirectionToLport, util.IngressAllowPriority, ovnnb.ACLActionAllowRelated, "np/external-sample.default/ingress/IPv4/1")
	require.NoError(t, client.CreateAcls(pgName, portGroupKey, acl, independent))
	waitForACLCount(t, client, pgName, 2)
	actual, err := client.ListAcls("", map[string]string{aclParentKey: pgName})
	require.NoError(t, err)
	actualACL := aclByName(t, actual, "np/external-sample.default/ingress/IPv4/0")
	acl = &actualACL
	externalSample := &ovnnb.Sample{UUID: ovsclient.NamedUUID(), Metadata: 99}
	createOps, err := client.Create(externalSample)
	require.NoError(t, err)
	acl.SampleNew = &externalSample.UUID
	updateOps, err := client.Database.Where(acl).Update(acl, &acl.SampleNew)
	require.NoError(t, err)
	require.NoError(t, client.Transact("seed-unowned-acl-sample-reference", append(createOps, updateOps...)))
	require.Eventually(t, func() bool {
		acls, listErr := client.ListAcls("", map[string]string{aclParentKey: pgName})
		return listErr == nil && len(acls) == 2 && aclByName(t, acls, "np/external-sample.default/ingress/IPv4/0").SampleNew != nil
	}, time.Second, 10*time.Millisecond)

	request, err := client.PrepareNetworkPolicyACLSampling(pgName, "default", "external-sample", "ffffffff-eeee-dddd-cccc-bbbbbbbbbbbb")
	require.NoError(t, err)
	config := validACLSamplingConfig()
	require.NoError(t, client.ReconcileACLSampling(config))
	waitForACLSamplingObjects(t, client)
	err = client.ApplyNetworkPolicyACLSampling(config, request)
	require.ErrorContains(t, err, "has unowned sample references")
	require.Eventually(t, func() bool {
		acls, listErr := client.ListAcls("", map[string]string{aclParentKey: pgName})
		if listErr != nil {
			return false
		}
		attached := aclByName(t, acls, "np/external-sample.default/ingress/IPv4/1")
		return attached.SampleNew != nil && attached.SampleEst != nil
	}, time.Second, 10*time.Millisecond)

	config.AllowProbabilityPercent = 0
	require.NoError(t, client.ApplyNetworkPolicyACLSampling(config, request))
	actual, err = client.ListAcls("", map[string]string{aclParentKey: pgName})
	require.NoError(t, err)
	require.Len(t, actual, 2)
	preserved := aclByName(t, actual, "np/external-sample.default/ingress/IPv4/0")
	require.NotNil(t, preserved.SampleNew)
	require.NotContains(t, preserved.ExternalIDs, sampleFeatureExternalID)
}

func TestClassifyNetworkPolicySamplingACLUsesUntruncatedName(t *testing.T) {
	fullName := "np/" + strings.Repeat("long-policy-name.", 5) + "default/ingress/IPv4/12/ipBlock"
	priority, err := strconv.Atoi(util.IngressAllowPriority)
	require.NoError(t, err)
	acl := ovnnb.ACL{
		Action:    ovnnb.ACLActionAllowRelated,
		Direction: ovnnb.ACLDirectionToLport,
		Priority:  priority,
		Tier:      util.NetpolACLTier,
		ExternalIDs: map[string]string{
			networkPolicyACLNameExternalID: fullName,
			ExternalIDVendor:               util.CniTypeName,
		},
	}
	setACLName(&acl, fullName)
	require.LessOrEqual(t, len(*acl.Name), 63)

	candidate, ok, err := classifyNetworkPolicySamplingACL(acl)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 12, *candidate.ruleIndex)
	require.Equal(t, "IPv4", candidate.protocol)
}

func TestNetworkPolicySampleMetadata(t *testing.T) {
	type testCase struct {
		name     string
		value    int
		expected uint32
		wantErr  bool
	}
	tests := []testCase{
		{name: "negative", value: -1, wantErr: true},
		{name: "zero", value: 0, wantErr: true},
		{name: "positive", value: 1, expected: 1},
	}
	if strconv.IntSize == 64 {
		maxUint32 := uint64(math.MaxUint32)
		tests = append(tests,
			testCase{name: "maximum", value: int(maxUint32), expected: math.MaxUint32},
			testCase{name: "overflow", value: int(maxUint32 + 1), wantErr: true},
		)
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual, err := networkPolicySampleMetadata(test.value)
			if test.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.expected, actual)
		})
	}
}

func newNetworkPolicySamplingTestACL(t *testing.T, client *OVNNbClient, pgName, direction, priority, action, name string) *ovnnb.ACL {
	t.Helper()
	acl, err := client.newACLWithoutCheck(pgName, direction, priority, "ip && test == "+strconv.Quote(name), action, util.NetpolACLTier, func(acl *ovnnb.ACL) {
		setNetworkPolicyACLName(acl, name)
	})
	require.NoError(t, err)
	return acl
}

func waitForPortGroup(t *testing.T, client *OVNNbClient, pgName string) {
	t.Helper()
	require.Eventually(t, func() bool {
		pg, err := client.GetPortGroup(pgName, true)
		return err == nil && pg != nil
	}, time.Second, 10*time.Millisecond)
}

func waitForACLCount(t *testing.T, client *OVNNbClient, pgName string, count int) {
	t.Helper()
	require.Eventually(t, func() bool {
		acls, err := client.ListAcls("", map[string]string{aclParentKey: pgName})
		return err == nil && len(acls) == count
	}, time.Second, 10*time.Millisecond)
}

func countSampledACLs(acls []ovnnb.ACL) int {
	count := 0
	for _, acl := range acls {
		if acl.ExternalIDs[sampleFeatureExternalID] == networkPolicySampleFeature {
			count++
		}
	}
	return count
}

func aclByName(t *testing.T, acls []ovnnb.ACL, name string) ovnnb.ACL {
	t.Helper()
	for _, acl := range acls {
		if acl.Name != nil && *acl.Name == name {
			return acl
		}
	}
	t.Fatalf("ACL %q not found", name)
	return ovnnb.ACL{}
}

func aclByNameAndAction(t *testing.T, acls []ovnnb.ACL, name, action string) ovnnb.ACL {
	t.Helper()
	for _, acl := range acls {
		if acl.Name != nil && *acl.Name == name && acl.Action == action {
			return acl
		}
	}
	t.Fatalf("ACL %q with action %q not found", name, action)
	return ovnnb.ACL{}
}
