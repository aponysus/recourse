package retry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aponysus/recourse/controlplane"
	"github.com/aponysus/recourse/observe"
	"github.com/aponysus/recourse/policy"
)

func benchmarkExecutor(key policy.PolicyKey, pol policy.EffectivePolicy, obs observe.Observer) *Executor {
	provider := &controlplane.StaticProvider{Policies: map[policy.PolicyKey]policy.EffectivePolicy{key: pol}}
	exec := NewExecutor(WithProvider(provider), WithObserver(obs))
	exec.sleep = func(context.Context, time.Duration) error { return nil }
	fixedNow := time.Unix(0, 0)
	exec.clock = func() time.Time { return fixedNow }
	return exec
}

func benchmarkPolicy(maxAttempts int) policy.EffectivePolicy {
	return policy.EffectivePolicy{
		Retry: policy.RetryPolicy{
			MaxAttempts:       maxAttempts,
			InitialBackoff:    time.Millisecond,
			MaxBackoff:        time.Millisecond,
			BackoffMultiplier: 1,
			Jitter:            policy.JitterNone,
		},
	}
}

func BenchmarkDoValue_SingleAttempt_Success(b *testing.B) {
	key := policy.ParseKey("bench.single_success")
	exec := benchmarkExecutor(key, benchmarkPolicy(1), &observe.NoopObserver{})
	ctx := context.Background()
	op := func(context.Context) (int, error) { return 1, nil }

	b.ReportAllocs()
	_, _ = DoValue(ctx, exec, key, op)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = DoValue(ctx, exec, key, op)
	}
}

func BenchmarkDoValue_ThreeAttempts_AllFail(b *testing.B) {
	key := policy.ParseKey("bench.three_fail")
	exec := benchmarkExecutor(key, benchmarkPolicy(3), &observe.NoopObserver{})
	ctx := context.Background()
	errFailure := errors.New("bench failure")
	op := func(context.Context) (int, error) { return 0, errFailure }

	b.ReportAllocs()
	_, _ = DoValue(ctx, exec, key, op)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = DoValue(ctx, exec, key, op)
	}
}

func BenchmarkDoValue_WithTimelineCapture(b *testing.B) {
	key := policy.ParseKey("bench.timeline")
	exec := benchmarkExecutor(key, benchmarkPolicy(1), &observe.NoopObserver{})
	baseCtx := context.Background()
	op := func(context.Context) (int, error) { return 1, nil }

	b.ReportAllocs()
	ctx, capture := observe.RecordTimeline(baseCtx)
	_, _ = DoValue(ctx, exec, key, op)
	_ = capture.Timeline()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ctx, capture := observe.RecordTimeline(baseCtx)
		_, _ = DoValue(ctx, exec, key, op)
		_ = capture.Timeline()
	}
}

func BenchmarkDoValue_WithObserver(b *testing.B) {
	key := policy.ParseKey("bench.observer")
	exec := benchmarkExecutor(key, benchmarkPolicy(1), observe.BaseObserver{})
	ctx := context.Background()
	op := func(context.Context) (int, error) { return 1, nil }

	b.ReportAllocs()
	_, _ = DoValue(ctx, exec, key, op)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = DoValue(ctx, exec, key, op)
	}
}

func BenchmarkDoValue_FastPath_NoTimeline(b *testing.B) {
	key := policy.ParseKey("bench.fast")
	exec := benchmarkExecutor(key, benchmarkPolicy(3), &observe.NoopObserver{})
	ctx := context.Background()
	op := func(context.Context) (int, error) { return 1, nil }

	b.ReportAllocs()
	_, _ = DoValue(ctx, exec, key, op)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = DoValue(ctx, exec, key, op)
	}
}
