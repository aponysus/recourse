# Extending `rego`

This document explains how to extend `rego` safely and how the registry-based extension points are intended to work.

If you’re brand new, read **[Onboarding](onboarding.md)** first.

---

## Extension points (high level)

`rego` is designed around “pluggable” subsystems:

- **Classifiers** (`classify`): decide whether an outcome is retryable vs not
- **Budgets** (`budget`): gate retries/hedges to prevent storms
- **Hedge triggers** (`hedge`): decide when to spawn hedges (and how often to re-check)
- **Observers** (`observe`): export timelines/attempt events to logs/metrics/traces

The executor (`retry.Executor`) depends on these via interfaces and registries so the public API can stay simple while internals remain composable.

---

## Registries (the pattern)

Registries are thread-safe maps from string names to implementations.

Common characteristics:
- `Register(name, impl)` is called at startup.
- `Get(name)` resolves per call based on policy.
- Implementations should be safe for concurrent use.

### Naming conventions
Names are stable API. Treat them like config identifiers:
- prefer short lowercase names like `"http"`, `"unlimited"`, `"fixed_delay"`
- avoid embedding version numbers unless you truly need distinct behavior

---

## Writing a custom classifier

### What classifiers do
A classifier turns `(value, err)` into an `Outcome`:

- success
- retryable
- non-retryable
- abort

**Important:** classification should reflect *domain semantics*.
Example: in HTTP, `404` is usually non-retryable while `429` is often retryable.

### Skeleton example

```go
type MyClassifier struct{}

func (MyClassifier) Classify(value any, err error) classify.Outcome {
    if err == nil {
        return classify.Outcome{Kind: classify.OutcomeSuccess}
    }

    // Example: your domain might expose typed errors.
    var e *MyDomainError
    if errors.As(err, &e) {
        if e.Code == "INVALID_ARGUMENT" {
            return classify.Outcome{
                Kind:   classify.OutcomeNonRetryable,
                Reason: "invalid_argument",
            }
        }
    }

    return classify.Outcome{Kind: classify.OutcomeRetryable, Reason: "default_retry"}
}
```

### Registering it

```go
reg := classify.NewRegistry()
classify.RegisterBuiltins(reg)
reg.Register("my_domain", MyClassifier{})
```

Then select it via policy (e.g., `RetryPolicy.ClassifierName = "my_domain"`), or via an integration helper that chooses it by default.

### Testing guidance
Test classifiers as pure functions:
- table-driven tests for each important error/value case
- ensure context cancellation errors result in `Abort` (so loops stop quickly)

---

## Writing a custom budget

Budgets are consulted **per attempt** and can deny retries and/or hedges.

### Interface overview
Budgets receive:
- key
- attempt index (monotonic launch order)
- attempt kind (retry vs hedge)
- a policy reference (name + optional cost)

Budgets return a decision:
- Allowed bool
- Reason string
- optional Release() func

### Skeleton example

```go
type NAttemptsBudget struct {
    mu sync.Mutex
    n  int
    max int
}

func (b *NAttemptsBudget) AllowAttempt(
    ctx context.Context,
    key policy.PolicyKey,
    attemptIdx int,
    kind budget.AttemptKind,
    ref policy.BudgetRef,
) budget.Decision {
    b.mu.Lock()
    defer b.mu.Unlock()

    if b.n >= b.max {
        return budget.Decision{Allowed: false, Reason: "budget_denied"}
    }

    b.n++
    return budget.Decision{
        Allowed: true,
        Reason:  "allowed",
        Release: func() {
            // Optional: reservation-style budget might release capacity here.
        },
    }
}
```

### Critical rule: Release must be safe
If you return a `Release` function:
- it must be safe to call exactly once
- it must tolerate being called even when the attempt exits via cancellation/timeouts
- it should not block for long (avoid introducing deadlocks)

### Testing guidance
- verify denied attempts do not run the operation at all
- verify release is called exactly once per allowed attempt

---

## Writing a hedge trigger

Hedge triggers decide:
- should we spawn a hedge now?
- when should we check again?

This “schedule-aware” design is important: it prevents tight polling loops.

### Trigger contract (simplified)
A trigger gets a `HedgeState` containing:
- how long since call start
- number of attempts already launched
- max hedges allowed
- (later) latency statistics snapshot

It returns:
- `shouldSpawn`
- `nextCheckIn` duration

### Guidance for safe triggers
- Do not busy-loop: always return a non-zero-ish next check interval
- Respect max hedges: avoid returning `shouldSpawn=true` repeatedly without waiting
- Be conservative when stats are missing: don’t hedge aggressively with little data

### Testing guidance
- unit-test triggers by feeding synthetic `HedgeState`
- include tests for “not enough samples” in latency-aware triggers

---

## Writing an observer

Observers are called by the executor to stream events and final timelines.

### Concurrency expectations
Observers must assume:
- callbacks may be called concurrently (especially once hedging exists)
- callbacks should be fast and non-blocking
- observers should not panic (but the executor can optionally recover panics when configured)

### Common patterns
- **LoggingObserver**: print attempt records and final timeline
- **MetricsObserver**: record counts/latency histograms by key and outcome reason
- **TracingObserver**: annotate traces with attempt indices and reasons

### Testing guidance
Build a test observer that records calls and assert:
- `OnStart` exactly once
- `OnAttempt` called once per attempt record
- `OnSuccess` or `OnFailure` exactly once

---

## Where to go next

- **[Onboarding](onboarding.md)** – mental model + repo map.
- **[Distributed Systems Primer](distributed-systems-primer.md)** – why these patterns exist.
