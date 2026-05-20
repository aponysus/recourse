package retry

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aponysus/recourse/budget"
	"github.com/aponysus/recourse/controlplane"
	"github.com/aponysus/recourse/observe"
	"github.com/aponysus/recourse/policy"
)

type denySecondAttemptBudget struct{}

func (denySecondAttemptBudget) AllowAttempt(_ context.Context, _ policy.PolicyKey, attemptIdx int, _ budget.AttemptKind, _ policy.BudgetRef) budget.Decision {
	if attemptIdx == 1 {
		return budget.Decision{Allowed: false, Reason: budget.ReasonBudgetDenied}
	}
	return budget.Decision{Allowed: true, Reason: budget.ReasonAllowed}
}

type denyAllBudget struct {
	calls int32
}

func (b *denyAllBudget) AllowAttempt(_ context.Context, _ policy.PolicyKey, _ int, _ budget.AttemptKind, _ policy.BudgetRef) budget.Decision {
	atomic.AddInt32(&b.calls, 1)
	return budget.Decision{Allowed: false, Reason: budget.ReasonBudgetDenied}
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

type budgetEventObserver struct {
	events []observe.BudgetDecisionEvent
}

func (o *budgetEventObserver) OnStart(context.Context, policy.PolicyKey, policy.EffectivePolicy)  {}
func (o *budgetEventObserver) OnAttempt(context.Context, policy.PolicyKey, observe.AttemptRecord) {}
func (o *budgetEventObserver) OnHedgeSpawn(context.Context, policy.PolicyKey, observe.AttemptRecord) {
}
func (o *budgetEventObserver) OnHedgeCancel(context.Context, policy.PolicyKey, observe.AttemptRecord, string) {
}
func (o *budgetEventObserver) OnBudgetDecision(_ context.Context, e observe.BudgetDecisionEvent) {
	o.events = append(o.events, e)
}
func (o *budgetEventObserver) OnSuccess(context.Context, policy.PolicyKey, observe.Timeline) {}
func (o *budgetEventObserver) OnFailure(context.Context, policy.PolicyKey, observe.Timeline) {}

func TestExecutor_RetryBudgetSkipsBaseAttempt_FastPath(t *testing.T) {
	key := policy.PolicyKey{Name: "x"}

	db := &denyAllBudget{}
	budgets := budget.NewRegistry()
	budgets.MustRegister("b", db)

	exec := NewExecutorFromOptions(ExecutorOptions{
		Budgets: budgets,
		Provider: &controlplane.StaticProvider{
			Policies: map[policy.PolicyKey]policy.EffectivePolicy{
				key: {
					Key: key,
					Retry: policy.RetryPolicy{
						MaxAttempts: 1,
						Budget:      policy.BudgetRef{Name: "b", Cost: 1},
					},
				},
			},
		},
	})

	calls := 0
	got, err := DoValue[int](context.Background(), exec, key, func(context.Context) (int, error) {
		calls++
		return 42, nil
	})
	if err != nil {
		t.Fatalf("err=%v, want nil", err)
	}
	if got != 42 {
		t.Fatalf("got=%d, want 42", got)
	}
	if calls != 1 {
		t.Fatalf("calls=%d, want 1", calls)
	}
	if budgetCalls := atomic.LoadInt32(&db.calls); budgetCalls != 0 {
		t.Fatalf("budget calls=%d, want 0", budgetCalls)
	}
}

func TestExecutor_RetryBudgetSkipsBaseAttempt_Timeline(t *testing.T) {
	key := policy.PolicyKey{Name: "x"}

	db := &denyAllBudget{}
	budgets := budget.NewRegistry()
	budgets.MustRegister("b", db)

	exec := NewExecutorFromOptions(ExecutorOptions{
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

	opErr := errors.New("first attempt error")
	calls := 0
	ctx, capture := observe.RecordTimeline(context.Background())
	_, err := DoValue[int](ctx, exec, key, func(context.Context) (int, error) {
		calls++
		return 0, opErr
	})
	tl := capture.Timeline()

	if calls != 1 {
		t.Fatalf("calls=%d, want 1", calls)
	}
	if err != opErr {
		t.Fatalf("err=%v, want %v", err, opErr)
	}
	if budgetCalls := atomic.LoadInt32(&db.calls); budgetCalls != 1 {
		t.Fatalf("budget calls=%d, want 1", budgetCalls)
	}
	if len(tl.Attempts) != 2 {
		t.Fatalf("attempts=%d, want 2", len(tl.Attempts))
	}
	if !tl.Attempts[0].BudgetAllowed {
		t.Fatalf("attempt[0].BudgetAllowed=false, want true")
	}
	if tl.Attempts[0].BudgetReason != budget.ReasonNoBudget {
		t.Fatalf("attempt[0].BudgetReason=%q, want %q", tl.Attempts[0].BudgetReason, budget.ReasonNoBudget)
	}
	if tl.Attempts[1].BudgetAllowed {
		t.Fatalf("attempt[1].BudgetAllowed=true, want false")
	}
	if tl.Attempts[1].BudgetReason != budget.ReasonBudgetDenied {
		t.Fatalf("attempt[1].BudgetReason=%q, want %q", tl.Attempts[1].BudgetReason, budget.ReasonBudgetDenied)
	}
}

func TestExecutor_BudgetDeniesSecondAttempt_StopsRetryAndReturnsLastErr(t *testing.T) {
	key := policy.PolicyKey{Name: "x"}

	budgets := budget.NewRegistry()
	budgets.MustRegister("b", denySecondAttemptBudget{})

	exec := NewExecutorFromOptions(ExecutorOptions{
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
	ctx, capture := observe.RecordTimeline(context.Background())
	_, err := DoValue[int](ctx, exec, key, func(context.Context) (int, error) {
		calls++
		return 0, opErr
	})
	tl := capture.Timeline()

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

func TestExecutor_MissingBudgetName_DeniesRetriesByDefault(t *testing.T) {
	key := policy.PolicyKey{Name: "x"}

	exec := NewExecutorFromOptions(ExecutorOptions{
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
	ctx, capture := observe.RecordTimeline(context.Background())
	_, err := DoValue[int](ctx, exec, key, func(context.Context) (int, error) {
		calls++
		return 0, errors.New("nope")
	})
	tl := capture.Timeline()

	if err == nil {
		t.Fatalf("expected error")
	}
	if calls != 1 {
		t.Fatalf("calls=%d, want 1", calls)
	}
	if len(tl.Attempts) != 2 {
		t.Fatalf("attempts=%d, want 2", len(tl.Attempts))
	}

	if !tl.Attempts[0].BudgetAllowed {
		t.Fatalf("attempt[0].BudgetAllowed=false, want true")
	}
	if tl.Attempts[0].BudgetReason != budget.ReasonNoBudget {
		t.Fatalf("attempt[0].BudgetReason=%q, want %q", tl.Attempts[0].BudgetReason, budget.ReasonNoBudget)
	}

	rec := tl.Attempts[1]
	if rec.BudgetAllowed {
		t.Fatalf("BudgetAllowed=true, want false")
	}
	if rec.BudgetReason != budget.ReasonBudgetNotFound {
		t.Fatalf("BudgetReason=%q, want %q", rec.BudgetReason, budget.ReasonBudgetNotFound)
	}
}

func TestExecutor_MissingBudgetName_AllowsAttemptsWithUnsafeOptIn(t *testing.T) {
	key := policy.PolicyKey{Name: "x"}

	exec := NewExecutorFromOptions(ExecutorOptions{
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
	ctx, capture := observe.RecordTimeline(context.Background())
	_, err := DoValue[int](ctx, exec, key, func(context.Context) (int, error) {
		calls++
		return 0, errors.New("nope")
	})
	tl := capture.Timeline()
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
		wantReason := budget.ReasonBudgetNotFound
		if i == 0 {
			wantReason = budget.ReasonNoBudget
		}
		if rec.BudgetReason != wantReason {
			t.Fatalf("attempt[%d].BudgetReason=%q, want %q", i, rec.BudgetReason, wantReason)
		}
	}
}

func TestExecutor_BudgetRelease_CalledOncePerAllowedAttempt(t *testing.T) {
	key := policy.PolicyKey{Name: "x"}

	cb := &countingReleaseBudget{}
	budgets := budget.NewRegistry()
	budgets.MustRegister("b", cb)

	exec := NewExecutorFromOptions(ExecutorOptions{
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
	_, err := DoValue[int](context.Background(), exec, key, func(context.Context) (int, error) {
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
	if got, want := atomic.LoadInt32(&cb.allowCalls), int32(1); got != want {
		t.Fatalf("allowCalls=%d, want %d", got, want)
	}
	if got, want := atomic.LoadInt32(&cb.releases), int32(1); got != want {
		t.Fatalf("releases=%d, want %d", got, want)
	}
}

func TestExecutor_AllowAttempt_ReleaseIsIdempotent(t *testing.T) {
	key := policy.PolicyKey{Name: "x"}
	cb := &countingReleaseBudget{}
	budgets := budget.NewRegistry()
	budgets.MustRegister("b", cb)

	exec := NewExecutorFromOptions(ExecutorOptions{
		Budgets: budgets,
	})

	// Call allowAttempt directly
	d, allowed := exec.allowAttempt(context.Background(), key, policy.BudgetRef{Name: "b", Cost: 1}, 1, budget.KindRetry)
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

type panicBudget struct{}

func (panicBudget) AllowAttempt(context.Context, policy.PolicyKey, int, budget.AttemptKind, policy.BudgetRef) budget.Decision {
	panic("boom")
}

type emptyReasonBudget struct {
	allowed bool
}

func (b emptyReasonBudget) AllowAttempt(context.Context, policy.PolicyKey, int, budget.AttemptKind, policy.BudgetRef) budget.Decision {
	return budget.Decision{Allowed: b.allowed}
}

func TestExecutor_AllowAttempt_NilExecutorAllows(t *testing.T) {
	var exec *Executor
	d, ok := exec.allowAttempt(context.Background(), policy.PolicyKey{}, policy.BudgetRef{Name: "b", Cost: 2}, 0, budget.KindRetry)
	if !ok || !d.Allowed || d.Reason != budget.ReasonNoBudget {
		t.Fatalf("decision=%+v ok=%v, want allowed/no_budget", d, ok)
	}
}

func TestExecutor_AllowAttempt_EmptyBudgetNameAllows(t *testing.T) {
	obs := &budgetEventObserver{}
	exec := &Executor{observer: obs}

	d, ok := exec.allowAttempt(context.Background(), policy.PolicyKey{}, policy.BudgetRef{Name: "   ", Cost: 1}, 1, budget.KindRetry)
	if !ok || !d.Allowed || d.Reason != budget.ReasonNoBudget {
		t.Fatalf("decision=%+v ok=%v, want allowed/no_budget", d, ok)
	}
	if len(obs.events) != 0 {
		t.Fatalf("expected no budget events, got %d", len(obs.events))
	}
}

func TestExecutor_AllowAttempt_BaseRetryAttemptBypassesBudget(t *testing.T) {
	obs := &budgetEventObserver{}
	exec := &Executor{
		observer:          obs,
		missingBudgetMode: FailureDeny,
	}

	d, ok := exec.allowAttempt(context.Background(), policy.PolicyKey{Name: "op"}, policy.BudgetRef{Name: "b", Cost: 2}, 0, budget.KindRetry)
	if !ok || !d.Allowed || d.Reason != budget.ReasonNoBudget {
		t.Fatalf("decision=%+v ok=%v, want allowed/no_budget", d, ok)
	}
	if len(obs.events) != 0 {
		t.Fatalf("expected no budget events, got %d", len(obs.events))
	}
}

func TestExecutor_AllowAttempt_BudgetRegistryNil_Denies(t *testing.T) {
	obs := &budgetEventObserver{}
	exec := &Executor{
		observer:          obs,
		missingBudgetMode: FailureDeny,
	}

	d, ok := exec.allowAttempt(context.Background(), policy.PolicyKey{Name: "op"}, policy.BudgetRef{Name: "b", Cost: 2}, 1, budget.KindRetry)
	if ok || d.Allowed || d.Reason != budget.ReasonBudgetRegistryNil {
		t.Fatalf("decision=%+v ok=%v, want denied/budget_registry_nil", d, ok)
	}
	if len(obs.events) != 1 {
		t.Fatalf("expected 1 budget event, got %d", len(obs.events))
	}
	evt := obs.events[0]
	if evt.Mode != "deny" {
		t.Fatalf("mode=%q, want %q", evt.Mode, "deny")
	}
	if evt.BudgetName != "b" || evt.Cost != 2 {
		t.Fatalf("event=%+v, want budget b cost 2", evt)
	}
}

func TestExecutor_AllowAttempt_MissingBudget_AllowUnsafe(t *testing.T) {
	obs := &budgetEventObserver{}
	exec := &Executor{
		observer:          obs,
		budgets:           budget.NewRegistry(),
		missingBudgetMode: FailureAllowUnsafe,
	}

	d, ok := exec.allowAttempt(context.Background(), policy.PolicyKey{Name: "op"}, policy.BudgetRef{Name: "missing", Cost: 1}, 1, budget.KindRetry)
	if !ok || !d.Allowed || d.Reason != budget.ReasonBudgetNotFound {
		t.Fatalf("decision=%+v ok=%v, want allowed/budget_not_found", d, ok)
	}
	if len(obs.events) != 1 {
		t.Fatalf("expected 1 budget event, got %d", len(obs.events))
	}
	if obs.events[0].Mode != "allow_unsafe" {
		t.Fatalf("mode=%q, want %q", obs.events[0].Mode, "allow_unsafe")
	}
}

func TestExecutor_AllowAttempt_PanicInBudget(t *testing.T) {
	obs := &budgetEventObserver{}
	budgets := budget.NewRegistry()
	budgets.MustRegister("panic", panicBudget{})

	exec := &Executor{
		observer:      obs,
		budgets:       budgets,
		recoverPanics: true,
	}

	d, ok := exec.allowAttempt(context.Background(), policy.PolicyKey{Name: "op"}, policy.BudgetRef{Name: "panic", Cost: 1}, 1, budget.KindRetry)
	if ok || d.Allowed || d.Reason != budget.ReasonPanicInBudget {
		t.Fatalf("decision=%+v ok=%v, want denied/panic_in_budget", d, ok)
	}
	if len(obs.events) != 1 {
		t.Fatalf("expected 1 budget event, got %d", len(obs.events))
	}
	if obs.events[0].Mode != "standard" {
		t.Fatalf("mode=%q, want %q", obs.events[0].Mode, "standard")
	}
}

func TestExecutor_AllowAttempt_DefaultReason(t *testing.T) {
	obs := &budgetEventObserver{}
	budgets := budget.NewRegistry()
	budgets.MustRegister("allow", emptyReasonBudget{allowed: true})
	budgets.MustRegister("deny", emptyReasonBudget{allowed: false})

	exec := &Executor{
		observer: obs,
		budgets:  budgets,
	}

	d, ok := exec.allowAttempt(context.Background(), policy.PolicyKey{Name: "op"}, policy.BudgetRef{Name: "allow", Cost: 1}, 1, budget.KindRetry)
	if !ok || !d.Allowed || d.Reason != budget.ReasonAllowed {
		t.Fatalf("decision=%+v ok=%v, want allowed/allowed", d, ok)
	}

	d, ok = exec.allowAttempt(context.Background(), policy.PolicyKey{Name: "op"}, policy.BudgetRef{Name: "deny", Cost: 1}, 1, budget.KindRetry)
	if ok || d.Allowed || d.Reason != budget.ReasonBudgetDenied {
		t.Fatalf("decision=%+v ok=%v, want denied/budget_denied", d, ok)
	}
}
