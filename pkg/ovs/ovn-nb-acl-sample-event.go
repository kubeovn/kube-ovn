package ovs

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/kubeovn/kube-ovn/pkg/aclsampling"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

var (
	ErrACLSampleNotFound  = errors.New("ACL sample event was not found")
	ErrACLSampleAmbiguous = errors.New("ACL sample event is ambiguous")
)

// ResolveNetworkPolicyACLSample resolves a local psample cookie or metadata
// value to the single Kube-OVN NetworkPolicy ACL that owns the sample. The
// caller does not need to understand OVN strong references or external IDs.
func (c *OVNNbClient) ResolveNetworkPolicyACLSample(reference aclsampling.SampleReference) (*aclsampling.Event, error) {
	if err := reference.Validate(); err != nil {
		return nil, err
	}
	if err := c.ensureACLSamplingMonitor(); err != nil {
		return nil, err
	}

	application, err := c.resolveACLSampleApplication(reference.ApplicationID)
	if err != nil {
		return nil, err
	}
	sample, err := c.resolveSampleMetadata(reference.Metadata)
	if err != nil {
		return nil, err
	}

	acls, err := c.ListAcls("", map[string]string{sampleFeatureExternalID: networkPolicySampleFeature})
	if err != nil {
		return nil, fmt.Errorf("list NetworkPolicy ACL sample owners: %w", err)
	}
	matches := make([]ovnnb.ACL, 0, 1)
	for i := range acls {
		if aclReferencesSample(acls[i], sample.UUID, application) {
			matches = append(matches, acls[i])
		}
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("%w: no Kube-OVN NetworkPolicy ACL references metadata %d", ErrACLSampleNotFound, reference.Metadata)
	}
	if len(matches) != 1 {
		return nil, fmt.Errorf("%w: metadata %d is referenced by %d Kube-OVN NetworkPolicy ACLs", ErrACLSampleAmbiguous, reference.Metadata, len(matches))
	}

	event, err := networkPolicyACLSampleEvent(matches[0], reference, application)
	if err != nil {
		return nil, fmt.Errorf("decode NetworkPolicy ACL %s: %w", matches[0].UUID, err)
	}
	return event, nil
}

func (c *OVNNbClient) resolveACLSampleApplication(applicationID *uint32) (string, error) {
	if applicationID == nil {
		return "", nil
	}
	apps, err := c.listSamplingApps()
	if err != nil {
		return "", err
	}
	matches := make([]ovnnb.SamplingApp, 0, 1)
	for i := range apps {
		if apps[i].ID == int(*applicationID) {
			matches = append(matches, apps[i])
		}
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("%w: sampling application ID %d does not exist", ErrACLSampleNotFound, *applicationID)
	}
	if len(matches) != 1 {
		return "", fmt.Errorf("%w: sampling application ID %d has %d rows", ErrACLSampleAmbiguous, *applicationID, len(matches))
	}
	switch matches[0].Type {
	case ovnnb.SamplingAppTypeACLNew:
		return aclsampling.ApplicationACLNew, nil
	case ovnnb.SamplingAppTypeACLEst:
		return aclsampling.ApplicationACLEst, nil
	default:
		return "", fmt.Errorf("%w: sampling application ID %d has unsupported type %s", ErrACLSampleNotFound, *applicationID, matches[0].Type)
	}
}

func (c *OVNNbClient) resolveSampleMetadata(metadata uint32) (*ovnnb.Sample, error) {
	samples, err := c.listSamples()
	if err != nil {
		return nil, err
	}
	matches := make([]ovnnb.Sample, 0, 1)
	for i := range samples {
		if samples[i].Metadata == int(metadata) {
			matches = append(matches, samples[i])
		}
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("%w: sample metadata %d does not exist", ErrACLSampleNotFound, metadata)
	}
	if len(matches) != 1 {
		return nil, fmt.Errorf("%w: sample metadata %d has %d rows", ErrACLSampleAmbiguous, metadata, len(matches))
	}
	return &matches[0], nil
}

func aclReferencesSample(acl ovnnb.ACL, sampleUUID, application string) bool {
	switch application {
	case aclsampling.ApplicationACLNew:
		return acl.SampleNew != nil && *acl.SampleNew == sampleUUID
	case aclsampling.ApplicationACLEst:
		return acl.SampleEst != nil && *acl.SampleEst == sampleUUID
	default:
		return (acl.SampleNew != nil && *acl.SampleNew == sampleUUID) ||
			(acl.SampleEst != nil && *acl.SampleEst == sampleUUID)
	}
}

