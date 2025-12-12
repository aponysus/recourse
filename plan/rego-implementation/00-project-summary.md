# rego – Consolidated, Detailed Implementation Plan

This directory is the canonical implementation plan for `rego`, replacing the earlier option‑1/option‑2 drafts. It adopts Option‑2’s modular architecture, with a few targeted tweaks to keep early shipping fast and the long‑term design stable.

---

## Vision

Build a **policy‑driven, observability‑first resilience library for Go** that supports:

- **Retries** with bounded backoff and timeouts.
- **Hedging** to reduce tail latency.
- **Budgets/backpressure** to prevent retry storms.
- **Pluggable classifiers** so retryability is protocol/domain‑aware.
- **Control‑plane integration** so policies can be tuned without redeploys.

The end state should feel like “reflexio‑style retries/hedges, but native to Go.”

---

## Core Principles

1. **Policy‑first**  
   Call sites provide a key; policies decide behavior. No hand‑rolled retry loops.

2. **Correctness before cleverness**  
   Context cancellation, per‑attempt and overall timeouts, and safe defaults are non‑negotiable.

3. **Observability is mandatory**  
   Every attempt/hedge is captured in structured data and streamed to observers.

4. **Composable internals, simple public API**  
   Internals are modular packages; public usage is `rego.Do(ctx, key, op)` with sane defaults.

5. **Safe under partial failure**  
   Missing policies, missing classifiers/budgets/triggers, and control‑plane outages all degrade to conservative behavior.

---

## Architecture Overview

Packages and their responsibilities:

- `policy`  
  Key and policy schema. No dependencies on other `rego` packages.

- `controlplane`  
  Policy providers (static + remote). Converts control‑plane configs → `policy.EffectivePolicy`.

- `retry`  
  Executor implementing retries and hedging. Depends on the other subsystems via interfaces/registries.

- `classify`  
  `(value, err)` → `Outcome` and registries. Built‑ins for generic and HTTP live in core; gRPC support is provided via an opt‑in integration package to preserve a stdlib‑only core.

- `budget`  
  Attempt‑level budget decisions + registry. Supports both gatekeeping and reservation semantics via optional release handles.

- `hedge`  
  Hedge triggers + registry, and latency tracking used by latency‑aware triggers.

- `observe`  
  Attempt records, timelines, and observers.

- `rego` (root package)  
  Thin facade that re‑exports key types and provides global default executor helpers.

Dependency rule: **core packages depend on stdlib only**. External integrations (Prometheus, OTel, etc.) live in optional subpackages later.

---

## Public API Baseline (goal by Phase 2)

Users should be able to start with:

```go
exec := rego.NewDefaultExecutor()

// simplest path
user, err := rego.DoValue[User](ctx, rego.ParseKey("user-service.GetUser"), op)

// structured observability when needed
user, tl, err := rego.DoValueWithTimeline[User](ctx, rego.ParseKey("user-service.GetUser"), op)
```

Key points:

- **Ergonomic keys**: `policy.PolicyKey` is structured (`Namespace`, `Name`) but a helper like `policy.ParseKey("svc.Method")` (re‑exported as `rego.ParseKey`) preserves string ergonomics.
- **Constructor‑first**: users create executors via `retry.NewExecutor` / `rego.NewDefaultExecutor`, not struct literals, so internals can add fields without breaking callers.
- **Aggressive defaults**: missing registries/providers/observers all fall back to safe built‑ins.
- **Additive observability**: timeline‑returning variants are additive (`DoValueWithTimeline`), keeping core APIs stable through v0.x.

## Non‑goals for v1.0

- Circuit breaking is intentionally out of scope for the initial release; `rego` should compose cleanly with external circuit breakers. A first‑class breaker subsystem can be revisited in v1.x once core APIs stabilize.

---

## Shipping Milestones (pre‑1.0)

These are *feature milestones*, not promises of API stability. We keep everything `v0.x` until the surface is stable.

1. **Phase 0–2 → `v0.1` alpha**  
   Usable retry library with timelines/observers.

2. **Phase 3–4 → `v0.2` beta**  
   Classifiers + budgets: feature‑complete for retry use cases.

3. **Phase 5–6 → `v0.3`**  
   Hedging (static + latency‑aware). Major differentiator.

4. **Phase 7 → `v0.4`**  
   Remote control‑plane provider with caching + safe fallbacks.

5. **Phase 8–10 → `v1.0`**  
   Integrations, docs, hardening, API freeze.

---

## Glossary

- **Attempt**: one execution of the user operation.
- **Retry group**: all attempts (including hedges) for a given retry index.
- **Hedge**: a parallel attempt launched because the primary is slow.
- **Classifier**: decides if an attempt is success, retryable, non‑retryable, or abort.
- **Budget**: backpressure gate for each attempt/hedge.
- **Observer**: receives attempt/timeline events for logs/metrics/traces.
