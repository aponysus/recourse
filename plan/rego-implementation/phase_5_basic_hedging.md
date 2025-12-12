# Phase 5 – Basic Hedging (Fixed‑Delay)

## Objective

Add policy‑driven hedging to reduce tail latency. Hedging is integrated into the retry model as **parallel attempts within a retry group**, not as a separate feature.

---

## Tasks

### 5.1 Hedge triggers (`hedge`)

Introduce triggers now so Phase 6 only extends them.

```go
type HedgeState struct {
    CallStart        time.Time
    Now              time.Time
    AttemptsLaunched int
    MaxHedges        int
    Elapsed          time.Duration

    // Latency stats added in Phase 6.
    LatencyStats LatencySnapshot
}

type HedgeTrigger interface {
    // Returns whether to spawn a hedge now, and when to re‑evaluate.
    // nextCheckIn == 0 means "check soon"; the executor should clamp to a small floor (e.g. 50ms).
    ShouldSpawnHedge(state HedgeState) (should bool, nextCheckIn time.Duration)
}
```

Trigger registry:

```go
type TriggerRegistry struct {
    mu sync.RWMutex
    m  map[string]HedgeTrigger
}
func NewTriggerRegistry() *TriggerRegistry
func (r *TriggerRegistry) Register(name string, t HedgeTrigger)
func (r *TriggerRegistry) Get(name string) (HedgeTrigger, bool)
```

Provide fixed‑delay trigger:

```go
type FixedDelayTrigger struct {
    DelayPerHedge time.Duration
}
```

### 5.2 Finalize hedge policy (`policy`)

Activate placeholder fields:

```go
type HedgePolicy struct {
    Enabled               bool
    MaxHedges             int           // total attempts per retry group (primary + hedges)
    HedgeDelay            time.Duration // used for fixed delay trigger if TriggerName empty
    TriggerName           string        // registry lookup (Phase 6 adds more)
    CancelOnFirstTerminal bool          // if true, fail fast on terminal failure; if false, allow in-flight attempts to complete so success can still win
    Budget                BudgetRef
}
```

Default when enabled but unset:

- `MaxHedges = 2`
- `HedgeDelay = 200ms`

### 5.3 Executor integration (`retry`)

Extend options:

```go
type ExecutorOptions struct {
    // ...
    Triggers *hedge.TriggerRegistry
}
```

Default:

- If nil, create empty registry. Executor still supports fixed‑delay via `HedgeDelay`.

#### Execution model

Indexing (used in timelines, budgets, and `observe.AttemptInfo`):

- `RetryIndex`: outer retry‑group index as defined in Phase 2.
- `HedgeIndex`: `0` primary, `1..MaxHedges-1` hedges within a group.
- `attemptIdx`: global Attempt number (monotonic per call), not reset per retry group.

**Outer loop (retries):** for retryIdx = 0 .. MaxAttempts‑1:

1. Run a *retry group* for this index:
   - If hedging disabled → single attempt (same as Phase 4).
   - If enabled → potentially multiple parallel attempts.
2. Group outcome decides whether to continue:
   - **Success always wins**: if any attempt succeeds, overall success.
   - Otherwise, make failure behavior deterministic and explicit:
     - `CancelOnFirstTerminal == true` (fail fast): the first observed **terminal failure** (`OutcomeNonRetryable` or `OutcomeAbort`, excluding internal-cancel) ends the group; cancel other in‑flight attempts.
     - `CancelOnFirstTerminal == false` (success can still win): do **not** stop the group early on non‑success terminal outcomes; stop spawning new hedges, but allow in‑flight attempts to complete. If no success occurs by the time the group is done, choose the final outcome by precedence: `NonRetryable > Abort > Retryable`.

Outcome precedence & internal cancellation (must be explicit to avoid races):

- Precedence order for the group: `Success > NonRetryable > Abort > Retryable`.
- Treat cancellation caused by the executor’s own coordination (winner/terminal cancellation) as **internal-cancel**, not a real abort:
  - Record it in the attempt record/observer (e.g., `OutcomeAbort{Reason:"canceled_internal"}` with attributes like `cancel_reason=winner|terminal`).
  - Do **not** let internal-cancel outcomes influence group success/failure decisions.
- Treat cancellation from the caller’s context (external) as a real abort and stop the call promptly.

Group backoff rule:

