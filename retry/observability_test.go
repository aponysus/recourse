package retry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aponysus/recourse/budget"
	"github.com/aponysus/recourse/controlplane"
	"github.com/aponysus/recourse/observe"
	"github.com/aponysus/recourse/policy"
)

func TestDoValueWithTimeline_ObserverCallbacks_Success(t *testing.T) {
	key := policy.PolicyKey{Name: "x"}
	obs := &testObserver{}
	exec := NewExecutor(ExecutorOptions{
		Provider: &controlplane.StaticProvider{
			Policies: map[policy.PolicyKey]policy.EffectivePolicy{
				key: {
					Key: key,
					Retry: policy.RetryPolicy{
						MaxAttempts: 3,
						Jitter:      policy.JitterNone,
					},
				},
			},
		},
		Observer: obs,
	})
	exec.sleep = func(context.Context, time.Duration) error { return nil }

	calls := 0
	val, tl, err := DoValueWithTimeline[int](context.Background(), exec, key, func(ctx context.Context) (int, error) {
		calls++
		if calls < 3 {
			return 0, errors.New("nope")
		}
		return 42, nil
	})
	if err != nil {
		t.Fatalf("err=%v, want nil", err)
	}
	if val != 42 {
		t.Fatalf("val=%d, want 42", val)
	}

	if obs.starts != 1 {
		t.Fatalf("starts=%d, want 1", obs.starts)
	}
	if obs.successes != 1 || obs.failures != 0 {
		t.Fatalf("successes=%d failures=%d, want 1/0", obs.successes, obs.failures)
	}
	if len(obs.attempts) != 3 {
		t.Fatalf("attempts=%d, want 3", len(obs.attempts))
	}

	for i, rec := range obs.attempts {
		if rec.Attempt != i {
			t.Fatalf("attempt[%d].Attempt=%d, want %d", i, rec.Attempt, i)
		}
	}

	if len(tl.Attempts) != len(obs.attempts) {
		t.Fatalf("timeline attempts=%d observer attempts=%d", len(tl.Attempts), len(obs.attempts))
	}
	if tl.FinalErr != nil {
		t.Fatalf("tl.FinalErr=%v, want nil", tl.FinalErr)
	}
	if tl.Key != key {
		t.Fatalf("tl.Key=%v, want %v", tl.Key, key)
	}
	if obs.lastSuccess.Key != key {
		t.Fatalf("observer tl.Key=%v, want %v", obs.lastSuccess.Key, key)
	}
	if len(obs.attemptInfos) != 3 {
		t.Fatalf("attemptInfos=%d, want 3", len(obs.attemptInfos))
	}
	for i, info := range obs.attemptInfos {
		if info.Attempt != i || info.RetryIndex != i {
			t.Fatalf("attemptInfos[%d]=%+v, want Attempt/RetryIndex=%d", i, info, i)
		}
	}
}

