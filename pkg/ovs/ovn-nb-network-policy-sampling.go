package ovs

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ovn-kubernetes/libovsdb/model"
	"github.com/ovn-kubernetes/libovsdb/ovsdb"

	"github.com/kubeovn/kube-ovn/pkg/aclsampling"
	ovsclient "github.com/kubeovn/kube-ovn/pkg/ovsdb/client"
	"github.com/kubeovn/kube-ovn/pkg/ovsdb/ovnnb"
	"github.com/kubeovn/kube-ovn/pkg/util"
)

const (
	sampleSchemaVersionExternalID  = "kube-ovn.io/sample-schema-version"
	sampleFeatureExternalID        = "kube-ovn.io/sample-feature"
	sampleRoleExternalID           = "kube-ovn.io/sample-role"
	policyAPIVersionExternalID     = "kube-ovn.io/policy-api-version"
	policyKindExternalID           = "kube-ovn.io/policy-kind"
	policyNamespaceExternalID      = "kube-ovn.io/policy-namespace"
	policyNameExternalID           = "kube-ovn.io/policy-name"
	policyUIDExternalID            = "kube-ovn.io/policy-uid"
	policyDirectionExternalID      = "kube-ovn.io/policy-direction"
	policyRuleIndexExternalID      = "kube-ovn.io/policy-rule-index"
	policyVerdictExternalID        = "kube-ovn.io/policy-verdict"
	ovnActionExternalID            = "kube-ovn.io/ovn-action"
	aclMatchHashExternalID         = "kube-ovn.io/acl-match-hash"
	sampleKeyHashExternalID        = "kube-ovn.io/sample-key-hash"
	sampleMetadataExternalID       = "kube-ovn.io/sample-metadata"
	networkPolicyACLNameExternalID = "kube-ovn.io/network-policy-acl-name"

	networkPolicySampleFeature = "network-policy"
	networkPolicyAPIVersion    = "networking.k8s.io/v1"
	networkPolicyKind          = "NetworkPolicy"
	defaultDenyProtocol        = "IP"

	networkPolicyVerdictAllow       networkPolicyVerdict = "allow"
	networkPolicyVerdictDefaultDeny networkPolicyVerdict = "default-deny"
)

type networkPolicyVerdict string

var networkPolicySamplingExternalIDs = []string{
	sampleSchemaVersionExternalID,
	sampleFeatureExternalID,
	sampleRoleExternalID,
	policyAPIVersionExternalID,
	policyKindExternalID,
	policyNamespaceExternalID,
	policyNameExternalID,
	policyUIDExternalID,
	policyDirectionExternalID,
	policyRuleIndexExternalID,
	policyVerdictExternalID,
	ovnActionExternalID,
	aclMatchHashExternalID,
	sampleKeyHashExternalID,
	sampleMetadataExternalID,
}

// NetworkPolicySamplingRequest carries the metadata snapshot that must be
// taken before enforcement replaces a NetworkPolicy's ACLs. Callers treat the
// value as opaque and apply it only after all enforcement transactions succeed.
type NetworkPolicySamplingRequest struct {
	portGroup       string
	policyNamespace string
	policyName      string
	policyUID       string
	previous        []aclsampling.OccupiedMetadata
}

type eligibleNetworkPolicyACL struct {
	acl       ovnnb.ACL
	direction aclsampling.PolicyDirection
	ruleIndex *int
	role      aclsampling.ACLRole
	protocol  string
	verdict   networkPolicyVerdict
}

// PrepareNetworkPolicyACLSampling snapshots the stable metadata mappings from
// ACLs that an enforcement transaction is about to replace.
func (c *OVNNbClient) PrepareNetworkPolicyACLSampling(pgName, namespace, name, uid string) (*NetworkPolicySamplingRequest, error) {
	request := &NetworkPolicySamplingRequest{
		portGroup:       pgName,
		policyNamespace: namespace,
		policyName:      name,
		policyUID:       uid,
	}
	if pgName == "" || namespace == "" || name == "" || uid == "" {
		return request, errors.New("network policy sampling request fields must not be empty")
	}

	acls, err := c.ListAcls("", map[string]string{aclParentKey: pgName})
	if err != nil {
		return request, fmt.Errorf("snapshot NetworkPolicy ACL sampling metadata: %w", err)
	}
	request.previous, err = storedNetworkPolicySampleMappings(acls)
	if err != nil {
		return request, fmt.Errorf("snapshot NetworkPolicy ACL sampling metadata: %w", err)
	}
	return request, nil
}

