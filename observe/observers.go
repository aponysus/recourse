package observe

import (
	"context"

	"github.com/aponysus/recourse/policy"
)

// BaseObserver implements Observer with no-op methods.
//
// Users can embed BaseObserver to implement only the callbacks they need.
type BaseObserver struct{}

func (BaseObserver) OnStart(context.Context, policy.PolicyKey, policy.EffectivePolicy) {}
func (BaseObserver) OnAttempt(context.Context, policy.PolicyKey, AttemptRecord)        {}
func (BaseObserver) OnHedgeSpawn(context.Context, policy.PolicyKey, AttemptRecord)     {}
func (BaseObserver) OnHedgeCancel(context.Context, policy.PolicyKey, AttemptRecord, string) {
}

func (BaseObserver) OnBudgetDecision(context.Context, BudgetDecisionEvent) {}
func (BaseObserver) OnSuccess(context.Context, policy.PolicyKey, Timeline) {}
func (BaseObserver) OnFailure(context.Context, policy.PolicyKey, Timeline) {}

// MultiObserver fans out events to multiple observers.
type MultiObserver struct {
	Observers []Observer
}

func (m MultiObserver) OnStart(ctx context.Context, key policy.PolicyKey, pol policy.EffectivePolicy) {
	for _, o := range m.Observers {
		if o != nil {
			o.OnStart(ctx, key, pol)
		}
	}
}

func (m MultiObserver) OnAttempt(ctx context.Context, key policy.PolicyKey, rec AttemptRecord) {
	for _, o := range m.Observers {
		if o != nil {
			o.OnAttempt(ctx, key, rec)
		}
	}
}

func (m MultiObserver) OnHedgeSpawn(ctx context.Context, key policy.PolicyKey, rec AttemptRecord) {
	for _, o := range m.Observers {
		if o != nil {
			o.OnHedgeSpawn(ctx, key, rec)
		}
	}
}

func (m MultiObserver) OnHedgeCancel(ctx context.Context, key policy.PolicyKey, rec AttemptRecord, reason string) {
	for _, o := range m.Observers {
		if o != nil {
			o.OnHedgeCancel(ctx, key, rec, reason)
		}
	}
}

func (m MultiObserver) OnBudgetDecision(ctx context.Context, ev BudgetDecisionEvent) {
	for _, o := range m.Observers {
		if o != nil {
			o.OnBudgetDecision(ctx, ev)
		}
	}
}

func (m MultiObserver) OnSuccess(ctx context.Context, key policy.PolicyKey, tl Timeline) {
	for _, o := range m.Observers {
		if o != nil {
			o.OnSuccess(ctx, key, tl)
		}
	}
}

func (m MultiObserver) OnFailure(ctx context.Context, key policy.PolicyKey, tl Timeline) {
	for _, o := range m.Observers {
		if o != nil {
			o.OnFailure(ctx, key, tl)
		}
	}
}
