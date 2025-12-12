# Phase 10 – Documentation, Examples, and v1.0 Release

## Objective

Finalize docs and examples, review API for stability, and cut the first `v1.0.0` release.

---

## Tasks

### 10.1 README

Include:

- quick‑start with `NewDefaultExecutor`.
- explanation of PolicyKey, policies, classifiers, budgets, hedging.
- control‑plane usage.
- a “Non‑goals / composition” section (e.g., circuit breakers are external in v1.0).
- a prominent “Gotchas” section:
  - **binaries vs libraries**: when it’s appropriate to use `retry.SetGlobal`/`rego.Init`, and when to prefer explicit `*Executor` injection
  - **low-cardinality keys**: what not to put in `PolicyKey`
  - **idempotency/replayability**: when retries/hedges are safe
  - **HTTP semantics**: `DoHTTP` returns `nil` response on non‑2xx and guarantees response bodies are closed on retry paths

### 10.2 GoDoc

- Add package‑level docs for every public package.
- Ensure exported symbols have concise comments.

### 10.3 Extension guide

Write `docs/extending.md`:

- how registries work,
- writing custom classifiers, budgets, triggers,
- testing extension points.

### 10.4 Examples as living docs

- Ensure examples build in CI.
- Add diagrams or timelines in example READMEs.

### 10.5 API freeze & versioning

- Audit for naming consistency.
- Remove footguns.
- Finalize additive `Do*WithTimeline` variants and deprecations.
- Tag `v1.0.0` when stable.

---

## Exit Criteria

- Documentation complete and coherent.
- Examples compile and demonstrate key features.
- API stable enough for `v1.0.0`.
