# Retries are policy, not control flow

A retry is not free.

Every extra attempt is more concurrency, more sockets, more CPU, and more pressure on the dependency that is already unhappy.

That means retry behavior is not just local control flow. It is operational policy.

The moment you add retries, you are making a promise:

- attempts are bounded
- failures are classified correctly
- backoff is deliberate
- load is gated when a dependency is struggling
- behavior is explainable during an incident
<!-- Claim-ID: CLM-019 -->
<!-- Claim-ID: CLM-023 -->
<!-- Claim-ID: CLM-010 -->
<!-- Claim-ID: CLM-013 -->
<!-- Claim-ID: CLM-014 -->

`recourse` exists to make that promise repeatable in Go services.

It gives call sites a boring API and moves resilience decisions into a policy-keyed envelope: retries, timeouts, budgets, hedging, circuit breaking, classifiers, and timelines.
<!-- Claim-ID: CLM-019 -->
<!-- Claim-ID: CLM-010 -->
<!-- Claim-ID: CLM-017 -->
<!-- Claim-ID: CLM-016 -->
<!-- Claim-ID: CLM-023 -->
<!-- Claim-ID: CLM-013 -->

As of `v1.2.0`, the core API is covered by the `v1.x` compatibility guarantee, and optional integrations such as gRPC and OpenTelemetry live in separate modules so the root module stays small.
<!-- Claim-ID: CLM-020 -->

## Who this is for

`recourse` is not for every retry.

If you need one small retry loop at one or two call sites, a focused backoff helper is probably the right tool. Keep the dependency small. Keep the behavior local. There is no virtue in turning a simple call into a platform.

`recourse` is for the point where retry behavior starts becoming an operating concern:

- multiple services have copied slightly different retry loops
- teams disagree about which failures are retryable
- incidents are made worse by retry amplification
- nobody can tell what happened on each attempt
- retry, timeout, circuit, and budget behavior needs to be governed consistently

That is the line where retries stop being a helper function and start being policy.

## The core move: the call site picks a key rather than a mechanism

Most retry libraries start with mechanism:

- how many attempts?
- which backoff?
- which errors?
- which timeout?

`recourse` starts from a different primitive:

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

The call site names the operation. The policy defines the envelope.

That envelope can include:

- retry limits and backoff
- per-attempt and overall timeouts
- budgets as backpressure gates
- hedging configuration
- circuit breaking configuration
<!-- Claim-ID: CLM-019 -->
<!-- Claim-ID: CLM-010 -->
<!-- Claim-ID: CLM-017 -->
<!-- Claim-ID: CLM-016 -->

Classification is handled separately by classifiers that map `(value, err)` into an `Outcome`.
<!-- Claim-ID: CLM-023 -->

That distinction matters. Call sites should not have to re-implement resilience semantics every time they call another service.

## Keys must be low-cardinality or the model breaks

Once keys select policy, they also become observability dimensions. They feed caches, breakers, budgets, latency trackers, metrics, traces, and logs.

So keys must be stable and low-cardinality.

Good keys:

- `"payments.Charge"`
- `"user-service.GetUser"`
- `"db.Users.Query"`

Bad keys:

- `"GET /users/123"`
- `"user-service.GetUser?user_id=123"`
- `"payments.Charge:tenant=acme"`

A policy key is not a request label. It is the name of an operation class.

Dynamic data belongs in logs, traces, structured fields, or application-level diagnostics. It does not belong in the policy key.

ADR-001 is explicit about this because a lot of the system assumes keys are safe to aggregate on.

## Policies are untrusted input, so normalize them

If policy is data, policy can be wrong.

That has to be treated as a real failure mode.

Every `EffectivePolicy` is normalized and clamped through `EffectivePolicy.Normalize()`:

- unsafe or missing values are pushed into documented ranges
- normalization metadata is recorded
<!-- Claim-ID: CLM-003 -->

Policy resolution failure is also explicit. By default, `recourse` fails closed with `FailureDeny` and returns `retry.NoPolicyError`; callers can check it with `errors.Is(err, retry.ErrNoPolicy)`. If a service wants different behavior, it can opt into a single-attempt allow mode or a fallback policy.
<!-- Claim-ID: CLM-002 -->

