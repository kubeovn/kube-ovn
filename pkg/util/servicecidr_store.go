package util

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubeovnv1 "github.com/kubeovn/kube-ovn/pkg/apis/kubeovn/v1"
)

// ReadyServiceCIDRs returns Spec.CIDRs when the ServiceCIDR object's Ready
// condition is True. Objects still being initialized or already terminating
// are skipped, matching the apiserver allocator's own behavior.
func ReadyServiceCIDRs(sc *networkingv1.ServiceCIDR) []string {
	if sc == nil {
		return nil
	}
	for _, cond := range sc.Status.Conditions {
		if cond.Type == networkingv1.ServiceCIDRConditionReady {
			if cond.Status == metav1.ConditionTrue {
				return sc.Spec.CIDRs
			}
			return nil
		}
	}
	return nil
}

// ServiceCIDRStore is the merged source of truth for Service CIDRs known to
// kube-ovn. It combines the values from --service-cluster-ip-range (fallback)
// with the CIDRs found in networking.k8s.io/v1 ServiceCIDR objects, when the
// API is available.
//
// The store dedupes by string equality, filters by IP family, and notifies
// registered handlers when the merged set actually changes (debounced 1s to
// coalesce informer initial-list bursts).
type ServiceCIDRStore struct {
	mu       sync.RWMutex
	fallback []string
	fromAPI  map[string][]string
	handlers []func()
	debounce *time.Timer
	cached   []string

	debounceInterval time.Duration
}

// NewServiceCIDRStore parses the --service-cluster-ip-range flag value and
// returns a store seeded with those CIDRs as the permanent fallback set.
func NewServiceCIDRStore(flagValue string) *ServiceCIDRStore {
	v4, v6 := SplitStringIP(flagValue)
	fallback := make([]string, 0, 2)
	if v4 != "" {
		fallback = append(fallback, v4)
	}
	if v6 != "" {
		fallback = append(fallback, v6)
	}
	s := &ServiceCIDRStore{
		fallback:         fallback,
		fromAPI:          make(map[string][]string),
		debounceInterval: time.Second,
	}
	s.cached = s.merged()
	return s
}

// merged returns the deduped + sorted set of effective Service CIDRs. The
// flag-derived fallback is used only when no ServiceCIDR object has supplied a
// valid CIDR — the moment the API takes over (e.g. the default `kubernetes`
// ServiceCIDR is observed) the flag steps aside so that deletions/migrations
// of ServiceCIDR objects can shrink the live set. If the API set later becomes
// empty (no Ready objects), the fallback re-engages so the data plane keeps a
// usable baseline.
// Caller must hold s.mu.
func (s *ServiceCIDRStore) merged() []string {
	seen := make(map[string]struct{}, len(s.fallback)+len(s.fromAPI)*2)
	out := make([]string, 0, len(s.fallback)+len(s.fromAPI)*2)
	add := func(cidr string) {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			return
		}
		if CheckProtocol(cidr) == "" {
			return
		}
		if _, ok := seen[cidr]; ok {
			return
		}
		seen[cidr] = struct{}{}
		out = append(out, cidr)
	}
	for _, cidrs := range s.fromAPI {
		for _, cidr := range cidrs {
			add(cidr)
		}
	}
	if len(out) == 0 {
		for _, cidr := range s.fallback {
			add(cidr)
		}
	}
	sort.Strings(out)
	return out
}

// AllCIDRs returns the merged set in sorted order.
func (s *ServiceCIDRStore) AllCIDRs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, len(s.cached))
	copy(out, s.cached)
	return out
}

// V4CIDRs returns the IPv4 subset of the merged set.
func (s *ServiceCIDRStore) V4CIDRs() []string { return s.byProtocol(kubeovnv1.ProtocolIPv4) }

// V6CIDRs returns the IPv6 subset of the merged set.
func (s *ServiceCIDRStore) V6CIDRs() []string { return s.byProtocol(kubeovnv1.ProtocolIPv6) }

func (s *ServiceCIDRStore) byProtocol(proto string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.cached))
	for _, cidr := range s.cached {
		if CheckProtocol(cidr) == proto {
			out = append(out, cidr)
		}
	}
	return out
}

// Hash returns a stable SHA-256 over the sorted merged set. Used as a
// deployment annotation to roll vpc-lb pods when the set changes.
func (s *ServiceCIDRStore) Hash() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	h := sha256.New()
	for _, cidr := range s.cached {
		h.Write([]byte(cidr))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// UpsertFromAPI sets the CIDRs for a given ServiceCIDR object name. Returns
// true if the merged set changed (handlers will fire after debounce).
func (s *ServiceCIDRStore) UpsertFromAPI(name string, cidrs []string) bool {
	s.mu.Lock()
	cleaned := make([]string, 0, len(cidrs))
	for _, c := range cidrs {
		c = strings.TrimSpace(c)
		if c != "" {
			cleaned = append(cleaned, c)
		}
	}
	if slices.Equal(s.fromAPI[name], cleaned) {
		s.mu.Unlock()
		return false
	}
	s.fromAPI[name] = cleaned
	changed := s.recomputeLocked()
	s.mu.Unlock()
	if changed {
		s.scheduleFire()
	}
	return changed
}

// DeleteFromAPI removes a ServiceCIDR object from the store.
func (s *ServiceCIDRStore) DeleteFromAPI(name string) bool {
	s.mu.Lock()
	if _, ok := s.fromAPI[name]; !ok {
		s.mu.Unlock()
		return false
	}
	delete(s.fromAPI, name)
	changed := s.recomputeLocked()
	s.mu.Unlock()
	if changed {
		s.scheduleFire()
	}
	return changed
}

// recomputeLocked refreshes s.cached. Caller must hold s.mu (write).
// Returns true if the slice content changed.
func (s *ServiceCIDRStore) recomputeLocked() bool {
	next := s.merged()
	if slices.Equal(s.cached, next) {
		return false
	}
	s.cached = next
	return true
}

// OnChange registers a handler called (off the lock, in a goroutine) whenever
// the merged set changes. Multiple handlers are supported.
func (s *ServiceCIDRStore) OnChange(h func()) {
	s.mu.Lock()
	s.handlers = append(s.handlers, h)
	s.mu.Unlock()
}

// scheduleFire coalesces bursts of updates into a single round of handler
// invocations after debounceInterval.
func (s *ServiceCIDRStore) scheduleFire() {
	s.mu.Lock()
	if s.debounce != nil {
		s.debounce.Stop()
	}
	s.debounce = time.AfterFunc(s.debounceInterval, s.fireOnChange)
	s.mu.Unlock()
}

func (s *ServiceCIDRStore) fireOnChange() {
	s.mu.RLock()
	handlers := make([]func(), len(s.handlers))
	copy(handlers, s.handlers)
	s.mu.RUnlock()
	for _, h := range handlers {
		go h()
	}
}
