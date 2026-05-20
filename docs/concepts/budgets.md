# Budgets & backpressure

Retries and hedges multiply load. Without explicit backpressure, an incident can devolve into a retry storm.

Budgets provide a gate for retries and hedges:

- **Allow** the attempt to proceed
- **Deny** the attempt to prevent more load
- Optionally return a **release** handle to model reservation-style resources
<!-- Claim-ID: CLM-010 -->

The base attempt (`attempt=0`) is not charged to the retry budget. Retry budgets
start applying when the executor is about to launch a retry attempt
(`attempt>=1`). Hedge budgets apply to hedge attempts.

## Wiring

Budgets are referenced from policy and resolved through an executor registry:

- Policy: `policy.RetryPolicy.Budget` (`Name`, `Cost`)
- Executor: `retry.ExecutorOptions.Budgets` (`*budget.Registry`)

## Built-in budgets

- `budget.UnlimitedBudget`: always allows
- `budget.TokenBucketBudget`: token bucket with capacity + refill rate
<!-- Claim-ID: CLM-011 -->

Example:

```go
budgets := budget.NewRegistry()
if err := budgets.Register("global", budget.NewTokenBucketBudget(100, 50)); err != nil {
	panic(err)
}

exec := retry.NewExecutor(retry.ExecutorOptions{
	Budgets: budgets,
})
```

## Missing budgets and failures

- If the budget name is empty, budgeted attempts are allowed with reason `"no_budget"`.
- If the registry is nil, the budget is missing, or the budget is nil, behavior is controlled by `retry.ExecutorOptions.MissingBudgetMode` (default: `retry.FailureDeny`) and the attempt records `"budget_registry_nil"`, `"budget_not_found"`, or `"budget_nil"`.
<!-- Claim-ID: CLM-012 -->
