package retry

import (
	"context"

	"github.com/aponysus/rego/policy"
)

type Operation func(ctx context.Context) error

type Executor struct{}

func (e *Executor) Do(ctx context.Context, key policy.PolicyKey, op Operation) error {
	return op(ctx)
}
