# Classifiers

Retries are only safe when they respect protocol/domain semantics.

Classifiers implement:

```go
Classify(value any, err error) classify.Outcome
```

The executor records `Outcome` on every attempt, and uses it to decide whether to retry, stop, or abort immediately.

## Built-ins

Core built-ins include:

- `classify.AutoClassifier` (default): Intelligently dispatches to `HTTPClassifier` or `AlwaysRetryOnError` based on error type.
- `classify.ClassifierHTTP` (`"http"`): HTTP-aware decisions (e.g., 5xx retryable, 404 non-retryable, 429 may respect `Retry-After`).
- `integrations/grpc.Classifier`: gRPC status-code aware decisions (available via `integrations/grpc` module).

Select a classifier by name via `policy.RetryPolicy.ClassifierName`.

## Safety: type mismatches

If a classifier expects a specific value/error shape and receives something else, it should fail loudly and safely (e.g., non-retryable with a clear reason), not “retry blindly”.

