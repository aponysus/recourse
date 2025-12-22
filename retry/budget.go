package retry

import (
	"context"
	"strings"
	"sync"

	"github.com/aponysus/recourse/budget"
	"github.com/aponysus/recourse/internal"
	"github.com/aponysus/recourse/observe"
	"github.com/aponysus/recourse/policy"
)

func (e *Executor) allowAttempt(ctx context.Context, key policy.PolicyKey, ref policy.BudgetRef, attemptIdx int, kind budget.AttemptKind) (decision budget.Decision, allowed bool) {
	if e == nil {
		return budget.Decision{Allowed: true, Reason: budget.ReasonNoBudget}, true
	}

	ref.Name = strings.TrimSpace(ref.Name)
	if ref.Name == "" {
		return budget.Decision{Allowed: true, Reason: budget.ReasonNoBudget}, true
	}

	// Prepare event (Mode and Outcome will be filled later)
	event := observe.BudgetDecisionEvent{
		Key:        key,
		Attempt:    attemptIdx,
		Kind:       kind,
		BudgetName: ref.Name,
		Cost:       ref.Cost,
		Allowed:    false,
	}

	// Helper to emit event before returning
	emit := func(d budget.Decision, ok bool) {
		if e.observer != nil {
			event.Allowed = d.Allowed
			event.Reason = d.Reason
			// Mode logic? "allow_unsafe" if allowed via missing budget mode?
			// Simplify: Mode is just failureModeString if missing, else "standard"
			if event.Mode == "" {
				event.Mode = "standard"
			}
			e.observer.OnBudgetDecision(ctx, event)
		}
	}

	var missingReason string
	var b budget.Budget
	var ok bool

	if e.budgets == nil {
		missingReason = budget.ReasonBudgetRegistryNil
	} else if b, ok = e.budgets.Get(ref.Name); !ok {
		missingReason = budget.ReasonBudgetNotFound
	} else if internal.IsTypedNil(b) {
		missingReason = budget.ReasonBudgetNil
	}

	if missingReason != "" {
		event.Mode = failureModeString(e.missingBudgetMode)
		if e.missingBudgetMode == FailureAllow || e.missingBudgetMode == FailureAllowUnsafe {
			// Allow unsafe
			d := budget.Decision{Allowed: true, Reason: missingReason}
			emit(d, true)
			return d, true
		}
		// Deny safe
		d := budget.Decision{Allowed: false, Reason: missingReason}
		emit(d, false)
		return d, false
	}

	// Budget exists and is valid
	if e.recoverPanics {
		defer func() {
			if r := recover(); r != nil {
				d := budget.Decision{Allowed: false, Reason: budget.ReasonPanicInBudget}
				emit(d, false)
				decision = d
				allowed = false
				// Re-panic? No, recover means we handle it. But caller expects allowAttempt to return safely.
				// We update named return values.
			}
		}()
	}

	decision = b.AllowAttempt(ctx, key, attemptIdx, kind, ref)
	if decision.Reason == "" {
		if decision.Allowed {
			decision.Reason = budget.ReasonAllowed
		} else {
			decision.Reason = budget.ReasonBudgetDenied
		}
	}

	if decision.Release != nil {
		originalRelease := decision.Release
		var once sync.Once
		decision.Release = func() {
			once.Do(originalRelease)
		}
	}

	emit(decision, decision.Allowed)
	return decision, decision.Allowed
}

func (e *Executor) handleMissingBudget(ctx context.Context, reason string) (budget.Decision, bool) {
	switch e.missingBudgetMode {
	case FailureAllow, FailureAllowUnsafe:
		// Unsafe opt-in: allow the attempt even though budget is broken/missing.
		return budget.Decision{Allowed: true, Reason: reason}, true
	case FailureDeny:
		fallthrough
	default:
		// Default safe behavior: fail closed.
		return budget.Decision{Allowed: false, Reason: reason}, false
	}
}
