package observe

import (
	"context"

	"github.com/aponysus/recourse/policy"
)

// NoopObserver implements Observer with no-op methods.
type NoopObserver struct{}

func (NoopObserver) OnStart(context.Context, policy.PolicyKey, policy.EffectivePolicy) {}
func (NoopObserver) OnAttempt(context.Context, policy.PolicyKey, AttemptRecord)        {}
func (NoopObserver) OnHedgeSpawn(context.Context, policy.PolicyKey, AttemptRecord)     {}
func (NoopObserver) OnHedgeCancel(context.Context, policy.PolicyKey, AttemptRecord, string) {
}
func (NoopObserver) OnBudgetDecision(context.Context, BudgetDecisionEvent) {}
func (NoopObserver) OnSuccess(context.Context, policy.PolicyKey, Timeline) {}
func (NoopObserver) OnFailure(context.Context, policy.PolicyKey, Timeline) {}
