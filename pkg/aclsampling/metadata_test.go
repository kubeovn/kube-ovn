package aclsampling

import (
	"crypto/sha256"
	"fmt"
	"math"
	"sync"
	"testing"
)

func validAllowSampleKey() SampleKey {
	return SampleKey{
		SchemaVersion: SchemaVersionV1,
		PolicyUID:     "11111111-2222-3333-4444-555555555555",
		Direction:     DirectionIngress,
		RuleIndex:     new(0),
		Role:          RoleRuleAllow,
		Protocol:      "IPv4",
		ACLMatchHash:  HashACLMatch("outport == @policy && ip4.src == $allow"),
		OVNAction:     ActionAllowRelated,
	}
}

func TestSampleKeyCanonicalHash(t *testing.T) {
	allocator, err := NewAllocator(nil)
	if err != nil {
		t.Fatal(err)
	}
	allocation, err := allocator.Allocate(validAllowSampleKey())
	if err != nil {
		t.Fatal(err)
	}

	const wantKeyHash = "cbef9de432159cede19e36ee3748e823a814fa28805569c45977be4ace46181d"
	if allocation.KeyHash != wantKeyHash {
		t.Fatalf("key hash = %q, want %q", allocation.KeyHash, wantKeyHash)
	}
	if allocation.Metadata != 3421478372 {
		t.Fatalf("metadata = %d, want 3421478372", allocation.Metadata)
	}
}

func TestAllocatorReusesExistingCanonicalKey(t *testing.T) {
	key := validAllowSampleKey()
	first, err := NewAllocator(nil)
	if err != nil {
		t.Fatal(err)
	}
	allocation, err := first.Allocate(key)
	if err != nil {
		t.Fatal(err)
	}

	allocator, err := NewAllocator([]OccupiedMetadata{{Metadata: 42, KeyHash: allocation.KeyHash}})
	if err != nil {
		t.Fatal(err)
	}
	reused, err := allocator.Allocate(key)
	if err != nil {
		t.Fatal(err)
	}
	if reused.Metadata != 42 {
		t.Fatalf("metadata = %d, want 42", reused.Metadata)
	}
}

func TestAllocatorProbesPastCollisions(t *testing.T) {
	key := validAllowSampleKey()
	canonical, err := key.canonical()
	if err != nil {
		t.Fatal(err)
	}
	initial := metadataCandidate(sha256.Sum256([]byte(canonical)))
	second := nextMetadata(initial)
	want := nextMetadata(second)

	allocator, err := NewAllocator([]OccupiedMetadata{
		{Metadata: initial},
		{Metadata: second, KeyHash: HashACLMatch("another canonical key")},
	})
	if err != nil {
		t.Fatal(err)
	}
	allocation, err := allocator.Allocate(key)
	if err != nil {
		t.Fatal(err)
	}
	if allocation.Metadata != want {
		t.Fatalf("metadata = %d, want %d", allocation.Metadata, want)
	}
}

func TestAllocatorReservesTransactionMetadata(t *testing.T) {
	allocator, err := NewAllocator(nil)
	if err != nil {
		t.Fatal(err)
	}
	firstKey := validAllowSampleKey()
	first, err := allocator.Allocate(firstKey)
	if err != nil {
		t.Fatal(err)
	}
	again, err := allocator.Allocate(firstKey)
	if err != nil {
		t.Fatal(err)
	}
	if again != first {
		t.Fatalf("repeated allocation = %#v, want %#v", again, first)
	}

	secondKey := firstKey
	secondKey.RuleIndex = new(1)
	second, err := allocator.Allocate(secondKey)
	if err != nil {
		t.Fatal(err)
	}
	if second.Metadata == first.Metadata {
		t.Fatalf("distinct keys reused metadata %d", first.Metadata)
	}
}

func TestAllocatorAllocatesDefaultDenyWithoutRuleIndex(t *testing.T) {
	key := validAllowSampleKey()
	key.RuleIndex = nil
	key.Role = RoleDefaultDeny
	key.Protocol = "IP"
	key.OVNAction = ActionDrop

	allocator, err := NewAllocator(nil)
	if err != nil {
		t.Fatal(err)
	}
	allocation, err := allocator.Allocate(key)
	if err != nil {
		t.Fatal(err)
	}
	if allocation.Metadata == 0 {
		t.Fatal("Allocate() returned zero metadata")
	}
}

