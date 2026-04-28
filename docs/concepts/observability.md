# Observability

When timeline capture is enabled, calls can produce an `observe.Timeline`, which contains an `observe.AttemptRecord` for each attempt.

## Timeline

A timeline captures:

- Call metadata (key, policy ID, attributes)
- Attempt records (start/end, outcome, error, backoff, budget gating)
- Final error
<!-- Claim-ID: CLM-013 -->

## Observer hooks

To stream events to logs/metrics/tracing, implement `observe.Observer` and pass it via `retry.ExecutorOptions.Observer`.

The observer receives detailed lifecycle events:
*   `OnStart` / `OnSuccess` / `OnFailure`: Request lifecycle.
*   `OnAttempt`: Individual attempt outcome.
*   `OnHedgeSpawn`: When a parallel hedge is launched.
*   `OnBudgetDecision`: When a budget token is requested (allowed/denied reason).
<!-- Claim-ID: CLM-014 -->

Reason codes are documented in the references; use them for consistent metrics.

For OpenTelemetry tracing, use the first-class `github.com/aponysus/recourse/integrations/otel` module. It converts completed recourse timelines into spans with low-cardinality policy and attempt attributes.

## Attempt metadata in context

Each attempt context includes `observe.AttemptInfo` (attempt index, retry index, hedge fields, policy ID), accessible via:

```go
info, ok := observe.AttemptFromContext(ctx)
```
<!-- Claim-ID: CLM-024 -->
