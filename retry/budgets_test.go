package retry

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aponysus/recourse/budget"
	"github.com/aponysus/recourse/controlplane"
	"github.com/aponysus/recourse/policy"
)

type denySecondAttemptBudget struct{}

func (denySecondAttemptBudget) AllowAttempt(_ context.Context, _ policy.PolicyKey, attemptIdx int, _ budget.AttemptKind, _ policy.BudgetRef) budget.Decision {
	if attemptIdx == 1 {
		return budget.Decision{Allowed: false, Reason: budget.ReasonBudgetDenied}
	}
	return budget.Decision{Allowed: true, Reason: budget.ReasonAllowed}
}

type countingReleaseBudget struct {
	allowCalls int32
	releases   int32
}

func (b *countingReleaseBudget) AllowAttempt(_ context.Context, _ policy.PolicyKey, _ int, _ budget.AttemptKind, _ policy.BudgetRef) budget.Decision {
	atomic.AddInt32(&b.allowCalls, 1)
	return budget.Decision{
		Allowed: true,
		Reason:  budget.ReasonAllowed,
		Release: func() {
			atomic.AddInt32(&b.releases, 1)
		},
	}
}

func TestExecutor_BudgetDeniesSecondAttempt_StopsRetryAndReturnsLastErr(t *testing.T) {
	key := policy.PolicyKey{Name: "x"}

	budgets := budget.NewRegistry()
	budgets.Register("b", denySecondAttemptBudget{})

	exec := NewExecutor(ExecutorOptions{
		Budgets: budgets,
		Provider: &controlplane.StaticProvider{
			Policies: map[policy.PolicyKey]policy.EffectivePolicy{
				key: {
					Key: key,
					Retry: policy.RetryPolicy{
						MaxAttempts: 3,
						Budget:      policy.BudgetRef{Name: "b", Cost: 1},
					},
				},
			},
		},
	})
	exec.sleep = func(context.Context, time.Duration) error { return nil }

	calls := 0
	opErr := errors.New("first attempt error")
	_, tl, err := DoValueWithTimeline[int](context.Background(), exec, key, func(context.Context) (int, error) {
		calls++
		return 0, opErr
	})

	if calls != 1 {
		t.Fatalf("calls=%d, want 1", calls)
	}
	if err != opErr {
		t.Fatalf("err=%v, want %v", err, opErr)
	}
	if tl.FinalErr != opErr {
		t.Fatalf("tl.FinalErr=%v, want %v", tl.FinalErr, opErr)
	}
	if len(tl.Attempts) != 2 {
		t.Fatalf("attempts=%d, want 2", len(tl.Attempts))
	}
	if tl.Attempts[1].BudgetAllowed {
		t.Fatalf("attempt[1].BudgetAllowed=true, want false")
	}
	if tl.Attempts[1].BudgetReason != budget.ReasonBudgetDenied {
		t.Fatalf("attempt[1].BudgetReason=%q, want %q", tl.Attempts[1].BudgetReason, budget.ReasonBudgetDenied)
	}
	if tl.Attempts[1].Err != nil {
		t.Fatalf("attempt[1].Err=%v, want nil (op not called)", tl.Attempts[1].Err)
	}
}

func TestExecutor_MissingBudgetName_DeniesAttemptsByDefault(t *testing.T) {
	key := policy.PolicyKey{Name: "x"}

	exec := NewExecutor(ExecutorOptions{
		Budgets: budget.NewRegistry(), // empty registry
		Provider: &controlplane.StaticProvider{
			Policies: map[policy.PolicyKey]policy.EffectivePolicy{
				key: {
					Key: key,
					Retry: policy.RetryPolicy{
						MaxAttempts: 2,
						Budget:      policy.BudgetRef{Name: "missing", Cost: 1},
					},
				},
			},
		},
	})
	exec.sleep = func(context.Context, time.Duration) error { return nil }

	calls := 0
	_, tl, err := DoValueWithTimeline[int](context.Background(), exec, key, func(context.Context) (int, error) {
		calls++
		return 0, errors.New("nope")
	})

	// Should fail immediately with budget error, 0 calls
	if err == nil {
		t.Fatalf("expected error")
	}
	if calls != 0 {
		t.Fatalf("calls=%d, want 0", calls)
	}
	if len(tl.Attempts) != 1 {
		// Even if denied immediately, we record an "attempt" (or rather a timeline event for the denial)?
		// Wait, allowAttempt returns false.
		// In doValueFast/doValueWithTimeline:
		// decision, ok := allowAttempt(...)
		// if !ok { return zero, terminalError(...) }

		// Wait, if !ok, we do append an attempt record in doValueWithTimeline!
		// But in doValueFast we return immediately.
		// Detailed check:
		// doValueWithTimeline line 388: if !ok { ... tl.Attempts = append(...) ... return ... }
		// So we expect 1 attempt record (the denial).
		t.Fatalf("attempts=%d, want 1", len(tl.Attempts))
	}

	rec := tl.Attempts[0]
	if rec.BudgetAllowed {
		t.Fatalf("BudgetAllowed=true, want false")
	}
	if rec.BudgetReason != budget.ReasonBudgetNotFound {
		t.Fatalf("BudgetReason=%q, want %q", rec.BudgetReason, budget.ReasonBudgetNotFound)
	}
}

