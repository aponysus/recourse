# Observability

Every call can produce an `observe.Timeline`, which contains an `observe.AttemptRecord` for each attempt.

## Timeline

A timeline captures:

- Call metadata (key, policy ID, attributes)
- Attempt records (start/end, outcome, error, backoff, budget gating)
- Final error

## Observer hooks

To stream events to logs/metrics/tracing, implement `observe.Observer` and pass it via `retry.ExecutorOptions.Observer`.

The observer receives detailed lifecycle events:
*   `OnStart` / `OnSuccess` / `OnFailure`: Request lifecycle.
*   `OnAttempt`: Individual attempt outcome.
*   `OnHedgeSpawn`: When a parallel hedge is launched.
*   `OnBudgetDecision`: When a budget token is requested (allowed/denied reason).

Standardized reasons (e.g., `"budget_denied"`, `"circuit_open"`) are provided for consistent metrics.

## Attempt metadata in context

Each attempt context includes `observe.AttemptInfo` (attempt index, retry index, hedge fields reserved for later phases, policy ID), accessible via:

```go
info, ok := observe.AttemptFromContext(ctx)
```

