# Circuit Breaking

Circuit Breakers prevent cascading failures by temporarily halting requests to a dependency that is known to be failing.

## Overview

When a downstream service fails repeatedly, continuing to send requests wastes resources and can exacerbate the failure. A Circuit Breaker detects this pattern and "trips" (opens), causing subsequent requests to fail fast without invoking the dependency.

`recourse` implements a **Consecutive Failure Breaker**.

## States

1.  **Closed (Normal)**: Requests are allowed. Successes and failures are monitored.
2.  **Open (Failing)**: Threshold exceeded. All requests fail fast with `CircuitOpenError`.
3.  **Half-Open (Probing)**: After a `Cooldown` period, the breaker allows a limited number of "probe" requests.
    *   **Success**: The breaker resets to **Closed**.
    *   **Failure**: The breaker returns to **Open**.

## Configuration

Circuit Breaking is configured via `CircuitPolicy` within `EffectivePolicy`.

```go
pol := policy.New("remote-api")
pol.Circuit = policy.CircuitPolicy{
    Enabled:   true,
    Threshold: 5,               // Open after 5 consecutive failures
    Cooldown:  10*time.Second,  // Wait 10s before probing
}
```

## Behavior

*   **Fast Fail**: When open, requests return a `CircuitOpenError` immediately.
*   **Probing**: In Half-Open state, only one probe is allowed at a time.
*   **Hedging**: Hedging is **disabled** when the breaker is in Half-Open state to avoid overloading the recovering dependency.
*   **Observability**: `CircuitOpenError` includes the state and reason (`"circuit_open"`, `"circuit_half_open_probe_limit"`).
