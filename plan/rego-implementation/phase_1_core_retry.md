# Phase 1 – Core Policy Model & Basic Retry Executor (No Hedging Yet)

## Objective

Deliver a correct, bounded retry engine with exponential backoff and timeouts. Public API should be usable for simple retry‑on‑error cases, while leaving room for richer semantics later.

---

## Tasks

### 1.1 Finalize key + policy schema (`policy`)

#### Policy keys

`policy.PolicyKey` remains structured:

```go
type PolicyKey struct {
    Namespace string // e.g. "user-service"
    Name      string // e.g. "GetUser"
}
```

Provide ergonomics:

- `ParseKey("svc.Method")` → `{Namespace:"svc", Name:"Method"}`
- If no dot is present, treat entire string as `Name` with empty `Namespace`.
- Provide `String()` for logging and map keys.
- **Low cardinality required:** keys back caches/latency trackers and must not embed request/tenant/user IDs.

#### Retry policy

Define retry‑only fields now, with placeholders for later phases:

```go
type JitterKind string
const (
    JitterNone  JitterKind = "none"
    JitterFull  JitterKind = "full"
    JitterEqual JitterKind = "equal"
)

type BudgetRef struct {
    Name string
    Cost int // default 1 if unset
}

type RetryPolicy struct {
    MaxAttempts       int
    InitialBackoff    time.Duration
    MaxBackoff        time.Duration
    BackoffMultiplier float64
    Jitter            JitterKind

    TimeoutPerAttempt time.Duration
    OverallTimeout    time.Duration

    // Used starting Phase 3/4; safe to keep empty for now.
    ClassifierName string
    Budget         BudgetRef
}
```

#### Hedge policy (placeholder)

Add the shape now so the control‑plane schema doesn’t need later breaking changes. Fields are ignored until Phase 5.

```go
type HedgePolicy struct {
    Enabled               bool
    MaxHedges             int
    HedgeDelay            time.Duration
    TriggerName           string
    CancelOnFirstTerminal bool // Phase 5 defines fail-fast vs "success can still win" behavior
    Budget                BudgetRef
}
```

#### Effective policy

```go
type EffectivePolicy struct {
    Key   PolicyKey
    ID    string // optional revision for observability
    Retry RetryPolicy
    Hedge HedgePolicy
}
```

Provide a conservative `DefaultPolicyFor(key)` helper:

- `MaxAttempts = 3`
- `InitialBackoff = 10ms`
- `BackoffMultiplier = 2`
- `MaxBackoff = 250ms`
- `TimeoutPerAttempt = 0` (caller controls)
- `OverallTimeout = 0`
- hedging disabled

Conservative defaults in `rego` mean:

- **Safety for retries/hedges**: bounded attempts, bounded hedges, capped backoff, and safe protocol gating.
- **Availability for missing components**: fail‑open with explicit observability unless a stricter `FailureMode` is configured.

Implement `Normalize()` for `EffectivePolicy` (see Phase 0) and call it:

- inside every `PolicyProvider` implementation,
- at the executor boundary before executing a call.

Normalization should:

- apply defaults (bounded attempts, capped backoff, hedging disabled by default),
- enforce explicit safety caps/floors (so “busy loop” and “parallel storm” configs are made safe),
- record any clamping/defaulting in **policy metadata** so the executor can surface it in timelines/observer attributes,
- validate enum fields like jitter kinds and return a typed error on fundamentally invalid config.

Minimum expectations (match the Phase 0 recommended guardrails):

- clamp `Retry.MaxAttempts` and `Hedge.MaxHedges` to hard maximums
- clamp tiny backoffs/timeouts to minimum floors when set
- clamp or reject invalid multipliers/jitter enums

The executor should emit a timeline/attempt attribute when normalization modified the policy (e.g., `"policy_normalized": "true"`, `"policy_clamped_fields": "retry.max_attempts,hedge.max_hedges"`), so operators can tell why runtime behavior differs from control-plane config.

### 1.2 Static policy provider (`controlplane`)

Define:

```go
type PolicyProvider interface {
    GetEffectivePolicy(ctx context.Context, key policy.PolicyKey) (policy.EffectivePolicy, error)
}
```

Provider semantics:

- Providers may return a **non-zero policy alongside a non-nil error** to signal that the policy was obtained via a fallback path (e.g., last-known-good) and should be treated according to `MissingPolicyMode` and recorded in timelines.
- Providers should set policy metadata for source when possible (e.g., `"static"`, `"remote"`, `"lkg"`, `"default"`), but the executor must not depend on metadata being present.

Implement `StaticProvider`:

- `Policies map[PolicyKey]EffectivePolicy`
- `Default policy.EffectivePolicy`

Behavior:

- If key exists → return it.
- Else if Default non‑zero → return Default with `Key` overwritten.
- Else → return `policy.DefaultPolicyFor(key)` (never erroring for missing policies).

Add typed errors for real provider failures:

```go
var ErrProviderUnavailable = errors.New("rego: policy provider unavailable")
var ErrPolicyNotFound = errors.New("rego: policy not found")
var ErrPolicyFetchFailed = errors.New("rego: policy fetch failed")
```

### 1.3 Executor constructor + options (`retry`)

Provide constructor to avoid users using composite literals:

