# recourse

> recourse (n.): a source of help or strength.

`recourse` coordinates resilience for Go services. Retries, hedging, circuit breaking, timeouts, and budgets work together under a shared policy, with structured visibility into execution.

Call sites provide a stable **policy key**. Local or remote policies define how calls handle failures, slow responses, and additional load. Teams can tune that behavior consistently across services without rebuilding resilience controls at every call site.

New here? Start with [Design overview](design-overview.md), then move to [Getting started](getting-started.md).

## Why `recourse`?

A slow or failing dependency forces several decisions: whether to wait, start another attempt, reject the call, or limit additional work. Those decisions affect both latency and load.

`recourse` brings these controls into one execution path:

| What you need to control | What recourse provides |
| --- | --- |
| Transient failures | [Retries](concepts/policies.md) with bounded attempts, backoff, and classifiers that decide which outcomes warrant another attempt. |
| Slow responses | [Hedging](concepts/hedging.md) that can start additional attempts after a fixed delay or a latency-based trigger. |
| Repeated dependency failures | [Circuit breaking](concepts/circuit-breaking.md) that rejects calls while the circuit is open and probes for recovery after a cooldown. |
| Additional load | [Budgets](concepts/budgets.md) that allow or deny retry and hedge attempts. |
| Time spent on a call | Per-attempt and overall [timeouts](concepts/policies.md) configured through policy. |
| Incident diagnosis | [Timelines and observer hooks](concepts/observability.md) that expose attempt outcomes, timing, and execution decisions. |
<!-- Claim-ID: CLM-019 -->
<!-- Claim-ID: CLM-023 -->
<!-- Claim-ID: CLM-017 -->
<!-- Claim-ID: CLM-016 -->
<!-- Claim-ID: CLM-010 -->
<!-- Claim-ID: CLM-013 -->
<!-- Claim-ID: CLM-014 -->

The controls also account for one another. When a circuit breaker probes for recovery, hedging is disabled so the probe does not launch extra requests. Configured budgets limit the additional work from retries and hedges.

## What “policy-driven” means

Call sites supply a stable, low-cardinality **key**, such as `"payments.Charge"`. The provider resolves a policy for that key, and the executor applies its configured behavior:
<!-- Claim-ID: CLM-025 -->

- Attempt limits, backoff, and jitter.
- Hedge limits and triggers for starting concurrent attempts.
- Circuit breaker thresholds and recovery cooldowns.
- Per-attempt and overall timeouts.
- Classifier selection to interpret errors and results.
- Budgets to gate retries and hedges.

Policies can live in your application through `StaticProvider` or come from an external source through `RemoteProvider`. The remote provider caches policies, coalesces concurrent fetches, and offers opt-in last-known-good fallback during source failures. See [Remote Configuration](concepts/remote-configuration.md).
<!-- Claim-ID: CLM-018 -->

This separates the operation from its resilience configuration: the call site keeps doing its work while the policy defines how that work is protected.

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

This minimal example uses the default policy, which provides bounded retries. Hedging, circuit breaking, timeouts, and budget limits are configured explicitly. See [Getting started](getting-started.md) to install your policies and executor at startup.

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

When a call fails or takes longer than expected, you need to know which policy ran, which attempts were launched, and what stopped further work.

`recourse` captures a structured `observe.Timeline` (attempt timings, outcomes, budget decisions, errors) and can also stream attempt/timeline events to your own logging/metrics/tracing via `observe.Observer`.
<!-- Claim-ID: CLM-013 -->
<!-- Claim-ID: CLM-014 -->

## What’s inside

- **Policy keys**: stable, low-cardinality keys (`"svc.Method"`) that select behavior.
- **Policies + providers**: `policy.EffectivePolicy` resolved through in-process `controlplane.StaticProvider` or cached `controlplane.RemoteProvider` lookups.
- **Execution**: `retry.Executor` coordinates retries, hedging, circuit breaking, and timeouts according to policy.
- **Classifiers**: pluggable `(value, err) → Outcome` decisions to retry, stop, or abort based on protocol and domain semantics.
- **Budgets/backpressure**: gates on retry and hedge attempts to control additional load.
- **Observability**: structured `observe.Timeline` plus streaming hooks via `observe.Observer`.

## Where to go next

- [Design overview](design-overview.md) – decision-first intro and tradeoffs.
- [Getting started](getting-started.md) – install and first examples.
- [Gotchas & safety checklist](gotchas.md) – avoid common operational failures.
- [Adoption guide](adoption-guide.md) – staged rollout plan.
- [Incident debugging](incident-debugging.md) – timeline-based runbook.
- [Migrating from cenkalti/backoff](migration/from-cenkalti-backoff.md) – translate a familiar retry loop into governed retry policy.
- [API compatibility policy](reference/compatibility.md) – v1 stability contract.
- [Defaults and safety model](reference/defaults-safety.md) – generated defaults and failure modes.
- [Policy schema reference](reference/policy-schema.md) – generated field reference.
- [Reason codes & timeline fields](reference/reason-codes.md) – generated reference.
- [Changelog](https://github.com/aponysus/recourse/blob/main/CHANGELOG.md) – release history.
- Concepts:
  - [Policy keys](concepts/policy-keys.md)
  - [Key patterns and taxonomy](concepts/key-patterns.md)
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
