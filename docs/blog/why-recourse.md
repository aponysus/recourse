# Retries are load multipliers: policy-keyed resilience for Go

!!! note "Docs readers start here"
    For a decision-focused intro, start with [Design overview](../design-overview.md) and [Getting started](../getting-started.md).

A retry is not free. Every extra attempt is more concurrency, more sockets, more CPU, and more pressure on the dependency that is already unhappy.

The moment you add retries you are making an operational promise:

- attempts are bounded
- failures are classified correctly
- load is gated when a dependency is struggling
- behavior is explainable during an incident

recourse exists to make that promise repeatable in Go services. Call sites supply a stable policy key, and recourse executes a policy envelope for that key: retries, timeouts, budgets, hedges, circuit breaking, and observability.

If you have seen my Python library redress, think of recourse as the Go counterpart that leans harder into "control plane" ideas: central envelopes keyed by operation, plus backpressure and tail-latency tooling.

## The core move: the call site picks a key, not a mechanism

Most retry libraries start from "how many attempts" and "which backoff." recourse starts from a different primitive:

**A low-cardinality policy key is the unit of control.**

```go
user, err := recourse.DoValue[User](
    ctx,
    "user-service.GetUser",
    func(ctx context.Context) (User, error) {
        return client.GetUser(ctx, userID)
    },
)
```

That key selects the envelope, which can include:

- retry limits and backoff + jitter
- per-attempt and overall timeouts
- a classifier (how to interpret values and errors)
- budgets (backpressure gates)
- hedging configuration
- circuit breaking configuration
- observer hooks and timeline capture

The point is to keep call sites boring. You should not re-implement resilience semantics in every package that calls an API.

## Design principle: keys must be low-cardinality or nothing scales

Once keys drive policy selection, they also drive observability dimensions, caches, breakers, and latency trackers. High-cardinality keys are a reliability bug. They blow up memory and produce unusable telemetry.

Good keys:

- "payments.Charge"
- "user-service.GetUser"

Bad keys:

- "GET /users/123"
- "user-service.GetUser?user_id=123"

ADR-001 is explicit about this because a lot of recourse assumes keys are safe to aggregate on.

## Design principle: policies are untrusted input, so normalize them

If policy is data, then policy can be wrong.

recourse treats this as a first-class failure mode. Every `EffectivePolicy` is normalized/clamped via `EffectivePolicy.Normalize()`:

- fill missing values with safe defaults
- clamp values to documented safe ranges
- reject invalid enum values
- record normalization metadata for observability

If policy resolution fails, behavior is explicit. By default, recourse fails closed (`FailureDeny`) with `retry.ErrNoPolicy`, but you can opt into "single attempt" (`FailureAllow`) or a safe fallback policy (`FailureFallback`).

This is a guardrail. It prevents accidental busy loops and storms even when configuration is imperfect.

## Design principle: semantics come from classifiers, not heuristics

A timeout, a 429, and a 404 are not the same thing. Treating them the same is how you get self-inflicted incidents.

recourse uses **classifiers**: a classifier maps `(value, err)` into an `Outcome`, and the executor uses that outcome to decide whether to retry, stop, or abort.

Built-ins include:

- `classify.AutoClassifier` (default): dispatches based on error type (for example HTTP-aware when using the HTTP integration)
- `classify.ClassifierHTTP`: understands HTTP semantics (5xx vs 404 vs 429 plus `Retry-After`)
- a gRPC classifier in the gRPC integration module that interprets gRPC status codes

A key safety rule: type mismatches should fail loudly and safely, not degrade into "retry blindly." If a classifier expects one shape and receives another, the conservative answer is non-retryable or abort, with a clear reason.

## Design principle: backpressure is part of the retry contract

Retries and hedges multiply load. Without explicit backpressure, a small incident becomes a retry storm.

Budgets in recourse provide a per-attempt gate:

- allow the attempt
- deny the attempt (and record why)
- optionally return a release handle for reservation-style resources

There are built-ins like:

- `budget.UnlimitedBudget` (always allows)
- `budget.TokenBucketBudget` (capacity plus refill rate)

Failure modes are explicit and show up in observability:

- empty budget name -> allowed with reason "no_budget"
- missing registry / missing budget / nil budget -> controlled by `MissingBudgetMode` (default is fail-closed) and recorded as "budget_registry_nil", "budget_not_found", or "budget_nil"

