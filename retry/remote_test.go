package retry

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aponysus/recourse/controlplane"
	"github.com/aponysus/recourse/observe"
	"github.com/aponysus/recourse/policy"
)

// Local mock since controlplane test helpers are internal.
type MockSource struct {
	GetPolicyFunc func(ctx context.Context, key policy.PolicyKey) (policy.EffectivePolicy, error)
	Calls         int32
}

func TestExecutor_RemoteProvider_LKGWithFailureDeny(t *testing.T) {
	for _, timeline := range []bool{false, true} {
		name := "fast"
		if timeline {
			name = "timeline"
		}
		t.Run(name, func(t *testing.T) {
			key := policy.ParseKey("remote.lkg")
			var offline bool
			source := &MockSource{GetPolicyFunc: func(context.Context, policy.PolicyKey) (policy.EffectivePolicy, error) {
				if offline {
					return policy.EffectivePolicy{}, controlplane.ErrProviderUnavailable
				}
				return policy.EffectivePolicy{ID: "remote-v1", Retry: policy.RetryPolicy{MaxAttempts: 1}}, nil
			}}
			provider := controlplane.NewRemoteProvider(source, controlplane.WithCacheTTL(0), controlplane.WithLKGTTL(time.Minute))
			if _, err := provider.GetEffectivePolicy(context.Background(), key); err != nil {
				t.Fatal(err)
			}
			offline = true
			exec := NewExecutor(WithProvider(provider)) // Default MissingPolicyMode is FailureDeny.
			ctx := context.Background()
			var capture *observe.TimelineCapture
			if timeline {
				ctx, capture = observe.RecordTimeline(ctx)
			}
			calls := 0
			err := exec.Do(ctx, key, func(context.Context) error {
				calls++
				return nil
			})
			if err != nil || calls != 1 {
				t.Fatalf("LKG should execute under FailureDeny, calls=%d, err=%v", calls, err)
			}
			if timeline {
				tl := capture.Timeline()
				if tl == nil || tl.PolicyID != "remote-v1" || tl.Attributes["policy_source"] != "lkg" || tl.Attributes["policy_mode"] != "standard" {
					t.Fatalf("expected LKG resolution metadata, got %+v", tl)
				}
			}
		})
	}
}

func TestExecutor_RemoteProvider_RecoverSourcePanic(t *testing.T) {
	key := policy.ParseKey("remote.panic")
	source := &MockSource{GetPolicyFunc: func(context.Context, policy.PolicyKey) (policy.EffectivePolicy, error) {
		panic("source panic")
	}}
	exec := NewExecutorFromOptions(ExecutorOptions{
		Provider:      controlplane.NewRemoteProvider(source),
		RecoverPanics: true,
	})
	err := exec.Do(context.Background(), key, func(context.Context) error {
		t.Fatal("operation must not execute after a source panic")
		return nil
	})
	var panicErr *PanicError
	if !errors.As(err, &panicErr) || panicErr.Component != "policy_provider" {
		t.Fatalf("expected recoverable provider panic, got %v", err)
	}
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
		// Expect NoPolicyError wrapping ErrPolicyNotFound.
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