func TestDoValue_ObserverEnabled_CallsObserver(t *testing.T) {
	key := policy.PolicyKey{Name: "x"}
	obs := &testObserver{}
	exec := NewExecutor(ExecutorOptions{
		Provider: &controlplane.StaticProvider{
			Policies: map[policy.PolicyKey]policy.EffectivePolicy{
				key: {
					Key: key,
					Retry: policy.RetryPolicy{
						MaxAttempts: 2,
					},
				},
			},
		},
		Observer: obs,
	})
	exec.sleep = func(context.Context, time.Duration) error { return nil }

	calls := 0
	_, err := DoValue[int](context.Background(), exec, key, func(context.Context) (int, error) {
		calls++
		return 0, errors.New("nope")
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if calls != 2 {
		t.Fatalf("calls=%d, want 2", calls)
	}
	if obs.starts != 1 {
		t.Fatalf("starts=%d, want 1", obs.starts)
	}
	if obs.successes != 0 || obs.failures != 1 {
		t.Fatalf("successes=%d failures=%d, want 0/1", obs.successes, obs.failures)
	}
	if len(obs.attempts) != 2 {
		t.Fatalf("attempts=%d, want 2", len(obs.attempts))
	}
	if obs.lastFailure.FinalErr == nil {
		t.Fatalf("expected FinalErr")
	}
}

func TestDoValue_FastPath_AttemptInfoInContext(t *testing.T) {
	key := policy.PolicyKey{Name: "x"}
	exec := NewExecutor(ExecutorOptions{
		Provider: &controlplane.StaticProvider{
			Policies: map[policy.PolicyKey]policy.EffectivePolicy{
				key: {
					Key: key,
					Retry: policy.RetryPolicy{
						MaxAttempts: 1,
					},
				},
			},
		},
		Observer: observe.NoopObserver{},
	})

	_, err := DoValue[int](context.Background(), exec, key, func(ctx context.Context) (int, error) {
		info, ok := observe.AttemptFromContext(ctx)
		if !ok {
			t.Fatalf("expected AttemptInfo in context")
		}
		if info.Attempt != 0 || info.RetryIndex != 0 {
			t.Fatalf("info=%+v, want Attempt/RetryIndex=0", info)
		}
		return 1, nil
	})
	if err != nil {
		t.Fatalf("err=%v, want nil", err)
	}
}

func TestExecutor_Observer_BudgetDecisions(t *testing.T) {
	key := policy.PolicyKey{Name: "budget_event"}
	obs := &testObserver{}

	// Create a budget implementation that allows
	budgets := budget.NewRegistry()
	budgets.MustRegister("allow", budget.UnlimitedBudget{})
	budgets.MustRegister("deny", denySecondAttemptBudget{}) // Uses existing denySecondAttemptBudget from budgets_test.go if accessible (same package)
	// Actually denySecondAttemptBudget is in budgets_test.go but unexported.
	// We can redefine or reuse if in same package. Ideally verify reuse.
	// Since tests in same package 'retry' share scope, it should work.

	exec := NewExecutor(ExecutorOptions{
		Provider: &controlplane.StaticProvider{
			Policies: map[policy.PolicyKey]policy.EffectivePolicy{
				key: {
					Key: key,
					Retry: policy.RetryPolicy{
						MaxAttempts: 2,
						Budget:      policy.BudgetRef{Name: "allow", Cost: 1},
					},
				},
			},
		},
		Budgets:  budgets,
		Observer: obs,
	})
	exec.sleep = func(context.Context, time.Duration) error { return nil }

	// Run successful with unlimited budget
	_, err := DoValue[int](context.Background(), exec, key, func(ctx context.Context) (int, error) {
		return 1, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(obs.budgetDecisions) != 1 {
		t.Fatalf("expected 1 budget decision, got %d", len(obs.budgetDecisions))
	}
	evt := obs.budgetDecisions[0]
	if !evt.Allowed {
		t.Errorf("expected allowed=true")
	}
	if evt.Reason != budget.ReasonAllowed {
		t.Errorf("expected reason=allowed, got %s", evt.Reason)
	}

	// Now try with denying budget
	exec = NewExecutor(ExecutorOptions{
		Provider: &controlplane.StaticProvider{
			Policies: map[policy.PolicyKey]policy.EffectivePolicy{
				key: {
					Key: key,
					Retry: policy.RetryPolicy{
						MaxAttempts: 2,
						Budget:      policy.BudgetRef{Name: "missing_does_not_exist", Cost: 1},
					},
				},
			},
		},
		Budgets:           budgets,
		Observer:          obs,
		MissingBudgetMode: FailureDeny,
	})
	obs.budgetDecisions = nil // reset

	_, err = DoValue[int](context.Background(), exec, key, func(ctx context.Context) (int, error) {
		return 1, nil
	})
	if err == nil {
		t.Fatal("expected error due to missing budget")
	}

	if len(obs.budgetDecisions) != 1 {
		t.Fatalf("expected 1 budget decision, got %d", len(obs.budgetDecisions))
	}
	evt = obs.budgetDecisions[0]
	if evt.Allowed {
		t.Errorf("expected allowed=false")
	}
	if evt.Reason != budget.ReasonBudgetNotFound {
		t.Errorf("expected reason=budget_not_found, got %s", evt.Reason)
	}
}

type testObserver struct {
	starts    int
	attempts  []observe.AttemptRecord
	successes int
	failures  int

	lastSuccess observe.Timeline
	lastFailure observe.Timeline

	attemptInfos    []observe.AttemptInfo
	budgetDecisions []observe.BudgetDecisionEvent
}

func (o *testObserver) OnStart(context.Context, policy.PolicyKey, policy.EffectivePolicy) {
	o.starts++
}

func (o *testObserver) OnAttempt(ctx context.Context, _ policy.PolicyKey, rec observe.AttemptRecord) {
	o.attempts = append(o.attempts, rec)
	if info, ok := observe.AttemptFromContext(ctx); ok {
		o.attemptInfos = append(o.attemptInfos, info)
	}
}

func (o *testObserver) OnHedgeSpawn(context.Context, policy.PolicyKey, observe.AttemptRecord) {}

func (o *testObserver) OnHedgeCancel(context.Context, policy.PolicyKey, observe.AttemptRecord, string) {
}

func (o *testObserver) OnBudgetDecision(_ context.Context, e observe.BudgetDecisionEvent) {
	o.budgetDecisions = append(o.budgetDecisions, e)
}

func (o *testObserver) OnSuccess(_ context.Context, _ policy.PolicyKey, tl observe.Timeline) {
	o.successes++
	o.lastSuccess = tl
}

func (o *testObserver) OnFailure(_ context.Context, _ policy.PolicyKey, tl observe.Timeline) {
	o.failures++
	o.lastFailure = tl
}
