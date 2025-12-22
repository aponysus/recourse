package retry

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aponysus/recourse/controlplane"
	"github.com/aponysus/recourse/policy"
)

// MockSource is a copy of the mock from controlplane, but we can't import internal test helpers.
// So we define a local one or make the other one exported.
// For now, defining local simple mock.
type MockSource struct {
	GetPolicyFunc func(ctx context.Context, key policy.PolicyKey) (policy.EffectivePolicy, error)
	Calls         int32
}

func (m *MockSource) GetPolicy(ctx context.Context, key policy.PolicyKey) (policy.EffectivePolicy, error) {
	atomic.AddInt32(&m.Calls, 1)
	if m.GetPolicyFunc != nil {
		return m.GetPolicyFunc(ctx, key)
	}
	return policy.EffectivePolicy{}, controlplane.ErrPolicyNotFound
}

func TestExecutor_RemoteProvider_Integration(t *testing.T) {
	key := policy.ParseKey("remote.integration")
	expected := policy.EffectivePolicy{
		Retry: policy.RetryPolicy{MaxAttempts: 5},
	}

	source := &MockSource{
		GetPolicyFunc: func(ctx context.Context, k policy.PolicyKey) (policy.EffectivePolicy, error) {
			return expected, nil
		},
	}

	// Create RemoteProvider
	provider := controlplane.NewRemoteProvider(source, controlplane.WithCacheTTL(100*time.Millisecond))

	// Create Executor using RemoteProvider
	exec := NewExecutor(WithProvider(provider), WithMissingPolicyMode(FailureDeny))

	// 1. First call - should trigger fetch
	opCalls := 0
	op := func(ctx context.Context) error {
		opCalls++
		return nil
	}

	err := exec.Do(context.Background(), key, op)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opCalls != 1 {
		t.Errorf("expected 1 op call, got %d", opCalls)
	}
	if atomic.LoadInt32(&source.Calls) != 1 {
		t.Errorf("expected 1 source call, got %d", source.Calls)
	}

	// 2. Second call - should hit cache
	opCalls = 0
	err = exec.Do(context.Background(), key, op)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if atomic.LoadInt32(&source.Calls) != 1 {
		t.Errorf("expected 1 source call (cached), got %d", source.Calls)
	}

	// 3. Wait for expiration
	time.Sleep(150 * time.Millisecond)

	// 4. Third call - should fetch again
	err = exec.Do(context.Background(), key, op)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if atomic.LoadInt32(&source.Calls) != 2 {
		t.Errorf("expected 2 source calls (expired), got %d", source.Calls)
	}
}

func TestExecutor_RemoteProvider_NegativeLink(t *testing.T) {
	key := policy.ParseKey("remote.missing")
	source := &MockSource{
		GetPolicyFunc: func(ctx context.Context, k policy.PolicyKey) (policy.EffectivePolicy, error) {
			return policy.EffectivePolicy{}, controlplane.ErrPolicyNotFound
		},
	}

	provider := controlplane.NewRemoteProvider(source, controlplane.WithNegativeCacheTTL(100*time.Millisecond))

	// Create Executor
	exec := NewExecutor(WithProvider(provider), WithMissingPolicyMode(FailureDeny))

	// 1. First call - should fail (denied) and cache missing
	err := exec.Do(context.Background(), key, func(ctx context.Context) error { return nil })
	if err == nil {
		t.Error("expected error, got nil")
	} else if !errors.Is(err, ErrNoPolicy) {
		// Executor wraps the error in NoPolicyError, which wraps the underlying error.
		// ErrNoPolicy is the generic sentinel? Or NoPolicyError.
		// Executor returns &NoPolicyError{Key: key, Err: err}.
		// check if As
		var npe *NoPolicyError
		if errors.As(err, &npe) {
			if !errors.Is(npe.Err, controlplane.ErrPolicyNotFound) {
				t.Errorf("expected underlying ErrPolicyNotFound, got %v", npe.Err)
			}
		} else {
			t.Errorf("expected NoPolicyError, got %T: %v", err, err)
		}
	}

	if atomic.LoadInt32(&source.Calls) != 1 {
		t.Errorf("expected 1 source call, got %d", source.Calls)
	}

	// 2. Second call - should fail fast from cache
	err = exec.Do(context.Background(), key, func(ctx context.Context) error { return nil })
	if err == nil {
		t.Error("expected error")
	}
	if atomic.LoadInt32(&source.Calls) != 1 {
		t.Errorf("expected 1 source call (cached negative), got %d", source.Calls)
	}
}
