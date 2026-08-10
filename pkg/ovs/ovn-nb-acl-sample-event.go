package ovs

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/kubeovn/kube-ovn/pkg/aclsampling"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
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
	matches := make([]*aclsampling.Event, 0, 1)
	for i := range acls {
		acl := acls[i]
		if !isOwnedNetworkPolicySamplingACL(acl.ExternalIDs) {
			continue
		}
		candidate, eligible, err := classifyNetworkPolicySamplingACL(acl)
		if err != nil {
			return nil, fmt.Errorf("classify NetworkPolicy ACL %s: %w", acl.UUID, err)
		}
		if !eligible || !aclReferencesSample(acl, sample.UUID, application) {
			continue
		}
		event, err := networkPolicyACLSampleEvent(candidate, sample.UUID, reference, application)
		if err != nil {
			return nil, fmt.Errorf("decode NetworkPolicy ACL %s: %w", acl.UUID, err)
		}
		matches = append(matches, event)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("%w: no Kube-OVN NetworkPolicy ACL references metadata %d", ErrACLSampleNotFound, reference.Metadata)
	}
	if len(matches) != 1 {
		return nil, fmt.Errorf("%w: metadata %d is referenced by %d Kube-OVN NetworkPolicy ACLs", ErrACLSampleAmbiguous, reference.Metadata, len(matches))
	}

	return matches[0], nil
}

