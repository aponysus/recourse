# recourse

Policy-driven, observability-first resilience library in Go for distributed systems.

This repository is **early-stage**. Retry, budgets/backpressure, timelines, and outcome classifiers exist today; hedging, remote control plane, and integrations are still in progress.

Docs site: https://aponysus.github.io/recourse/

## What you get today

- A retry executor with bounded attempts, backoff/jitter, and per-attempt/overall timeouts
- Per-attempt budgets/backpressure to prevent retry storms (opt-in via `retry.ExecutorOptions.Budgets`; fail-closed by default)
- A low-cardinality key model (`"svc.Method"`) that policies are attached to
- Easy-to-use functional options for configuration (`policy.New("svc.Method", policy.HTTPDefaults())`)
- Outcome classifiers for protocol/domain-aware retry decisions
- Structured, per-attempt observability via `observe.Timeline` (captured via `observe.RecordTimeline`)
- Optional callback-based observability via `observe.Observer`

## Requirements

- Go 1.22+

## Why

In production systems, resilience code tends to drift:

- Hand-rolled retry loops behave differently across services/repos.
- Retry decisions are often too naive (“retry on error”), ignoring protocol/domain semantics.
- Observability is incomplete (you know it retried, but not why/when/how often).

`recourse` centralizes resilience behavior behind a **policy key** and emits structured, per-attempt telemetry.

## Installation

```bash
go get github.com/aponysus/recourse@latest
```

## Key concepts

### Policy keys (low cardinality)

Call sites provide a low-cardinality key like `"user-service.GetUser"`. Policies decide retry behavior for that key.

Keys are intentionally **not** request-scoped. Do not embed IDs (user_id, tenant_id, request_id) in the key.

### Policies

Policies live in `policy.EffectivePolicy` and are resolved through a `controlplane.PolicyProvider` (today: `controlplane.StaticProvider`).

Policy normalization/clamping is handled by `EffectivePolicy.Normalize()` to keep execution safe even if configs are out of range.

### Timeouts and cancellation

`recourse` always respects `context.Context` cancellation.

- If `ctx` is canceled before the first attempt, the operation is not called and `ctx.Err()` is returned.
- `policy.Retry.TimeoutPerAttempt` wraps each attempt in a per-attempt context timeout.
- `policy.Retry.OverallTimeout` wraps the entire call in an overall context timeout.
- Backoff sleeps are also canceled by `ctx.Done()`.

### Observability

Every call can produce an `observe.Timeline`:

- One `Timeline` per call
- One `AttemptRecord` per attempt
- Optional observer callbacks (`observe.Observer`) for logs/metrics/tracing integrations
- Budget gating decisions (`observe.BudgetDecisionEvent`)

## Quick start

Use the facade package (import path `github.com/aponysus/recourse/recourse`) for the simplest path:

```go
package main

import (
	"context"

	"github.com/aponysus/recourse/recourse"
)

type User struct{ ID string }

func main() {
	ctx := context.Background()

	user, err := recourse.DoValue[User](ctx, "user-service.GetUser", func(ctx context.Context) (User, error) {
		// call dependency here
		return User{ID: "123"}, nil
	})
	_ = user
	_ = err
}
```

### Choosing a good key

Keys should be stable and low-cardinality:

Good:
- `"user-service.GetUser"`
- `"payments.Charge"`
- `"db.Users.Query"`

Bad:
- `"user-service.GetUser?user_id=123"`
- `"GET /users/123"`
- `"payments.Charge:tenant=acme"`

`policy.ParseKey` parses `"namespace.name"` into `{Namespace, Name}`; if there is no dot, the entire string becomes `Name`.

## Getting a timeline

When you need structured “what happened?” data:

```go
ctx, capture := observe.RecordTimeline(ctx)
user, err := recourse.DoValue(ctx, "user-service.GetUser", op)
_ = user
_ = err

// Access timeline after call
tl := capture.Timeline()
for _, a := range tl.Attempts {
	// a.Attempt, a.StartTime, a.EndTime, a.Err, ...
}
```

The timeline also includes optional `tl.Attributes` such as:

- `policy_source` (when available)
- `policy_normalized=true` and `policy_clamped_fields=...` when policy normalization changed values
- `policy_error=...` and `missing_policy_mode=...` when policy resolution involved an error/fallback

## Advanced usage (explicit executor + structured keys)

The facade uses a lazily-initialized default executor. For explicit wiring, build a `retry.Executor` and call the `retry` package directly.

```go
```go
import (
	"context"
	"time"

	"github.com/aponysus/recourse/policy"
	"github.com/aponysus/recourse/retry"
)

exec := retry.NewExecutor(
	retry.WithPolicy("user-service.GetUser",
		policy.MaxAttempts(5),
		policy.Backoff(25*time.Millisecond, 250*time.Millisecond, 2.0),
		policy.Jitter(policy.JitterEqual),
		policy.OverallTimeout(2*time.Second),
	),
)

