package budget

import (
	"context"

	"github.com/aponysus/recourse/policy"
)

// AttemptKind describes the attempt type being gated.
type AttemptKind int

const (
	KindRetry AttemptKind = iota
	KindHedge
)

// Standard Decision.Reason strings are defined in reasons.go.

// Decision is the result of a budget check.
type Decision struct {
	Allowed bool
	Reason  string

	// Release, when non-nil, is called exactly once after an allowed attempt finishes.
	Release func()
}

// Budget gates attempts to prevent retry/hedge storms.
type Budget interface {
	AllowAttempt(ctx context.Context, key policy.PolicyKey, attemptIdx int, kind AttemptKind, ref policy.BudgetRef) Decision
}
