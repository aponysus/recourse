# recourse

> recourse (n.): a source of help or strength.

Policy-driven, observability-first resilience library for Go services.

New here? Start with [Design overview](design-overview.md), then move to [Getting started](getting-started.md).

## Why `recourse`?

Retry logic is deceptively easy to write and notoriously hard to operate.

- **Resilience code drifts**: each service ends up with slightly different retry semantics, timeouts, logging, and metrics.
- **Naive retries amplify outages**: retries turn “a little latency” into “a lot more load” unless there is explicit backpressure.
- **Protocol/domain semantics matter**: a timeout, a 429, and a 404 should not all be treated the same.
- **Debuggability is non-negotiable**: when an incident happens, you need to answer “what happened on each attempt, and why?”.

`recourse` centralizes resilience behavior behind a low-cardinality **policy key** and makes every decision observable.

Concretely, `recourse` gives you:

- **Deterministic envelopes**: bounded attempts, bounded backoff, and explicit timeouts.
- **Domain-aware retry decisions**: pluggable classifiers (instead of “retry on any error”).
- **Backpressure**: per-attempt budgets to prevent retry storms.
- **Structured observability**: timelines and hooks that make behavior explainable in production.

## The problem with ad-hoc retries

This is a common shape:

```go
var lastErr error
for attempt := 0; attempt < 3; attempt++ {
	if err := callDependency(ctx); err == nil {
		return nil
	} else {
		lastErr = err
	}
	time.Sleep(time.Duration(attempt+1) * 50 * time.Millisecond)
}
return lastErr
```

But production requirements pile up quickly:

- Is the error retryable (HTTP 500 vs 404 vs 429)? Should we treat `context.Canceled` differently?
- Are we respecting **per-attempt** timeouts vs **overall** timeouts?
- Are we emitting consistent logs/metrics/traces across *all* call sites?
- Do we have **backpressure** so retries don’t turn an outage into a storm?
- When this goes wrong at 3am, can we reconstruct the exact sequence of attempts and decisions?

## What “policy-driven” means

In `recourse`, call sites supply a **key** (e.g., `"payments.Charge"`). Policies decide the retry envelope for that key:

- maximum attempts
- backoff/jitter
- per-attempt and overall timeouts
- classifier selection (how to interpret errors/results)
- optional budgets/backpressure (whether to allow each attempt)

This makes behavior consistent, tunable, and observable without re-implementing retry loops everywhere.

## Quick start

The facade API takes a string key like `"user-service.GetUser"`:

```go
package main

import (
	"context"

	"github.com/aponysus/recourse/recourse"
)

type User struct{ ID string }

func main() {
	user, err := recourse.DoValue[User](context.Background(), "user-service.GetUser", func(ctx context.Context) (User, error) {
		// call dependency here
		return User{ID: "123"}, nil
	})
	_ = user
	_ = err
}
```

When you need to know what happened, request a timeline:

```go
ctx, capture := observe.RecordTimeline(ctx)
user, err := recourse.DoValue(ctx, "user-service.GetUser", op)
_ = user
_ = err

tl := capture.Timeline()
for _, a := range tl.Attempts {
	// a.Attempt, a.Outcome, a.BudgetAllowed, a.Backoff, a.Err, ...
}
```

## Observability-first

Retries are only “safe” if they are observable.

`recourse` captures a structured `observe.Timeline` (attempt timings, outcomes, budget decisions, errors) and can also stream attempt/timeline events to your own logging/metrics/tracing via `observe.Observer`.

## What’s inside

- **Policy keys**: stable, low-cardinality keys (`"svc.Method"`) that select behavior.
- **Policies + providers**: `policy.EffectivePolicy` resolved via `controlplane.PolicyProvider` (today: in-process static).
- **Retry executor**: bounded attempts with backoff/jitter and per-attempt/overall timeouts.
- **Classifiers**: pluggable `(value, err) → Outcome` so retry decisions are protocol/domain-aware.
- **Budgets/backpressure**: per-attempt gates to prevent retry storms (with optional release semantics).
- **Observability**: structured `observe.Timeline` plus streaming hooks via `observe.Observer`.

## Where to go next

- [Design overview](design-overview.md) – decision-first intro and tradeoffs.
- [Getting started](getting-started.md) – install and first examples.
- [Gotchas & safety checklist](gotchas.md) – avoid common operational failures.
- [Adoption guide](adoption-guide.md) – staged rollout plan.
- [Incident debugging](incident-debugging.md) – timeline-based runbook.
- [Policy schema reference](reference/policy-schema.md) – generated field reference.
- [Reason codes & timeline fields](reference/reason-codes.md) – generated reference.
- [Changelog](https://github.com/aponysus/recourse/blob/main/CHANGELOG.md) – release history.
- Concepts:
  - [Policy keys](concepts/policy-keys.md)
  - [Policies & providers](concepts/policies.md)
  - [Classifiers](concepts/classifiers.md)
  - [Observability](concepts/observability.md)
  - [Budgets & backpressure](concepts/budgets.md)
  - [Hedging](concepts/hedging.md)
  - [Circuit Breaking](concepts/circuit-breaking.md)
  - [Remote Configuration](concepts/remote-configuration.md)
  - [Integrations](concepts/integrations.md)
- Architecture decisions:
  - [ADR 001: Low-cardinality policy keys](adr/001-low-cardinality-keys.md)
  - [ADR 003: Policy normalization](adr/003-policy-normalization.md)
- [Extending](extending.md) – write custom classifiers/budgets/observers.
