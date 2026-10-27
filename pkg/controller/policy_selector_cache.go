package controller

import (
	"slices"
	"sync"
)

type policyAddressCacheEntry struct {
	selected []string
	except   []string
}

type policyPodScanCacheEntry struct {
	ports []string
	v4    []string
	v6    []string
}

// policySelectorCache avoids repeating the same informer scans while the
// policy workers reconcile several rules that use an identical selector.
// Entries are discarded whenever a Pod or Namespace event can change the
// selected result.
type policySelectorCache struct {
	mu         sync.RWMutex
	addresses  map[string]policyAddressCacheEntry
	podScans   map[string]policyPodScanCacheEntry
	generation uint64
}

func (c *Controller) policySelectorCacheGeneration() uint64 {
	c.policySelectorCache.mu.RLock()
	generation := c.policySelectorCache.generation
	c.policySelectorCache.mu.RUnlock()
	return generation
}

func (c *Controller) getPolicyAddressCache(key string) ([]string, []string, bool) {
	c.policySelectorCache.mu.RLock()
	entry, ok := c.policySelectorCache.addresses[key]
	c.policySelectorCache.mu.RUnlock()
	if !ok {
		return nil, nil, false
	}
	return slices.Clone(entry.selected), slices.Clone(entry.except), true
}

func (c *Controller) setPolicyAddressCache(key string, selected, except []string, generation uint64) {
	c.policySelectorCache.mu.Lock()
	if generation != c.policySelectorCache.generation {
		c.policySelectorCache.mu.Unlock()
		return
	}
	if c.policySelectorCache.addresses == nil {
		c.policySelectorCache.addresses = make(map[string]policyAddressCacheEntry)
	}
	c.policySelectorCache.addresses[key] = policyAddressCacheEntry{
		selected: slices.Clone(selected),
		except:   slices.Clone(except),
	}
	c.policySelectorCache.mu.Unlock()
}

func (c *Controller) getPolicyPodScanCache(key string) ([]string, []string, []string, bool) {
	c.policySelectorCache.mu.RLock()
	entry, ok := c.policySelectorCache.podScans[key]
	c.policySelectorCache.mu.RUnlock()
	if !ok {
		return nil, nil, nil, false
	}
	return slices.Clone(entry.ports), slices.Clone(entry.v4), slices.Clone(entry.v6), true
}

func (c *Controller) setPolicyPodScanCache(key string, ports, v4, v6 []string, generation uint64) {
	c.policySelectorCache.mu.Lock()
	if generation != c.policySelectorCache.generation {
		c.policySelectorCache.mu.Unlock()
		return
	}
	if c.policySelectorCache.podScans == nil {
		c.policySelectorCache.podScans = make(map[string]policyPodScanCacheEntry)
	}
	c.policySelectorCache.podScans[key] = policyPodScanCacheEntry{
		ports: slices.Clone(ports),
		v4:    slices.Clone(v4),
		v6:    slices.Clone(v6),
	}
	c.policySelectorCache.mu.Unlock()
}

func (c *Controller) invalidatePolicySelectorCache() {
	c.policySelectorCache.mu.Lock()
	c.policySelectorCache.generation++
	clear(c.policySelectorCache.addresses)
	clear(c.policySelectorCache.podScans)
	c.policySelectorCache.mu.Unlock()
}
