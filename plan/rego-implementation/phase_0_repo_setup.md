# Phase 0 – Repo Setup, Package Boundaries, and Ground Rules

## Objective

Lay stable foundations so later phases don’t require breaking structural refactors. Establish package boundaries, dependency rules, CI, and a minimal compiling skeleton.

---

## Tasks

### 0.1 Create module and repo

- Initialize module:

  ```bash
  go mod init github.com/you/rego
  ```

- Decide minimum Go version (recommend `go 1.22`).
- Commit baseline `.gitignore`.

### 0.2 Establish package layout

Create directories:

```text
/rego         # facade; global helpers
/retry        # executor + public Do/DoValue API
/policy       # keys + policy schema
/classify     # classifiers + registry
/budget       # budgets + registry
/hedge        # triggers + latency tracking
/observe      # observers, attempt records, timelines
/controlplane # providers: static + remote
/internal     # shared utilities that must stay private
/examples     # later phases
/integrations/grpc # opt‑in gRPC classifier + interceptor (non‑stdlib)
/integrations/http # opt‑in HTTP helpers/classifier config (stdlib)
```

Add short `doc.go` for each package describing its role.

### 0.3 Dependency rules

- **Core packages (`retry`, `policy`, `classify`, `budget`, `hedge`, `observe`, `controlplane`) use stdlib only.**
- Integration packages later may import third‑party libs under subfolders like:
  - `observe/prometheus`
  - `observe/otel`
  - `integrations/grpc`
  - `integrations/http`

### 0.4 CI / lint

Set up CI to run on every PR:

```bash
go test ./...
go vet ./...
```

Optional:

```bash
golangci-lint run ./...
```

### 0.5 Minimal compiling skeleton

Create placeholder types/functions that compile but don’t implement semantics yet.

Examples:

`policy/key.go`:

```go
package policy

type PolicyKey struct {
    Namespace string
    Name      string
}

func ParseKey(s string) PolicyKey { /* stub */ }
```

`retry/executor.go`:

```go
package retry

type Executor struct{}

func (e *Executor) Do(ctx context.Context, key policy.PolicyKey, op Operation) error {
    return op(ctx)
}
```

### 0.6 Canonical policy schema + normalization stub

Create `policy/schema.go` defining the canonical JSON‑tagged schema used in all phases:

- `PolicyKey`
- `BudgetRef{Name, Cost}`
- `RetryPolicy`, `HedgePolicy`
- `EffectivePolicy`

Also include **non-JSON policy metadata** (e.g., `json:"-"`) so the executor can record operational facts without changing the control-plane schema, such as:

- policy source (`"remote" | "lkg" | "default" | "static"`)
- normalization/clamping notes (what got defaulted/clamped)

Stub `func (p EffectivePolicy) Normalize() (EffectivePolicy, error)` that:

- applies defaults (`MaxAttempts`, backoff, jitter, costs),
- enforces explicit hard safety caps/floors (documented constants),
- clamps out-of-range values *and records that it happened* in policy metadata,
- validates enum fields like jitter kinds,
- returns a typed error on fundamentally invalid config.

Recommended guardrails to implement in `Normalize()` (exact numbers can be tuned, but make them explicit and testable):

- `Retry.MaxAttempts`: default `3`; clamp to `1..10`
- `Hedge.MaxHedges`: default `2` (when enabled); clamp to `1..3`
- `Retry.BackoffMultiplier`: clamp to `1.0..10.0`
- `Retry.InitialBackoff`: default `10ms`; clamp to `>= 1ms` when set
- `Retry.MaxBackoff`: default `250ms`; clamp to `<= 30s` and `>= InitialBackoff`
- `Retry.TimeoutPerAttempt`: if set, clamp to `>= 1ms`
- `Retry.OverallTimeout`: if set, clamp to `>= 1ms`
- `Hedge.HedgeDelay`: default `200ms` (when enabled); clamp to `>= 10ms`
- `BudgetRef.Cost`: default `1`; clamp to `>= 1` (optionally cap to a reasonable max)

All policy providers and the executor should call `Normalize()` before use.

### 0.7 Tiny “toolchain works” test

Add a trivial unit test in `retry` that verifies Go setup and CI:

```go
func TestExecutor_Do_Trivial(t *testing.T) {
    exec := &Executor{}
    called := false
    err := exec.Do(context.Background(), policy.PolicyKey{}, func(context.Context) error {
        called = true
        return nil
    })
    if err != nil || !called {
        t.Fatalf("unexpected result")
    }
}
```

### 0.8 Minimal extension‑points doc

Add a lightweight `docs/extending.md` that explains:

- the registry pattern used for classifiers/budgets/triggers,
- how a user will register custom implementations,
- that the detailed extension APIs will stabilize by v1.0.

Keep this short (1–2 pages) and expand later in Phase 10.

---

## Exit Criteria

- `go build ./...` and `go test ./...` succeed.
- Packages and dependency rules are in place.
- A minimal compiling surface exists for Phase 1 to fill in.
