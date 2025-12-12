# Phase 2 – Observability Layer & Timelines

## Objective

Make every call fully observable without changing call sites. Introduce structured timelines and a stable observer interface that will remain compatible through hedging/budget phases.

---

## Tasks

### 2.1 Observability types (`observe`)

#### AttemptRecord

```go
type AttemptRecord struct {
    Attempt    int
    StartTime  time.Time
    EndTime    time.Time

    // Hedging (Phase 5+)
    IsHedge    bool
    HedgeIndex int

    // Classification (Phase 3+)
    Outcome classify.Outcome

    Err error

    Backoff time.Duration // backoff before this attempt

    // Budgets (Phase 4+)
    BudgetAllowed bool
    BudgetReason  string
}
```

#### Timeline

```go
type Timeline struct {
    Key      policy.PolicyKey
    PolicyID string
    Start    time.Time
    End      time.Time
    // Optional call-level metadata (policy source, fallbacks, normalization notes, etc.).
    Attributes map[string]string
    Attempts []AttemptRecord
    FinalErr error
}
```

#### Observer interface

Stabilize the full interface now to avoid breaking user observers later:

```go
type Observer interface {
    OnStart(ctx context.Context, key policy.PolicyKey, pol policy.EffectivePolicy)
    OnAttempt(ctx context.Context, key policy.PolicyKey, rec AttemptRecord)

    // Hedging hooks (no‑ops until Phase 5)
    OnHedgeSpawn(ctx context.Context, key policy.PolicyKey, rec AttemptRecord)
    OnHedgeCancel(ctx context.Context, key policy.PolicyKey, rec AttemptRecord, reason string)

    OnSuccess(ctx context.Context, key policy.PolicyKey, tl Timeline)
    OnFailure(ctx context.Context, key policy.PolicyKey, tl Timeline)
}
```

Provide helpers:

- `NoopObserver` implementing all methods as no‑ops.
- `BaseObserver` struct with no‑op methods users can embed.
- `MultiObserver` to fan‑out to several observers.

#### Attempt metadata in context

Expose lightweight attempt metadata for downstream logging/tracing without forcing observers:

```go
type AttemptInfo struct {
    RetryIndex int
    Attempt    int
    IsHedge    bool
    HedgeIndex int
    PolicyID   string
}

func AttemptFromContext(ctx context.Context) (AttemptInfo, bool)
```

Index invariants (apply to `AttemptRecord`, `AttemptInfo`, budgets, and triggers):

- `RetryIndex`: outer retry‑group index, `0..MaxAttempts-1`.
- `HedgeIndex`: `0` for the primary attempt in a group, `1..MaxHedges-1` for hedges within that group.
- `Attempt`: monotonically increasing per call in launch order; completion order may differ from launch order.

The executor should attach an `AttemptInfo` to each per‑attempt context before invoking the user op.

### 2.2 Wire observer into executor (`retry`)

Update `ExecutorOptions` / `NewExecutor`:

- If `Observer` nil, set to `observe.NoopObserver{}`.

In a shared internal helper used by `DoValueWithTimeline` (and by `DoValue` which discards the timeline):

Implement two internal execution paths:

- **Full timeline path** (used when the caller requests a timeline, or when the observer is not noop): build a complete `Timeline` and per-attempt records.
- **Fast path** (used when the observer is `NoopObserver` and the caller did not request a timeline): avoid allocating/storing per-attempt records and attributes; preserve behavior and cancellation semantics.

Full timeline path steps:

1. Resolve policy (see Phase 1 provider semantics):
   - if provider returned an error and `MissingPolicyMode` caused fallback/allow/deny behavior, record it in `Timeline.Attributes` (e.g., `policy_error`, `policy_source`, `missing_policy_mode`).
   - if policy normalization/clamping occurred, record it in `Timeline.Attributes` (e.g., `policy_normalized=true`, `policy_clamped_fields=...`).
2. Initialize `Timeline{Key: key, PolicyID: pol.ID, Start: now(), Attributes: map[string]string{...}}`.
3. `Observer.OnStart(ctx, key, pol)`.
4. For each attempt:
   - Create `AttemptRecord{Attempt: i, StartTime: now(), Backoff: lastBackoff, BudgetAllowed: true, BudgetReason: "not_used"}`.
   - Run op (Phase 1 behavior).
   - Fill `EndTime`, `Err`.
   - Fill a naive `Outcome`:
     - `err == nil` → `OutcomeSuccess`
     - else → `OutcomeRetryable`
     (This requires `classify.OutcomeKind` and `classify.Outcome` to exist as minimal types; if they are still stubs from Phase 0, keep them simple here and fully flesh out classifiers/registry in Phase 3.)
   - Append to timeline and call `Observer.OnAttempt`.
5. On terminal success/failure:
   - Fill `Timeline.End`, `FinalErr`.
   - Call `OnSuccess` or `OnFailure`.

Observer ordering:

- `OnAttempt` is called after attempt completes.
- `OnSuccess`/`OnFailure` called exactly once.

### 2.3 Root facade updates (`rego`)

Keep `Do/DoValue` signatures stable and add additive variants:

- `DoWithTimeline(ctx, key, op) (observe.Timeline, error)`
- `DoValueWithTimeline[T](ctx, key, op) (T, observe.Timeline, error)`

Mirror these in the root `rego` facade and zero‑config `retry.Do*/DoValue*` wrappers.

### 2.4 Tests

Create a `testObserver` that records callbacks.

Assertions:

- `OnStart` called once with correct key/policy.
- `OnAttempt` called per attempt.
- Attempt indices monotonic.
- Timeline attempts match observer records.
- `OnSuccess` vs `OnFailure` fired correctly.
- Nil observer case still works via NoopObserver.

---

## Exit Criteria

- Every call yields a consistent `Timeline`.
- Observers can be plugged in without affecting behavior.
- Interface stable for later hedging/budget enhancements.