- If any retryable attempt in the group provides `Outcome.BackoffOverride`, use the **maximum** override across attempts (bounded by policy `MaxBackoff`) before the next retry group.
- Otherwise use the normal exponential backoff.

**Inner hedged group (scheduled, not tight polling):**

- Launch primary attempt immediately (hedgeIndex = 0).
- Resolve trigger:
  - If `TriggerName` non‑empty and found → use that trigger.
  - If `TriggerName` is set but not found, follow `MissingTriggerMode`:
    - `FailureFallback` (default): record `"trigger_not_found"` and use `FixedDelayTrigger{DelayPerHedge: HedgeDelay}`.
    - `FailureDeny`: record `"trigger_missing_disable_hedging"` and disable hedging for this group.
  - If `TriggerName` empty, use `FixedDelayTrigger{DelayPerHedge: HedgeDelay}`.
- Re‑evaluate trigger on a `time.Timer`:
  - Build HedgeState and call `trigger.ShouldSpawnHedge`.
  - If `should == true` and `AttemptsLaunched < MaxHedges`, launch a new hedge (hedgeIndex increments).
  - Schedule the next check based on `nextCheckIn` (clamped to a small floor).
  - If `RecoverPanics` is enabled and the trigger panics, abort the call with `OutcomeAbort` reason `"panic_in_trigger"`.

Each launched attempt:

1. Check hedge budget (`pol.Hedge.Budget`) with kind `budget.KindHedge`.
2. If denied, record a denied AttemptRecord and do not launch goroutine.
3. Otherwise:
   - Derive per‑attempt context and attach `observe.AttemptInfo`:
     - `RetryIndex = retryIdx`, `Attempt = attemptIdx`,
     - `IsHedge = (hedgeIndex > 0)`, `HedgeIndex = hedgeIndex`,
     - `PolicyID = pol.ID`.
   - Execute op.
   - Classify outcome.
   - Send result to coordinator channel.

Coordinator responsibilities:

- Track in‑flight attempts with `sync.WaitGroup`.
- Ensure group outcome is deterministic under races:
  - First success wins immediately; cancel other in‑flight attempts (winner cancellation).
  - Track whether any non‑retryable/abort outcomes occurred (excluding internal-cancel).
  - If `CancelOnFirstTerminal` is true, cancel other in‑flight attempts when the first non‑retryable/abort arrives (terminal cancellation).
  - If `CancelOnFirstTerminal` is false, stop spawning new hedges after observing a non‑success terminal outcome, but allow in‑flight attempts to finish so success can still win.
  - After the group finishes (all launched attempts completed or canceled), choose the group result by the precedence rules above.

Cancellation rules:

- Overall ctx cancellation cancels all attempts and aborts.
- Winner cancellation should not leak goroutines.
- Use a mechanism that can distinguish **internal** cancellation from **external** cancellation (e.g., `context.WithCancelCause` with sentinel causes), so internal-cancel attempts don’t get mis-classified as “abort” outcomes that override success.

### 5.4 Observability hooks

- Call `Observer.OnHedgeSpawn` when hedgeIndex > 0 is launched.
- Call `Observer.OnHedgeCancel` when an attempt is canceled due to winner/terminal, with reason (`"winner"`, `"terminal"`, `"ctx_canceled"`).
- `OnAttempt` called for every completed attempt and every denied attempt record.

### 5.5 Tests

Scenarios:

1. **Hedge wins**
   - Primary sleeps; hedge returns quickly.
   - Hedge outcome success; primary canceled.
   - Timeline marks hedge attempt as winner; primary cancellation is recorded as internal-cancel and does not override success.

2. **Terminal non‑retryable**
   - One attempt returns non‑retryable.
   - If `CancelOnFirstTerminal == true`, others canceled and group fails fast.
   - If `CancelOnFirstTerminal == false`, no new hedges spawn; in‑flight attempts may still succeed (success wins), otherwise final failure follows precedence.

3. **Retry after group failure**
   - All hedges retryable.
   - Outer loop sleeps and re‑runs next group.

4. **Budget denies hedges**
   - Hedge budget denies hedge attempts; primary still runs.

Run `go test -race` for this phase.

---

## Exit Criteria

- Fixed‑delay hedging works and composes with retries/classifiers/budgets.
- No goroutine leaks or races under stress tests.
- Observability clearly shows hedges and cancellations.