// ApplyNetworkPolicyACLSampling attaches best-effort sampling to the eligible
// ACLs in a transaction separate from NetworkPolicy enforcement.
func (c *OVNNbClient) ApplyNetworkPolicyACLSampling(config aclsampling.ControllerConfig, request *NetworkPolicySamplingRequest) error {
	if request == nil {
		return errors.New("network policy sampling request must not be nil")
	}
	if !config.Enabled {
		return nil
	}
	if err := c.ReconcileACLSampling(config); err != nil {
		return err
	}

	acls, err := c.ListAcls("", map[string]string{aclParentKey: request.portGroup})
	if err != nil {
		return fmt.Errorf("list NetworkPolicy ACLs for sampling: %w", err)
	}
	eligible := make([]eligibleNetworkPolicyACL, 0, len(acls))
	for _, acl := range acls {
		candidate, ok, err := classifyNetworkPolicySamplingACL(acl)
		if err != nil {
			return fmt.Errorf("classify NetworkPolicy ACL %s for sampling: %w", acl.UUID, err)
		}
		if ok {
			eligible = append(eligible, candidate)
		}
	}
	if len(eligible) == 0 {
		return nil
	}

	samples, err := c.listSamples()
	if err != nil {
		return err
	}
	allACLs, err := c.ListAcls("", nil)
	if err != nil {
		return fmt.Errorf("list ACL sample metadata reservations: %w", err)
	}
	occupied, err := currentSampleMetadata(samples, allACLs, request.previous)
	if err != nil {
		return err
	}
	allocator, err := aclsampling.NewAllocator(occupied)
	if err != nil {
		return fmt.Errorf("initialize ACL sample metadata allocator: %w", err)
	}

	collectors, err := c.networkPolicySampleCollectors()
	if err != nil {
		return err
	}
	samplesByMetadata := make(map[uint32]*ovnnb.Sample, len(samples)+len(eligible))
	for i := range samples {
		sample := &samples[i]
		metadata, err := networkPolicySampleMetadata(sample.Metadata)
		if err != nil {
			return fmt.Errorf("sample %s: %w", sample.UUID, err)
		}
		samplesByMetadata[metadata] = sample
	}
	applyErrors := make([]error, 0)
	for i := range eligible {
		candidate := &eligible[i]
		operations, createdMetadata, err := c.networkPolicySamplingOps(config, request, candidate, allocator, collectors, samplesByMetadata)
		if err != nil {
			applyErrors = append(applyErrors, fmt.Errorf("ACL %s: %w", candidate.acl.UUID, err))
			continue
		}
		if len(operations) == 0 {
			continue
		}
		if err := c.Transact("network-policy-acl-sampling-attach", operations); err != nil {
			applyErrors = append(applyErrors, fmt.Errorf("attach sampling to ACL %s: %w", candidate.acl.UUID, err))
			continue
		}
		if createdMetadata != 0 {
			sample, err := c.waitForNetworkPolicySample(createdMetadata)
			if err != nil {
				applyErrors = append(applyErrors, fmt.Errorf("ACL %s: %w", candidate.acl.UUID, err))
				continue
			}
			samplesByMetadata[createdMetadata] = sample
		}
	}
	return errors.Join(applyErrors...)
}

