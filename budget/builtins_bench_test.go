package budget

import (
	"context"
	"testing"

	"github.com/aponysus/recourse/policy"
)

func BenchmarkTokenBucketBudget_AllowAttempt(b *testing.B) {
	b.ReportAllocs()
	bucket := NewTokenBucketBudget(b.N+1, 0)
	ref := policy.BudgetRef{Cost: 1}
	key := policy.PolicyKey{Name: "bench"}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = bucket.AllowAttempt(ctx, key, i, KindRetry, ref)
	}
}

func BenchmarkTokenBucketBudget_Concurrent(b *testing.B) {
	b.ReportAllocs()
	bucket := NewTokenBucketBudget(b.N+1, 0)
	ref := policy.BudgetRef{Cost: 1}
	key := policy.PolicyKey{Name: "bench"}
	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		idx := 0
		for pb.Next() {
			_ = bucket.AllowAttempt(ctx, key, idx, KindRetry, ref)
			idx++
		}
	})
}
