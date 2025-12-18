# Getting started

## Install

```bash
go get github.com/aponysus/rego@latest
```

## The simplest path (facade)

Use `rego.Do` / `rego.DoValue` for the “zero config” path:

```go
user, err := rego.DoValue[User](ctx, "user-service.GetUser", func(ctx context.Context) (User, error) {
	return client.GetUser(ctx, userID)
})
```

Keys must be **low-cardinality** (stable across requests). Good: `"payments.Charge"`. Bad: `"GET /users/123"`.

## Getting a timeline

```go
user, tl, err := rego.DoValueWithTimeline[User](ctx, "user-service.GetUser", op)
_ = user
_ = err

for _, a := range tl.Attempts {
	// inspect outcome, error, backoff, budget gating, timings
}
```

## Explicit wiring (advanced)

If you want to supply policies, classifiers, and budgets explicitly, build a `retry.Executor`:

```go
key := policy.ParseKey("user-service.GetUser")

budgets := budget.NewRegistry()
budgets.Register("global", budget.NewTokenBucketBudget(100, 50)) // capacity=100, refill=50 tokens/sec

exec := retry.NewExecutor(retry.ExecutorOptions{
	Budgets: budgets,
	Provider: &controlplane.StaticProvider{
		Policies: map[policy.PolicyKey]policy.EffectivePolicy{
			key: {
				Key: key,
				Retry: policy.RetryPolicy{
					MaxAttempts:    3,
					ClassifierName: classify.ClassifierHTTP,
					Budget:         policy.BudgetRef{Name: "global", Cost: 1},
				},
			},
		},
	},
})

user, tl, err := retry.DoValueWithTimeline[User](ctx, exec, key, op)
_ = user
_ = tl
_ = err
```

