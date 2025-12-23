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

## Standard usage (custom defaults)

For most applications, you want standard defaults (HTTP classification, unlimited budgets, p99 hedging) but with your own instance.

```go
// Create a pre-configured executor
exec := retry.NewDefaultExecutor()

// Use it
user, err := retry.DoValue[User](ctx, exec, "user-service.GetUser", op)
```

`NewDefaultExecutor` comes with:

- **Classifiers**: `AutoClassifier` (handles HTTP, generic errors, and registered integrations like gRPC)
- **Budgets**: `UnlimitedBudget` registered as `"unlimited"`
- **Hedging**: `FixedDelay` and `Latency` (`p90`, `p99`) triggers registered

## Explicit wiring (advanced)

If you want to supply policies, classifiers, and budgets explicitly, build a `retry.Executor`:

```go
budgets := budget.NewRegistry()
budgets.Register("global", budget.NewTokenBucketBudget(100, 50)) // capacity=100, refill=50 tokens/sec

pol := policy.New("user-service.GetUser",
	policy.MaxAttempts(3),
	policy.Classifier(classify.ClassifierHTTP),
	policy.Budget("global"),
	// Enable Hedging (fixed delay)
	policy.EnableHedging(),
	policy.HedgeDelay(10*time.Millisecond),
	policy.HedgeMaxAttempts(2),
)
// Enable Circuit Breaking
pol.Circuit = policy.CircuitPolicy{
	Enabled:   true,
	Threshold: 5,
	Cooldown:  10 * time.Second,
}
key := pol.Key

provider := &controlplane.StaticProvider{
	Policies: map[policy.PolicyKey]policy.EffectivePolicy{
		pol.Key: pol,
	},
}

exec := retry.NewExecutor(
	retry.WithProvider(provider),
	retry.WithBudgetRegistry(budgets),
)

user, err := retry.DoValue[User](ctx, exec, key, op)
```