That is not ceremony. It is a guardrail against accidental busy loops, runaway attempts, and configuration mistakes becoming incidents.

## Failure semantics belong in classifiers

A timeout, a 429, a 404, a connection reset, and quota exhaustion are not the same operational event.

Treating them the same is how retry code causes self-inflicted outages.

`recourse` uses classifiers. A classifier maps `(value, err)` into an `Outcome`, and the executor uses that outcome to decide whether to retry, stop, or abort.
<!-- Claim-ID: CLM-023 -->

Built-ins include:

- `classify.AutoClassifier`, which dispatches to HTTP semantics when the error implements `HTTPError` and otherwise falls back to retry-on-error
- `classify.HTTPClassifier`, which understands idempotent HTTP methods, transport errors, 5xx, 408/429, configured extra 4xx, and `Retry-After`
- a gRPC classifier in the gRPC integration module that interprets gRPC status codes and delegates non-gRPC errors
<!-- Claim-ID: CLM-005 -->
<!-- Claim-ID: CLM-006 -->
<!-- Claim-ID: CLM-007 -->

The important part is not that these classifiers exist. The important part is that retryability is a named semantic decision, not an accidental side effect of `if err != nil`.

## Backpressure is part of the retry contract

Retries and hedges multiply load.

Without explicit backpressure, a small dependency problem can become a retry storm.

Budgets in `recourse` provide a per-attempt gate:

- allow the attempt
- deny the attempt and record why
- optionally return a release handle for reservation-style resources
<!-- Claim-ID: CLM-010 -->

Built-ins include:

- `budget.UnlimitedBudget`
- `budget.TokenBucketBudget`
<!-- Claim-ID: CLM-011 -->

Budget failure modes are explicit and observable:

- empty budget name is allowed with reason `"no_budget"`
- missing registry, missing budget, and nil budget are controlled by `MissingBudgetMode`
- the default for missing budget dependencies is fail-closed
<!-- Claim-ID: CLM-012 -->

If a dependency is already overloaded, retrying harder is often the wrong answer. Budgets are how `recourse` makes that part of the contract.

## Hedging is tail-latency tooling, not just parallel retries

Average latency can look fine while p95 and p99 latency dominate user experience.

`recourse` supports hedging: starting another attempt while the first is still in flight, then returning the first successful result.

Supported modes include:

- fixed-delay hedging
- latency-aware hedging based on recent per-key latency stats

The behavior is explicit:

- first success wins and cancels the group context
- `CancelOnFirstTerminal` can stop the group on a non-retryable outcome
- hedges can use their own budget separate from normal retry attempts
<!-- Claim-ID: CLM-017 -->

Hedging is powerful and dangerous. It only belongs in systems with budgets, cancellation, and observability.

## Circuit breaking belongs in the same envelope

Retries help when failures are transient.

When failures are persistent, retries are waste.

`recourse` includes a consecutive-failure circuit breaker with standard states:

- closed
- open
- half-open

When the circuit is open, calls fail fast with `CircuitOpenError`. After cooldown, the breaker allows limited half-open probes. Success closes the circuit; failure reopens it.

Because circuit breaking is integrated with the rest of the envelope, the behavior can be coordinated. For example, hedging is disabled during half-open probing so a recovering dependency is not hammered by parallel probes.
<!-- Claim-ID: CLM-016 -->

The goal is not to bolt several resilience mechanisms together. The goal is to make them cooperate.

## Explainability: timelines and observer hooks

Retry behavior is invisible unless you deliberately surface it.

During an incident, “the call failed” is not enough.

You usually need to know:

- how many attempts happened
- why each attempt was retryable or terminal
- whether budget/backpressure allowed the attempt
- how long each attempt took
- what backoff was chosen
- whether the final failure was exhaustion, cancellation, denial, or a terminal classification

`recourse` gives you two complementary paths.

### Timeline capture

For debugging, capture an `observe.Timeline` on demand:

```go
ctx, capture := observe.RecordTimeline(ctx)

user, err := recourse.DoValue(ctx, "user-service.GetUser", op)

tl := capture.Timeline()
for _, a := range tl.Attempts {
    // a.Outcome, a.Err, a.Backoff, a.IsHedge, a.BudgetAllowed, ...
}
```

A representative timeline might tell this story:

```text
attempt=0 reason=http_503 budget_allowed=true backoff=50ms err=upstream 503
attempt=1 reason=http_503 budget_allowed=true backoff=100ms err=upstream 503
attempt=2 reason=success budget_allowed=true backoff=0s err=<nil>
```

The timeline records per-attempt timings, outcomes, errors, backoff decisions, and budget gating, plus call-level attributes when present.
<!-- Claim-ID: CLM-013 -->

### Streaming observers

For logs, metrics, and tracing integrations, implement `observe.Observer`.

The OpenTelemetry integration lives in `integrations/otel` as a separate module, so tracing support is available without adding OTel dependencies to the core library. It can record attempts as span events by default, or as child spans when you want per-attempt trace structure.

Observers receive lifecycle events, attempt events, hedge spawn events, and budget decision events.
<!-- Claim-ID: CLM-014 -->

The OpenTelemetry integration is provided as a separate module, so tracing support is available without adding OpenTelemetry dependencies to the root module.

The point is simple: `recourse` does not just retry. It tells you what it did.

## Remote configuration fails explicitly

Once retry behavior is policy-driven, runtime policy updates become the obvious next step.

`recourse` supports a `controlplane.RemoteProvider` that fetches policies from an external source and caches them:

- TTL caching for fetched policies
- negative caching for not-found policies
- explicit fallback behavior through `MissingPolicyMode` when the source is unavailable
<!-- Claim-ID: CLM-018 -->
<!-- Claim-ID: CLM-002 -->

A control plane outage should not turn into mysterious retry behavior. Remote policy resolution has to fail visibly and deliberately.

## Integrations should be useful without becoming a framework

Go libraries should not force a framework on users just to make a service call.

`recourse` keeps the root module light and pushes heavier dependencies into separate integration modules.

The integration philosophy is:

- target standard interfaces such as `net/http` and gRPC interceptors
- keep optional dependencies optional
- handle correctness details that are easy to get wrong at every call site
- preserve observability through the same policy/timeline model

For example, the HTTP integration wraps transport errors and non-2xx responses as `StatusError` so HTTP-aware classifiers can interpret status codes and `Retry-After`. It also drains and closes failed response bodies so connections can be reused.
<!-- Claim-ID: CLM-008 -->
<!-- Claim-ID: CLM-006 -->

The gRPC and OpenTelemetry integrations live in separate modules so users can opt into their dependency surface deliberately.

## Adoption should be incremental

You do not need a rewrite to use `recourse`.

A reasonable adoption path is:

1. Start with one idempotent or otherwise retry-safe call site.
2. Give it a stable low-cardinality key.
3. Use the facade API with the default policy to observe behavior.
4. Capture a timeline in tests or staging.
5. Move to explicit executors and policies when the call site needs standardized behavior.
6. Add budgets before scaling retries or hedging broadly.
7. Introduce provider-backed policy only after the governance model is clear.

The call sites stay boring. The policy envelope becomes the place where reliability decisions live.

## Closing thought

Retries are not a feature. They are an operational commitment.

`recourse` tries to make that commitment explicit: policy-keyed control, bounded envelopes, protocol-aware classification, backpressure, hedging, circuit breaking, and observability that tells the truth about what happened.

That is the model I want feedback on from Go teams: not whether another retry helper should exist, but whether retry behavior deserves to be governed as policy once it crosses service boundaries.

For a decision-focused intro, start with [Design overview](../design-overview.md).
Then see [Getting started](../getting-started.md), [Adoption guide](../adoption-guide.md), [Gotchas](../gotchas.md), [Incident debugging](../incident-debugging.md), and [Migration from cenkalti/backoff](../migration/from-cenkalti-backoff.md).