func (c *OVNNbClient) networkPolicySamplingOps(config aclsampling.ControllerConfig, request *NetworkPolicySamplingRequest, candidate *eligibleNetworkPolicyACL, allocator *aclsampling.Allocator, collectors map[string]*ovnnb.SampleCollector, samplesByMetadata map[uint32]*ovnnb.Sample) ([]ovsdb.Operation, uint32, error) {
	owned := isOwnedNetworkPolicySamplingACL(candidate.acl.ExternalIDs)
	enabled := config.AllowProbabilityPercent != 0
	collector := collectors[aclSamplingRoleAllow]
	if candidate.role == aclsampling.RoleDefaultDeny {
		enabled = config.DefaultDenyProbabilityPercent != 0
		collector = collectors[aclSamplingRoleDefaultDeny]
	}
	if !enabled {
		if !owned {
			return nil, 0, nil
		}
		ops, err := c.clearNetworkPolicySamplingOps(&candidate.acl)
		return ops, 0, err
	}
	if feature := candidate.acl.ExternalIDs[sampleFeatureExternalID]; feature != "" && !owned {
		return nil, 0, fmt.Errorf("sampling metadata is owned by feature %s", feature)
	}
	if !owned && (candidate.acl.SampleNew != nil || candidate.acl.SampleEst != nil) {
		return nil, 0, errors.New("has unowned sample references")
	}

	allocation, err := allocator.Allocate(aclsampling.SampleKey{
		SchemaVersion: aclsampling.SchemaVersionV1,
		PolicyUID:     request.policyUID,
		Direction:     candidate.direction,
		RuleIndex:     candidate.ruleIndex,
		Role:          candidate.role,
		Protocol:      candidate.protocol,
		ACLMatchHash:  aclsampling.HashACLMatch(candidate.acl.Match),
		OVNAction:     aclsampling.OVNAction(candidate.acl.Action),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("allocate sample metadata: %w", err)
	}

	sampleUUID, sampleOps, err := c.ensureNetworkPolicySample(allocation.Metadata, collector, samplesByMetadata)
	if err != nil {
		return nil, 0, err
	}
	aclOps, err := c.setNetworkPolicySamplingOps(&candidate.acl, request, *candidate, allocation, sampleUUID)
	if err != nil {
		return nil, 0, err
	}
	createdMetadata := uint32(0)
	if len(sampleOps) != 0 {
		createdMetadata = allocation.Metadata
	}
	return append(sampleOps, aclOps...), createdMetadata, nil
}

func (c *OVNNbClient) waitForNetworkPolicySample(metadata uint32) (*ovnnb.Sample, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		samples, err := c.listSamples()
		if err != nil {
			return nil, err
		}
		for i := range samples {
			sampleMetadata, err := networkPolicySampleMetadata(samples[i].Metadata)
			if err != nil {
				return nil, fmt.Errorf("sample %s: %w", samples[i].UUID, err)
			}
			if sampleMetadata == metadata {
				return &samples[i], nil
			}
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("wait for sample metadata %d: %w", metadata, ctx.Err())
		case <-ticker.C:
		}
	}
}

func (c *OVNNbClient) listSamples() ([]ovnnb.Sample, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()

	samples := make([]ovnnb.Sample, 0)
	if err := c.Database.Table(&ovnnb.Sample{}).Filter(ctx, func(*ovnnb.Sample) bool { return true }, &samples); err != nil {
		return nil, fmt.Errorf("list OVN samples: %w", err)
	}
	return samples, nil
}

func (c *OVNNbClient) networkPolicySampleCollectors() (map[string]*ovnnb.SampleCollector, error) {
	collectors, err := c.listSampleCollectors()
	if err != nil {
		return nil, err
	}
	result := make(map[string]*ovnnb.SampleCollector, 2)
	for i := range collectors {
		collector := &collectors[i]
		if !isOwnedACLSamplingObject(collector.ExternalIDs) {
			continue
		}
		role := collector.ExternalIDs[aclSamplingRoleExternalID]
		if role != aclSamplingRoleAllow && role != aclSamplingRoleDefaultDeny {
			continue
		}
		if result[role] != nil {
			return nil, fmt.Errorf("multiple owned sample collectors have role %s", role)
		}
		result[role] = collector
	}
	for _, role := range []string{aclSamplingRoleAllow, aclSamplingRoleDefaultDeny} {
		if result[role] == nil {
			return nil, fmt.Errorf("owned %s sample collector is not available", role)
		}
	}
	return result, nil
}

