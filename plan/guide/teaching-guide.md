# rego Teaching Guide

This guide is meant to onboard a Go developer to the `rego` project: what it is, why it exists, how the architecture fits together, and how the phased plan builds the system. It follows the consolidated roadmap in `plan/rego-implementation/`.

---

## 1. What `rego` is trying to build

`rego` is a **policy‑driven resilience runtime/library in Go for distributed systems**. You don’t write bespoke retry loops at each call site; you provide a **PolicyKey**, and `rego`:

- fetches an **EffectivePolicy** for that key,
- executes your operation using retries and/or hedges,
- consults budgets to avoid retry storms,
- classifies outcomes to decide what is retryable,
- emits structured observability (timelines + observer callbacks),
- and can later be controlled remotely by a control plane.

At the call site you want the simplest case to look like:

```go
user, err := rego.DoValue[User](ctx, rego.ParseKey("user-service.GetUser"), op)
```

And when you need rich telemetry:

```go
user, tl, err := rego.DoValueWithTimeline[User](ctx, rego.ParseKey("user-service.GetUser"), op)
```

### Why this exists

In production Go services, resilience code often has these failure modes:

- Every repo has ad‑hoc retry helpers that behave differently.
- Retry decisions are simplistic (“retry on error”), ignoring protocol semantics.
- Hedging is rare or implemented unsafely.
- Observability is thin (logs like “retrying…” with no structure).
- Under partial outages, retries amplify load and can collapse dependencies.

`rego` centralizes these concerns into a single library with predictable semantics and safe defaults.

---

## 2. Core concepts and data flow

Think of a `rego` call as a pipeline:

```
Call site
   │
   ▼
PolicyKey  ──► PolicyProvider ──► EffectivePolicy.Normalize()
   │                                 │
   │                                 ▼
   │                           Executor (retry/hedge)
   │                                 │
   │       ┌──────────── classify.Registry ────────────┐
   │       │                                           │
   │       ▼                                           ▼
   │  Classifier (value, err)→Outcome           budget.Registry
   │       │                                           │
   │       ▼                                           ▼
   │  retry/abort decision                     AllowAttempt per attempt
   │                                 │
   │                                 ▼
   │                          observe.Observer
   │                                 │
   ▼                                 ▼
Return value/error             Timeline + AttemptRecords
```

Where each component has a single responsibility and explicit extension points.

---

## 3. Package architecture (what lives where)

The plan establishes stable package boundaries in Phase 0 to avoid later refactors.

### `policy`

Holds the canonical schema:

- `PolicyKey` (structured namespace/name).
- `RetryPolicy`, `HedgePolicy`, `BudgetRef`.
- `EffectivePolicy`.
- `Normalize()` which applies defaults and validates/clamps configs.

Rationale:

- Putting schema here avoids import cycles.
- A single `Normalize()` path prevents “default drift” between providers and executor.

### `controlplane`

Defines `PolicyProvider` and implementations:

- `StaticProvider` for in‑process policies and defaults.
- `RemoteProvider` for control‑plane integration (Phase 7).

Rationale:

- Providers own fetching/caching, not execution.
- Remote provider includes TTL cache, LKG fallback, bounded LRU eviction, and refresh dedupe to be safe under outage and high concurrency.

### `retry`

The executor runtime:

- `Executor` and constructors (`NewExecutor`, `NewDefaultExecutor`).
- Core retry loop.
- Hedging coordinator (Phase 5/6).
- Global lazy singleton and wrappers for zero‑config usage (Phase 8).

Rationale:

- Keeps the “brains” in one place.
- Constructors hide internal fields so the API stays stable.
- Global executor is lazy and unexported to avoid import‑time side effects.

### `classify`

Retry semantics:

- `OutcomeKind` (success/retryable/non‑retryable/abort).
- `Outcome` (with optional `BackoffOverride`).
- `Classifier` interface + registry.
- Built‑ins: `AlwaysRetryOnError` (default), `HTTPClassifier` (safe defaults).

Rationale:

- Executor shouldn’t know protocol rules.
- “Retryability” is domain‑specific; registries make it pluggable.
- `BackoffOverride` allows protocols to steer backoff (e.g., HTTP `Retry‑After`) without baking protocol logic into the executor.

