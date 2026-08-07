package aclsampling

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

const (
	ApplicationACLNew = "acl-new"
	ApplicationACLEst = "acl-est"

	VerdictAllow       = "allow"
	VerdictDefaultDeny = "default-deny"

	ReasonNetworkPolicyDefaultDeny = "network-policy-default-deny"
	AttributionNonExclusive        = "non-exclusive"
)

// SampleReference identifies the OVN observation attached to a local psample
// event. ApplicationID is nil when the input contains metadata only.
type SampleReference struct {
	ObservationDomain *uint32 `json:"observationDomain,omitempty" yaml:"observationDomain,omitempty"`
	ApplicationID     *uint32 `json:"applicationID,omitempty" yaml:"applicationID,omitempty"`
	DatapathKey       *uint32 `json:"datapathKey,omitempty" yaml:"datapathKey,omitempty"`
	Metadata          uint32  `json:"metadata" yaml:"metadata"`
}

// PolicyReference identifies the Kubernetes NetworkPolicy data stored on an
// eligible ACL.
type PolicyReference struct {
	APIVersion string `json:"apiVersion" yaml:"apiVersion"`
	Kind       string `json:"kind" yaml:"kind"`
	Namespace  string `json:"namespace" yaml:"namespace"`
	Name       string `json:"name" yaml:"name"`
	UID        string `json:"uid" yaml:"uid"`
	Direction  string `json:"direction" yaml:"direction"`
	RuleIndex  *int   `json:"ruleIndex,omitempty" yaml:"ruleIndex,omitempty"`
}

// OVNACLReference describes the OVN ACL that owns a sample reference.
type OVNACLReference struct {
	UUID      string `json:"aclUUID" yaml:"aclUUID"`
	Name      string `json:"aclName,omitempty" yaml:"aclName,omitempty"`
	Action    string `json:"action" yaml:"action"`
	Priority  int    `json:"priority" yaml:"priority"`
	Tier      int    `json:"tier" yaml:"tier"`
	Direction string `json:"direction" yaml:"direction"`
	MatchHash string `json:"matchHash" yaml:"matchHash"`
}

// SampleDetails describes how OVN encoded a resolved sample.
type SampleDetails struct {
	App               string  `json:"app,omitempty" yaml:"app,omitempty"`
	ObservationDomain *uint32 `json:"observationDomain,omitempty" yaml:"observationDomain,omitempty"`
	ApplicationID     *uint32 `json:"applicationID,omitempty" yaml:"applicationID,omitempty"`
	DatapathKey       *uint32 `json:"datapathKey,omitempty" yaml:"datapathKey,omitempty"`
	Metadata          uint32  `json:"metadata" yaml:"metadata"`
}

// Event is the stable debug representation of a sampled NetworkPolicy ACL
// decision. Allow events populate Policy; default-deny events populate
// PolicyOwner to avoid claiming exclusive policy attribution.
type Event struct {
	SchemaVersion string           `json:"schemaVersion" yaml:"schemaVersion"`
	Feature       string           `json:"feature" yaml:"feature"`
	Verdict       string           `json:"verdict" yaml:"verdict"`
	Reason        string           `json:"reason,omitempty" yaml:"reason,omitempty"`
	Attribution   string           `json:"attribution,omitempty" yaml:"attribution,omitempty"`
	Policy        *PolicyReference `json:"policy,omitempty" yaml:"policy,omitempty"`
	PolicyOwner   *PolicyReference `json:"policyOwner,omitempty" yaml:"policyOwner,omitempty"`
	OVN           OVNACLReference  `json:"ovn" yaml:"ovn"`
	Sample        SampleDetails    `json:"sample" yaml:"sample"`
}

// ParseSampleReference accepts a decimal or 0x-prefixed hexadecimal metadata
// value or local-sampling cookie. A cookie stores the observation domain in
// the high 32 bits and the ACL metadata in the low 32 bits. OVN puts the
// application ID in the domain's high byte and the datapath key in its low 24
// bits.
func ParseSampleReference(value string) (SampleReference, error) {
	if value == "" {
		return SampleReference{}, errors.New("ACL sample cookie or metadata must not be empty")
	}
	if strings.TrimSpace(value) != value || strings.HasPrefix(value, "+") || strings.HasPrefix(value, "-") {
		return SampleReference{}, fmt.Errorf("invalid ACL sample cookie or metadata %q", value)
	}

	base := 10
	digits := value
	if strings.HasPrefix(value, "0x") || strings.HasPrefix(value, "0X") {
		base = 16
		digits = value[2:]
	}
	if digits == "" {
		return SampleReference{}, fmt.Errorf("invalid ACL sample cookie or metadata %q", value)
	}

	raw, err := strconv.ParseUint(digits, base, 64)
	if err != nil {
		return SampleReference{}, fmt.Errorf("invalid ACL sample cookie or metadata %q: %w", value, err)
	}
	if raw == 0 {
		return SampleReference{}, errors.New("ACL sample metadata must be greater than zero")
	}
	if raw <= math.MaxUint32 {
		return SampleReference{Metadata: uint32(raw)}, nil
	}

	observationDomain := uint32(raw >> 32)
	applicationID := observationDomain >> 24
	datapathKey := observationDomain & 0x00ffffff
	metadata := uint32(raw) // #nosec G115 -- OVN encodes metadata in the cookie's low 32 bits.
	if applicationID == 0 || applicationID > maxOVNObjectID {
		return SampleReference{}, fmt.Errorf("ACL sample application ID %d must be in the range 1-255", applicationID)
	}
	if metadata == 0 {
		return SampleReference{}, errors.New("ACL sample cookie metadata must be greater than zero")
	}
	return SampleReference{
		ObservationDomain: &observationDomain,
		ApplicationID:     &applicationID,
		DatapathKey:       &datapathKey,
		Metadata:          metadata,
	}, nil
}

// Validate checks whether a programmatically constructed sample reference can
// be resolved through the OVN northbound sampling schema.
func (r SampleReference) Validate() error {
	if r.Metadata == 0 {
		return errors.New("ACL sample metadata must be greater than zero")
	}
	if r.ApplicationID != nil && (*r.ApplicationID == 0 || *r.ApplicationID > maxOVNObjectID) {
		return fmt.Errorf("ACL sample application ID %d must be in the range 1-255", *r.ApplicationID)
	}
	if (r.ObservationDomain == nil) != (r.ApplicationID == nil) || (r.DatapathKey == nil) != (r.ApplicationID == nil) {
		return errors.New("ACL sample observation domain, application ID, and datapath key must be provided together")
	}
	if r.ObservationDomain != nil {
		if *r.ApplicationID != *r.ObservationDomain>>24 {
			return errors.New("ACL sample application ID does not match the observation domain")
		}
		if *r.DatapathKey != *r.ObservationDomain&0x00ffffff {
			return errors.New("ACL sample datapath key does not match the observation domain")
		}
	}
	return nil
}