func classifyNetworkPolicySamplingACL(acl ovnnb.ACL) (eligibleNetworkPolicyACL, bool, error) {
	aclName := acl.ExternalIDs[networkPolicyACLNameExternalID]
	if acl.ExternalIDs[ExternalIDVendor] != util.CniTypeName || aclName == "" {
		return eligibleNetworkPolicyACL{}, false, nil
	}
	if acl.Tier != util.NetpolACLTier {
		return eligibleNetworkPolicyACL{}, false, nil
	}
	direction, ok := networkPolicyDirection(acl.Direction)
	if !ok {
		return eligibleNetworkPolicyACL{}, false, nil
	}
	allowPriority, err := strconv.Atoi(util.IngressAllowPriority)
	if err != nil {
		return eligibleNetworkPolicyACL{}, false, fmt.Errorf("parse ingress allow priority: %w", err)
	}
	defaultDropPriority, err := strconv.Atoi(util.IngressDefaultDrop)
	if err != nil {
		return eligibleNetworkPolicyACL{}, false, fmt.Errorf("parse ingress default-drop priority: %w", err)
	}
	if direction == aclsampling.DirectionEgress {
		allowPriority, err = strconv.Atoi(util.EgressAllowPriority)
		if err != nil {
			return eligibleNetworkPolicyACL{}, false, fmt.Errorf("parse egress allow priority: %w", err)
		}
		defaultDropPriority, err = strconv.Atoi(util.EgressDefaultDrop)
		if err != nil {
			return eligibleNetworkPolicyACL{}, false, fmt.Errorf("parse egress default-drop priority: %w", err)
		}
	}

	if acl.Action == ovnnb.ACLActionDrop && acl.Priority == defaultDropPriority {
		return eligibleNetworkPolicyACL{
			acl:       acl,
			direction: direction,
			role:      aclsampling.RoleDefaultDeny,
			protocol:  defaultDenyProtocol,
			verdict:   networkPolicyVerdictDefaultDeny,
		}, true, nil
	}
	if acl.Action != ovnnb.ACLActionAllowRelated || acl.Priority != allowPriority {
		return eligibleNetworkPolicyACL{}, false, nil
	}

	parts := strings.Split(aclName, "/")
	if len(parts) != 5 && (len(parts) != 6 || parts[5] != "ipBlock") {
		return eligibleNetworkPolicyACL{}, false, nil
	}
	if parts[0] != "np" || (parts[2] != "ingress" && parts[2] != "egress") || parts[3] == "" {
		return eligibleNetworkPolicyACL{}, false, nil
	}
	if (parts[2] == "ingress") != (direction == aclsampling.DirectionIngress) {
		return eligibleNetworkPolicyACL{}, false, nil
	}
	ruleIndex, err := strconv.Atoi(parts[4])
	if err != nil || ruleIndex < 0 {
		return eligibleNetworkPolicyACL{}, false, nil
	}
	return eligibleNetworkPolicyACL{
		acl:       acl,
		direction: direction,
		ruleIndex: &ruleIndex,
		role:      aclsampling.RoleRuleAllow,
		protocol:  parts[3],
		verdict:   networkPolicyVerdictAllow,
	}, true, nil
}

func setNetworkPolicyACLName(acl *ovnnb.ACL, name string) {
	setACLName(acl, name)
	acl.ExternalIDs[networkPolicyACLNameExternalID] = name
}

func networkPolicyDirection(direction string) (aclsampling.PolicyDirection, bool) {
	switch direction {
	case ovnnb.ACLDirectionToLport:
		return aclsampling.DirectionIngress, true
	case ovnnb.ACLDirectionFromLport:
		return aclsampling.DirectionEgress, true
	default:
		return "", false
	}
}

func storedNetworkPolicySampleMappings(acls []ovnnb.ACL) ([]aclsampling.OccupiedMetadata, error) {
	mappings := make([]aclsampling.OccupiedMetadata, 0)
	for _, acl := range acls {
		if !isOwnedNetworkPolicySamplingACL(acl.ExternalIDs) {
			continue
		}
		mapping, err := storedNetworkPolicySampleMapping(acl.ExternalIDs)
		if err != nil {
			return nil, fmt.Errorf("ACL %s: %w", acl.UUID, err)
		}
		mappings = append(mappings, mapping)
	}
	return mappings, nil
}