func networkPolicyACLSampleEvent(acl ovnnb.ACL, reference aclsampling.SampleReference, application string) (*aclsampling.Event, error) {
	externalIDs := acl.ExternalIDs
	if externalIDs[sampleSchemaVersionExternalID] != aclsampling.SchemaVersionV1 {
		return nil, fmt.Errorf("unsupported sample schema version %q", externalIDs[sampleSchemaVersionExternalID])
	}
	if externalIDs[sampleFeatureExternalID] != networkPolicySampleFeature {
		return nil, fmt.Errorf("unsupported sample feature %q", externalIDs[sampleFeatureExternalID])
	}
	if externalIDs[policyAPIVersionExternalID] != networkPolicyAPIVersion || externalIDs[policyKindExternalID] != networkPolicyKind {
		return nil, fmt.Errorf("unsupported policy identity %s %s", externalIDs[policyAPIVersionExternalID], externalIDs[policyKindExternalID])
	}
	if acl.Tier != util.NetpolACLTier {
		return nil, fmt.Errorf("ACL tier %d is not the NetworkPolicy tier", acl.Tier)
	}

	for _, key := range []string{policyNamespaceExternalID, policyNameExternalID, policyUIDExternalID, policyDirectionExternalID} {
		if externalIDs[key] == "" {
			return nil, fmt.Errorf("required external ID %s is missing", key)
		}
	}
	direction, ok := networkPolicyDirection(acl.Direction)
	if !ok || externalIDs[policyDirectionExternalID] != direction {
		return nil, fmt.Errorf("policy direction %q does not match ACL direction %q", externalIDs[policyDirectionExternalID], acl.Direction)
	}
	if externalIDs[ovnActionExternalID] != acl.Action {
		return nil, fmt.Errorf("OVN action external ID %q does not match ACL action %q", externalIDs[ovnActionExternalID], acl.Action)
	}
	matchHash := aclsampling.HashACLMatch(acl.Match)
	if externalIDs[aclMatchHashExternalID] != matchHash {
		return nil, errors.New("ACL match hash does not match the current ACL match")
	}
	mapping, err := storedNetworkPolicySampleMapping(externalIDs)
	if err != nil {
		return nil, err
	}
	if mapping.Metadata != reference.Metadata {
		return nil, fmt.Errorf("sample metadata external ID %d does not match observed metadata %d", mapping.Metadata, reference.Metadata)
	}

	policy := &aclsampling.PolicyReference{
		APIVersion: networkPolicyAPIVersion,
		Kind:       networkPolicyKind,
		Namespace:  externalIDs[policyNamespaceExternalID],
		Name:       externalIDs[policyNameExternalID],
		UID:        externalIDs[policyUIDExternalID],
		Direction:  direction,
	}
	event := &aclsampling.Event{
		SchemaVersion: aclsampling.SchemaVersionV1,
		Feature:       networkPolicySampleFeature,
		OVN: aclsampling.OVNACLReference{
			UUID:      acl.UUID,
			Action:    acl.Action,
			Priority:  acl.Priority,
			Tier:      acl.Tier,
			Direction: acl.Direction,
			MatchHash: matchHash,
		},
		Sample: aclsampling.SampleDetails{
			App:               application,
			ObservationDomain: reference.ObservationDomain,
			ApplicationID:     reference.ApplicationID,
			DatapathKey:       reference.DatapathKey,
			Metadata:          reference.Metadata,
		},
	}
	if acl.Name != nil {
		event.OVN.Name = *acl.Name
	}

	switch externalIDs[sampleRoleExternalID] {
	case aclsampling.RoleRuleAllow:
		if externalIDs[policyVerdictExternalID] != aclsampling.VerdictAllow || acl.Action != ovnnb.ACLActionAllowRelated {
			return nil, errors.New("rule-allow sample metadata does not describe an allow-related ACL")
		}
		ruleIndex, err := strconv.Atoi(externalIDs[policyRuleIndexExternalID])
		if err != nil || ruleIndex < 0 {
			return nil, fmt.Errorf("invalid NetworkPolicy rule index %q", externalIDs[policyRuleIndexExternalID])
		}
		policy.RuleIndex = &ruleIndex
		event.Verdict = aclsampling.VerdictAllow
		event.Policy = policy
	case aclsampling.RoleDefaultDeny:
		if externalIDs[policyVerdictExternalID] != aclsampling.VerdictDefaultDeny || acl.Action != ovnnb.ACLActionDrop {
			return nil, errors.New("default-deny sample metadata does not describe a drop ACL")
		}
		if _, exists := externalIDs[policyRuleIndexExternalID]; exists {
			return nil, errors.New("default-deny sample metadata must not include a rule index")
		}
		event.Verdict = aclsampling.VerdictDefaultDeny
		event.Reason = aclsampling.ReasonNetworkPolicyDefaultDeny
		event.Attribution = aclsampling.AttributionNonExclusive
		event.PolicyOwner = policy
	default:
		return nil, fmt.Errorf("unsupported NetworkPolicy ACL sample role %q", externalIDs[sampleRoleExternalID])
	}
	return event, nil
}