func TestExecutor_MissingBudgetName_AllowsAttemptsWithUnsafeOptIn(t *testing.T) {
	key := policy.PolicyKey{Name: "x"}

	exec := NewExecutor(ExecutorOptions{
		Budgets:           budget.NewRegistry(),
		MissingBudgetMode: FailureAllowUnsafe,
		Provider: &controlplane.StaticProvider{
			Policies: map[policy.PolicyKey]policy.EffectivePolicy{
				key: {
					Key: key,
					Retry: policy.RetryPolicy{
						MaxAttempts: 2,
						Budget:      policy.BudgetRef{Name: "missing", Cost: 1},
					},
				},
			},
		},
	})
	exec.sleep = func(context.Context, time.Duration) error { return nil }

	calls := 0
	_, tl, err := DoValueWithTimeline[int](context.Background(), exec, key, func(context.Context) (int, error) {
		calls++
		return 0, errors.New("nope")
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if calls != 2 {
		t.Fatalf("calls=%d, want 2", calls)
	}
	if len(tl.Attempts) != 2 {
		t.Fatalf("attempts=%d, want 2", len(tl.Attempts))
	}
	for i, rec := range tl.Attempts {
		if !rec.BudgetAllowed {
			t.Fatalf("attempt[%d].BudgetAllowed=false, want true", i)
		}
		// In UnsafeAllow, reason should still be "budget_not_found" but Allowed=true
		if rec.BudgetReason != budget.ReasonBudgetNotFound {
			t.Fatalf("attempt[%d].BudgetReason=%q, want %q", i, rec.BudgetReason, budget.ReasonBudgetNotFound)
		}
	}
}

func TestExecutor_BudgetRelease_CalledOncePerAllowedAttempt(t *testing.T) {
	key := policy.PolicyKey{Name: "x"}

	cb := &countingReleaseBudget{}
	budgets := budget.NewRegistry()
	budgets.Register("b", cb)

	exec := NewExecutor(ExecutorOptions{
		Budgets: budgets,
		Provider: &controlplane.StaticProvider{
			Policies: map[policy.PolicyKey]policy.EffectivePolicy{
				key: {
					Key: key,
					Retry: policy.RetryPolicy{
						MaxAttempts: 2,
						Budget:      policy.BudgetRef{Name: "b", Cost: 1},
					},
				},
			},
		},
	})
	exec.sleep = func(context.Context, time.Duration) error { return nil }

	calls := 0
	_, _, err := DoValueWithTimeline[int](context.Background(), exec, key, func(context.Context) (int, error) {
		calls++
		if calls == 1 {
			return 0, errors.New("nope")
		}
		return 42, nil
	})
	if err != nil {
		t.Fatalf("err=%v, want nil", err)
	}
	if calls != 2 {
		t.Fatalf("calls=%d, want 2", calls)
	}
	if got, want := atomic.LoadInt32(&cb.allowCalls), int32(2); got != want {
		t.Fatalf("allowCalls=%d, want %d", got, want)
	}
	if got, want := atomic.LoadInt32(&cb.releases), int32(2); got != want {
		t.Fatalf("releases=%d, want %d", got, want)
	}
}

func TestExecutor_AllowAttempt_ReleaseIsIdempotent(t *testing.T) {
	key := policy.PolicyKey{Name: "x"}
	cb := &countingReleaseBudget{}
	budgets := budget.NewRegistry()
	budgets.MustRegister("b", cb)

	exec := NewExecutor(ExecutorOptions{
		Budgets: budgets,
	})

	// Call allowAttempt directly
	d, allowed := exec.allowAttempt(context.Background(), key, policy.BudgetRef{Name: "b", Cost: 1}, 0, budget.KindRetry)
	if !allowed {
		t.Fatal("expected allowed")
	}
	if d.Release == nil {
		t.Fatal("expected release function")
	}

	// Call release multiple times
	d.Release()
	d.Release()
	d.Release()

	if releases := atomic.LoadInt32(&cb.releases); releases != 1 {
		t.Fatalf("releases=%d, want 1", releases)
	}
}
