package budget

import (
	"context"
	"math"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aponysus/recourse/policy"
)

func TestTokenBucketBudget_ConcurrentUsage(t *testing.T) {
	// Capacity 1000, no refill.
	b := NewTokenBucketBudget(1000, 0)

	var allowedCount int32
	var deniedCount int32

	var wg sync.WaitGroup
	workers := 10
	attemptsPerWorker := 200 // Total 2000 attempts

	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < attemptsPerWorker; j++ {
				// Random sleep to scramble timing
				time.Sleep(time.Duration(rand.Intn(100)) * time.Microsecond)

				d := b.AllowAttempt(context.Background(), policy.PolicyKey{}, 0, KindRetry, policy.BudgetRef{Cost: 1})
				if d.Allowed {
					atomic.AddInt32(&allowedCount, 1)
				} else {
					atomic.AddInt32(&deniedCount, 1)
				}
			}
		}()
	}

	wg.Wait()

	if allowedCount != 1000 {
		t.Errorf("allowedCount=%d, want 1000", allowedCount)
	}
	if deniedCount != 1000 {
		t.Errorf("deniedCount=%d, want 1000", deniedCount)
	}
}

func TestUnlimitedBudget_AllowsAttempts(t *testing.T) {
	b := UnlimitedBudget{}
	d := b.AllowAttempt(context.Background(), policy.PolicyKey{}, 0, KindRetry, policy.BudgetRef{})
	if !d.Allowed || d.Reason != ReasonAllowed {
		t.Fatalf("decision=%+v, want allowed with reason %q", d, ReasonAllowed)
	}
}

func TestTokenBucketBudget_NilReceiver(t *testing.T) {
	var b *TokenBucketBudget
	d := b.AllowAttempt(context.Background(), policy.PolicyKey{}, 0, KindRetry, policy.BudgetRef{})
	if d.Allowed || d.Reason != ReasonBudgetNil {
		t.Fatalf("decision=%+v, want denied with reason %q", d, ReasonBudgetNil)
	}
}

func TestTokenBucketBudget_RefillAndCost(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	b := NewTokenBucketBudget(2, 1, WithClock(clock.Now))

	d := b.AllowAttempt(context.Background(), policy.PolicyKey{}, 0, KindRetry, policy.BudgetRef{Cost: 1})
	if !d.Allowed {
		t.Fatalf("expected first attempt to be allowed")
	}
	d = b.AllowAttempt(context.Background(), policy.PolicyKey{}, 1, KindRetry, policy.BudgetRef{Cost: 1})
	if !d.Allowed {
		t.Fatalf("expected second attempt to be allowed")
	}
	d = b.AllowAttempt(context.Background(), policy.PolicyKey{}, 2, KindRetry, policy.BudgetRef{Cost: 1})
	if d.Allowed || d.Reason != ReasonBudgetDenied {
		t.Fatalf("decision=%+v, want denied with reason %q before refill", d, ReasonBudgetDenied)
	}

	clock.Advance(2 * time.Second)

	d = b.AllowAttempt(context.Background(), policy.PolicyKey{}, 3, KindRetry, policy.BudgetRef{Cost: 1})
	if !d.Allowed {
		t.Fatalf("expected allowed attempt after refill")
	}
	if b.tokens != 1 {
		t.Fatalf("tokens=%v, want 1", b.tokens)
	}

	d = b.AllowAttempt(context.Background(), policy.PolicyKey{}, 4, KindRetry, policy.BudgetRef{Cost: 2})
	if d.Allowed || d.Reason != ReasonBudgetDenied {
		t.Fatalf("decision=%+v, want denied with reason %q", d, ReasonBudgetDenied)
	}
}

func TestTokenBucketBudget_SetClock(t *testing.T) {
	b := NewTokenBucketBudget(1, 1)
	clock := &fakeClock{now: time.Unix(0, 0)}
	b.SetClock(clock.Now)
	b.tokens = 0
	b.last = clock.Now()

	d := b.AllowAttempt(context.Background(), policy.PolicyKey{}, 0, KindRetry, policy.BudgetRef{Cost: 1})
	if d.Allowed {
		t.Fatalf("expected denied attempt before advancing fake clock")
	}

	clock.Advance(time.Second)

	d = b.AllowAttempt(context.Background(), policy.PolicyKey{}, 1, KindRetry, policy.BudgetRef{Cost: 1})
	if !d.Allowed {
		t.Fatalf("expected allowed attempt after advancing fake clock")
	}
}

func TestTokenBucketBudget_InvalidConfig(t *testing.T) {
	b := NewTokenBucketBudget(-1, math.NaN())
	if b.capacity != 0 || b.refillPerSecond != 0 {
		t.Fatalf("capacity=%v refill=%v, want 0,0", b.capacity, b.refillPerSecond)
	}

	d := b.AllowAttempt(context.Background(), policy.PolicyKey{}, 0, KindRetry, policy.BudgetRef{})
	if d.Allowed {
		t.Fatalf("expected denied attempt with zero capacity")
	}
}

type fakeClock struct {
	now time.Time
}

func (f *fakeClock) Now() time.Time {
	return f.now
}

func (f *fakeClock) Advance(d time.Duration) {
	f.now = f.now.Add(d)
}
