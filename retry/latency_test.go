package retry

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aponysus/recourse/observe"

	"github.com/aponysus/recourse/controlplane"
	"github.com/aponysus/recourse/hedge"
	"github.com/aponysus/recourse/policy"
)

func TestExecutor_DynamicHedging_P50(t *testing.T) {
	key := policy.PolicyKey{Name: "dynamic_hedge"}

	// 1. Setup Trigger Registry
	registry := hedge.NewRegistry()
	registry.Register("p50_trigger", hedge.LatencyTrigger{Percentile: "p50"})

	// 2. Setup Executor
	exec := NewExecutorFromOptions(ExecutorOptions{
		Triggers: registry,
		Provider: &controlplane.StaticProvider{
			Policies: map[policy.PolicyKey]policy.EffectivePolicy{
				key: {
					Key: key,
					Retry: policy.RetryPolicy{
						MaxAttempts: 1, // Only 1 primary allowed per retry
					},
					Hedge: policy.HedgePolicy{
						Enabled:     true,
						MaxHedges:   1, // Allow 1 hedge
						TriggerName: "p50_trigger",
					},
				},
			},
		},
	})

	// Override clock/sleep for deterministic testing?
	// Real-time testing is easier for stats if we inject delays.
	// But flaky.
	// Let's use real time but large enough margins.

	// 3. Prime the tracker with "fast" calls (e.g., 5ms)
	// We need to execute calls that finish quickly.
	for i := 0; i < 20; i++ {
		_, err := DoValue[int](context.Background(), exec, key, func(ctx context.Context) (int, error) {
			time.Sleep(5 * time.Millisecond)
			return i, nil
		})
		if err != nil {
			t.Fatalf("prime failed: %v", err)
		}
	}

	// Tracker should now have P50 ~ 5ms.

	// 4. Do a "slow" call (e.g., 50ms)
	// The trigger "p50" should fire around 5ms+epsilon.
	// Primary: Sleep 50ms.
	// Hedge: Sleep 1ms (wins).

	var primaryCalls int32
	var hedgeCalls int32

	val, err := DoValue[int](context.Background(), exec, key, func(ctx context.Context) (int, error) {
		// Identify attempt
		// IsHedge only available via context?
		// We can use closure state?
		// AttemptInfo is injected.
		/*
			info := observe.AttemptFromContext(ctx) // helper?
			// But helpers are internal?
			// We use side effects?
		*/

		// Simple approach: atomic counter with sleep.
		// Since we don't know for sure which is primary vs hedge without context,
		// we rely on timing.

		// If prompt return -> it's the hedge.
		// If long delay -> it's the primary.

		info, ok := observe.AttemptFromContext(ctx)
		if !ok {
			return 0, context.DeadlineExceeded // Should indicate error
		}

		if info.IsHedge {
			atomic.AddInt32(&hedgeCalls, 1)
			return 100, nil // Fast return
		}

		// Primary
		atomic.AddInt32(&primaryCalls, 1)

		// Wait 50ms.
		select {
		case <-ctx.Done():
			// Cancelled
			return 0, ctx.Err()
		case <-time.After(50 * time.Millisecond):
			return 200, nil
		}
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// We expect the result to be 100 (from hedge) because it finishes first.
	// Primary takes 50ms. Hedge takes 0ms (immediate).
	// Hedge Trigger P50 is ~5ms.
	// Primary starts T=0.
	// T=5ms -> Hedge spawns.
	// T=5ms+epsilon -> Hedge returns 100.
	// Group returns 100.
	// Primary is cancelled.

	if val != 100 {
		t.Errorf("val=%d, want 100 (from hedge)", val)
	}

	p := atomic.LoadInt32(&primaryCalls)
	h := atomic.LoadInt32(&hedgeCalls)

	if p != 1 {
		t.Errorf("primaryCalls=%d, want 1", p)
	}
	if h != 1 {
		t.Errorf("hedgeCalls=%d, want 1", h)
	}
}
