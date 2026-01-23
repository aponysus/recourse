package policy

import (
	"testing"
	"time"
)

func TestEffectivePolicyNormalize_DefaultsAndBounds(t *testing.T) {
	p := EffectivePolicy{
		Retry: RetryPolicy{
			MaxAttempts:       0,
			InitialBackoff:    -1,
			MaxBackoff:        0,
			BackoffMultiplier: 0,
			Jitter:            "",
			TimeoutPerAttempt: -1,
			OverallTimeout:    -2,
			Budget:            BudgetRef{Cost: 0},
		},
		Hedge: HedgePolicy{
			Budget: BudgetRef{Cost: 0},
		},
	}

	normalized, err := p.Normalize()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if normalized.Retry.MaxAttempts != 3 {
		t.Fatalf("maxAttempts=%d, want 3", normalized.Retry.MaxAttempts)
	}
	if normalized.Retry.InitialBackoff != 10*time.Millisecond {
		t.Fatalf("initialBackoff=%v, want 10ms", normalized.Retry.InitialBackoff)
	}
	if normalized.Retry.MaxBackoff != 250*time.Millisecond {
		t.Fatalf("maxBackoff=%v, want 250ms", normalized.Retry.MaxBackoff)
	}
	if normalized.Retry.BackoffMultiplier != 2 {
		t.Fatalf("backoffMultiplier=%v, want 2", normalized.Retry.BackoffMultiplier)
	}
	if normalized.Retry.Jitter != JitterNone {
		t.Fatalf("jitter=%v, want %v", normalized.Retry.Jitter, JitterNone)
	}
	if normalized.Retry.TimeoutPerAttempt != 0 || normalized.Retry.OverallTimeout != 0 {
		t.Fatalf("timeouts=%v/%v, want 0/0", normalized.Retry.TimeoutPerAttempt, normalized.Retry.OverallTimeout)
	}
	if normalized.Retry.Budget.Cost != 1 {
		t.Fatalf("retry budget cost=%d, want 1", normalized.Retry.Budget.Cost)
	}
	if normalized.Hedge.Budget.Cost != 1 {
		t.Fatalf("hedge budget cost=%d, want 1", normalized.Hedge.Budget.Cost)
	}
	if !normalized.Meta.Normalization.Changed {
		t.Fatalf("expected normalization to mark changes")
	}
}

func TestEffectivePolicyNormalize_InvalidJitter(t *testing.T) {
	p := EffectivePolicy{
		Retry: RetryPolicy{
			MaxAttempts:       1,
			InitialBackoff:    time.Millisecond,
			MaxBackoff:        time.Millisecond,
			BackoffMultiplier: 1,
			Jitter:            JitterKind("bogus"),
		},
	}

	normalized, err := p.Normalize()
	if err == nil {
		t.Fatal("expected error")
	}
	if _, ok := err.(*NormalizeError); !ok {
		t.Fatalf("expected NormalizeError, got %T", err)
	}
	if normalized.Key != (PolicyKey{}) ||
		normalized.ID != "" ||
		normalized.Retry != (RetryPolicy{}) ||
		normalized.Hedge != (HedgePolicy{}) ||
		normalized.Circuit != (CircuitPolicy{}) ||
		normalized.Meta.Source != "" ||
		normalized.Meta.Normalization.Changed ||
		len(normalized.Meta.Normalization.ChangedFields) != 0 {
		t.Fatalf("expected zero policy on error, got %+v", normalized)
	}
}

func TestEffectivePolicyNormalize_HedgeAndCircuit(t *testing.T) {
	p := EffectivePolicy{
		Retry: RetryPolicy{
			MaxAttempts:       1,
			InitialBackoff:    10 * time.Millisecond,
			MaxBackoff:        20 * time.Millisecond,
			BackoffMultiplier: 2,
			Jitter:            JitterNone,
		},
		Hedge: HedgePolicy{
			Enabled:    true,
			MaxHedges:  0,
			HedgeDelay: 1 * time.Millisecond,
			Budget:     BudgetRef{Cost: 0},
		},
		Circuit: CircuitPolicy{
			Enabled:   true,
			Threshold: 0,
			Cooldown:  0,
		},
	}

	normalized, err := p.Normalize()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if normalized.Hedge.MaxHedges != 2 {
		t.Fatalf("maxHedges=%d, want 2", normalized.Hedge.MaxHedges)
	}
	if normalized.Hedge.HedgeDelay != 10*time.Millisecond {
		t.Fatalf("hedgeDelay=%v, want 10ms", normalized.Hedge.HedgeDelay)
	}
	if normalized.Hedge.Budget.Cost != 1 {
		t.Fatalf("hedge budget cost=%d, want 1", normalized.Hedge.Budget.Cost)
	}
	if normalized.Circuit.Threshold != 5 {
		t.Fatalf("threshold=%d, want 5", normalized.Circuit.Threshold)
	}
	if normalized.Circuit.Cooldown != 10*time.Second {
		t.Fatalf("cooldown=%v, want 10s", normalized.Circuit.Cooldown)
	}
	if !normalized.Meta.Normalization.Changed {
		t.Fatalf("expected normalization to mark changes")
	}
}

