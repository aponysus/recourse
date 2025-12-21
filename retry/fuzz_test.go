package retry

import (
	"context"
	"testing"
	"time"

	"github.com/aponysus/recourse/policy"
)

func FuzzExecutor(f *testing.F) {
	f.Add(3, "retry-policy")
	f.Add(0, "no-retry")

	f.Fuzz(func(t *testing.T, attempts int, polName string) {
		if attempts < 0 || attempts > 100 {
			return
		}
		key := policy.PolicyKey{Name: polName}
		// Create executor with minimal config
		opts := ExecutorOptions{}
		exec := NewExecutor(opts)
		exec.sleep = func(context.Context, time.Duration) error { return nil }

		// Mock provider via simple logic if needed, or rely on static defaults?
		// NewExecutor creates static provider if nil.
		// But GetEffectivePolicy will fail unless we register one.
		// So default fallback logic will trigger.

		_, err := DoValue[int](context.Background(), exec, key, func(ctx context.Context) (int, error) {
			return 1, nil
		})
		if err != nil {
			// Error is fine, just shouldn't panic
		}
	})
}