func storedNetworkPolicySampleMapping(externalIDs map[string]string) (aclsampling.OccupiedMetadata, error) {
	metadataValue := externalIDs[sampleMetadataExternalID]
	keyHash := externalIDs[sampleKeyHashExternalID]
	metadata, err := strconv.ParseUint(metadataValue, 10, 32)
	if err != nil || metadata == 0 {
		return aclsampling.OccupiedMetadata{}, fmt.Errorf("invalid sample metadata %q", metadataValue)
	}
	decoded, err := hex.DecodeString(keyHash)
	if err != nil || len(decoded) != 32 || strings.ToLower(keyHash) != keyHash {
		return aclsampling.OccupiedMetadata{}, fmt.Errorf("invalid sample key hash %q", keyHash)
	}
	return aclsampling.OccupiedMetadata{Metadata: uint32(metadata), KeyHash: keyHash}, nil
}

func networkPolicySampleMetadata(value int) (uint32, error) {
	metadata, err := strconv.ParseUint(strconv.Itoa(value), 10, 32)
	if err != nil || metadata == 0 {
		return 0, fmt.Errorf("invalid sample metadata %d", value)
	}
	return uint32(metadata), nil
}

func currentSampleMetadata(samples []ovnnb.Sample, acls []ovnnb.ACL, previous []aclsampling.OccupiedMetadata) ([]aclsampling.OccupiedMetadata, error) {
	metadataIndex := make(map[uint32]int, len(samples))
	sampleMetadata := make(map[string]uint32, len(samples))
	occupied := make([]aclsampling.OccupiedMetadata, 0, len(samples)+len(previous))
	for _, sample := range samples {
		metadata, err := networkPolicySampleMetadata(sample.Metadata)
		if err != nil {
			return nil, fmt.Errorf("sample %s: %w", sample.UUID, err)
		}
		metadataIndex[metadata] = len(occupied)
		sampleMetadata[sample.UUID] = metadata
		occupied = append(occupied, aclsampling.OccupiedMetadata{Metadata: metadata})
	}

	for _, acl := range acls {
		if !isOwnedNetworkPolicySamplingACL(acl.ExternalIDs) {
			continue
		}
		mapping, err := storedNetworkPolicySampleMapping(acl.ExternalIDs)
		if err != nil {
			return nil, fmt.Errorf("read ACL %s sample metadata: %w", acl.UUID, err)
		}
		for _, sampleUUID := range compactSampleReferences(acl) {
			metadata, ok := sampleMetadata[sampleUUID]
			if !ok {
				return nil, fmt.Errorf("ACL %s references unknown sample %s", acl.UUID, sampleUUID)
			}
			if metadata != mapping.Metadata {
				return nil, fmt.Errorf("ACL %s sample reference metadata %d does not match external ID %d", acl.UUID, metadata, mapping.Metadata)
			}
			index := metadataIndex[metadata]
			if occupied[index].KeyHash != "" && occupied[index].KeyHash != mapping.KeyHash {
				return nil, fmt.Errorf("sample metadata %d has conflicting key hashes", metadata)
			}
			occupied[index].KeyHash = mapping.KeyHash
		}
	}

	for _, mapping := range previous {
		if index, ok := metadataIndex[mapping.Metadata]; ok {
			if occupied[index].KeyHash == mapping.KeyHash {
				continue
			}
			// The previous metadata has been claimed since the snapshot. Keep
			// the current reservation and let the allocator probe a new value.
			continue
		}
		metadataIndex[mapping.Metadata] = len(occupied)
		occupied = append(occupied, mapping)
	}
	return occupied, nil
}

func compactSampleReferences(acl ovnnb.ACL) []string {
	references := make([]string, 0, 2)
	if acl.SampleNew != nil {
		references = append(references, *acl.SampleNew)
	}
	if acl.SampleEst != nil && (acl.SampleNew == nil || *acl.SampleEst != *acl.SampleNew) {
		references = append(references, *acl.SampleEst)
	}
	return references
}

