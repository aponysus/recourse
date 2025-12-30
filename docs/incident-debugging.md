# Incident debugging with timelines

This is a practical runbook for understanding what recourse did during a call. The goal is to answer: what happened on each attempt, and why did it stop.

## Capture a timeline

Use `observe.RecordTimeline` at the call site you want to inspect:

```go
package main

import (
    "context"

    "github.com/aponysus/recourse/observe"
    "github.com/aponysus/recourse/recourse"
)

func main() {
    ctx, capture := observe.RecordTimeline(context.Background())

    _ = recourse.Do(ctx, "user-service.GetUser", func(ctx context.Context) error {
        return nil
    })

    tl := capture.Timeline()
    _ = tl
}
```

If you need streaming events for logs or metrics, implement `observe.Observer` and attach it in the executor options. See [Observability](concepts/observability.md).

## Triage checklist

Start with the basics:

1. **How many attempts ran?** Check `len(tl.Attempts)`.
2. **What was the final error?** Inspect `tl.FinalErr` and compare it to the last attempt error.
3. **Why did each attempt stop?** Inspect `AttemptRecord.Outcome.Kind` and `AttemptRecord.Outcome.Reason`.

Then dig into common failure modes:

- **Backoff and timing**: Compare `AttemptRecord.Backoff` to the time between attempts.
- **Budgets**: Check `AttemptRecord.BudgetAllowed` and `AttemptRecord.BudgetReason`. If you use an observer, the `BudgetDecisionEvent` will include the mode and reason.
- **Hedging**: Look for `AttemptRecord.IsHedge` and `AttemptRecord.HedgeIndex` to see which attempts were hedges.
- **Circuit breaking**: Inspect `AttemptRecord.Err` and `AttemptRecord.Outcome.Reason` for signals that the circuit short-circuited the call.
- **Policy resolution**: Inspect `tl.Attributes` for provider and normalization metadata when present.

## Questions you should be able to answer

- Did the call retry, or stop after the first attempt?
- Was the stop due to classification, budgets, circuit state, or timeouts?
- Did hedging improve latency or just add load?
- Were any attempts denied before they ran?

## Suggested fields to log

For logs and metrics, capture a small, low-cardinality set of fields:

- `key` (policy key)
- `attempt`
- `outcome.kind` and `outcome.reason`
- `backoff_ms`
- `budget_allowed` and `budget_reason`
- `is_hedge`
- `duration_ms`

Keep labels low-cardinality and avoid embedding IDs in keys or attributes.
