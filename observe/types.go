package observe

import (
	"context"
	"time"

	"github.com/aponysus/recourse/budget"
	"github.com/aponysus/recourse/classify"
	"github.com/aponysus/recourse/policy"
)

// BudgetDecisionEvent describes a budget gating decision.
type BudgetDecisionEvent struct {
	Key        policy.PolicyKey
	Attempt    int
	Kind       budget.AttemptKind
	BudgetName string
	Cost       int
	Mode       string // "allow", "deny", "fallback", "allow_unsafe"
	Allowed    bool
	Reason     string
}

// AttemptRecord describes a single attempt (or hedge) execution.
type AttemptRecord struct {
	Attempt   int
	StartTime time.Time
	EndTime   time.Time

	// Hedging (Phase 5+)
	IsHedge    bool
	HedgeIndex int

	// Classification (Phase 3+)
	Outcome classify.Outcome

	Err error

	Backoff time.Duration // backoff before this attempt

	// Budgets (Phase 4+)
	BudgetAllowed bool
	BudgetReason  string
}

// Timeline is the structured record of a single call and all of its attempts.
type Timeline struct {
	Key      policy.PolicyKey
	PolicyID string
	Start    time.Time
	End      time.Time

	// Attributes holds call-level metadata (policy source, fallbacks, normalization notes, etc.).
	Attributes map[string]string

	Attempts []AttemptRecord
	FinalErr error
}

// Observer receives lifecycle callbacks for a single call.
type Observer interface {
	OnStart(ctx context.Context, key policy.PolicyKey, pol policy.EffectivePolicy)
	OnAttempt(ctx context.Context, key policy.PolicyKey, rec AttemptRecord)

	// Hedging hooks (no-ops until Phase 5).
	OnHedgeSpawn(ctx context.Context, key policy.PolicyKey, rec AttemptRecord)
	OnHedgeCancel(ctx context.Context, key policy.PolicyKey, rec AttemptRecord, reason string)

	OnBudgetDecision(ctx context.Context, ev BudgetDecisionEvent)

	OnSuccess(ctx context.Context, key policy.PolicyKey, tl Timeline)
	OnFailure(ctx context.Context, key policy.PolicyKey, tl Timeline)
}
