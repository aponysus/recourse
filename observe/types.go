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
	Key        policy.PolicyKey   // Policy key for the attempted call.
	Attempt    int                // Attempt index (0-based).
	Kind       budget.AttemptKind // Retry or hedge attempt.
	BudgetName string             // Budget registry name.
	Cost       int                // Units requested from the budget.
	Mode       string             // "standard", "allow", "deny", "fallback", "allow_unsafe", "unknown"
	Allowed    bool               // Whether the attempt was allowed.
	Reason     string             // Decision reason (see budget reasons).
}

// AttemptRecord describes a single attempt (or hedge) execution.
type AttemptRecord struct {
	Attempt   int       // Attempt index (0-based).
	StartTime time.Time // Attempt start time.
	EndTime   time.Time // Attempt end time.

	IsHedge    bool // Whether this attempt is a hedge.
	HedgeIndex int  // Hedge index within the attempt group.

	Outcome classify.Outcome // Classification outcome for this attempt.

	Err error // Error returned by the attempt (if any).

	Backoff time.Duration // Backoff delay before this attempt.

	BudgetAllowed bool   // Whether budget gating allowed this attempt.
	BudgetReason  string // Budget decision reason (see budget reasons).
}

// Timeline is the structured record of a single call and all of its attempts.
type Timeline struct {
	Key      policy.PolicyKey // Policy key for the call.
	PolicyID string           // Policy identifier (if set).
	Start    time.Time        // Call start time.
	End      time.Time        // Call end time.

	// Attributes holds call-level metadata (policy source, fallbacks, normalization notes, etc.).
	Attributes map[string]string

	Attempts []AttemptRecord // Per-attempt records in execution order.
	FinalErr error           // Final error returned to the caller.
}

// Observer receives lifecycle callbacks for a single call.
type Observer interface {
	OnStart(ctx context.Context, key policy.PolicyKey, pol policy.EffectivePolicy)
	OnAttempt(ctx context.Context, key policy.PolicyKey, rec AttemptRecord)

	OnHedgeSpawn(ctx context.Context, key policy.PolicyKey, rec AttemptRecord)
	OnHedgeCancel(ctx context.Context, key policy.PolicyKey, rec AttemptRecord, reason string)

	OnBudgetDecision(ctx context.Context, ev BudgetDecisionEvent)

	OnSuccess(ctx context.Context, key policy.PolicyKey, tl Timeline)
	OnFailure(ctx context.Context, key policy.PolicyKey, tl Timeline)
}
