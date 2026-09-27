package controlplane

import (
	"testing"
	"time"

	"github.com/aponysus/recourse/policy"
)

func TestPolicyCache_SetGetAndInvalidate(t *testing.T) {
	cache := NewPolicyCache()
	key := policy.ParseKey("svc.method")
	pol := policy.EffectivePolicy{Retry: policy.RetryPolicy{MaxAttempts: 2}}

	cache.Set(key, pol, 50*time.Millisecond)
	got, found, negative := cache.Get(key)
	if !found || negative {
		t.Fatalf("expected positive cache hit")
	}
	if got.Retry.MaxAttempts != 2 {
		t.Fatalf("got MaxAttempts=%d, want 2", got.Retry.MaxAttempts)
	}

	cache.SetMissing(key, 50*time.Millisecond)
	_, found, negative = cache.Get(key)
	if !found || !negative {
		t.Fatalf("expected negative cache hit")
	}

	cache.Invalidate(key)
	_, found, negative = cache.Get(key)
	if found || negative {
		t.Fatalf("expected cache miss after invalidate")
	}
}

func TestPolicyCache_Expiry(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	cache := NewPolicyCache()
	cache.nowFn = clock.Now
	key := policy.ParseKey("svc.expire")
	pol := policy.EffectivePolicy{}

	cache.Set(key, pol, 10*time.Millisecond)
	clock.Advance(20 * time.Millisecond)

	_, found, negative := cache.Get(key)
	if found || negative {
		t.Fatalf("expected expired cache entry to miss")
	}
}

func TestPolicyCache_LKGWindow(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	cache := NewPolicyCache()
	cache.nowFn = clock.Now
	key := policy.ParseKey("svc.lkg")
	pol := policy.DefaultPolicyFor(key)
	cache.SetWithLKG(key, pol, 10*time.Second, 20*time.Second)

	if _, found, negative := cache.Get(key); !found || negative {
		t.Fatal("expected fresh policy")
	}
	clock.Advance(10 * time.Second)
	if _, found, _ := cache.Get(key); found {
		t.Fatal("fresh entry must expire at the TTL boundary")
	}
	if got, ok := cache.GetLKG(key); !ok || got.Meta.Source != pol.Meta.Source {
		t.Fatalf("expected retained policy with unchanged metadata, got %+v, %v", got, ok)
	}
	clock.Advance(20 * time.Second)
	if _, ok := cache.GetLKG(key); ok {
		t.Fatal("LKG must expire at the retention boundary")
	}
}

func TestPolicyCache_DiscardLKG(t *testing.T) {
	key := policy.ParseKey("svc.lkg")
	pol := policy.DefaultPolicyFor(key)
	for name, discard := range map[string]func(*PolicyCache){
		"invalidate": func(c *PolicyCache) { c.Invalidate(key) },
		"missing":    func(c *PolicyCache) { c.SetMissing(key, time.Second) },
		"set":        func(c *PolicyCache) { c.Set(key, pol, time.Second) },
		"disabled":   func(c *PolicyCache) { c.SetWithLKG(key, pol, time.Second, 0) },
		"negative":   func(c *PolicyCache) { c.SetWithLKG(key, pol, time.Second, -time.Second) },
	} {
		t.Run(name, func(t *testing.T) {
			clock := &fakeClock{now: time.Unix(0, 0)}
			cache := NewPolicyCache()
			cache.nowFn = clock.Now
			cache.SetWithLKG(key, pol, time.Second, time.Minute)
			discard(cache)
			if _, ok := cache.GetLKG(key); ok {
				t.Fatal("expected LKG to be discarded")
			}
			clock.Advance(2 * time.Second)
			if _, ok := cache.GetLKG(key); ok {
				t.Fatal("LKG must not return after replacement expires")
			}
		})
	}
}

func TestPolicyCache_LKGWithoutFreshCaching(t *testing.T) {
	for _, ttl := range []time.Duration{0, -time.Second} {
		t.Run(ttl.String(), func(t *testing.T) {
			clock := &fakeClock{now: time.Unix(0, 0)}
			cache := NewPolicyCache()
			cache.nowFn = clock.Now
			key := policy.ParseKey("svc.lkg")
			cache.SetWithLKG(key, policy.DefaultPolicyFor(key), ttl, time.Second)
			if _, found, _ := cache.Get(key); found {
				t.Fatal("non-positive TTL must disable fresh caching")
			}
			if _, ok := cache.GetLKG(key); !ok {
				t.Fatal("expected LKG retention independent of fresh caching")
			}
			clock.Advance(time.Second)
			if _, ok := cache.GetLKG(key); ok {
				t.Fatal("expected LKG expiry")
			}
		})
	}
}

type fakeClock struct {
	now time.Time
}

func (f *fakeClock) Now() time.Time {
	return f.now
}

func (f *fakeClock) Advance(d time.Duration) {
	f.now = f.now.Add(d)
}