If you care about outage safety, budgets are the difference between "retries helped" and "retries amplified the blast radius."

## Tail latency is reliability: hedging

Even if average latency looks fine, p99 and p999 spikes can dominate user experience and upstream timeouts.

recourse supports hedging: starting a second (or third) attempt while the first is still in flight, racing them so the first success wins.

Two modes:

- Fixed delay: spawn a hedge after N milliseconds
- Latency-aware: spawn a hedge dynamically based on recent latency stats (for example p99), using a per-key tracker

Behavior is explicit:

- winner takes all: first success cancels other in-flight attempts
- fail-fast option: `CancelOnFirstTerminal` can stop the group on a non-retryable error; otherwise, other in-flight attempts can still win
- budgets: hedges can have their own budget (`Hedge.Budget`) independent of the retry budget

Hedging is powerful and dangerous. It only belongs in a system that has budgets and explainability.

## When it is failing, stop talking to it: circuit breaking

Retries help when failures are transient. When failures are persistent, retries are waste.

recourse includes a consecutive-failure circuit breaker with standard states:

- Closed: normal operation, record successes and failures
- Open: fail fast with `CircuitOpenError`
- Half-open: after cooldown, allow limited probes; success closes the circuit, failure re-opens it

One important detail: circuit breaking is integrated with the rest of the envelope. For example, hedging is disabled in half-open probing to avoid hammering a recovering dependency.

## Explainability: timelines and observer hooks

Retries hide behavior unless you surface it deliberately. If you cannot answer "what happened on each attempt, and why", you do not really control your retry system.

recourse gives you two complementary paths:

### Timeline capture (best for debugging)

You can capture an `observe.Timeline` on demand:

```go
ctx, capture := observe.RecordTimeline(ctx)

user, err := recourse.DoValue(ctx, "user-service.GetUser", op)

// After the call:
tl := capture.Timeline()
for _, a := range tl.Attempts {
    // a.Outcome, a.Err, a.Backoff, a.IsHedge, a.BudgetAllowed, ...
}
```

The timeline records per-attempt timings, outcomes, errors, backoff decisions, and budget gating, plus call-level attributes (like policy source and whether normalization happened).

### Streaming observers (best for metrics/logging/tracing)

For logs, metrics, and tracing integrations, implement `observe.Observer`. The observer receives lifecycle events, attempt events, hedge spawn events, and budget decision events with standardized reasons like "budget_denied" or "circuit_open".

The goal is simple: recourse does not just retry. It tells you exactly what it did.

## Operating model: remote configuration that fails safely

Once you accept policy-driven behavior, runtime policy updates become the obvious next step.

recourse supports a `controlplane.RemoteProvider` that fetches policies from an external source and caches them:

- TTL caching for fetched policies
- negative caching for not-found policies (for example 404) to prevent hot-spotting on missing keys
- explicit fallback behavior via `MissingPolicyMode` when the source is unavailable

Remote config is not useful if a control plane outage turns into a service outage. The provider and executor semantics exist to make partial failure survivable and explicit.

## Integration philosophy: drop-in, correctness-focused

In Go, resilience libraries often become frameworks with heavy wrappers. recourse explicitly tries not to do that:

- integrations target standard interfaces (`net/http`, gRPC interceptors) rather than forcing custom clients
- the core library stays light; heavy dependencies live in separate modules
- helpers handle correctness edge cases (like draining and closing response bodies on HTTP retries so connections can be reused)

For example, the HTTP integration (`integrations/http`) provides a `DoHTTP` helper that classifies HTTP status codes, respects `Retry-After` where relevant, and manages response bodies for failed attempts.

## How to adopt recourse without a rewrite

recourse is designed for incremental adoption:

- Start by using the facade API with a stable key (`Do` / `DoValue`).
- Capture timelines when debugging matters.
- Move to explicit executors, registries, and providers when you want a real control plane and standardized policies.

The call sites stay boring. The policy envelope becomes the place where reliability decisions live.

## Closing thought

Retries are not a feature. They are an operational commitment.

recourse tries to make that commitment explicit: policy-keyed control, bounded envelopes, protocol-aware semantics, backpressure, tail-latency tooling, and observability that tells the truth about what happened.
