# Budgets & backpressure

Retries and hedges multiply load. Without explicit backpressure, an incident can devolve into a retry storm.

Budgets provide a per-attempt gate:

- **Allow** the attempt to proceed
- **Deny** the attempt to prevent more load
- Optionally return a **release** handle to model reservation-style resources

## Wiring

Budgets are referenced from policy and resolved through an executor registry:

- Policy: `policy.RetryPolicy.Budget` (`Name`, `Cost`)
- Executor: `retry.ExecutorOptions.Budgets` (`*budget.Registry`)

## Built-in budgets

- `budget.UnlimitedBudget`: always allows
- `budget.TokenBucketBudget`: token bucket with capacity + refill rate

Example:

```go
budgets := budget.NewRegistry()
budgets.Register("global", budget.NewTokenBucketBudget(100, 50))

exec := retry.NewExecutor(retry.ExecutorOptions{
	Budgets: budgets,
})
```

## Missing budgets and failures

- If no budget is configured (empty name or no registry), attempts are allowed with reason `"no_budget"`.
- If a policy references an unknown budget name, behavior is controlled by `retry.ExecutorOptions.MissingBudgetMode` (default: allow) and the attempt records `"budget_not_found"`.