```go
type FailureMode int

const (
    FailureFallback FailureMode = iota // use safe defaults
    FailureAllow                       // proceed without constraint
    FailureDeny                        // fail fast
)

type ExecutorOptions struct {
    Provider controlplane.PolicyProvider
    Observer observe.Observer // nil → NoopObserver in Phase 2
    Clock    func() time.Time // for tests

    // Failure modes for missing components (used in later phases).
    MissingPolicyMode     FailureMode // default: FailureFallback
    MissingClassifierMode FailureMode // default: FailureFallback (Phase 3+)
    MissingBudgetMode     FailureMode // default: FailureAllow (Phase 4+)
    MissingTriggerMode    FailureMode // default: FailureFallback/disable hedging (Phase 5+)

    // Panic isolation for user hooks (Phase 3+).
    RecoverPanics bool // default false; when true, panics in classifier/budget/observer/trigger are recovered and treated as abort outcomes.
}

func NewExecutor(opts ExecutorOptions) *Executor
```

Rules:

- `Provider` required; if nil, use a `StaticProvider` with only default policy.
- If `Clock` nil, use `time.Now`.
- If `Observer` nil (Phase 1), stash nil; Phase 2 will default.

Missing‑component behavior (implemented in the relevant later phases):

- **MissingPolicyMode** (when a provider can’t supply a policy):
  - `FailureFallback` (default): use `policy.DefaultPolicyFor(key)` and record `"policy_fallback"` in the timeline.
  - `FailureAllow`: treat as “no policy” → run a single attempt with no retries/hedges.
  - `FailureDeny`: fail fast with a typed `ErrNoPolicy`.

Treat provider errors (`err != nil`) as “can’t supply an authoritative policy”, even if the provider also returns a policy (e.g., LKG). Under `FailureFallback`, prefer using the provider’s returned policy when it is non-zero; otherwise fall back to `policy.DefaultPolicyFor(key)`. Always record which path was taken (error kind / policy source) in the timeline.
- **MissingClassifierMode** (Phase 3+): fallback to the default classifier, or deny fast if configured.
- **MissingBudgetMode** (Phase 4+): allow (fail‑open) by default with `"budget_not_found"` recorded; `FailureDeny` aborts before running the attempt.
- **MissingTriggerMode** (Phase 5+): fallback to fixed‑delay hedging (using `HedgeDelay`) by default; `FailureDeny` disables hedging for the group.

All modes should be surfaced via `BudgetReason`/`Outcome.Reason`/timeline attributes so operators can tell when fallbacks occurred.

Panic behavior:

- By default, panics in user‑supplied hooks propagate (standard Go expectation).
- If `RecoverPanics` is true, the executor `recover()`s around classifier/budget/observer/trigger calls, records an `OutcomeAbort` with a reason like `"panic_in_classifier"`, and returns a typed error.

### 1.4 Implement retry loop

Implement `Executor.Do` / `DoValue`:

```go
type Operation func(ctx context.Context) error
type OperationValue[T any] func(ctx context.Context) (T, error)

func (e *Executor) Do(ctx context.Context, key policy.PolicyKey, op Operation) error
func (e *Executor) DoValue[T any](ctx context.Context, key policy.PolicyKey, op OperationValue[T]) (T, error)
```

Algorithm:

1. Resolve effective policy from provider.
   - If provider returns `err != nil`, apply `MissingPolicyMode` as defined above (and record the resolution path in the timeline).
2. Apply `OverallTimeout` by wrapping `ctx` if non‑zero.
3. Initialize backoff = `InitialBackoff` (default to 10ms if <=0).
4. For attempt = 0 .. `MaxAttempts-1`:
   - Derive `attemptCtx` with `TimeoutPerAttempt` if set.
   - Call `op(attemptCtx)`.
   - If `err == nil` → return value.
   - If attempt is last → return last error.
   - Sleep backoff with jitter:
     - compute `nextBackoff = min(backoff * multiplier, MaxBackoff)`
     - apply jitter:
       - `none`: unchanged
       - `full`: random in `[0, backoff]`
       - `equal`: random in `[backoff/2, backoff]`
   - If `ctx.Done()` while sleeping → abort early with `ctx.Err()`.

Important semantics:

- If `MaxAttempts <= 0`, treat as 1.
- Never sleep a negative duration.
- Respect cancellation before the first attempt (zero attempts executed).

### 1.5 Root facade (`rego`)

Provide minimal root helpers:

```go
package rego

type Key = policy.PolicyKey
func ParseKey(s string) Key { return policy.ParseKey(s) }

func Do(ctx context.Context, key Key, op retry.Operation) error
func DoValue[T any](ctx context.Context, key Key, op retry.OperationValue[T]) (T, error)
```

At Phase 1, `rego.Do/DoValue` should delegate to a **lazy** default executor (owned by `retry`) built from conservative static defaults.  
Avoid exporting a mutable `var Global`; Phase 8 formalizes `retry.DefaultExecutor()` + `retry.SetGlobal(exec)` and zero‑config wrappers.  
`rego.DoValue` remains `(T, error)`; Phase 2 adds additive `DoWithTimeline` / `DoValueWithTimeline` variants.

### 1.6 Tests

Unit tests in `retry`:

- **Attempts**
  - `MaxAttempts=1` → no retry.
  - `MaxAttempts=3` → exactly 3 calls on repeated failure.
- **Stops on success**
  - failures then success → stops immediately.
- **Backoff**
  - jitter disabled → exact sequence asserted.
  - jitter enabled → bounds asserted.
- **Timeouts**
  - per attempt timeout cancels a slow op and retries.
  - overall timeout stops the loop even if attempts remain.
- **Context cancellation**
  - cancel before first attempt → zero calls.
  - cancel during sleep → abort with `context.Canceled`.

---

## Exit Criteria

- Retry behavior correct for simple “retry on error”.
- Timeouts/backoff/cancellation semantics tested.
- Public construction via `NewExecutor` and root helpers exists.
