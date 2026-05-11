package util

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestServiceCIDRStoreFlagOnly(t *testing.T) {
	s := NewServiceCIDRStore("10.96.0.0/12,fd00::/108")
	v4 := s.V4CIDRs()
	v6 := s.V6CIDRs()
	if len(v4) != 1 || v4[0] != "10.96.0.0/12" {
		t.Fatalf("unexpected v4: %v", v4)
	}
	if len(v6) != 1 || v6[0] != "fd00::/108" {
		t.Fatalf("unexpected v6: %v", v6)
	}
	if got := s.AllCIDRs(); len(got) != 2 {
		t.Fatalf("expected 2 cidrs, got %v", got)
	}
}

func TestServiceCIDRStoreUpsertAndDelete(t *testing.T) {
	s := NewServiceCIDRStore("10.96.0.0/12")
	s.debounceInterval = 5 * time.Millisecond

	var fired int32
	done := make(chan struct{}, 8)
	s.OnChange(func() {
		atomic.AddInt32(&fired, 1)
		done <- struct{}{}
	})

	// First API entry is identical to the flag — the merged set's content is
	// unchanged, so UpsertFromAPI returns false even though the source has
	// shifted from fallback to API. A second, distinct entry is what brings
	// the set to two CIDRs.
	if s.UpsertFromAPI("primary", []string{"10.96.0.0/12"}) {
		t.Fatal("expected no change when API entry equals flag content")
	}
	if !s.UpsertFromAPI("extra", []string{"10.97.0.0/16"}) {
		t.Fatal("expected change on second upsert")
	}

	v4 := s.V4CIDRs()
	if len(v4) != 2 || v4[0] != "10.96.0.0/12" || v4[1] != "10.97.0.0/16" {
		t.Fatalf("unexpected v4: %v", v4)
	}

	waitFor(t, done, 1)
	time.Sleep(2 * s.debounceInterval) // ensure first fire's debounce window ended

	if !s.DeleteFromAPI("extra") {
		t.Fatal("expected change on delete")
	}
	if s.DeleteFromAPI("extra") {
		t.Fatal("expected no change on second delete")
	}
	if got := s.AllCIDRs(); len(got) != 1 || got[0] != "10.96.0.0/12" {
		t.Fatalf("expected only the remaining API entry after delete, got %v", got)
	}

	waitFor(t, done, 1)
	if atomic.LoadInt32(&fired) < 2 {
		t.Fatalf("expected at least 2 fires across upsert+delete, got %d", fired)
	}
}

func TestServiceCIDRStoreDedup(t *testing.T) {
	s := NewServiceCIDRStore("10.96.0.0/12")
	s.UpsertFromAPI("k", []string{"10.96.0.0/12"})
	if got := s.AllCIDRs(); len(got) != 1 {
		t.Fatalf("expected dedup to 1, got %v", got)
	}
}

func TestServiceCIDRStoreFlagYieldsToAPI(t *testing.T) {
	// flag is the initial bootstrap value; it must yield as soon as the API
	// observes any valid ServiceCIDR, otherwise deletions/migrations cannot
	// shrink the effective set.
	s := NewServiceCIDRStore("10.96.0.0/12")
	if got := s.AllCIDRs(); len(got) != 1 || got[0] != "10.96.0.0/12" {
		t.Fatalf("flag should be live before any API entry: %v", got)
	}

	s.UpsertFromAPI("kubernetes", []string{"10.97.0.0/16"})
	got := s.AllCIDRs()
	if len(got) != 1 || got[0] != "10.97.0.0/16" {
		t.Fatalf("flag should yield to API entries: %v", got)
	}

	// All API entries removed → fallback re-engages so the data plane keeps a
	// usable set.
	s.DeleteFromAPI("kubernetes")
	got = s.AllCIDRs()
	if len(got) != 1 || got[0] != "10.96.0.0/12" {
		t.Fatalf("fallback must re-engage when API set empties: %v", got)
	}
}

func TestServiceCIDRStoreNonReadyDoesNotShadowFlag(t *testing.T) {
	// readyServiceCIDRs returns nil for non-Ready objects; UpsertFromAPI then
	// stores an empty slice. The merged set must still fall back to the flag
	// so the data plane keeps the baseline programmed.
	s := NewServiceCIDRStore("10.96.0.0/12")
	s.UpsertFromAPI("pending", nil)
	got := s.AllCIDRs()
	if len(got) != 1 || got[0] != "10.96.0.0/12" {
		t.Fatalf("non-ready API entries should not shadow flag fallback: %v", got)
	}
}

func TestServiceCIDRStoreHashStability(t *testing.T) {
	a := NewServiceCIDRStore("10.96.0.0/12,fd00::/108")
	b := NewServiceCIDRStore("fd00::/108,10.96.0.0/12") // order-insensitive after merge
	if a.Hash() != b.Hash() {
		t.Fatalf("hash should be order-insensitive: a=%s b=%s", a.Hash(), b.Hash())
	}
	original := a.Hash()
	a.UpsertFromAPI("x", []string{"10.97.0.0/16"})
	if a.Hash() == original {
		t.Fatalf("hash should change after upsert")
	}
	a.DeleteFromAPI("x")
	if a.Hash() != original {
		t.Fatalf("hash should restore after delete: got=%s want=%s", a.Hash(), original)
	}
}

func TestServiceCIDRStoreInvalidIgnored(t *testing.T) {
	s := NewServiceCIDRStore("10.96.0.0/12")
	s.UpsertFromAPI("bogus", []string{"not-a-cidr", "", " "})
	if got := s.AllCIDRs(); len(got) != 1 || got[0] != "10.96.0.0/12" {
		t.Fatalf("invalid entries should be ignored, got %v", got)
	}
}

func TestServiceCIDRStoreDebounceCoalesces(t *testing.T) {
	s := NewServiceCIDRStore("10.96.0.0/12")
	s.debounceInterval = 30 * time.Millisecond

	var fired int32
	s.OnChange(func() { atomic.AddInt32(&fired, 1) })

	for range 5 {
		s.UpsertFromAPI("k", []string{"10.97.0.0/16"})
		s.UpsertFromAPI("k", []string{"10.98.0.0/16"})
	}
	time.Sleep(120 * time.Millisecond)
	if got := atomic.LoadInt32(&fired); got > 2 {
		t.Fatalf("debounce should coalesce bursts, fired=%d", got)
	}
}

func waitFor(t *testing.T, ch <-chan struct{}, atLeast int) {
	t.Helper()
	deadline := time.After(500 * time.Millisecond)
	got := 0
	for got < atLeast {
		select {
		case <-ch:
			got++
		case <-deadline:
			t.Fatalf("timed out waiting for %d events, got %d", atLeast, got)
		}
	}
}
