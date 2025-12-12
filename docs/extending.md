# Extending `rego` (early draft)

This document describes the extension pattern used by `rego` and how to plug in custom behavior.

`rego` is designed so the core stays **stdlib-only** and most behavior is selected by **policy** and resolved via **registries**.

## Extension points

`rego` supports (or will support) extension via these interfaces:

- **Classifiers** (`classify.Classifier`): decide whether an attempt outcome is success, retryable, non-retryable, or abort.
- **Budgets** (`budget.Budget`): gate attempts to prevent retry/hedge storms.
- **Hedge triggers** (`hedge.HedgeTrigger`): decide when to spawn hedged attempts.
- **Observers** (`observe.Observer`): receive structured attempt/timeline events.

## Registries

Registries are thread-safe maps from a low-cardinality name (string) to an implementation. Policies typically refer to implementations by name.

At a high level:

1. Create a registry (`classify.NewRegistry()`, `budget.NewRegistry()`, `hedge.NewTriggerRegistry()`).
2. Register your implementation under a name (e.g. `"my_classifier"`).
3. Configure your executor to use that registry.
4. Reference the name from policy (`Retry.ClassifierName`, etc.).

## Writing a custom classifier

Implement:

- `Classify(value any, err error) classify.Outcome`

Guidelines:

- Prefer conservative behavior: if you can’t determine retry safety, return `OutcomeNonRetryable` (or `OutcomeAbort` for cancellation).
- Avoid high-cardinality attributes.
- Treat type mismatches as configuration errors (not retryable).

## Writing a custom budget

Implement:

- `AllowAttempt(ctx, key, attemptIdx, kind, ref) budget.Decision`

Guidelines:

- Keep `AllowAttempt` fast and concurrency-safe.
- Use `ref.Cost` to support weighted backpressure if applicable.
- If you return a `Decision.Release`, it must be safe to call exactly once.

## Writing a custom hedge trigger

Implement:

- `ShouldSpawnHedge(state hedge.HedgeState) (should bool, nextCheckIn time.Duration)`

Guidelines:

- Return a sensible `nextCheckIn` to avoid tight polling.
- Respect `MaxHedges` and don’t spawn multiple hedges in a single evaluation tick.

## Versioning note

This extension surface will stabilize during the `v0.x` series. Until `v1.0.0`, consider all APIs provisional.

