# Policies & providers

Execution is driven by `policy.EffectivePolicy`, resolved by a `controlplane.PolicyProvider`.

## Effective policy

Policies are per-key and have (today) two main sub-policies:

- `Retry`: bounded attempts/backoff/timeouts + classifier/budget references
- `Hedge`: present in schema but hedging is not implemented yet

All policies are normalized/clamped via `EffectivePolicy.Normalize()` to prevent unsafe configs (busy loops, tiny timeouts, unbounded concurrency).

## Providers

Providers implement:

```go
GetEffectivePolicy(ctx context.Context, key policy.PolicyKey) (policy.EffectivePolicy, error)
```

Today, `rego` ships with `controlplane.StaticProvider` for in-process policy maps.

## Missing policy behavior

If policy resolution fails, the executor consults `ExecutorOptions.MissingPolicyMode`:

- `retry.FailureFallback` (default): use a safe default policy
- `retry.FailureAllow`: run a single attempt
- `retry.FailureDeny`: fail fast with `retry.ErrNoPolicy`

