package budget

import (
	"context"
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
