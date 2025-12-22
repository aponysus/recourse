package observe_test

import (
	"context"
	"testing"

	"github.com/aponysus/recourse/observe"
	"github.com/aponysus/recourse/policy"
)

type countingObserver struct {
	starts       int
	attempts     int
	hedgeSpawns  int
	hedgeCancels int
	budgets      int
	successes    int
	failures     int
}

func (c *countingObserver) OnStart(context.Context, policy.PolicyKey, policy.EffectivePolicy) {
	c.starts++
}

func (c *countingObserver) OnAttempt(context.Context, policy.PolicyKey, observe.AttemptRecord) {
	c.attempts++
}

func (c *countingObserver) OnHedgeSpawn(context.Context, policy.PolicyKey, observe.AttemptRecord) {
	c.hedgeSpawns++
}

func (c *countingObserver) OnHedgeCancel(context.Context, policy.PolicyKey, observe.AttemptRecord, string) {
	c.hedgeCancels++
}

func (c *countingObserver) OnBudgetDecision(context.Context, observe.BudgetDecisionEvent) {
	c.budgets++
}

func (c *countingObserver) OnSuccess(context.Context, policy.PolicyKey, observe.Timeline) {
	c.successes++
}

func (c *countingObserver) OnFailure(context.Context, policy.PolicyKey, observe.Timeline) {
	c.failures++
}

func TestMultiObserver_FansOut(t *testing.T) {
	obsA := &countingObserver{}
	obsB := &countingObserver{}
	multi := observe.MultiObserver{Observers: []observe.Observer{obsA, nil, obsB}}

	ctx := context.Background()
	key := policy.PolicyKey{Namespace: "svc", Name: "method"}
	pol := policy.DefaultPolicyFor(key)
	rec := observe.AttemptRecord{Attempt: 1}
	tl := observe.Timeline{Key: key}
	ev := observe.BudgetDecisionEvent{Key: key, Attempt: 1}

	multi.OnStart(ctx, key, pol)
	multi.OnAttempt(ctx, key, rec)
	multi.OnHedgeSpawn(ctx, key, rec)
	multi.OnHedgeCancel(ctx, key, rec, "test")
	multi.OnBudgetDecision(ctx, ev)
	multi.OnSuccess(ctx, key, tl)
	multi.OnFailure(ctx, key, tl)

	requireCounts(t, obsA, "obsA")
	requireCounts(t, obsB, "obsB")
}

func requireCounts(t *testing.T, obs *countingObserver, name string) {
	t.Helper()
	if obs.starts != 1 {
		t.Fatalf("%s starts: expected 1, got %d", name, obs.starts)
	}
	if obs.attempts != 1 {
		t.Fatalf("%s attempts: expected 1, got %d", name, obs.attempts)
	}
	if obs.hedgeSpawns != 1 {
		t.Fatalf("%s hedge spawns: expected 1, got %d", name, obs.hedgeSpawns)
	}
	if obs.hedgeCancels != 1 {
		t.Fatalf("%s hedge cancels: expected 1, got %d", name, obs.hedgeCancels)
	}
	if obs.budgets != 1 {
		t.Fatalf("%s budget decisions: expected 1, got %d", name, obs.budgets)
	}
	if obs.successes != 1 {
		t.Fatalf("%s successes: expected 1, got %d", name, obs.successes)
	}
	if obs.failures != 1 {
		t.Fatalf("%s failures: expected 1, got %d", name, obs.failures)
	}
}