func TestEffectivePolicyNormalize_ClampsRetryAndTimeouts(t *testing.T) {
	p := EffectivePolicy{
		Retry: RetryPolicy{
			MaxAttempts:       -2,
			InitialBackoff:    500 * time.Microsecond,
			MaxBackoff:        2 * time.Minute,
			BackoffMultiplier: 0.5,
			Jitter:            JitterFull,
			TimeoutPerAttempt: 500 * time.Microsecond,
			OverallTimeout:    500 * time.Microsecond,
			Budget:            BudgetRef{Cost: -3},
		},
		Hedge: HedgePolicy{
			Enabled:    true,
			MaxHedges:  10,
			HedgeDelay: 5 * time.Millisecond,
			Budget:     BudgetRef{Cost: -2},
		},
		Circuit: CircuitPolicy{
			Enabled:   true,
			Threshold: 2,
			Cooldown:  50 * time.Millisecond,
		},
	}

	normalized, err := p.Normalize()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if normalized.Retry.MaxAttempts != 1 {
		t.Fatalf("maxAttempts=%d, want 1", normalized.Retry.MaxAttempts)
	}
	if normalized.Retry.InitialBackoff != 1*time.Millisecond {
		t.Fatalf("initialBackoff=%v, want 1ms", normalized.Retry.InitialBackoff)
	}
	if normalized.Retry.MaxBackoff != 30*time.Second {
		t.Fatalf("maxBackoff=%v, want 30s", normalized.Retry.MaxBackoff)
	}
	if normalized.Retry.BackoffMultiplier != 1 {
		t.Fatalf("multiplier=%v, want 1", normalized.Retry.BackoffMultiplier)
	}
	if normalized.Retry.TimeoutPerAttempt != 1*time.Millisecond {
		t.Fatalf("timeoutPerAttempt=%v, want 1ms", normalized.Retry.TimeoutPerAttempt)
	}
	if normalized.Retry.OverallTimeout != 1*time.Millisecond {
		t.Fatalf("overallTimeout=%v, want 1ms", normalized.Retry.OverallTimeout)
	}
	if normalized.Retry.Budget.Cost != 1 {
		t.Fatalf("retry budget cost=%d, want 1", normalized.Retry.Budget.Cost)
	}

	if normalized.Hedge.MaxHedges != 3 {
		t.Fatalf("maxHedges=%d, want 3", normalized.Hedge.MaxHedges)
	}
	if normalized.Hedge.HedgeDelay != 10*time.Millisecond {
		t.Fatalf("hedgeDelay=%v, want 10ms", normalized.Hedge.HedgeDelay)
	}
	if normalized.Hedge.Budget.Cost != 1 {
		t.Fatalf("hedge budget cost=%d, want 1", normalized.Hedge.Budget.Cost)
	}

	if normalized.Circuit.Cooldown != 100*time.Millisecond {
		t.Fatalf("cooldown=%v, want 100ms", normalized.Circuit.Cooldown)
	}
}

func TestEffectivePolicyNormalize_CapsMaxAttemptsAndBackoff(t *testing.T) {
	p := EffectivePolicy{
		Retry: RetryPolicy{
			MaxAttempts:       100,
			InitialBackoff:    200 * time.Millisecond,
			MaxBackoff:        100 * time.Millisecond,
			BackoffMultiplier: 100,
			Jitter:            JitterNone,
		},
	}

	normalized, err := p.Normalize()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if normalized.Retry.MaxAttempts != 10 {
		t.Fatalf("maxAttempts=%d, want 10", normalized.Retry.MaxAttempts)
	}
	if normalized.Retry.MaxBackoff != 200*time.Millisecond {
		t.Fatalf("maxBackoff=%v, want 200ms", normalized.Retry.MaxBackoff)
	}
	if normalized.Retry.BackoffMultiplier != 10 {
		t.Fatalf("multiplier=%v, want 10", normalized.Retry.BackoffMultiplier)
	}
}
