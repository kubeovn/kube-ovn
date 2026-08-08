package aclsampling

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

const (
	SchemaVersionV1 = "v1"

	DirectionIngress = "Ingress"
	DirectionEgress  = "Egress"

	RoleRuleAllow   = "rule-allow"
	RoleDefaultDeny = "default-deny"

	ActionAllowRelated = "allow-related"
	ActionDrop         = "drop"
)

// SampleKey contains the stable identity of an eligible NetworkPolicy ACL.
type SampleKey struct {
	SchemaVersion string
	PolicyUID     string
	Direction     string
	RuleIndex     *int
	Role          string
	Protocol      string
	ACLMatchHash  string
	OVNAction     string
}

// OccupiedMetadata describes a metadata value already present in OVN. KeyHash
// may be empty when the value is not attributable to a Kube-OVN ACL.
type OccupiedMetadata struct {
	Metadata uint32
	KeyHash  string
}

// Allocation is the stable metadata allocated for a sample key.
type Allocation struct {
	Metadata uint32
	KeyHash  string
}

// Allocator reserves globally unique, non-zero OVN sample metadata values.
// Allocations made through one instance are visible to subsequent calls so a
// controller can reserve values for a complete transaction before committing.
type Allocator struct {
	mu                sync.Mutex
	metadataToKeyHash map[uint32]string
	keyHashToMetadata map[string]uint32
}

// NewAllocator creates an allocator from existing OVN Sample rows and known
// ACL key hashes.
func NewAllocator(occupied []OccupiedMetadata) (*Allocator, error) {
	a := &Allocator{
		metadataToKeyHash: make(map[uint32]string, len(occupied)),
		keyHashToMetadata: make(map[string]uint32, len(occupied)),
	}
	for _, item := range occupied {
		if item.Metadata == 0 {
			return nil, errors.New("occupied sample metadata must be non-zero")
		}

		if current, ok := a.metadataToKeyHash[item.Metadata]; ok {
			if current != "" && item.KeyHash != "" && current != item.KeyHash {
				return nil, fmt.Errorf("sample metadata %d has conflicting key hashes", item.Metadata)
			}
			if current == "" && item.KeyHash != "" {
				a.metadataToKeyHash[item.Metadata] = item.KeyHash
			}
		} else {
			a.metadataToKeyHash[item.Metadata] = item.KeyHash
		}

		if item.KeyHash == "" {
			continue
		}
		if current, ok := a.keyHashToMetadata[item.KeyHash]; ok && current != item.Metadata {
			return nil, fmt.Errorf("sample key hash %s has multiple metadata values", item.KeyHash)
		}
		a.keyHashToMetadata[item.KeyHash] = item.Metadata
	}
	return a, nil
}

// Allocate returns the existing allocation for the same canonical key or
// deterministically probes for the next available non-zero metadata value.
func (a *Allocator) Allocate(key SampleKey) (Allocation, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	canonical, err := key.canonical()
	if err != nil {
		return Allocation{}, err
	}

	sum := sha256.Sum256([]byte(canonical))
	keyHash := hex.EncodeToString(sum[:])
	if metadata, ok := a.keyHashToMetadata[keyHash]; ok {
		return Allocation{Metadata: metadata, KeyHash: keyHash}, nil
	}

	initial := metadataCandidate(sum)
	metadata := initial
	for {
		if _, occupied := a.metadataToKeyHash[metadata]; !occupied {
			a.metadataToKeyHash[metadata] = keyHash
			a.keyHashToMetadata[keyHash] = metadata
			return Allocation{Metadata: metadata, KeyHash: keyHash}, nil
		}
		metadata = nextMetadata(metadata)
		if metadata == initial {
			return Allocation{}, errors.New("no non-zero ACL sample metadata is available")
		}
	}
}

// HashACLMatch returns the lowercase SHA-256 digest stored instead of the full
// generated ACL match.
func HashACLMatch(match string) string {
	sum := sha256.Sum256([]byte(match))
	return hex.EncodeToString(sum[:])
}

func (key SampleKey) canonical() (string, error) {
	if err := key.validate(); err != nil {
		return "", err
	}

	fields := []string{
		"schema-version=" + key.SchemaVersion,
		"policy-uid=" + key.PolicyUID,
		"direction=" + key.Direction,
	}
	if key.RuleIndex != nil {
		fields = append(fields, "rule-index="+strconv.Itoa(*key.RuleIndex))
	}
	fields = append(fields,
		"acl-role="+key.Role,
		"protocol="+key.Protocol,
		"acl-match-hash="+key.ACLMatchHash,
		"ovn-action="+key.OVNAction,
	)
	return strings.Join(fields, "\n") + "\n", nil
}

func (key SampleKey) validate() error {
	if key.SchemaVersion != SchemaVersionV1 {
		return fmt.Errorf("unsupported sample schema version %q", key.SchemaVersion)
	}
	if key.PolicyUID == "" {
		return errors.New("policy UID must not be empty")
	}
	if key.Direction != DirectionIngress && key.Direction != DirectionEgress {
		return fmt.Errorf("unsupported policy direction %q", key.Direction)
	}
	if key.Protocol == "" {
		return errors.New("protocol must not be empty")
	}
	if len(key.ACLMatchHash) != sha256.Size*2 {
		return errors.New("ACL match hash must be a SHA-256 digest")
	}
	if _, err := hex.DecodeString(key.ACLMatchHash); err != nil {
		return errors.New("ACL match hash must be a lowercase SHA-256 digest")
	}
	if strings.ToLower(key.ACLMatchHash) != key.ACLMatchHash {
		return errors.New("ACL match hash must be a lowercase SHA-256 digest")
	}

	switch key.Role {
	case RoleRuleAllow:
		if key.RuleIndex == nil || *key.RuleIndex < 0 {
			return errors.New("rule-allow sample key requires a non-negative rule index")
		}
		if key.OVNAction != ActionAllowRelated {
			return fmt.Errorf("rule-allow sample key requires OVN action %q", ActionAllowRelated)
		}
	case RoleDefaultDeny:
		if key.RuleIndex != nil {
			return errors.New("default-deny sample key must not include a rule index")
		}
		if key.OVNAction != ActionDrop {
			return fmt.Errorf("default-deny sample key requires OVN action %q", ActionDrop)
		}
	default:
		return fmt.Errorf("unsupported ACL sample role %q", key.Role)
	}

	return nil
}

func metadataCandidate(sum [sha256.Size]byte) uint32 {
	candidate := binary.BigEndian.Uint32(sum[:4])
	if candidate == 0 {
		return 1
	}
	return candidate
}

func nextMetadata(metadata uint32) uint32 {
	metadata++
	if metadata == 0 {
		return 1
	}
	return metadata
}
