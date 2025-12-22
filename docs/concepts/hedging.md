# Hedging

Hedging determines whether to execute a subsequent attempt while the previous one is still in flight, aiming to reduce tail latency by racing multiple attempts.

## Overview

In distributed systems, a single request might occasionally experience high latency due to transient issues (GC pauses, network congestion, etc.). Hedging mitigates this by speculatively starting a second request if the first one is slow. The first successful response "wins", and all other attempts are cancelled.

`recourse` supports two types of hedging:
1.  **Fixed-Delay**: Spawn a hedge after a static duration (e.g., 10ms).
2.  **Latency-Aware**: Spawn a hedge dynamically based on recent latency statistics (e.g., if slower than P99).

## Configuration

Hedging is configured via the `HedgePolicy` struct within an `EffectivePolicy`.

### Zero-Config (Fixed Delay)

To enable simple fixed-delay hedging:

```go
policy.New("my-service",
    policy.WithHedge(policy.HedgePolicy{
        Enabled:    true,
        HedgeDelay: 10 * time.Millisecond,
        MaxHedges:  2,
    }),
)
```

If the primary attempt takes longer than `10ms`, a second attempt is launched. If that also takes longer than `10ms` (relative to its start), a third is launched (up to `MaxHedges`).

### Latency-Aware (Dynamic)

To enable dynamic hedging based on observed latency:

```go
policy.New("my-service",
    policy.WithHedge(policy.HedgePolicy{
        Enabled:     true,
        TriggerName: "p99", // dynamic trigger
        MaxHedges:   2,
    }),
)
```

This requires registering a `LatencyTrigger` with the executor:

```go
triggers := hedge.NewRegistry()
triggers.Register("p99", hedge.LatencyTrigger{Percentile: "p99"})

exec := retry.NewExecutor(retry.WithHedgeTriggerRegistry(triggers))
```

The executor automatically tracks latency P-values (P50, P90, P99) for each policy key using a ring buffer.

## Behavior

*   **Winner-Takes-All**: The first successful response cancels all other in-flight attempts.
*   **Fail-Fast**: If `CancelOnFirstTerminal` is set to `true`, a non-retryable error from *any* attempt will cancel the entire group. Otherwise, the executor waits for other attempts.
*   **Budgets**: Hedged attempts consume budget tokens (checked against `Hedge.Budget` if specified, or falling back to the retry group budget).
*   **Observability**: `OnHedgeSpawn` is called on the observer when a hedge is launched. `AttemptRecord` includes `IsHedge` and `HedgeIndex`.
