package ovs

import (
	"errors"
	"fmt"
	"maps"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubeovn/kube-ovn/pkg/aclsampling"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

func TestResolveNetworkPolicyACLSample(t *testing.T) {
	client, config, allow, deny := newNetworkPolicySampleResolverFixture(t, "event-resolver")
	allowMetadata := parseSampleMetadataForTest(t, allow)
	const datapathKey = uint32(0x0abcde)
	newCookie := aclSampleCookieForTest(config.AppIDNew, datapathKey, allowMetadata)
	reference, err := aclsampling.ParseSampleReference(newCookie)
	require.NoError(t, err)
	event, err := client.ResolveNetworkPolicyACLSample(reference)
	require.NoError(t, err)
	require.Equal(t, aclsampling.VerdictAllow, event.Verdict)
	require.NotNil(t, event.Policy)
	require.Nil(t, event.PolicyOwner)
	require.Equal(t, 3, *event.Policy.RuleIndex)
	require.Equal(t, aclsampling.ApplicationACLNew, event.Sample.App)
	require.Equal(t, config.AppIDNew, *event.Sample.ApplicationID)
	require.Equal(t, datapathKey, *event.Sample.DatapathKey)
	require.Equal(t, config.AppIDNew<<24|datapathKey, *event.Sample.ObservationDomain)
	require.Equal(t, allowMetadata, event.Sample.Metadata)
	require.Equal(t, allow.UUID, event.OVN.UUID)

	establishedCookie := aclSampleCookieForTest(config.AppIDEstablished, datapathKey, allowMetadata)
	reference, err = aclsampling.ParseSampleReference(establishedCookie)
	require.NoError(t, err)
	event, err = client.ResolveNetworkPolicyACLSample(reference)
	require.NoError(t, err)
	require.Equal(t, aclsampling.ApplicationACLEst, event.Sample.App)
	require.Equal(t, aclsampling.VerdictAllow, event.Verdict)

	metadataOnly, err := aclsampling.ParseSampleReference(strconv.FormatUint(uint64(allowMetadata), 10))
	require.NoError(t, err)
	event, err = client.ResolveNetworkPolicyACLSample(metadataOnly)
	require.NoError(t, err)
	require.Empty(t, event.Sample.App)
	require.Nil(t, event.Sample.ObservationDomain)
	require.Nil(t, event.Sample.ApplicationID)
	require.Nil(t, event.Sample.DatapathKey)

	denyMetadata := parseSampleMetadataForTest(t, deny)
	denyCookie := aclSampleCookieForTest(config.AppIDNew, datapathKey, denyMetadata)
	reference, err = aclsampling.ParseSampleReference(denyCookie)
	require.NoError(t, err)
	event, err = client.ResolveNetworkPolicyACLSample(reference)
	require.NoError(t, err)
	require.Equal(t, aclsampling.VerdictDefaultDeny, event.Verdict)
	require.Nil(t, event.Policy)
	require.NotNil(t, event.PolicyOwner)
	require.Nil(t, event.PolicyOwner.RuleIndex)
	require.Equal(t, aclsampling.ReasonNetworkPolicyDefaultDeny, event.Reason)
	require.Equal(t, aclsampling.AttributionNonExclusive, event.Attribution)

	establishedCookie = aclSampleCookieForTest(config.AppIDEstablished, datapathKey, denyMetadata)
	reference, err = aclsampling.ParseSampleReference(establishedCookie)
	require.NoError(t, err)
	_, err = client.ResolveNetworkPolicyACLSample(reference)
	require.ErrorIs(t, err, ErrACLSampleNotFound)
}

func TestResolveNetworkPolicyACLSampleRejectsInconsistentOwner(t *testing.T) {
	t.Run("unowned ACL", func(t *testing.T) {
		client, config, allow, _ := newNetworkPolicySampleResolverFixture(t, "event-resolver-unowned")
		metadata := parseSampleMetadataForTest(t, allow)
		allow.ExternalIDs = maps.Clone(allow.ExternalIDs)
		delete(allow.ExternalIDs, ExternalIDVendor)
		require.NoError(t, client.UpdateACL(&allow, &allow.ExternalIDs))

		reference, err := aclsampling.ParseSampleReference(aclSampleCookieForTest(config.AppIDNew, 1, metadata))
		require.NoError(t, err)
		require.EventuallyWithT(t, func(collect *assert.CollectT) {
			_, err := client.ResolveNetworkPolicyACLSample(reference)
			require.ErrorIs(collect, err, ErrACLSampleNotFound)
		}, 3*time.Second, 20*time.Millisecond)
	})

	t.Run("tampered policy UID", func(t *testing.T) {
		client, config, allow, _ := newNetworkPolicySampleResolverFixture(t, "event-resolver-uid")
		metadata := parseSampleMetadataForTest(t, allow)
		allow.ExternalIDs = maps.Clone(allow.ExternalIDs)
		allow.ExternalIDs[policyUIDExternalID] = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
		require.NoError(t, client.UpdateACL(&allow, &allow.ExternalIDs))

		reference, err := aclsampling.ParseSampleReference(aclSampleCookieForTest(config.AppIDNew, 1, metadata))
		require.NoError(t, err)
		require.EventuallyWithT(t, func(collect *assert.CollectT) {
			_, err := client.ResolveNetworkPolicyACLSample(reference)
			require.ErrorContains(collect, err, "sample key hash does not match")
		}, 3*time.Second, 20*time.Millisecond)
	})

	t.Run("tampered policy name", func(t *testing.T) {
		client, config, allow, _ := newNetworkPolicySampleResolverFixture(t, "event-resolver-name")
		metadata := parseSampleMetadataForTest(t, allow)
		allow.ExternalIDs = maps.Clone(allow.ExternalIDs)
		allow.ExternalIDs[policyNameExternalID] = "another-policy"
		require.NoError(t, client.UpdateACL(&allow, &allow.ExternalIDs))

		reference, err := aclsampling.ParseSampleReference(aclSampleCookieForTest(config.AppIDNew, 1, metadata))
		require.NoError(t, err)
		require.EventuallyWithT(t, func(collect *assert.CollectT) {
			_, err := client.ResolveNetworkPolicyACLSample(reference)
			require.ErrorContains(collect, err, "ACL name does not match")
		}, 3*time.Second, 20*time.Millisecond)
	})

	t.Run("allow references different samples", func(t *testing.T) {
		client, config, allow, _ := newNetworkPolicySampleResolverFixture(t, "event-resolver-allow-refs")
		metadata := parseSampleMetadataForTest(t, allow)
		allow.SampleEst = nil
		require.NoError(t, client.UpdateACL(&allow, &allow.SampleEst))

		reference, err := aclsampling.ParseSampleReference(aclSampleCookieForTest(config.AppIDNew, 1, metadata))
		require.NoError(t, err)
		require.EventuallyWithT(t, func(collect *assert.CollectT) {
			_, err := client.ResolveNetworkPolicyACLSample(reference)
			require.ErrorContains(collect, err, "same Sample")
		}, 3*time.Second, 20*time.Millisecond)
	})

	t.Run("default deny references established sample", func(t *testing.T) {
		client, config, _, deny := newNetworkPolicySampleResolverFixture(t, "event-resolver-deny-est")
		metadata := parseSampleMetadataForTest(t, deny)
		deny.SampleEst = deny.SampleNew
		require.NoError(t, client.UpdateACL(&deny, &deny.SampleEst))

		reference, err := aclsampling.ParseSampleReference(aclSampleCookieForTest(config.AppIDEstablished, 1, metadata))
		require.NoError(t, err)
		require.EventuallyWithT(t, func(collect *assert.CollectT) {
			_, err := client.ResolveNetworkPolicyACLSample(reference)
			require.ErrorContains(collect, err, "only from sample_new")
		}, 3*time.Second, 20*time.Millisecond)
	})
}

func TestResolveNetworkPolicyACLSampleRejectsUnknownValues(t *testing.T) {
	client := newACLSamplingTestClient(t, "event-resolver-not-found")
	require.NoError(t, client.ReconcileACLSampling(validACLSamplingConfig()))
	waitForACLSamplingObjects(t, client)

	_, err := client.ResolveNetworkPolicyACLSample(aclsampling.SampleReference{Metadata: 999})
	require.ErrorIs(t, err, ErrACLSampleNotFound)

	reference, parseErr := aclsampling.ParseSampleReference(aclSampleCookieForTest(200, 1, 999))
	require.NoError(t, parseErr)
	_, err = client.ResolveNetworkPolicyACLSample(reference)
	require.ErrorIs(t, err, ErrACLSampleNotFound)

	_, err = client.ResolveNetworkPolicyACLSample(aclsampling.SampleReference{})
	require.Error(t, err)
	require.False(t, errors.Is(err, ErrACLSampleNotFound))
}

func TestValidateNetworkPolicyACLIdentityAcceptsTruncatedOVNName(t *testing.T) {
	policyName := strings.Repeat("long-policy-name-", 5) + "tail"
	aclName := "np/" + policyName + ".default/ingress/IPv4/3"
	priority, err := strconv.Atoi(util.IngressAllowPriority)
	require.NoError(t, err)
	acl := ovnnb.ACL{
		Action:    ovnnb.ACLActionAllowRelated,
		Direction: ovnnb.ACLDirectionToLport,
		Priority:  priority,
		Tier:      util.NetpolACLTier,
		ExternalIDs: map[string]string{
			ExternalIDVendor:               util.CniTypeName,
			networkPolicyACLNameExternalID: aclName,
			policyNamespaceExternalID:      "default",
			policyNameExternalID:           policyName,
		},
	}
	setACLName(&acl, aclName)
	candidate, eligible, err := classifyNetworkPolicySamplingACL(acl)
	require.NoError(t, err)
	require.True(t, eligible)
	require.NoError(t, validateNetworkPolicyACLIdentity(candidate))
	require.Equal(t, limitedACLName(aclName), *acl.Name)
}

func TestValidateNetworkPolicyACLIdentityAcceptsNumericPolicyName(t *testing.T) {
	const policyName = "123-policy"
	aclName := "np/" + networkPolicyResourceName(policyName) + ".default/ingress/IPv4/3"
	priority, err := strconv.Atoi(util.IngressAllowPriority)
	require.NoError(t, err)
	acl := ovnnb.ACL{
		Action:    ovnnb.ACLActionAllowRelated,
		Direction: ovnnb.ACLDirectionToLport,
		Priority:  priority,
		Tier:      util.NetpolACLTier,
		ExternalIDs: map[string]string{
			ExternalIDVendor:               util.CniTypeName,
			networkPolicyACLNameExternalID: aclName,
			policyNamespaceExternalID:      "default",
			policyNameExternalID:           policyName,
		},
	}
	setACLName(&acl, aclName)
	candidate, eligible, err := classifyNetworkPolicySamplingACL(acl)
	require.NoError(t, err)
	require.True(t, eligible)
	require.NoError(t, validateNetworkPolicyACLIdentity(candidate))
}

func newNetworkPolicySampleResolverFixture(t *testing.T, testName string) (*OVNNbClient, aclsampling.ControllerConfig, ovnnb.ACL, ovnnb.ACL) {
	t.Helper()
	client := newACLSamplingTestClient(t, testName)
	pgName := testName + ".default"
	require.NoError(t, client.CreatePortGroup(pgName, nil))
	waitForPortGroup(t, client, pgName)

	allowName := "np/decoded-policy.default/ingress/IPv4/3"
	allowACL := newNetworkPolicySamplingTestACL(t, client, pgName, ovnnb.ACLDirectionToLport, util.IngressAllowPriority, ovnnb.ACLActionAllowRelated, allowName)
	denyACL := newNetworkPolicySamplingTestACL(t, client, pgName, ovnnb.ACLDirectionFromLport, util.EgressDefaultDrop, ovnnb.ACLActionDrop, "default/decoded-policy")
	require.NoError(t, client.CreateAcls(pgName, portGroupKey, allowACL, denyACL))
	waitForACLCount(t, client, pgName, 2)

	request, err := client.PrepareNetworkPolicyACLSampling(pgName, "default", "decoded-policy", "11111111-2222-3333-4444-555555555555")
	require.NoError(t, err)
	config := validACLSamplingConfig()
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		require.NoError(collect, client.ApplyNetworkPolicyACLSampling(config, request))
	}, 3*time.Second, 20*time.Millisecond)

	var sampled []ovnnb.ACL
	require.Eventually(t, func() bool {
		sampled, err = client.ListAcls("", map[string]string{sampleFeatureExternalID: networkPolicySampleFeature})
		return err == nil && len(sampled) == 2
	}, 3*time.Second, 20*time.Millisecond)
	return client, config, aclByName(t, sampled, allowName), aclByNameAndAction(t, sampled, "default/decoded-policy", ovnnb.ACLActionDrop)
}

func parseSampleMetadataForTest(t *testing.T, acl ovnnb.ACL) uint32 {
	t.Helper()
	metadata, err := strconv.ParseUint(acl.ExternalIDs[sampleMetadataExternalID], 10, 32)
	require.NoError(t, err)
	return uint32(metadata)
}

func aclSampleCookieForTest(applicationID, datapathKey, metadata uint32) string {
	observationDomain := applicationID<<24 | datapathKey
	return fmt.Sprintf("0x%08x%08x", observationDomain, metadata)
}
