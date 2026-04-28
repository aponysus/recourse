# Migrating from cenkalti/backoff

This guide is for teams using [`github.com/cenkalti/backoff/v5`](https://pkg.go.dev/github.com/cenkalti/backoff/v5) at several call sites and starting to need shared retry policy, classification, budgets, and attempt-level diagnostics.

## When not to migrate

Use `cenkalti/backoff` if you only need a local retry loop at one or two call sites, the retry decision is obvious, and you do not need shared policy or structured observability.

Use `recourse` when retry behavior is becoming operational policy: stable keys, common defaults, domain classifiers, backpressure budgets, rollout control, and timelines for incident debugging.

## Concept mapping

| cenkalti/backoff | recourse |
| --- | --- |
| `backoff.Retry` | `recourse.Do` / `recourse.DoValue` or `retry.DoValue` |
| Operation closure | Operation closure that receives `context.Context` |
| `BackOff` implementation | Policy retry envelope |
| `WithMaxTries` | `policy.MaxAttempts` |
| `WithMaxElapsedTime` | `policy.OverallTimeout` |
| `backoff.Permanent(err)` | Classifier outcome: non-retryable |
| `WithNotify` | `observe.Observer` or `observe.RecordTimeline` |
| Local retry loop | Stable low-cardinality policy key |
| Per-call throttling outside the loop | `budget.Budget` |

## Before: local backoff loop

This is representative `cenkalti/backoff/v5` code. It keeps the retry envelope, permanent-error decision, and notification hook local to the call site.

```go
bo := backoff.NewExponentialBackOff()
bo.InitialInterval = 50 * time.Millisecond
bo.MaxInterval = 500 * time.Millisecond

receipt, err := backoff.Retry(ctx, func() (Receipt, error) {
	receipt, err := gateway.Charge(ctx, accountID, cents)
	if errors.Is(err, errInvalidCard) {
		return Receipt{}, backoff.Permanent(err)
	}
	return receipt, err
},
	backoff.WithBackOff(bo),
	backoff.WithMaxTries(3),
	backoff.WithMaxElapsedTime(2*time.Second),
	backoff.WithNotify(func(err error, next time.Duration) {
		log.Printf("payment retry: err=%v next=%s", err, next)
	}),
)
```

This is a good fit while the decision is local. It gets harder to govern when many services each choose their own max attempts, retryability rules, logs, and load-shedding behavior.

## After: classifier instead of permanent errors

In `recourse`, retryability is selected by policy and implemented by a classifier. That keeps the operation closure focused on doing the work.

```go
type paymentClassifier struct{}

func (paymentClassifier) Classify(_ any, err error) classify.Outcome {
	switch {
	case err == nil:
		return classify.Outcome{Kind: classify.OutcomeSuccess, Reason: "success"}
	case errors.Is(err, context.Canceled):
		return classify.Outcome{Kind: classify.OutcomeAbort, Reason: "context_canceled"}
	case errors.Is(err, errInvalidCard):
		return classify.Outcome{Kind: classify.OutcomeNonRetryable, Reason: "payment_invalid_card"}
	default:
		return classify.Outcome{Kind: classify.OutcomeRetryable, Reason: "payment_transient"}
	}
}
```

See [Classifiers](../concepts/classifiers.md) for built-ins and custom classifier guidance.

## After: facade path

Use the facade when you want one process-wide executor installed during startup.

```go
recourse.Init(retry.NewDefaultExecutor(
	retry.WithClassifier("payment", paymentClassifier{}),
	retry.WithPolicy("payments.Charge",
		policy.MaxAttempts(3),
		policy.ExponentialBackoff(10*time.Millisecond, 100*time.Millisecond),
		policy.Classifier("payment"),
		policy.Budget("unlimited"),
	),
))

ctx, capture := observe.RecordTimeline(ctx)
receipt, err := recourse.DoValue[Receipt](ctx, "payments.Charge", func(ctx context.Context) (Receipt, error) {
	return gateway.Charge(ctx, accountID, cents)
})

for _, attempt := range capture.Timeline().Attempts {
	log.Printf("attempt=%d reason=%s err=%v", attempt.Attempt, attempt.Outcome.Reason, attempt.Err)
}
```

Use a stable, low-cardinality key such as `"payments.Charge"`. Do not include account IDs, URLs with IDs, request IDs, or other per-request values. See [Policy keys](../concepts/policy-keys.md) and [Key patterns and taxonomy](../concepts/key-patterns.md).

## After: explicit executor path

Use the explicit executor path when you do not want global process state or when tests should own their own executor instance.

```go
exec := retry.NewDefaultExecutor(
	retry.WithClassifier("payment", paymentClassifier{}),
	retry.WithPolicy("payments.Charge",
		policy.MaxAttempts(3),
		policy.ExponentialBackoff(10*time.Millisecond, 100*time.Millisecond),
		policy.Classifier("payment"),
		policy.Budget("unlimited"),
	),
)

ctx, capture := observe.RecordTimeline(ctx)
key := policy.ParseKey("payments.Charge")

receipt, err := retry.DoValue[Receipt](ctx, exec, key, func(ctx context.Context) (Receipt, error) {
	return gateway.Charge(ctx, accountID, cents)
})
```

A complete compiling version of the recourse path lives in [`examples/migration_backoff`](https://github.com/aponysus/recourse/tree/main/examples/migration_backoff).

## Behavioral differences

- `recourse` requires stable low-cardinality policy keys because keys drive policy lookup and observability dimensions.
- `recourse` classifies attempt outcomes instead of relying on each operation to wrap permanent errors.
- `recourse` can record timelines with outcome reasons, backoff, budget decisions, policy source, and final error. See [Incident debugging](../incident-debugging.md).
- `recourse` supports budgets to prevent retry amplification during dependency incidents. See [Budgets & backpressure](../concepts/budgets.md).
- `recourse` separates policy from execution, so you can move from static in-process policy to provider-backed rollout without rewriting call sites. See [Getting started](../getting-started.md).

## Migration checklist

1. Pick one safe call site with an idempotent or otherwise retry-safe operation.
2. Choose a stable key, for example `"payments.Charge"`.
3. Translate `WithMaxTries`, backoff, and elapsed-time settings into policy options.
4. Move permanent-error decisions into a classifier.
5. Capture a timeline in tests or staging and confirm the attempt count, reasons, and final error.
6. Add a budget before broad rollout if the dependency can become overloaded.
