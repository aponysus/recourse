package controlplane

import (
	"sync"
	"time"

	"github.com/aponysus/recourse/policy"
)

type cacheEntry struct {
	policy       policy.EffectivePolicy
	expiresAt    time.Time
	lkgExpiresAt time.Time
	found        bool // true if policy exists, false if this is a negative cache entry
}

// PolicyCache is a thread-safe cache for policies with TTL support.
type PolicyCache struct {
	mu      sync.RWMutex
	entries map[policy.PolicyKey]cacheEntry
	nowFn   func() time.Time
}

// NewPolicyCache creates a new, empty PolicyCache.
func NewPolicyCache() *PolicyCache {
	return &PolicyCache{
		entries: make(map[policy.PolicyKey]cacheEntry),
	}
}

// Get retrieves a policy from the cache.
// foundInCache is true for fresh positive and negative entries. A negative hit
// returns a zero policy and isNegativeCache=true. Expired entries are misses;
// use GetLKG to retrieve a policy retained for fallback.
func (c *PolicyCache) Get(key policy.PolicyKey) (pol policy.EffectivePolicy, foundInCache bool, isNegativeCache bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[key]
	if !ok {
		return policy.EffectivePolicy{}, false, false
	}

	if !c.now().Before(entry.expiresAt) {
		return policy.EffectivePolicy{}, false, false
	}

	return entry.policy, true, !entry.found
}

// Set adds or updates a policy without retaining it for LKG fallback.
func (c *PolicyCache) Set(key policy.PolicyKey, pol policy.EffectivePolicy, ttl time.Duration) {
	c.SetWithLKG(key, pol, ttl, 0)
}

// SetWithLKG caches a policy for ttl, retaining it for an additional lkgTTL
// after expiry. Non-positive ttl disables fresh caching; non-positive lkgTTL
// disables LKG. Callers must validate the policy before storing it.
func (c *PolicyCache) SetWithLKG(key policy.PolicyKey, pol policy.EffectivePolicy, ttl, lkgTTL time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if ttl < 0 {
		ttl = 0
	}
	entry := cacheEntry{
		policy:    pol,
		expiresAt: c.now().Add(ttl),
		found:     true,
	}
	if lkgTTL > 0 {
		entry.lkgExpiresAt = entry.expiresAt.Add(lkgTTL)
	}
	c.entries[key] = entry
}

// GetLKG retrieves a positive policy within its configured LKG retention
// window, including while fresh. It does not extend expiry or change metadata.
func (c *PolicyCache) GetLKG(key policy.PolicyKey) (policy.EffectivePolicy, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[key]
	if !ok || !entry.found || entry.lkgExpiresAt.IsZero() || !c.now().Before(entry.lkgExpiresAt) {
		return policy.EffectivePolicy{}, false
	}
	return entry.policy, true
}

// SetMissing records a negative cache entry, discarding any retained LKG.
func (c *PolicyCache) SetMissing(key policy.PolicyKey, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[key] = cacheEntry{
		expiresAt: c.now().Add(ttl),
		found:     false,
	}
}

// Invalidate removes both the fresh and retained LKG entry from the cache.
func (c *PolicyCache) Invalidate(key policy.PolicyKey) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}

func (c *PolicyCache) now() time.Time {
	if c.nowFn != nil {
		return c.nowFn()
	}
	return time.Now()
}