### `budget`

Backpressure:

- `Budget` interface + registry.
- `Decision{Allowed, Reason, Release}`.
- `AttemptKind` (retry vs hedge).
- Built‑ins like `UnlimitedBudget` and token buckets.

Rationale:

- Per‑attempt gating is the only reliable way to prevent storms.
- Passing full `BudgetRef` (including Cost) future‑proofs weighted budgets.
- Optional `Release` lets budgets model reservations without forcing complexity on simple budgets.

### `hedge`

Hedge scheduling:

- `HedgeTrigger` interface returning `(should, nextCheckIn)`.
- Trigger registry.
- Fixed delay trigger (Phase 5) and latency‑aware triggers (Phase 6).
- `LatencyTracker` and bounded per‑key tracker map.

Rationale:

- Schedule‑aware triggers avoid wasteful 10ms polling.
- Latency‑aware hedging is a differentiated feature, layered on top of basic hedging without breaking APIs.

### `observe`

Structured observability:

- `AttemptRecord`, `Timeline`.
- `Observer` interface + `NoopObserver`, `MultiObserver`.
- `AttemptInfo` injected into per‑attempt contexts.

Rationale:

- Timelines give a full causal record of what happened.
- Observer hooks are stabilized early so later phases don’t break users.
- Context attempt info supports tracing/logging inside ops without custom observers.

### `integrations/*`

Opt‑in protocol integrations:

- `integrations/grpc`: gRPC classifier + interceptor (non‑stdlib dependency).
- `integrations/http`: HTTP helpers (stdlib only but protocol‑specific).

Rationale:

- Core stays stdlib‑only for adoption and supply‑chain hygiene.
- Integrations can evolve independently.

### `rego` (root facade)

A thin convenience layer that:

- re‑exports key types and helpers,
- delegates to `retry`’s global executor for zero‑config usage,
- provides structured‑key APIs.

Rationale:

- Keeps user imports stable (`import "…/rego"`).
- Allows ergonomic helpers without leaking internal complexity.

---

## 4. Execution semantics

### 4.1 Retry loop

For each call:

1. Resolve policy from provider and run `Normalize()`.
2. Apply overall timeout to `ctx` if configured.
3. For `RetryIndex` in `0..MaxAttempts-1`:
   - Check retry budget (kind `KindRetry`) for the next attempt.
   - Run the attempt (with per‑attempt timeout if set).
   - Classify `(value, err)` to an `Outcome`.
   - Decide:
     - success → return immediately.
     - non‑retryable/abort → stop immediately.
     - retryable → if attempts remain, sleep backoff, then continue.

The default classifier (`AlwaysRetryOnError`) treats `context.Canceled` and `context.DeadlineExceeded` as abort to avoid retrying after cancellation.

### 4.2 Budgets

Budgets are consulted **before every attempt**:

- If denied, the attempt is not executed.
- The denial is recorded as an abort outcome and in the timeline.

Reasons are standardized (`no_budget`, `budget_not_found`, `budget_denied`, etc.) so operators can aggregate metrics.

### 4.3 Hedging within retry groups

If hedging is enabled:

- Each `RetryIndex` becomes a **retry group** with potentially multiple parallel attempts.
- Primary attempt launches immediately (`HedgeIndex=0`).
- Triggers decide when to spawn additional hedges (`HedgeIndex=1..MaxHedges-1`).
- First terminal outcome in the group decides the group’s fate:
  - success wins the call,
  - non‑retryable or abort ends the call,
  - all retryable outcomes → group fails retryably and the outer loop sleeps and retries.
- If multiple retryable attempts provide `BackoffOverride`, the group uses the **maximum** override (bounded) before the next retry group.

`CancelOnFirstTerminal` cancels other in‑flight attempts on **any** terminal outcome (success/non‑retryable/abort).

### 4.4 Failure modes

The executor has explicit modes for missing components:

- Missing policy, classifier, budget, or trigger can fallback, allow, or deny fast.
- Defaults are availability‑oriented (fail‑open) but every fallback is recorded for visibility.

