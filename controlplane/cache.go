package controlplane

import (
	"sync"
	"time"

	"github.com/aponysus/recourse/policy"
)

type cacheEntry struct {
	policy    policy.EffectivePolicy
	expiresAt time.Time
	found     bool // true if policy exists, false if this is a negative cache entry
}

// PolicyCache is a thread-safe cache for policies with TTL support.
type PolicyCache struct {
	mu      sync.RWMutex
	entries map[policy.PolicyKey]cacheEntry
}

// NewPolicyCache creates a new, empty PolicyCache.
func NewPolicyCache() *PolicyCache {
	return &PolicyCache{
		entries: make(map[policy.PolicyKey]cacheEntry),
	}
}

// Get retrieves a policy from the cache.
// Returns (policy, found=true) if a valid entry exists (even if it's a negative cache hit).
// Returns (policy, found=false) if the entry is missing or expired.
// If the entry is a negative cache hit, the returned policy will be zero value and found will be true.
// Check entry.found to distinguish between "cached missing" and "not in cache".
func (c *PolicyCache) Get(key policy.PolicyKey) (pol policy.EffectivePolicy, foundInCache bool, isNegativeCache bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[key]
	if !ok {
		return policy.EffectivePolicy{}, false, false
	}

	if time.Now().After(entry.expiresAt) {
		return policy.EffectivePolicy{}, false, false
	}

	return entry.policy, true, !entry.found
}

// Set adds or updates a policy in the cache.
func (c *PolicyCache) Set(key policy.PolicyKey, pol policy.EffectivePolicy, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[key] = cacheEntry{
		policy:    pol,
		expiresAt: time.Now().Add(ttl),
		found:     true,
	}
}

// SetMissing records a negative cache entry (policy not found).
func (c *PolicyCache) SetMissing(key policy.PolicyKey, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[key] = cacheEntry{
		expiresAt: time.Now().Add(ttl),
		found:     false,
	}
}

// Invalidate removes an entry from the cache.
func (c *PolicyCache) Invalidate(key policy.PolicyKey) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}
