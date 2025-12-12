# Onboarding Guide for `rego`

Welcome! This guide is written for engineers who know Go but may not have deep experience with distributed systems or resilience design.

If you’re brand new to distributed systems concepts, read **[Distributed Systems Primer](distributed-systems-primer.md)** first.

---

## 1) What is `rego`?

`rego` is a **policy-driven, observability-first resilience library for Go**.

It aims to make network-call resilience:
- consistent (no hand-rolled retry loops),
- safe (timeouts, cancellation, conservative defaults),
- configurable (policy + control plane),
- observable (timelines + observers),
- extensible (classifiers, budgets, triggers).

At a high level it supports:
- Retries with bounded backoff and timeouts
- Hedging to reduce tail latency
- Budgets/backpressure to prevent retry storms
- Pluggable classifiers (protocol/domain-aware retryability)
- Optional control-plane integration for dynamic policies

---

## 2) How `rego` is organized (repo map)

The project is explicitly modular. Each subsystem is a package.

> **Dependency rule:** core packages should use the Go standard library only. Integrations that need third-party deps live under `integrations/` or dedicated subpackages.

### Package overview

| Package | What it does | Why it exists |
|---|---|---|
| `policy` | Policy schema + key parsing + normalization | Keeps config stable and dependency-free |
| `controlplane` | Policy providers (static and remote) | Allows policies to come from code, files, or a control plane |
| `retry` | The executor: retries and (later) hedging orchestration | The core engine |
| `classify` | Classifiers and registry | Decouple protocol/domain semantics from retry orchestration |
| `budget` | Budgets and registry | Backpressure to avoid retry/hedge storms |
| `hedge` | Hedge triggers + latency tracking | Controls *when* to hedge and bounds resource usage |
| `observe` | Timelines, attempt records, observer interface | Makes behavior inspectable and debuggable |
| `rego` | Thin facade + “happy path” helpers | Keeps public API ergonomic |

---

## 3) Reading order (how to get productive fast)

If you want to understand the system end-to-end, read in this order:

1. `00-project-summary.md` (vision + API baseline + core principles)
2. `phase_0_repo_setup.md` (package boundaries + dependency rules)
3. `phase_1_core_retry.md` (basic retry loop and policy model)
4. `phase_2_observability.md` (timelines + observer interface)
5. `phase_3_classifiers.md` (retry semantics via outcomes)
6. `phase_4_budgets.md` (storm prevention + release semantics)
7. `phase_5_basic_hedging.md` (fixed-delay hedging)
8. `phase_6_latency_hedging.md` (latency-aware triggers)
9. `phase_7_control_plane.md` (remote provider caching + LKG)
10. `phase_8_integrations_ergonomics.md` (default executor + HTTP/gRPC helpers)
11. `phase_9_hardening.md` (race/leak/bench/fuzz expectations)
12. `phase_10_docs_release.md` (docs + examples + API freeze)

---

## 4) The public API (what users will write)

The “happy path” is:

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

And when you want structured “what actually happened” data:

```go
user, tl, err := rego.DoValueWithTimeline[User](
    ctx,
    rego.ParseKey("user-service.GetUser"),
    op,
)

fmt.Printf("attempts=%d final_err=%v\n", len(tl.Attempts), tl.FinalErr)
```

### Key idea: call sites provide keys; policies decide behavior
A key maps a call site (or dependency operation) to a policy. A key is structured `{Namespace, Name}` but is usually written as `"svc.Method"`.

---

## 5) Policy keys: cardinality matters

**Keys must be low-cardinality.** Keys back caches and latency trackers, so they must not embed request-specific data like user IDs.

✅ Good keys:
- `rego.ParseKey("user-service.GetUser")`
- `rego.ParseKey("payments.Charge")`
- `rego.ParseKey("db.Users.Query")`

❌ Bad keys:
- `rego.ParseKey("user-service.GetUser?user_id=123")`
- `rego.ParseKey("GET /users/123")`
- `rego.ParseKey("payments.Charge:tenant=acme")`

**Rule of thumb:** if it can take on “millions of unique values”, it doesn’t belong in the key.

Put request-specific values into logs/traces/attributes instead.

---

## 6) What happens inside `rego.DoValue` (mental model)

