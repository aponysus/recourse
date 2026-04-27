package retry_test

import (
	"context"
	"fmt"
	"time"

	"github.com/aponysus/recourse/controlplane"
	"github.com/aponysus/recourse/observe"
	"github.com/aponysus/recourse/policy"
	"github.com/aponysus/recourse/retry"
)

func ExampleNewDefaultExecutor_basicRetry() {
	key := policy.ParseKey("svc.Op")
	policies := map[policy.PolicyKey]policy.EffectivePolicy{
		key: policy.NewFromKey(key,
			policy.MaxAttempts(3),
			policy.ConstantBackoff(time.Millisecond),
		),
	}
	exec := retry.NewDefaultExecutor(
		retry.WithProvider(&controlplane.StaticProvider{Policies: policies}),
	)

	attempts := 0
	err := exec.Do(context.Background(), key, func(ctx context.Context) error {
		attempts++
		if attempts < 3 {
			return fmt.Errorf("transient error")
		}
		return nil
	})

	fmt.Println(err, attempts)
	// Output:
	// <nil> 3
}

func ExampleNewDefaultExecutor_captureTimeline() {
	key := policy.ParseKey("svc.Op")
	policies := map[policy.PolicyKey]policy.EffectivePolicy{
		key: policy.NewFromKey(key,
			policy.MaxAttempts(2),
			policy.ConstantBackoff(time.Millisecond),
		),
	}
	exec := retry.NewDefaultExecutor(
		retry.WithProvider(&controlplane.StaticProvider{Policies: policies}),
	)

	ctx, capture := observe.RecordTimeline(context.Background())
	_ = exec.Do(ctx, key, func(ctx context.Context) error {
		return nil
	})

	tl := capture.Timeline()
	fmt.Println(len(tl.Attempts), tl.FinalErr)
	// Output:
	// 1 <nil>
}
