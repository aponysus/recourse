# Phase 4 – Budgets & Backpressure

## Objective

Prevent retry/hedge storms by consulting budgets **per attempt**. Budgets can deny attempts, and can optionally return a release handle to model reservation‑style resources.

---

## Tasks

### 4.1 Budget types (`budget`)

#### Attempt kind

Budgets should know whether they’re gating retries or hedges:

```go
type AttemptKind int
const (
    KindRetry AttemptKind = iota
    KindHedge
)
```

#### Decision

```go
type Decision struct {
    Allowed bool
    Reason  string
    Release func() // optional; nil/no‑op for simple budgets
}
```

Standard `Decision.Reason` strings (recommend budgets stick to these, optionally with suffixes):

- `"no_budget"`: no budget ref or no budgets registry configured.
- `"budget_not_found"`: budget ref name not registered.
- `"budget_denied"`: budget explicitly denied the attempt.
- `"panic_in_budget"`: budget panicked and `RecoverPanics` converted it to a denial.

#### Budget interface

```go
type Budget interface {
    AllowAttempt(ctx context.Context, key policy.PolicyKey, attemptIdx int, kind AttemptKind, ref policy.BudgetRef) Decision
}
```

#### Registry

```go
type Registry struct {
    mu sync.RWMutex
    m  map[string]Budget
}
func NewRegistry() *Registry
func (r *Registry) Register(name string, b Budget)
func (r *Registry) Get(name string) (Budget, bool)
```

### 4.2 Built‑in budgets

Ship at least:

1. **UnlimitedBudget** – always allows.
2. **TokenBucketBudget** (reference impl):
   - capacity + refill rate.
   - allow returns `Release` no‑op (token already consumed).

Document how to write custom budgets.

### 4.3 Policy integration (`policy`)

Avoid import cycles by keeping refs in `policy`:

```go
type BudgetRef struct {
    Name string
    Cost int // optional; 1 if unset
}

type RetryPolicy struct {
    // ...
    Budget BudgetRef
}

type HedgePolicy struct {
    // ...
    Budget BudgetRef
}
```

### 4.4 Executor integration (`retry`)

Extend options:

```go
type ExecutorOptions struct {
    // ...
    Budgets *budget.Registry
}
```

Default:

- If `Budgets` nil, treat as “no budgets” (all attempts allowed).

Implement helper:

```go
func (e *Executor) allowAttempt(ctx context.Context, key policy.PolicyKey, ref policy.BudgetRef, attemptIdx int, kind budget.AttemptKind) (budget.Decision, bool)
```

`attemptIdx` is the global, monotonically increasing attempt number per call (launch order), not reset per retry group. Budgets must not assume completion order.

Rules:

- If `ref.Name == ""` or no registry → allow with reason `"no_budget"`.
- If budget not found → follow `MissingBudgetMode` (default allow) and record reason `"budget_not_found"`.
- Else delegate to `Budget.AllowAttempt`, passing the full `policy.BudgetRef` so budgets can interpret `Cost`. If `RecoverPanics` is enabled and a budget panics, recover and abort/deny with reason `"panic_in_budget"`.

In retry loop:

1. Call `allowAttempt` before launching op.
2. Record `BudgetAllowed/Reason`.
3. If denied:
   - Record a denied `AttemptRecord` (do not call the user op).
   - For `kind == KindRetry`:
     - if this is the **first attempt** of the call: set outcome to `OutcomeAbort{Reason:"budget_denied"}` and fail fast.
     - if this is a **subsequent retry attempt**: stop retrying due to backpressure and return the last attempt’s error (while recording a clear “stopped due to budget” signal in the timeline).
   - For `kind == KindHedge` (used in Phase 5): record denied attempt and continue coordinating the retry group; hedge denial should not be treated as a terminal failure of the whole call.
4. If allowed and decision has `Release`:
   - Defer `Release()` **after attempt finishes** (including context cancellation).

Budgets can ignore cost (treating it as 1), but having it wired enables weighted backpressure models later without breaking the interface.

### 4.5 Tests

- Budget denies second attempt:
  - op called once; timeline has one real attempt plus one denied record; final error is the first attempt error (not a synthetic “budget denied” error), with timeline noting “stopped due to budget”.
- Missing budget name:
  - attempts proceed; reason = `"budget_not_found"`.
- Release called exactly once per allowed attempt (use a counting budget).

---

## Exit Criteria

- Every attempt passes through budget gating.
- Denials abort safely and are visible in timelines.
- Optional release semantics supported without complicating simple budgets.
