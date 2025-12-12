# Phase 8 – Integrations & Ergonomics

## Objective

Hide complexity for simple users and provide common integrations. This phase is where `NewDefaultExecutor` becomes the “happy path.”

---

## Tasks

### 8.1 Default executor wiring (`rego` / `retry`)

Provide:

```go
func NewDefaultExecutor(opts ...DefaultOption) *retry.Executor
```

Defaults wired:

- `StaticProvider` with conservative default policy.
- Built‑in stdlib classifiers registered (generic + HTTP). gRPC classifiers are registered only if the gRPC integration package/option is enabled.
- Built‑in hedge triggers registered.
- `UnlimitedBudget` registered under `"unlimited"` and used as implicit fallback.
- `observe.NoopObserver` unless user supplies another.

Allow options to override:

- provider (static vs remote),
- observer(s),
- registries (classifiers/budgets/triggers),
- default classifier.

### 8.2 Root helpers

Provide a single, lazy global executor for zero‑config usage, and make the “one‑liner” path stable.

```go
// retry package owns the global to avoid dependency cycles.
func DefaultExecutor() *Executor            // lazy, conservative defaults
func SetGlobal(exec *Executor)             // override before first use

// Zero‑config wrappers (string keys for ergonomics).
func Do(ctx context.Context, key string, op Operation) error
func DoValue[T any](ctx context.Context, key string, op OperationValue[T]) (T, error)
func DoWithTimeline(ctx context.Context, key string, op Operation) (observe.Timeline, error)
func DoValueWithTimeline[T any](ctx context.Context, key string, op OperationValue[T]) (T, observe.Timeline, error)

// rego facade delegates to retry’s global.
rego.Init(exec) // calls retry.SetGlobal(exec)
rego.Do(...)
rego.DoValue(...)
```

Implementation notes:

- **No exported `var Global`.** The default executor is built on first use with `sync.Once` (or `atomic.Pointer`) to avoid import‑time side effects and unsafe mutation.
- `DefaultExecutor()` uses `NewDefaultExecutor()` under the hood.
- `SetGlobal()` is safe to call at startup in binaries; in libraries, prefer passing an explicit `*Executor`. If called after `DefaultExecutor()` has initialized the global, it should return an error (or warn/no‑op).
- `retry.Do/DoValue` accept string keys and internally call `policy.ParseKey(key)` for convenience.
- `rego.Do/DoValue` keep structured keys in the facade but still route through `retry.DefaultExecutor()` unless an explicit executor is used. `rego.DoWithTimeline` / `rego.DoValueWithTimeline` mirror the timeline variants with structured keys.

### 8.3 HTTP integration (`integrations/http`)

Provide helper:

```go
func DoHTTP(ctx context.Context, exec *retry.Executor, key policy.PolicyKey, client *http.Client, req *http.Request) (*http.Response, observe.Timeline, error)
```

Behavior:

- Clones request with attempt context.
- Uses HTTPClassifier by default if policy does not specify classifier.
- Retries only idempotent methods by default unless policy opts into non‑idempotent retries.
- Refuses retries when the request body is not replayable (unless a replayable body factory is provided).
- Prevent connection leaks on retries:
  - Treat non‑2xx HTTP statuses as a typed error (including status and parsed `Retry-After` when present). Prefer an error shape that the core HTTP classifier can recognize via a `classify`-owned interface (so `classify` does not need to import `integrations/http`).
  - For any non‑2xx response, drain (bounded) + close the response body before returning the error so retryable outcomes cannot leak connections.
  - Return `(*http.Response)(nil)` on non‑2xx outcomes (callers should not need to close bodies on error).
- Honors `Retry-After` on retryable outcomes by letting the classifier set `Outcome.BackoffOverride` (based on the typed status error).

### 8.4 gRPC integration (`integrations/grpc`)

Provide unary client interceptor and classifier in an opt‑in integration package (depends on `google.golang.org/grpc`):

```go
func UnaryClientInterceptor(exec *retry.Executor, keyFunc func(method string) policy.PolicyKey) grpc.UnaryClientInterceptor
```

Default keyFunc:

- `/pkg.Service/Method` → `{Namespace:"Service", Name:"Method"}`

### 8.5 Examples

Add runnable examples under `examples/`:

- `http_client`
- `grpc_client`
- `background_worker`

Each example:

- uses `NewDefaultExecutor`,
- includes a small README,
- compiles with `go build`.

---

## Exit Criteria

- Simple usage requires minimal wiring.
- Common integrations are low‑friction and documented.