func (c *OVNNbClient) ensureNetworkPolicySample(metadata uint32, collector *ovnnb.SampleCollector, samplesByMetadata map[uint32]*ovnnb.Sample) (string, []ovsdb.Operation, error) {
	if sample := samplesByMetadata[metadata]; sample != nil {
		if !slices.Equal(sample.Collectors, []string{collector.UUID}) {
			return "", nil, fmt.Errorf("sample metadata %d is associated with unexpected collectors", metadata)
		}
		return sample.UUID, nil, nil
	}

	sample := &ovnnb.Sample{
		UUID:       ovsclient.NamedUUID(),
		Collectors: []string{collector.UUID},
		Metadata:   int(metadata),
	}
	ops, err := c.Database.Table(&ovnnb.Sample{}).CreateOps(model.Model(sample))
	if err != nil {
		return "", nil, fmt.Errorf("build create operation for sample metadata %d: %w", metadata, err)
	}
	return sample.UUID, ops, nil
}

func (c *OVNNbClient) setNetworkPolicySamplingOps(acl *ovnnb.ACL, request *NetworkPolicySamplingRequest, candidate eligibleNetworkPolicyACL, allocation aclsampling.Allocation, sampleUUID string) ([]ovsdb.Operation, error) {
	externalIDs := maps.Clone(acl.ExternalIDs)
	if externalIDs == nil {
		externalIDs = make(map[string]string, len(networkPolicySamplingExternalIDs))
	}
	externalIDs[sampleSchemaVersionExternalID] = aclsampling.SchemaVersionV1
	externalIDs[sampleFeatureExternalID] = networkPolicySampleFeature
	externalIDs[sampleRoleExternalID] = string(candidate.role)
	externalIDs[policyAPIVersionExternalID] = networkPolicyAPIVersion
	externalIDs[policyKindExternalID] = networkPolicyKind
	externalIDs[policyNamespaceExternalID] = request.policyNamespace
	externalIDs[policyNameExternalID] = request.policyName
	externalIDs[policyUIDExternalID] = request.policyUID
	externalIDs[policyDirectionExternalID] = string(candidate.direction)
	externalIDs[policyVerdictExternalID] = string(candidate.verdict)
	externalIDs[ovnActionExternalID] = candidate.acl.Action
	externalIDs[aclMatchHashExternalID] = aclsampling.HashACLMatch(candidate.acl.Match)
	externalIDs[sampleKeyHashExternalID] = allocation.KeyHash
	externalIDs[sampleMetadataExternalID] = strconv.FormatUint(uint64(allocation.Metadata), 10)
	if candidate.ruleIndex == nil {
		delete(externalIDs, policyRuleIndexExternalID)
	} else {
		externalIDs[policyRuleIndexExternalID] = strconv.Itoa(*candidate.ruleIndex)
	}

	acl.ExternalIDs = externalIDs
	acl.SampleNew = &sampleUUID
	if candidate.role == aclsampling.RoleRuleAllow {
		acl.SampleEst = &sampleUUID
	} else {
		acl.SampleEst = nil
	}
	ops, err := c.Database.WhereTable(acl).Update(acl, &acl.ExternalIDs, &acl.SampleNew, &acl.SampleEst)
	if err != nil {
		return nil, fmt.Errorf("build sample attachment operation for ACL %s: %w", acl.UUID, err)
	}
	return ops, nil
}

func (c *OVNNbClient) clearNetworkPolicySamplingOps(acl *ovnnb.ACL) ([]ovsdb.Operation, error) {
	if acl.SampleNew == nil && acl.SampleEst == nil && !isOwnedNetworkPolicySamplingACL(acl.ExternalIDs) {
		return nil, nil
	}
	externalIDs := maps.Clone(acl.ExternalIDs)
	for _, key := range networkPolicySamplingExternalIDs {
		delete(externalIDs, key)
	}
	acl.ExternalIDs = externalIDs
	acl.SampleNew = nil
	acl.SampleEst = nil
	ops, err := c.Database.WhereTable(acl).Update(acl, &acl.ExternalIDs, &acl.SampleNew, &acl.SampleEst)
	if err != nil {
		return nil, fmt.Errorf("build sample cleanup operation for ACL %s: %w", acl.UUID, err)
	}
	return ops, nil
}

func isOwnedNetworkPolicySamplingACL(externalIDs map[string]string) bool {
	return externalIDs[ExternalIDVendor] == util.CniTypeName &&
		externalIDs[sampleFeatureExternalID] == networkPolicySampleFeature &&
		externalIDs[sampleSchemaVersionExternalID] == aclsampling.SchemaVersionV1
}
