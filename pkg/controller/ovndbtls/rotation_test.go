package ovndbtls

import (
	"testing"
	"time"
)

func TestNeedsRenewal(t *testing.T) {
	notBefore := time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC)
	notAfter := notBefore.Add(10 * time.Hour)

	if NeedsRenewal(notBefore.Add(4*time.Hour), notBefore, notAfter) {
		t.Fatal("NeedsRenewal before half-life = true, want false")
	}
	if !NeedsRenewal(notBefore.Add(5*time.Hour), notBefore, notAfter) {
		t.Fatal("NeedsRenewal at half-life = false, want true")
	}
	if !NeedsRenewal(notAfter.Add(time.Second), notBefore, notAfter) {
		t.Fatal("NeedsRenewal after expiry = false, want true")
	}
}