### 4.5 Panic isolation

By default, panics in hooks propagate (Go‑standard behavior). If `ExecutorOptions.RecoverPanics` is set:

- panics in classifier/budget/trigger/observer are recovered,
- an abort outcome is recorded with a `panic_in_*` reason,
- and a typed error is returned.

This gives production users a safety knob without forcing overhead on everyone.

---

## 5. How the phases build the system

### Phase 0: foundations

- Create module + packages.
- Freeze canonical policy schema + `Normalize()` stub.
- Set dependency rules and CI.

Goal: *structure first*, so later features don’t require breaking refactors.

### Phase 1: core retry engine

- Implement retry policies, defaults, and the bounded retry loop.
- Ensure correct cancellation and timeout semantics.
- Add failure‑mode options in `ExecutorOptions`.

Goal: ship a minimal but correct retry runtime.

### Phase 2: observability

- Introduce timelines and attempt records.
- Stabilize `Observer` interface.
- Add `AttemptInfo` to contexts.
- Add additive `Do*WithTimeline` APIs.

Goal: make behavior inspectable without changing call sites.

### Phase 3: classifiers

- Add `Outcome` model + registry.
- Implement safe HTTP classifier.
- Move gRPC classifier to `integrations/grpc`.
- Wire classifier outcomes into executor decisions.

Goal: make retryability *domain‑correct*.

### Phase 4: budgets

- Add per‑attempt budgets + registry.
- Thread `BudgetRef` (Cost, Kind) into budget decisions.
- Abort on denials with clear reasons.

Goal: prevent retry/hedge storms.

### Phase 5: basic hedging

- Add fixed‑delay hedging as parallel attempts inside retry groups.
- Integrate budgets/classifiers/observability in hedged execution.
- Avoid polling with schedule‑aware triggers.

Goal: reduce tail latency safely.

### Phase 6: latency‑aware hedging

- Add latency trackers and percentile triggers.
- Bound tracker maps to avoid high‑cardinality leaks.

Goal: make hedging smarter without changing APIs.

### Phase 7: remote control plane

- Add remote provider with TTL, LKG fallback, LRU eviction, refresh dedupe.
- Normalize policies on conversion.
- Future work note for ETag/If‑None‑Match.

Goal: allow operational tuning without redeploys.

### Phase 8: ergonomics + integrations

- `NewDefaultExecutor` wires safe defaults.
- Lazy global executor + `rego`/`retry` wrappers.
- `integrations/http` and `integrations/grpc` helpers.

Goal: make adoption frictionless.

### Phase 9: hardening

- Benchmarks, stress/fuzz, race checks.
- Validate eviction/cardinality guardrails.
- Add sampling/truncation if timelines are too expensive.

Goal: production readiness.

### Phase 10: docs + v1.0

- Full README, GoDoc, extension guide, examples.
- API freeze and deprecations.
- Document non‑goals and composition (e.g., circuit breakers).

Goal: stable public release.

---

## 6. Extension points (how users customize)

Users customize `rego` by:

- registering classifiers/budgets/triggers under names in registries,
- referencing those names in policies,
- plugging in observers or multi‑observers,
- swapping policy providers (static vs remote),
- choosing stricter failure modes or panic recovery.

This uniform “registry + name in policy” pattern is deliberate: once a user learns one subsystem, they understand them all.

---

## 7. Design tradeoffs recap

- **Modular packages early** → slightly more upfront work, far less refactor tax.
- **Stdlib‑only core** → maximizes adoption; integrations keep optional deps out of the hot path.
- **Classifiers** → prevent protocol leakage into executor and allow domain‑correct retries.
- **Per‑attempt budgets** → only reliable way to avoid storms; optional cost/release keep the interface stable.
- **Schedule‑aware hedging** → avoids timer churn at scale.
- **Additive timeline APIs** → reduce churn for early adopters.
- **Explicit failure modes + observability** → safe defaults without hiding degraded states.

---

If you’re new to the codebase, start by reading `plan/rego-implementation/00-project-summary.md`, then follow phases 1–3 to understand the policy → executor → classifier/observe loop. After that, budgets and hedging are natural extensions.  

