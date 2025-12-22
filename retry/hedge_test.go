package retry

import (
	"context"
	"testing"
	"time"

	"github.com/aponysus/recourse/observe"
	"github.com/aponysus/recourse/policy"
)

func TestExecutor_Hedge_PrimaryWins(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping time-dependent test in short mode")
	}

	key := policy.ParseKey("test.hedge.primary")
	pol := policy.EffectivePolicy{
		Key: key,
		Retry: policy.RetryPolicy{
			MaxAttempts: 1,
		},
		Hedge: policy.HedgePolicy{
			Enabled:    true,
			MaxHedges:  1,
			HedgeDelay: 10 * time.Millisecond,
		},
	}
	exec := newTestExecutor(t, key, pol)
	// Use real sleep for this test since doRetryGroup uses real ticker
	exec.sleep = sleepWithContext
	exec.clock = time.Now

	ctx, capture := observe.RecordTimeline(context.Background())

	val, err := DoValue[string](ctx, exec, key, func(ctx context.Context) (string, error) {
		// Both Primary and Hedge will run this.
		// Primary starts at 0. Sleep 50ms. Finishes at 50ms.
		// Hedge starts at 10ms. Sleep 50ms. Finishes at 60ms.
		// Primary should win.
		time.Sleep(50 * time.Millisecond)
		return "ok", nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "ok" {
		t.Errorf("got %v, want ok", val)
	}

	tl := capture.Timeline()

	count := len(tl.Attempts)
	if count < 1 {
		t.Fatalf("expected at least 1 attempt")
	}
}

func TestExecutor_Hedge_HedgeWins(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping time-dependent test in short mode")
	}

	key := policy.ParseKey("test.hedge.secondary")
	pol := policy.EffectivePolicy{
		Key: key,
		Retry: policy.RetryPolicy{
			MaxAttempts: 1,
		},
		Hedge: policy.HedgePolicy{
			Enabled:    true,
			MaxHedges:  1,
			HedgeDelay: 10 * time.Millisecond,
		},
	}
	exec := newTestExecutor(t, key, pol)
	exec.sleep = sleepWithContext
	exec.clock = time.Now

	ctx, capture := observe.RecordTimeline(context.Background())
	primaryDone := make(chan struct{})

	val, err := DoValue[string](ctx, exec, key, func(ctx context.Context) (string, error) {
		info, _ := observe.AttemptFromContext(ctx)
		if !info.IsHedge {
			// Primary: Sleep and wait for cancel
			select {
			case <-time.After(200 * time.Millisecond):
			case <-ctx.Done():
			}
			close(primaryDone)
			return "primary", ctx.Err()
		}
		// Hedge: Return fast
		return "hedge", nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "hedge" {
		t.Errorf("got %v, want hedge", val)
	}

	// Must wait for primary to finish recording
	<-primaryDone
	// Small buffer for mutex/recording
	time.Sleep(10 * time.Millisecond)

	tl := capture.Timeline()
	// Should show at least Hedge attempting.
	// Primary attempt might not be recorded if it finishes after return (due to async cancel).
	if len(tl.Attempts) < 1 {
		t.Errorf("expected at least 1 attempt, got %d", len(tl.Attempts))
	}

	// Verify one is hedge
	hasHedge := false
	for _, a := range tl.Attempts {
		if a.IsHedge {
			hasHedge = true
		}
	}
	if !hasHedge {
		t.Error("expected at least one hedge attempt")
	}
}

func TestExecutor_Hedge_RetryAndHedge(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping time-dependent test in short mode")
	}

	key := policy.ParseKey("test.hedge.retry")
	pol := policy.EffectivePolicy{
		Key: key,
		Retry: policy.RetryPolicy{
			MaxAttempts:    2,
			InitialBackoff: 1 * time.Millisecond,
		},
		Hedge: policy.HedgePolicy{
			Enabled:    true,
			MaxHedges:  1,
			HedgeDelay: 20 * time.Millisecond,
		},
	}
	exec := newTestExecutor(t, key, pol)
	exec.sleep = sleepWithContext
	exec.clock = time.Now

	ctx, capture := observe.RecordTimeline(context.Background())

	_, err := DoValue[string](ctx, exec, key, func(ctx context.Context) (string, error) {
		time.Sleep(50 * time.Millisecond) // Slow enough to trigger hedge (20ms)
		return "", context.DeadlineExceeded
	})

	if err == nil {
		t.Fatal("expected error")
	}

	tl := capture.Timeline()
	// Retry 0: Primary + Hedge.
	// Retry 1: Primary + Hedge.
	// Total 4.
	// We use larger delays (50ms vs 20ms) to ensure robustness.
	if len(tl.Attempts) < 3 {
		t.Errorf("expected at least 3-4 attempts, got %d", len(tl.Attempts))
	}
}
