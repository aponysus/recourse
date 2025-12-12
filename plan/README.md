# rego

`rego` is a **policy-driven, observability-first resilience library for Go**.

It provides:
- **Retries** with bounded backoff and timeouts
- **Hedging** to reduce tail latency
- **Budgets/backpressure** to prevent retry storms
- **Pluggable classifiers** so retryability is protocol/domain-aware
- **Control-plane integration** so policies can be tuned without redeploys

If you’re new to resilient/distributed systems, start with:
- **[Distributed Systems Primer](docs/distributed-systems-primer.md)**
- **[Onboarding](docs/onboarding.md)**

---

## Quick start

```go
exec := rego.NewDefaultExecutor()

user, err := rego.DoValue[User](
    ctx,
    rego.ParseKey("user-service.GetUser"),
    func(ctx context.Context) (User, error) {
        return client.GetUser(ctx, userID)
    },
)
```

For structured observability:

```go
user, tl, err := rego.DoValueWithTimeline[User](
    ctx,
    rego.ParseKey("user-service.GetUser"),
    op,
)
_ = tl // inspect tl.Attempts, tl.FinalErr, etc.
```

---

## Key concepts

### Policy keys
Call sites provide a **low-cardinality key** (e.g. `"svc.Method"`). Policies decide behavior for that key.

**Do not embed IDs** (user_id, tenant_id, request_id) in keys.

### Policies
Policies control:
- max attempts, timeouts, backoff/jitter
- classifier selection
- budgets for retries/hedges
- hedging behavior and triggers

### Classifiers
Classifiers decide if an attempt is:
- success
- retryable
- non-retryable
- abort (stop immediately)

HTTP/gRPC classifiers make retry decisions protocol-aware.

### Budgets
Budgets gate attempts to prevent retry/hedge storms.

### Hedging
Hedging runs parallel attempts inside a retry group to reduce tail latency.

### Observability
Every attempt produces structured data. Observers can export it to logs/metrics/tracing.

---

## Control-plane usage (conceptual)

A remote provider can fetch policies at runtime and cache them with TTL and “last known good” fallback.

This lets you tune retry behavior in incidents without redeploys.

---

## Non-goals (v1.0)

`rego` intentionally does **not** ship a circuit breaker in v1.0. It should compose cleanly with external circuit breakers.

---

## Documentation

- [Distributed Systems Primer](docs/distributed-systems-primer.md)
- [Onboarding](docs/onboarding.md)
- [Extending rego](docs/extending.md)

---

## License

(TBD)