func TestAllocatorSupportsConcurrentReservations(t *testing.T) {
	allocator, err := NewAllocator(nil)
	if err != nil {
		t.Fatal(err)
	}

	const count = 100
	results := make(chan Allocation, count)
	errors := make(chan error, count)
	var wg sync.WaitGroup
	for i := range count {
		wg.Go(func() {
			key := validAllowSampleKey()
			key.PolicyUID = fmt.Sprintf("00000000-0000-0000-0000-%012d", i)
			allocation, err := allocator.Allocate(key)
			if err != nil {
				errors <- err
				return
			}
			results <- allocation
		})
	}
	wg.Wait()
	close(results)
	close(errors)

	for err := range errors {
		t.Errorf("Allocate() error = %v", err)
	}
	seen := make(map[uint32]struct{}, count)
	for allocation := range results {
		if _, ok := seen[allocation.Metadata]; ok {
			t.Errorf("metadata %d allocated more than once", allocation.Metadata)
		}
		seen[allocation.Metadata] = struct{}{}
	}
	if len(seen) != count {
		t.Fatalf("allocated %d unique metadata values, want %d", len(seen), count)
	}
}

func TestNewAllocatorRejectsConflictingMappings(t *testing.T) {
	tests := []struct {
		name     string
		occupied []OccupiedMetadata
	}{
		{
			name: "zero metadata",
			occupied: []OccupiedMetadata{
				{Metadata: 0},
			},
		},
		{
			name: "metadata has multiple keys",
			occupied: []OccupiedMetadata{
				{Metadata: 1, KeyHash: "first"},
				{Metadata: 1, KeyHash: "second"},
			},
		},
		{
			name: "key has multiple metadata values",
			occupied: []OccupiedMetadata{
				{Metadata: 1, KeyHash: "same"},
				{Metadata: 2, KeyHash: "same"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewAllocator(tt.occupied); err == nil {
				t.Fatal("NewAllocator() expected an error")
			}
		})
	}
}

func TestSampleKeyValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*SampleKey)
	}{
		{name: "unsupported schema", mutate: func(key *SampleKey) { key.SchemaVersion = "v2" }},
		{name: "empty policy UID", mutate: func(key *SampleKey) { key.PolicyUID = "" }},
		{name: "invalid direction", mutate: func(key *SampleKey) { key.Direction = "Inbound" }},
		{name: "missing rule index", mutate: func(key *SampleKey) { key.RuleIndex = nil }},
		{name: "negative rule index", mutate: func(key *SampleKey) { key.RuleIndex = new(-1) }},
		{name: "invalid role", mutate: func(key *SampleKey) { key.Role = "exception" }},
		{name: "invalid action", mutate: func(key *SampleKey) { key.OVNAction = ActionDrop }},
		{name: "empty protocol", mutate: func(key *SampleKey) { key.Protocol = "" }},
		{name: "invalid match hash", mutate: func(key *SampleKey) { key.ACLMatchHash = "invalid" }},
		{name: "uppercase match hash", mutate: func(key *SampleKey) {
			key.ACLMatchHash = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
		}},
		{name: "default deny with rule index", mutate: func(key *SampleKey) {
			key.Role = RoleDefaultDeny
			key.OVNAction = ActionDrop
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := validAllowSampleKey()
			tt.mutate(&key)
			allocator, err := NewAllocator(nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := allocator.Allocate(key); err == nil {
				t.Fatal("Allocate() expected an error")
			}
		})
	}
}

func TestMetadataCandidateAndWrap(t *testing.T) {
	if got := metadataCandidate([sha256.Size]byte{}); got != 1 {
		t.Fatalf("zero candidate remapped to %d, want 1", got)
	}
	if got := nextMetadata(math.MaxUint32); got != 1 {
		t.Fatalf("wrapped metadata = %d, want 1", got)
	}
}
