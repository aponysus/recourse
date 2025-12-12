# Phase 9 – Hardening: Performance, Races, Failure Modes

## Objective

Make the library production‑ready: validate overhead, eliminate races/leaks, and lock in explicit failure behaviors.

---

## Tasks

### 9.1 Benchmarks

Benchmarks in `retry/executor_bench_test.go`:

- single attempt, no retry.
- 3 attempts, retryable failures then success.
- hedged group with 2–3 in‑flight attempts.
- **fast path**: noop observer + no timeline requested.
- with/without observer (and with/without timeline).

Measure:

- allocations per call (`-benchmem`),
- added latency vs raw call.

Optimize only hot paths if overhead is significant (avoid premature micro‑optimizations).

### 9.2 Stress & fuzz tests

- randomized latency/error patterns.
- varied classifier/budget/hedge configs.
- ensure no deadlocks, panics, or goroutine leaks.

### 9.3 Race detection

Run:

```bash
go test -race ./...
```

Fix races in:

- registries,
- latency trackers,
- control‑plane cache,
- hedging coordinator.

### 9.4 Failure modes audit

Document and test behavior for:

- missing policy → default conservative policy.
- provider returns LKG/default with typed error → executor records fallback in timeline and respects `MissingPolicyMode`.
- missing classifier/budget/trigger → allow with reason recorded, fallback to defaults.
- control‑plane down → LKG when available, otherwise executor fallback behavior; avoid hammering (refresh backoff).
- budget denial:
  - retry attempt denial stops retries and returns last attempt error (first-attempt denial fails fast),
  - hedge denial is recorded but does not fail the call.

### 9.5 Cardinality guardrails

- Verify LRU/TTL eviction for RemoteProvider cache and latency‑tracker maps.
- Add a warning (observer/log) when per‑key tracker/cache counts exceed a reasonable threshold, to surface accidental high‑cardinality keys.

### 9.6 Observability sampling / truncation (if needed)

If benchmarks show high allocation pressure from timelines/attributes:

- add optional sampling observers,
- allow capping stored attempts to the last N per timeline,
- prefer allocation‑friendly attribute maps.

---

## Exit Criteria

- Race detector clean.
- No goroutine leaks in stress tests.
- Benchmarks show acceptable overhead.
- Failure modes unsurprising and well‑documented.