// Use the executor
user, err := retry.DoValue[User](ctx, exec, policy.ParseKey("user-service.GetUser"), op)
```

### Static policies (per-key and default)

Use `controlplane.StaticProvider` to supply policies from code:

```go
exec := retry.NewExecutor(
	retry.WithPolicy("user-service.GetUser", policy.MaxAttempts(5)),
	// OR use a full provider
	// retry.WithProvider(myProvider),
)
```

### Missing policy behavior

If the provider returns an error, the executor uses `ExecutorOptions.MissingPolicyMode`:

- `retry.FailureFallback` (default): use a safe default policy
- `retry.FailureAllow`: run a single attempt (no retries)
- `retry.FailureDeny`: fail fast with `retry.ErrNoPolicy` (wrapped in `*retry.NoPolicyError`)

### Missing budget behavior

If a policy references a budget that is missing (or the registry is nil), the executor uses `ExecutorOptions.MissingBudgetMode`:

- `retry.FailureDeny` (default): fail fast (fail-closed)
- `retry.FailureAllow`: allow the attempt (fail-open)
- `retry.FailureAllowUnsafe`: allow the attempt (explicit unsafe opt-in)

## Observers

To stream attempt/timeline events to your own logging/metrics/tracing, implement `observe.Observer`.

Helpers:

- `observe.NoopObserver` (default)
- `observe.BaseObserver` (embed to implement only what you need)
- `observe.MultiObserver` (fan-out)

Per-attempt context contains `observe.AttemptInfo`:

```go
info, ok := observe.AttemptFromContext(ctx)
```

### Example observer

```go
type loggingObserver struct{ observe.BaseObserver }

func (loggingObserver) OnAttempt(ctx context.Context, key policy.PolicyKey, rec observe.AttemptRecord) {
	info, _ := observe.AttemptFromContext(ctx)
	_ = info
	// log key, rec.Attempt, rec.Err, rec.Outcome, rec.EndTime.Sub(rec.StartTime), ...
}
```

You can fan out to multiple observers with `observe.MultiObserver`.

## API cheat sheet

- Facade (`github.com/aponysus/recourse/recourse`)
  - `recourse.Do(ctx, key string, op)`
  - `recourse.DoValue[T](ctx, key string, op)`
  - `recourse.DoWithTimeline(ctx, key string, op)`
  - `recourse.DoValueWithTimeline[T](ctx, key string, op)`
  - `recourse.ParseKey(string) policy.PolicyKey`
- Advanced (`github.com/aponysus/recourse/retry`)
  - `retry.NewExecutor(opts...)`
  - `retry.NewExecutorFromOptions(opts)` (legacy)
  - `retry.DoValue[T](ctx, exec, key, op)`
  - `(*retry.Executor).Do(ctx, key, op)`
- Policies (`github.com/aponysus/recourse/policy`)
  - `policy.New(key, opts...)`
  - `policy.HTTPDefaults()`, `policy.DatabaseDefaults()`
  - `policy.MaxAttempts(n)`, `policy.Backoff(...)`
- Observability
  - `observe.RecordTimeline(ctx)`
  - `observe.TimelineCaptureFromContext(ctx)`
- Budgets (`github.com/aponysus/recourse/budget`)
  - `budget.NewRegistry()`
  - `budget.UnlimitedBudget{}`
  - `budget.NewTokenBucketBudget(capacity int, refillPerSecond float64)`

## Current status

Implemented:

- Policy keys + parsing (`policy.PolicyKey`, `policy.ParseKey`)
- Policy schema + normalization (`policy.EffectivePolicy`, `EffectivePolicy.Normalize`)
- Static policy provider (`controlplane.StaticProvider`)
- Retry executor with backoff/jitter and per-attempt/overall timeouts (`retry`)
- Timelines + observers (`observe.Timeline`, `observe.Observer`)
- Facade helpers that accept string keys (`recourse.Do*`)
- Outcome classifiers (protocol/domain-aware retry decisions)
- Budgets/backpressure (per-attempt gating via `policy.Retry.Budget` + `retry.ExecutorOptions.Budgets`, optional release semantics)

Planned (see `docs/roadmap.md`):

- Hedging (fixed-delay and latency-aware)
- Remote control-plane provider (caching + last-known-good)
- HTTP/gRPC integrations

## Project docs

- Roadmap: `docs/roadmap.md`
- Onboarding: `docs/onboarding.md`
- Extending: `docs/extending.md`

## Development

Run tests:

```bash
go test ./...
```

Build docs locally:

```bash
python -m venv .venv
source .venv/bin/activate
pip install -r requirements-docs.txt
mkdocs serve
```

## License

Apache-2.0. See `LICENSE`.
