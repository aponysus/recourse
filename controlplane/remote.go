package controlplane

import (
	"context"
	"errors"
	"time"

	"github.com/aponysus/recourse/policy"
)

// Source is the interface for fetching raw policy configuration.
type Source interface {
	// GetPolicy returns the policy for the given key.
	// If the policy is not found, it must return ErrPolicyNotFound.
	GetPolicy(ctx context.Context, key policy.PolicyKey) (policy.EffectivePolicy, error)
}

// RemoteProvider is a PolicyProvider that fetches policies from a Source and caches them.
type RemoteProvider struct {
	source           Source
	cache            *PolicyCache
	cacheTTL         time.Duration
	negativeCacheTTL time.Duration
}

// RemoteProviderOption configures a RemoteProvider.
type RemoteProviderOption func(*RemoteProvider)

// WithCacheTTL sets the TTL for successful policy lookups. Default is 1 minute.
func WithCacheTTL(ttl time.Duration) RemoteProviderOption {
	return func(p *RemoteProvider) {
		p.cacheTTL = ttl
	}
}

// WithNegativeCacheTTL sets the TTL for missing policy lookups. Default is 10 seconds.
func WithNegativeCacheTTL(ttl time.Duration) RemoteProviderOption {
	return func(p *RemoteProvider) {
		p.negativeCacheTTL = ttl
	}
}

// NewRemoteProvider creates a new RemoteProvider.
func NewRemoteProvider(source Source, opts ...RemoteProviderOption) *RemoteProvider {
	p := &RemoteProvider{
		source:           source,
		cache:            NewPolicyCache(),
		cacheTTL:         1 * time.Minute,
		negativeCacheTTL: 10 * time.Second,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// GetEffectivePolicy returns the policy for key, checking the cache first.
func (p *RemoteProvider) GetEffectivePolicy(ctx context.Context, key policy.PolicyKey) (policy.EffectivePolicy, error) {
	// 1. Check Cache
	pol, foundInCache, isNegative := p.cache.Get(key)
	if foundInCache {
		if isNegative {
			// Cached as missing. Return ErrPolicyNotFound so the Executor can handle it
			// according to MissingPolicyMode (e.g. FailureDeny, FailureAllow).
			return policy.EffectivePolicy{}, ErrPolicyNotFound
		}
		return pol, nil
	}

	// 2. Fetch from Source
	pol, err := p.source.GetPolicy(ctx, key)
	if err != nil {
		if errors.Is(err, ErrPolicyNotFound) {
			p.cache.SetMissing(key, p.negativeCacheTTL)
			return policy.EffectivePolicy{}, ErrPolicyNotFound
		}
		// Fetch error (network, etc) -> return error so executor can fallback (last-known-good not implemented yet here locally, but executor handles errors)
		return policy.EffectivePolicy{}, err
	}

	// 3. Cache and Return
	// Ensure metadata is set
	pol.Key = key
	if pol.Meta.Source == "" {
		pol.Meta.Source = "remote"
	}

	// Normalize before caching
	normalized, err := pol.Normalize()
	if err != nil {
		// If normalization fails, treat as error (don't cache corrupt policy)
		return policy.EffectivePolicy{}, err
	}

	p.cache.Set(key, normalized, p.cacheTTL)
	return normalized, nil
}
