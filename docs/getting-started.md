# Getting started

## Install

```bash
go get github.com/aponysus/recourse@latest
```

## The simplest path (facade)

Use `recourse.Do` / `recourse.DoValue` for the “zero config” path:

```go
user, err := recourse.DoValue[User](ctx, "user-service.GetUser", func(ctx context.Context) (User, error) {
	return client.GetUser(ctx, userID)
})
```

Keys must be **low-cardinality** (stable across requests). Good: `"payments.Charge"`. Bad: `"GET /users/123"`.

## Getting a timeline

```go
// Start timeline capture
ctx, capture := observe.RecordTimeline(ctx)

user, err := recourse.DoValue(ctx, "user-service.GetUser", op)

// Access timeline (safe even after function returns)
tl := capture.Timeline()
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

exec := retry.NewExecutor(
	retry.WithBudgetRegistry(budgets),
	retry.WithPolicy("user-service.GetUser",
		policy.MaxAttempts(3),
		policy.Classifier(classify.ClassifierHTTP),
		policy.Budget("global"),
		// Enable Circuit Breaking
		policy.WithCircuitBreaker(policy.CircuitPolicy{
			Enabled:   true,
			Threshold: 5,
			Cooldown:  10 * time.Second,
		}),
		// Enable Hedging
		policy.WithHedge(policy.HedgePolicy{
			Enabled:    true,
			HedgeDelay: 10 * time.Millisecond,
		}),
	),
)

user, err := retry.DoValue[User](ctx, exec, key, op)
```

