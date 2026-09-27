package controlplane

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/aponysus/recourse/policy"
)

// Source is the interface for fetching raw policy configuration.
type Source interface {
	// GetPolicy returns the policy for the given key.
	// If the policy is not found, it must return ErrPolicyNotFound.
	// Implementations must respect ctx cancellation. Other errors are eligible
	// for LKG fallback when enabled, provided ctx is still active.
	GetPolicy(ctx context.Context, key policy.PolicyKey) (policy.EffectivePolicy, error)
}

// RemoteProvider is a PolicyProvider that fetches policies from a Source and caches them.
type RemoteProvider struct {
	source           Source
	cache            *PolicyCache
	cacheTTL         time.Duration
	negativeCacheTTL time.Duration
	lkgTTL           time.Duration

	mu       sync.Mutex
	inflight map[policy.PolicyKey]*policyFetch
}

type policyFetch struct {
	done       chan struct{}
	policy     policy.EffectivePolicy
	err        error
	panicValue any
}

// RemoteProviderOption configures a RemoteProvider.
type RemoteProviderOption func(*RemoteProvider)

// WithCacheTTL sets the TTL for successful policy lookups. Default is 1 minute.
// Non-positive values disable fresh caching without disabling LKG retention.
func WithCacheTTL(ttl time.Duration) RemoteProviderOption {
	return func(p *RemoteProvider) {
		p.cacheTTL = ttl
	}
}

// WithNegativeCacheTTL sets the TTL for missing policy lookups. Default is 10 seconds.
// Non-positive values disable negative caching.
func WithNegativeCacheTTL(ttl time.Duration) RemoteProviderOption {
	return func(p *RemoteProvider) {
		p.negativeCacheTTL = ttl
	}
}

// WithLKGTTL retains the last successfully normalized policy for ttl beyond
// CacheTTL. During that window, source errors may use it as last-known-good
// (LKG). Non-positive values disable LKG, which is the default. Failed fetches
// do not extend this window; confirmed missing policies discard the LKG.
func WithLKGTTL(ttl time.Duration) RemoteProviderOption {
	return func(p *RemoteProvider) {
		p.lkgTTL = ttl
	}
}

// NewRemoteProvider creates a new RemoteProvider.
func NewRemoteProvider(source Source, opts ...RemoteProviderOption) *RemoteProvider {
	p := &RemoteProvider{
		source:           source,
		cache:            NewPolicyCache(),
		cacheTTL:         1 * time.Minute,
		negativeCacheTTL: 10 * time.Second,
		inflight:         make(map[policy.PolicyKey]*policyFetch),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// GetEffectivePolicy returns the policy for key, checking the cache first.
// Concurrent misses for the same key share a fetch using the first caller's
// context. Other callers can cancel their waits independently. Canceling the
// first caller cancels the shared fetch and its error is returned to waiters.
// Eligible LKG policies are returned with Source=lkg and a nil error.
func (p *RemoteProvider) GetEffectivePolicy(ctx context.Context, key policy.PolicyKey) (policy.EffectivePolicy, error) {
	if err := ctx.Err(); err != nil {
		return policy.EffectivePolicy{}, err
	}
	if pol, err, ok := p.cached(key); ok {
		return pol, err
	}

	p.mu.Lock()
	call, waiting := p.inflight[key]
	if !waiting {
		// The initial error also releases waiters if Source calls runtime.Goexit.
		call = &policyFetch{done: make(chan struct{}), err: ErrPolicyFetchFailed}
		p.inflight[key] = call
	}
	p.mu.Unlock()

	if waiting {
		select {
		case <-ctx.Done():
			return policy.EffectivePolicy{}, ctx.Err()
		case <-call.done:
		}
	} else {
		p.fetch(ctx, key, call)
	}

	if err := ctx.Err(); err != nil {
		return policy.EffectivePolicy{}, err
	}
	if call.panicValue != nil {
		panic(call.panicValue)
	}
	return call.policy, call.err
}

func (p *RemoteProvider) cached(key policy.PolicyKey) (policy.EffectivePolicy, error, bool) {
	pol, foundInCache, isNegative := p.cache.Get(key)
	if isNegative {
		return policy.EffectivePolicy{}, ErrPolicyNotFound, true
	}
	return pol, nil, foundInCache
}

func (p *RemoteProvider) fetch(ctx context.Context, key policy.PolicyKey, call *policyFetch) {
	defer func() {
		call.panicValue = recover()
		p.mu.Lock()
		delete(p.inflight, key)
		close(call.done)
		p.mu.Unlock()
		// Preserve the executor's ability to recover provider panics, and do not
		// leave other callers blocked if a Source panics.
		if call.panicValue != nil {
			panic(call.panicValue)
		}
	}()
	call.policy, call.err = p.refresh(ctx, key)
}

func (p *RemoteProvider) refresh(ctx context.Context, key policy.PolicyKey) (policy.EffectivePolicy, error) {
	if err := ctx.Err(); err != nil {
		return policy.EffectivePolicy{}, err
	}
	// A previous fetch may have filled the cache before we became the leader.
	if pol, err, ok := p.cached(key); ok {
		return pol, err
	}

	pol, err := p.source.GetPolicy(ctx, key)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return policy.EffectivePolicy{}, ctxErr
	}
	if err != nil {
		if errors.Is(err, ErrPolicyNotFound) {
			p.cache.SetMissing(key, p.negativeCacheTTL)
			return policy.EffectivePolicy{}, ErrPolicyNotFound
		}
		if lkg, ok := p.cache.GetLKG(key); ok {
			lkg.Meta.Source = policy.PolicySourceLKG
			return lkg, nil
		}
		return policy.EffectivePolicy{}, err
	}

	// Ensure metadata is set
	pol.Key = key
	if pol.Meta.Source == "" {
		pol.Meta.Source = policy.PolicySourceRemote
	}

	// Normalize before caching
	normalized, err := pol.Normalize()
	if err != nil {
		// If normalization fails, treat as error (don't cache corrupt policy)
		return policy.EffectivePolicy{}, err
	}

	p.cache.SetWithLKG(key, normalized, p.cacheTTL, p.lkgTTL)
	return normalized, nil
}
