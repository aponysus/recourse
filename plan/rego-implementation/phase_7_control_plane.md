# Phase 7 – Remote Control‑Plane Provider

## Objective

Support remote policy fetch with caching, TTL, and last‑known‑good (LKG) fallbacks. Ensure outages/misconfigurations degrade safely.

---

## Tasks

### 7.1 Control‑plane config structs (`controlplane`)

Define JSON‑friendly configs:

```go
type RetryConfig struct {
    MaxAttempts       int     `json:"max_attempts"`
    InitialBackoffMs  int64   `json:"initial_backoff_ms"`
    MaxBackoffMs      int64   `json:"max_backoff_ms"`
    BackoffMultiplier float64 `json:"backoff_multiplier"`
    Jitter            string  `json:"jitter"`

    TimeoutPerAttemptMs int64 `json:"timeout_per_attempt_ms"`
    OverallTimeoutMs    int64 `json:"overall_timeout_ms"`

    Classifier string             `json:"classifier"`
    BudgetRef  policy.BudgetRef   `json:"budget_ref,omitempty"`
}

type HedgeConfig struct {
    Enabled               bool   `json:"enabled"`
    MaxHedges             int    `json:"max_hedges"`
    HedgeDelayMs          int64  `json:"hedge_delay_ms"`
    Trigger               string `json:"trigger"`
    CancelOnFirstTerminal bool             `json:"cancel_on_first_terminal"`
    BudgetRef             policy.BudgetRef `json:"budget_ref,omitempty"`
}

type PolicyConfig struct {
    Key   policy.PolicyKey `json:"key"`
    ID    string           `json:"id"`
    Retry RetryConfig      `json:"retry"`
    Hedge HedgeConfig      `json:"hedge"`
}
```

Add `ToEffective()` conversion producing `policy.EffectivePolicy`.

`ToEffective()` should call `EffectivePolicy.Normalize()` so defaults/clamps are applied uniformly and normalization/clamping metadata survives on the returned policy (so the executor can record it in timelines). Fundamentally invalid configs should surface as conversion errors (triggering LKG fallback and/or executor `MissingPolicyMode` handling).

### 7.2 RemoteProvider

Config:

```go
type RemoteProviderConfig struct {
    Endpoint string
    TTL      time.Duration
    MaxEntries int // optional; 0 means unbounded (not recommended)
    Client     *http.Client
}
```

Implementation details:

- Cache map: `PolicyKey` → `{Policy, fetchedAt, nextRefreshAt}` guarded by RWMutex.
- If `MaxEntries > 0`, evict least‑recently‑used entries when over capacity (prefer a shared `internal/cache` so the same eviction logic can be reused for latency trackers in Phase 6).
- Deduplicate concurrent refreshes per key (singleflight‑style) using stdlib primitives to avoid thundering herds.
- `GetEffectivePolicy`:
  1. Check cache freshness.
  2. If entry is stale/missing and `now < nextRefreshAt`, return cached LKG immediately (skip re‑fetch).
  3. If refresh is allowed, fetch `GET /policies/{namespace}/{name}`.
  4. On success: convert, cache as fresh, return policy (mark policy source `"remote"` in policy metadata).
  5. On failure:
     - If cached LKG exists → return it **with a typed error** (e.g., `controlplane.ErrPolicyFetchFailed`) and mark policy source `"lkg"`.
     - Else → return a **zero policy** with a typed error (e.g., `controlplane.ErrProviderUnavailable`) so the executor can apply `MissingPolicyMode`.

Not found handling:

- For 404 / missing keys, return a typed `controlplane.ErrPolicyNotFound` (do not silently substitute defaults inside the provider, or per‑call timelines can’t capture “policy missing”).
- Add **negative caching** for not‑found keys (short TTL) to prevent control‑plane hammering for high‑QPS misconfigured keys.

Backoff/timeout for fetch:

- Respect request context.
- Use small client timeout (e.g., 500ms default).

Refresh backoff (outage safety):

- Avoid “stale + failing refresh” hammering loops: on fetch failure, set `nextRefreshAt = now + backoff` (bounded exponential, with a max) so subsequent calls can return LKG without retrying the fetch on every request.

### 7.3 Observability of provider failures

Expose optional provider observer hooks or just log via standard logger passed in config. Record:

- cache hits/misses,
- fetch errors,
- fallback mode used.

### 7.4 Tests

Use `httptest.Server` to validate:

- initial fetch + cache.
- TTL expiry triggers re‑fetch.
- network failure returns LKG **and a typed error** (so executor can record fallback and apply `MissingPolicyMode` if configured).
- 404 returns a typed `ErrPolicyNotFound` (and is negatively cached).

Future work (v1.x): support ETag/If‑None‑Match on policy fetches to reduce control‑plane load. Not required for initial v0.x.

---

## Exit Criteria

- RemoteProvider works end‑to‑end and is safe under outages.
- Control‑plane schema is stable and documented.