func (c *OVNNbClient) resolveACLSampleApplication(applicationID *uint32) (aclsampling.Application, error) {
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

func aclReferencesSample(acl ovnnb.ACL, sampleUUID string, application aclsampling.Application) bool {
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

func networkPolicyACLSampleEvent(candidate eligibleNetworkPolicyACL, sampleUUID string, reference aclsampling.SampleReference, application aclsampling.Application) (*aclsampling.Event, error) {
	acl := candidate.acl
	if err := validateNetworkPolicyACLSample(candidate, sampleUUID, reference, application); err != nil {
		return nil, err
	}
	externalIDs := acl.ExternalIDs

	policy := &aclsampling.PolicyReference{
		APIVersion: networkPolicyAPIVersion,
		Kind:       networkPolicyKind,
		Namespace:  externalIDs[policyNamespaceExternalID],
		Name:       externalIDs[policyNameExternalID],
		UID:        externalIDs[policyUIDExternalID],
		Direction:  candidate.direction,
	}
	matchHash := aclsampling.HashACLMatch(acl.Match)
	event := &aclsampling.Event{
		SchemaVersion: aclsampling.SchemaVersionV1,
		Feature:       networkPolicySampleFeature,
		OVN: aclsampling.OVNACLReference{
			UUID:      acl.UUID,
			Action:    aclsampling.OVNAction(acl.Action),
			Priority:  acl.Priority,
			Tier:      acl.Tier,
			Direction: aclsampling.OVNACLDirection(acl.Direction),
			MatchHash: matchHash,
		},
		Sample: aclsampling.SampleDetails{
			App:               application,
			SampleObservation: reference.SampleObservation,
		},
	}
	if acl.Name != nil {
		event.OVN.Name = *acl.Name
	}

	switch candidate.role {
	case aclsampling.RoleRuleAllow:
		policy.RuleIndex = candidate.ruleIndex
		event.Verdict = aclsampling.VerdictAllow
		event.Policy = policy
	case aclsampling.RoleDefaultDeny:
		event.Verdict = aclsampling.VerdictDefaultDeny
		event.Reason = aclsampling.ReasonNetworkPolicyDefaultDeny
		event.Attribution = aclsampling.AttributionNonExclusive
		event.PolicyOwner = policy
	default:
		return nil, fmt.Errorf("unsupported NetworkPolicy ACL sample role %q", externalIDs[sampleRoleExternalID])
	}
	return event, nil
}

func validateNetworkPolicyACLSample(candidate eligibleNetworkPolicyACL, sampleUUID string, reference aclsampling.SampleReference, application aclsampling.Application) error {
	acl := candidate.acl
	externalIDs := acl.ExternalIDs
	if !isOwnedNetworkPolicySamplingACL(externalIDs) {
		return errors.New("ACL is not owned by Kube-OVN NetworkPolicy sampling")
	}
	if externalIDs[policyAPIVersionExternalID] != networkPolicyAPIVersion || externalIDs[policyKindExternalID] != networkPolicyKind {
		return fmt.Errorf("unsupported policy identity %s %s", externalIDs[policyAPIVersionExternalID], externalIDs[policyKindExternalID])
	}
	for _, key := range []string{policyNamespaceExternalID, policyNameExternalID, policyUIDExternalID, policyDirectionExternalID} {
		if externalIDs[key] == "" {
			return fmt.Errorf("required external ID %s is missing", key)
		}
	}
	if externalIDs[policyDirectionExternalID] != string(candidate.direction) {
		return fmt.Errorf("policy direction %q does not match ACL direction %q", externalIDs[policyDirectionExternalID], acl.Direction)
	}
	if externalIDs[sampleRoleExternalID] != string(candidate.role) || externalIDs[policyVerdictExternalID] != string(candidate.verdict) {
		return errors.New("ACL sample role or verdict does not match the canonical NetworkPolicy ACL")
	}
	switch candidate.role {
	case aclsampling.RoleRuleAllow:
		ruleIndex, err := strconv.Atoi(externalIDs[policyRuleIndexExternalID])
		if err != nil || candidate.ruleIndex == nil || ruleIndex != *candidate.ruleIndex {
			return fmt.Errorf("NetworkPolicy rule index %q does not match the canonical ACL", externalIDs[policyRuleIndexExternalID])
		}
	case aclsampling.RoleDefaultDeny:
		if _, exists := externalIDs[policyRuleIndexExternalID]; exists {
			return errors.New("default-deny sample metadata must not include a rule index")
		}
	}
	if externalIDs[ovnActionExternalID] != acl.Action {
		return fmt.Errorf("OVN action external ID %q does not match ACL action %q", externalIDs[ovnActionExternalID], acl.Action)
	}
	matchHash := aclsampling.HashACLMatch(acl.Match)
	if externalIDs[aclMatchHashExternalID] != matchHash {
		return errors.New("ACL match hash does not match the current ACL match")
	}
	mapping, err := storedNetworkPolicySampleMapping(externalIDs)
	if err != nil {
		return err
	}
	if mapping.Metadata != reference.Metadata {
		return fmt.Errorf("sample metadata external ID %d does not match observed metadata %d", mapping.Metadata, reference.Metadata)
	}
	expectedKeyHash, err := (aclsampling.SampleKey{
		SchemaVersion: aclsampling.SchemaVersionV1,
		PolicyUID:     externalIDs[policyUIDExternalID],
		Direction:     candidate.direction,
		RuleIndex:     candidate.ruleIndex,
		Role:          candidate.role,
		Protocol:      candidate.protocol,
		ACLMatchHash:  matchHash,
		OVNAction:     aclsampling.OVNAction(acl.Action),
	}).KeyHash()
	if err != nil {
		return fmt.Errorf("build canonical sample key: %w", err)
	}
	if mapping.KeyHash != expectedKeyHash {
		return errors.New("sample key hash does not match the canonical NetworkPolicy ACL")
	}
	if err := validateNetworkPolicyACLIdentity(candidate); err != nil {
		return err
	}
	return validateNetworkPolicyACLSampleReferences(candidate, sampleUUID, application)
}

func validateNetworkPolicyACLIdentity(candidate eligibleNetworkPolicyACL) error {
	externalIDs := candidate.acl.ExternalIDs
	aclName := externalIDs[networkPolicyACLNameExternalID]
	policyNamespace := externalIDs[policyNamespaceExternalID]
	policyName := externalIDs[policyNameExternalID]
	if candidate.acl.Name == nil || *candidate.acl.Name != limitedACLName(aclName) {
		return errors.New("OVN ACL name does not match the canonical NetworkPolicy ACL name")
	}
	switch candidate.role {
	case aclsampling.RoleRuleAllow:
		if !strings.HasPrefix(aclName, "np/"+policyName+"."+policyNamespace+"/") {
			return errors.New("rule-allow ACL name does not match the NetworkPolicy identity")
		}
	case aclsampling.RoleDefaultDeny:
		if aclName != policyNamespace+"/"+policyName {
			return errors.New("default-deny ACL name does not match the NetworkPolicy identity")
		}
	default:
		return fmt.Errorf("unsupported NetworkPolicy ACL sample role %q", candidate.role)
	}
	return nil
}

func validateNetworkPolicyACLSampleReferences(candidate eligibleNetworkPolicyACL, sampleUUID string, application aclsampling.Application) error {
	acl := candidate.acl
	sampleNewMatches := acl.SampleNew != nil && *acl.SampleNew == sampleUUID
	sampleEstMatches := acl.SampleEst != nil && *acl.SampleEst == sampleUUID
	switch candidate.role {
	case aclsampling.RoleRuleAllow:
		if !sampleNewMatches || !sampleEstMatches {
			return errors.New("rule-allow ACL must reference the same Sample from sample_new and sample_est")
		}
	case aclsampling.RoleDefaultDeny:
		if !sampleNewMatches || acl.SampleEst != nil || application == aclsampling.ApplicationACLEst {
			return errors.New("default-deny ACL must reference its Sample only from sample_new")
		}
	default:
		return fmt.Errorf("unsupported NetworkPolicy ACL sample role %q", candidate.role)
	}
	return nil
}
