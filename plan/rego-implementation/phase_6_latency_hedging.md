# Phase 6 – Latency‑Aware Hedging

## Objective

Refine hedging so decisions are driven by latency statistics. Add latency tracking and additional triggers while keeping Phase‑5 APIs stable.

---

## Tasks

### 6.1 Latency tracking (`hedge`)

Add:

```go
type LatencySnapshot struct {
    P50 time.Duration
    P90 time.Duration
    P95 time.Duration
    P99 time.Duration
}

type LatencyTracker interface {
    Observe(d time.Duration)
    Snapshot() LatencySnapshot
}
```

Provide a simple core implementation:

- Fixed‑size ring buffer of last N latencies.
- Snapshot computes approximate quantiles.
- Thread‑safe via mutex.

Notes on cost:

- Ring buffer size is fixed and configurable (default small, e.g., 256 samples).
- Snapshot computation should be amortized O(N) without sorting on every check; avoid per‑check full sorts.
- Snapshots are only taken during scheduled trigger evaluations (not per attempt).

### 6.2 New triggers (`hedge`)

Implement:

1. **AbsoluteLatencyTrigger**
   - Spawn hedge once elapsed ≥ threshold.

2. **P95Trigger**
   - Spawn hedge when elapsed ≥ `Fraction * P95`.

3. **FixedDelayTrigger** remains as baseline.

Register defaults in `NewDefaultExecutor` (Phase 8), e.g.:

All triggers implement the schedule‑aware `ShouldSpawnHedge` signature from Phase 5, returning a `nextCheckIn` that lets the executor avoid tight polling.

Guardrails for latency‑based triggers:

- Do not spawn latency‑based hedges until the tracker has a minimum sample count (e.g., 50 observations), otherwise fall back to fixed delay or no hedging.
- Always respect `MaxHedges` and avoid spawning multiple hedges in a single evaluation tick.

- `"fixed_delay"` (constructed per policy using HedgeDelay)
- `"absolute_150ms"`
- `"p95_90pct"`

### 6.3 Shared cache primitive (`internal/cache`)

Avoid duplicating subtle concurrency + eviction logic across the codebase:

- Implement a small stdlib‑only `internal/cache` used by:
  - the per‑key latency‑tracker map in this phase, and
  - the RemoteProvider cache in Phase 7.

Minimum features:

- thread‑safe LRU by max entries
- optional TTL per entry
- “touch” on access
- optional on‑evict hook
- stress tests for races and correctness

### 6.4 Executor integration (`retry`)

Executor maintains per‑key latency trackers:

- Map `PolicyKey` → `LatencyTracker`.
- Updated on terminal attempts (winner or final group failure).
- Bound the map with `MaxTrackers` (executor option) and LRU/TTL eviction to avoid high‑cardinality leaks; warn when thresholds are exceeded.
- Prefer reusing the same `internal/cache` eviction primitive used by RemoteProvider (Phase 7) so there is only one concurrency/TTL/LRU implementation to harden.

In hedged group scheduled checks:

- Fetch snapshot for this key (if tracker present).
- Populate `HedgeState.LatencyStats`.
- Trigger can now use stats.

### 6.5 Tests

- Unit tests for triggers using synthetic HedgeState/snapshots.
- Integration test:
  - record latencies from several calls,
  - verify P95 trigger spawns hedges at expected times.

---

## Exit Criteria

- Hedging decisions delegated fully to triggers.
- Latency‑aware triggers function without API changes to call sites.
