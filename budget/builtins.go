package budget

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/aponysus/recourse/policy"
)

// UnlimitedBudget allows every attempt.
type UnlimitedBudget struct{}

func (UnlimitedBudget) AllowAttempt(_ context.Context, _ policy.PolicyKey, _ int, _ AttemptKind, _ policy.BudgetRef) Decision {
	return Decision{Allowed: true, Reason: ReasonAllowed}
}

// TokenBucketBudget is a simple token-bucket implementation.
//
// It starts full (capacity tokens) and refills at refillPerSecond tokens/second.
// Each attempt consumes ref.Cost tokens (defaulting to 1).
type TokenBucketBudget struct {
	mu sync.Mutex

	capacity        float64
	refillPerSecond float64

	tokens float64
	last   time.Time
}

func NewTokenBucketBudget(capacity int, refillPerSecond float64) *TokenBucketBudget {
	if capacity < 0 {
		capacity = 0
	}
	if refillPerSecond < 0 {
		refillPerSecond = 0
	}
	if math.IsNaN(refillPerSecond) || math.IsInf(refillPerSecond, 0) {
		refillPerSecond = 0
	}
	b := &TokenBucketBudget{
		capacity:        float64(capacity),
		refillPerSecond: refillPerSecond,
		tokens:          float64(capacity),
		last:            time.Now(),
	}
	return b
}

func (b *TokenBucketBudget) AllowAttempt(_ context.Context, _ policy.PolicyKey, _ int, _ AttemptKind, ref policy.BudgetRef) Decision {
	if b == nil {
		return Decision{Allowed: false, Reason: ReasonBudgetNil}
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	// Sanity check state
	if math.IsNaN(b.tokens) || math.IsInf(b.tokens, 0) {
		b.tokens = 0
	}

	if b.last.IsZero() {
		b.tokens = b.capacity
		b.last = now
	} else if b.refillPerSecond > 0 && !now.Before(b.last) {
		elapsed := now.Sub(b.last).Seconds()
		added := elapsed * b.refillPerSecond
		if math.IsNaN(added) || math.IsInf(added, 0) || added < 0 {
			added = 0
		}

		b.tokens += added
		if b.tokens > b.capacity {
			b.tokens = b.capacity
		}
		b.last = now
	} else {
		// Clock skew (now < last) or no refill.
		// We just advance last to now to avoid stuck timestamp if clock jumps back and forth?
		// Or keep last? If we keep last, we might refill properly when clock catches up.
		// Existing behavior: b.last = now. This resets logic, penalizing negative skew.
		// Let's stick to existing behavior but ensure last is updated.
		b.last = now
	}

	cost := 1
	if ref.Cost > 0 {
		cost = ref.Cost
	}
	need := float64(cost)
	if need <= 0 {
		need = 1
	}

	if b.tokens >= need {
		b.tokens -= need
		return Decision{Allowed: true, Reason: ReasonAllowed}
	}
	return Decision{Allowed: false, Reason: ReasonBudgetDenied}
}