A single call flows through these stages:

```
caller
  │
  ▼
executor (retry.Executor)
  │  1) fetch EffectivePolicy for key
  │  2) start timeline + observer.OnStart
  │
  ├─► attempt loop (retries)
  │     ├─► (optional) budget check for attempt
  │     ├─► run op with attempt context (timeouts + AttemptInfo)
  │     ├─► classify result -> Outcome
  │     ├─► record AttemptRecord + observer.OnAttempt
  │     ├─► decide: stop vs retry vs abort
  │     └─► sleep backoff (with jitter) unless ctx cancelled
  │
  ▼
finalize timeline + observer.OnSuccess/OnFailure
```

As features are enabled, this expands:
- classifiers determine retryability (Phase 3)
- budgets gate retries and hedges (Phase 4)
- hedging runs parallel attempts within a retry group (Phase 5/6)
- remote policy providers add caching and fallback behavior (Phase 7)

---

## 7) Observability: timelines and observers

If something goes wrong in production, you need answers like:

- How many attempts did we use?
- How much time was spent sleeping/backing off?
- Did we hedge? Did the hedge win?
- Did a budget deny additional attempts?
- Which classifier reason caused us to stop?

`rego` answers these via:
- `observe.Timeline` (returned from `Do*WithTimeline`)
- `observe.Observer` callbacks (for logging/metrics/tracing)

### AttemptInfo in context
Per-attempt context is augmented with attempt metadata so downstream code can tag logs/traces with attempt indices.

---

## 8) “Footguns” and how the design avoids them

This project explicitly tries to avoid common failure patterns:

### Footgun: retrying non-idempotent operations
Avoid by:
- requiring domain-aware classifiers (HTTP/gRPC)
- default behavior that only retries safe cases in integrations

### Footgun: retry storms
Avoid by:
- bounded attempts
- exponential backoff + jitter
- budgets/backpressure

### Footgun: hedging creates overload
Avoid by:
- limiting max hedges
- scheduling trigger checks (no busy loops)
- separate hedge budgets
- correct cancellation of losing attempts

### Footgun: high-cardinality keys blow up memory + metrics
Avoid by:
- documenting low-cardinality requirements
- adding eviction/limits for trackers and caches
- warning in hardening phase when thresholds are exceeded

---

## 9) Development workflow (how to work in this repo)

### Running tests
Recommended local commands:

```bash
go test ./...
go vet ./...
go test -race ./...
```

### Benchmarks and stress tests
The hardening plan calls for:
- `go test -bench . -benchmem`
- fuzz/stress tests for randomized timing/error patterns
- explicit goroutine leak checks in hedging paths

When you change concurrency-related code, always run with `-race`.

---

## 10) How to make a contribution safely

When you implement or change behavior, keep these invariants in mind:

1. **Context cancellation must always work**
   - cancelling the overall `ctx` should stop future sleeps/attempts quickly
   - per-attempt timeouts should not exceed overall timeouts

2. **No goroutine leaks**
   - especially in hedging coordination paths
   - if an attempt loses, it must be cancelled and allowed to exit

3. **Observer ordering must be stable**
   - `OnStart` once, then `OnAttempt` per attempt, then exactly one of `OnSuccess`/`OnFailure`

4. **Missing components must fail safely**
   - missing policy/classifier/budget/trigger must follow configured failure modes
   - fallbacks must be visible in timelines/attributes

5. **Core must remain stdlib-only**
   - keep third-party dependencies in integration packages

---

## 11) Starter exercises (recommended for new devs)

If you’re new to resilience libraries, these tasks build intuition:

1. Add a test for “cancel before first attempt” (zero attempts executed).
2. Add a test for “cancel during backoff sleep”.
3. Implement a tiny observer that prints attempt timelines and reasons.
4. Write a custom classifier for a domain-specific error type.
5. Implement a toy budget that denies attempts after N and verify release semantics.
6. (Advanced) Write a simple hedge trigger and test that it doesn’t busy-loop.

---

## 12) Where to go next

- **[Distributed Systems Primer](distributed-systems-primer.md)** – key concepts (idempotency, backoff, budgets, hedging).
- **[Extending `rego`](extending.md)** – how to write custom classifiers/budgets/triggers/observers.
