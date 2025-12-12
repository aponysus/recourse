# Phase 3 – Classifiers & Retry Semantics

## Objective

Move retry/abort decisions out of the executor and into pluggable classifiers. Ship a concrete HTTP classifier in core, and a gRPC classifier via an opt‑in integration package, to prove the abstraction while preserving stdlib‑only core dependencies.

---

## Tasks

### 3.1 Classification types (`classify`)

```go
type OutcomeKind int
const (
    OutcomeSuccess OutcomeKind = iota
    OutcomeRetryable
    OutcomeNonRetryable
    OutcomeAbort // stop immediately, treat as failure
)

type Outcome struct {
    Kind       OutcomeKind
    Reason     string
    Attributes map[string]string
    BackoffOverride time.Duration // optional; 0 means use policy backoff
}

type Classifier interface {
    Classify(value any, err error) Outcome
}
```

To support stdlib-only core *and* safe integrations, prefer interface-based classification over concrete integration types. For example, the HTTP classifier can recognize an error via a small `classify`-owned interface (implemented by `integrations/http`) rather than importing the integration package directly.

Classifier contract (type safety):

- If a classifier expects a specific value/error shape (e.g., HTTP) and receives an incompatible type, it must **fail loudly and safely**:
  - return `OutcomeNonRetryable` or `OutcomeAbort` with reason `"classifier_type_mismatch"`,
  - include attributes like `expected_type` / `got_type`,
  - never treat mismatches as retryable.

Provide a thread‑safe registry:

```go
type Registry struct {
    mu sync.RWMutex
    m  map[string]Classifier
}
func NewRegistry() *Registry
func (r *Registry) Register(name string, c Classifier)
func (r *Registry) Get(name string) (Classifier, bool)
```

### 3.2 Built‑in classifiers

Ship these in core:

1. **AlwaysRetryOnError** (default)
   - `err == nil` → success
   - `err` is `context.Canceled` or `context.DeadlineExceeded` → abort
   - else → retryable

2. **HTTPClassifier** (safe defaults)
   - transport error → retryable for idempotent/replayable requests
   - 5xx → retryable for idempotent requests
   - retryable 4xx: 408, 429 (configurable)
   - if a retryable response includes `Retry-After`, set `Outcome.BackoffOverride`
   - attributes: `status`, `method`, `retry_after`, etc.

   Integration note (Phase 8): prefer classifying a typed error produced by `integrations/http` (so intermediate retryable responses can be drained/closed before retrying and can’t leak connections). To avoid a core→integration dependency, have the integration error implement a `classify`-owned interface, e.g.:

   ```go
   type HTTPError interface {
       HTTPStatusCode() int            // 0 for transport errors
       HTTPMethod() string             // e.g. "GET"
       RetryAfter() (time.Duration, bool)
   }
   ```

   If `HTTPClassifier` also supports `*http.Response` values for advanced users, ensure type mismatches return `"classifier_type_mismatch"` rather than retrying blindly.

gRPC classifier support is provided in `integrations/grpc` and registered only when that integration is used.

Add `RegisterBuiltins(reg *Registry)` helper.

By default the executor uses `AlwaysRetryOnError` unless a policy (or integration helper like `DoHTTP`) explicitly selects `"http"` or another classifier.

### 3.3 Policy integration (`policy`)

Retry policies select classifier by name:

```go
type RetryPolicy struct {
    // ...
    ClassifierName string // empty → default
}
```

### 3.4 Executor integration (`retry`)

Extend executor options:

```go
type ExecutorOptions struct {
    Provider          controlplane.PolicyProvider
    Observer          observe.Observer
    Clock             func() time.Time
    Classifiers       *classify.Registry
    DefaultClassifier classify.Classifier
}
```

`NewExecutor` defaults:

- If `Classifiers` nil, create one and call `RegisterBuiltins`.
- If `DefaultClassifier` nil, use `AlwaysRetryOnError{}`.

Resolve classifier per call:

```go
classifier := e.DefaultClassifier
if name := pol.Retry.ClassifierName; name != "" {
    if c, ok := e.Classifiers.Get(name); ok {
        classifier = c
    }
}
```

If a policy refers to a missing classifier, follow `MissingClassifierMode` (default `FailureFallback`): use the default classifier and record `"classifier_not_found"` in the attempt outcome/attributes. In stricter modes (`FailureDeny`), abort immediately.

Update attempt control flow:

1. After op returns `(val, err)`, compute `out := classifier.Classify(val, err)`.
2. Record `AttemptRecord.Outcome = out`.
3. Switch on `out.Kind`:
   - **Success** → finalize timeline success.
   - **Retryable** → continue if attempts remain; else failure.
   - **NonRetryable** → stop immediately, failure.
   - **Abort** → stop immediately, failure; prefer returning `err` if non‑nil, else synthesize based on `out.Reason`.

Ensure overall/per‑attempt timeout and cancellation are still honored. If classifier marks `Abort` for context cancellation, executor should return `ctx.Err()` to avoid hiding the real reason.

If `RecoverPanics` is enabled, wrap `classifier.Classify` in a `recover()`; on panic, record `OutcomeAbort` with reason `"panic_in_classifier"` and stop the call.

When sleeping after a retryable attempt, prefer `out.BackoffOverride` if non‑zero, bounded by policy `MaxBackoff`; otherwise use the computed exponential backoff.

### 3.5 Tests

Classifier unit tests:

- HTTP: 200 → success; 500 retryable only for idempotent; 404 non‑retryable; 429 retryable with optional backoff override.
- gRPC classifier tests live with the opt‑in integration package.

Executor tests:

- For HTTP classifier:
  - 4xx returns immediately with 1 attempt.
  - 5xx retried up to `MaxAttempts`.
- Abort outcome stops without sleeping further.

---

## Exit Criteria

- Retry behavior determined entirely by classifiers.
- Built‑in generic/HTTP classifiers validate abstraction without forcing non‑stdlib deps.
- Timeline carries classifier outcomes/reasons and optional backoff overrides.
