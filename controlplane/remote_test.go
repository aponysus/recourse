package controlplane

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aponysus/recourse/policy"
)

// MockSource is a Source for testing.
type MockSource struct {
	GetPolicyFunc func(ctx context.Context, key policy.PolicyKey) (policy.EffectivePolicy, error)
	Calls         int32
}

func (m *MockSource) GetPolicy(ctx context.Context, key policy.PolicyKey) (policy.EffectivePolicy, error) {
	atomic.AddInt32(&m.Calls, 1)
	if m.GetPolicyFunc != nil {
		return m.GetPolicyFunc(ctx, key)
	}
	return policy.EffectivePolicy{}, ErrPolicyNotFound
}

func TestRemoteProvider_CacheHit(t *testing.T) {
	key := policy.ParseKey("test.key")
	expected := policy.EffectivePolicy{
		Retry: policy.RetryPolicy{MaxAttempts: 5},
	}

	source := &MockSource{
		GetPolicyFunc: func(ctx context.Context, k policy.PolicyKey) (policy.EffectivePolicy, error) {
			return expected, nil
		},
	}

	provider := NewRemoteProvider(source, WithCacheTTL(1*time.Minute))

	// First call - should hit source
	pol, err := provider.GetEffectivePolicy(context.Background(), key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pol.Retry.MaxAttempts != 5 {
		t.Errorf("got MaxAttempts=%d, want 5", pol.Retry.MaxAttempts)
	}
	if atomic.LoadInt32(&source.Calls) != 1 {
		t.Errorf("expected 1 call to source, got %d", source.Calls)
	}

	// Second call - should hit cache
	pol, err = provider.GetEffectivePolicy(context.Background(), key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if atomic.LoadInt32(&source.Calls) != 1 {
		t.Errorf("expected 1 call to source (cached), got %d", source.Calls)
	}
}

func TestRemoteProvider_NegativeCache(t *testing.T) {
	key := policy.ParseKey("missing.key")
	source := &MockSource{
		GetPolicyFunc: func(ctx context.Context, k policy.PolicyKey) (policy.EffectivePolicy, error) {
			return policy.EffectivePolicy{}, ErrPolicyNotFound
		},
	}

	provider := NewRemoteProvider(source, WithNegativeCacheTTL(10*time.Minute))

	// First call - should hit source and return Not Found
	_, err := provider.GetEffectivePolicy(context.Background(), key)
	if !errors.Is(err, ErrPolicyNotFound) {
		t.Errorf("want ErrPolicyNotFound, got %v", err)
	}
	if atomic.LoadInt32(&source.Calls) != 1 {
		t.Errorf("expected 1 call to source, got %d", source.Calls)
	}

	// Second call - should hit negative cache
	_, err = provider.GetEffectivePolicy(context.Background(), key)
	if !errors.Is(err, ErrPolicyNotFound) {
		t.Errorf("want ErrPolicyNotFound (from cache), got %v", err)
	}
	if atomic.LoadInt32(&source.Calls) != 1 {
		t.Errorf("expected 1 call to source (cached negative), got %d", source.Calls)
	}
}

func TestRemoteProvider_Expiration(t *testing.T) {
	key := policy.ParseKey("expire.key")
	expected := policy.EffectivePolicy{}

	source := &MockSource{
		GetPolicyFunc: func(ctx context.Context, k policy.PolicyKey) (policy.EffectivePolicy, error) {
			return expected, nil
		},
	}

	// Very short TTL
	provider := NewRemoteProvider(source, WithCacheTTL(10*time.Millisecond))

	// First call
	_, _ = provider.GetEffectivePolicy(context.Background(), key)
	if atomic.LoadInt32(&source.Calls) != 1 {
		t.Errorf("expected 1 call, got %d", source.Calls)
	}

	// Wait for expiration
	time.Sleep(20 * time.Millisecond)

	// Second call - should expire and re-fetch
	_, _ = provider.GetEffectivePolicy(context.Background(), key)
	if atomic.LoadInt32(&source.Calls) != 2 {
		t.Errorf("expected 2 calls (expired), got %d", source.Calls)
	}
}

func TestRemoteProvider_FetchError_NotCached(t *testing.T) {
	key := policy.ParseKey("error.key")
	networkErr := errors.New("network error")

	source := &MockSource{
		GetPolicyFunc: func(ctx context.Context, k policy.PolicyKey) (policy.EffectivePolicy, error) {
			return policy.EffectivePolicy{}, networkErr
		},
	}

	provider := NewRemoteProvider(source)

	// First call - error
	_, err := provider.GetEffectivePolicy(context.Background(), key)
	if !errors.Is(err, networkErr) {
		t.Errorf("want network error, got %v", err)
	}

	// Second call - should retry immediately (not cached)
	_, err = provider.GetEffectivePolicy(context.Background(), key)
	if !errors.Is(err, networkErr) {
		t.Errorf("want network error, got %v", err)
	}
	if atomic.LoadInt32(&source.Calls) != 2 {
		t.Errorf("expected 2 calls (no cache on error), got %d", source.Calls)
	}
}
