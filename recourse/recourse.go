package recourse

import (
	"context"

	"github.com/aponysus/recourse/policy"
	"github.com/aponysus/recourse/retry"
)

// Key is the structured form of a policy key.
type Key = policy.PolicyKey

// ParseKey parses "namespace.name" into a Key.
func ParseKey(s string) Key { return policy.ParseKey(s) }

// Init sets the global default executor.
// It must be called before Do/DoValue are used.
func Init(exec *retry.Executor) {
	retry.SetGlobal(exec)
}

// Do executes op using the default executor and the policy for key.
func Do(ctx context.Context, key string, op retry.Operation) error {
	return retry.DefaultExecutor().Do(ctx, policy.ParseKey(key), op)
}

// DoValue executes op using the default executor and the policy for key.
func DoValue[T any](ctx context.Context, key string, op retry.OperationValue[T]) (T, error) {
	return retry.DoValue(ctx, retry.DefaultExecutor(), policy.ParseKey(key), op)
}
