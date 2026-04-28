# Getting started

## Install

```bash
go get github.com/aponysus/recourse@latest
```

## The simplest path (facade)

Use `recourse.Do` / `recourse.DoValue` for the "zero config" path.
If you do not call `recourse.Init`, recourse lazily creates the default executor and uses `policy.DefaultPolicyFor(key)`.
That default policy is bounded and retries transient errors, so a first run can show the retry path and the timeline:

```go
package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/aponysus/recourse/observe"
	"github.com/aponysus/recourse/recourse"
)

type User struct{ ID string }

func main() {
	ctx, capture := observe.RecordTimeline(context.Background())
	attempts := 0

	user, err := recourse.DoValue[User](ctx, "user-service.GetUser", func(ctx context.Context) (User, error) {
		attempts++
		if attempts == 1 {
			return User{}, errors.New("temporary upstream error")
		}
		return User{ID: "123"}, nil
	})

	fmt.Printf("user=%s err=%v attempts=%d\n", user.ID, err, attempts)
	for _, attempt := range capture.Timeline().Attempts {
		fmt.Printf("attempt=%d reason=%s err=%v\n", attempt.Attempt, attempt.Outcome.Reason, attempt.Err)
	}
}
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

For most applications, you want standard defaults (auto classification, an unlimited budget, and latency-based hedging triggers) but with your own instance.

```go
// Create a pre-configured executor
exec := retry.NewDefaultExecutor()

// Use it
user, err := retry.DoValue[User](ctx, exec, "user-service.GetUser", op)
```

`NewDefaultExecutor` comes with:

- **Classifiers**: `AutoClassifier` (HTTPError-aware; otherwise uses `AlwaysRetryOnError`)
- **Budgets**: `UnlimitedBudget` registered as `"unlimited"`
- **Hedging**: `FixedDelay` and `Latency` (`p90`, `p95`, `p99`) triggers registered
<!-- Claim-ID: CLM-004 -->

## Explicit wiring (advanced)

If you want to supply policies, classifiers, and budgets explicitly, build a `retry.Executor` and either use it directly or initialize the facade:

```go
budgets := budget.NewRegistry()
if err := budgets.Register("global", budget.NewTokenBucketBudget(100, 50)); err != nil { // capacity=100, refill=50 tokens/sec
	panic(err)
}

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

recourse.Init(exec)
user, err := recourse.DoValue[User](ctx, key.String(), op)

// Or call retry directly when you want to pass the executor explicitly:
// user, err := retry.DoValue[User](ctx, exec, key, op)
```
