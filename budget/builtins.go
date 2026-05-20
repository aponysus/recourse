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
// Each budgeted attempt consumes ref.Cost tokens (defaulting to 1).
type TokenBucketBudget struct {
	mu sync.Mutex

	capacity        float64
	refillPerSecond float64

	tokens float64
	last   time.Time
	nowFn  func() time.Time
}

// TokenBucketBudgetOption configures a TokenBucketBudget at construction time.
type TokenBucketBudgetOption func(*TokenBucketBudget)

// WithClock overrides the bucket clock, primarily for tests.
func WithClock(f func() time.Time) TokenBucketBudgetOption {
	return func(b *TokenBucketBudget) {
		b.nowFn = f
	}
}

func NewTokenBucketBudget(capacity int, refillPerSecond float64, opts ...TokenBucketBudgetOption) *TokenBucketBudget {
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
	}
	for _, opt := range opts {
		if opt != nil {
			opt(b)
		}
	}
	b.last = b.now()
	return b
}

func (b *TokenBucketBudget) now() time.Time {
	if b.nowFn != nil {
		return b.nowFn()
	}
	return time.Now()
}

// SetClock overrides the bucket clock, primarily for tests.
func (b *TokenBucketBudget) SetClock(f func() time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nowFn = f
}

func (b *TokenBucketBudget) AllowAttempt(_ context.Context, _ policy.PolicyKey, _ int, _ AttemptKind, ref policy.BudgetRef) Decision {
	if b == nil {
		return Decision{Allowed: false, Reason: ReasonBudgetNil}
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.now()
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
		// Advance last on skew or no refill.
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
