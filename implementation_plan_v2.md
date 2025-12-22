# Recourse Implementation Plan (v2)

This document is the authoritative technical specification for the remaining implementation phases of `recourse`. 

**Architecture Philosophy**:
1.  **Stdlib-only Core**: Core packages (`retry`, `policy`, `hedge`, `circuit`) must remain dependency-free.
2.  **Policy-Driven**: All behavior is configured via `policy.EffectivePolicy`.
3.  **Observability-First**: State changes and decisions (circuit open, hedge spawn, budget denial) must be visible via `observe.Timeline` and `observe.Observer`.

---

## Phase 1: Basic Hedging (Fixed-Delay)

**Objective**: Integrate parallel attempt execution into the retry loop to reduce tail latency.

### 1.1 Trigger Interface (`hedge`)

The trigger decides *when* to spawn a parallel attempt.

```go
package hedge

type HedgeState struct {
    CallStart        time.Time     // When the overall call started
    AttemptStart     time.Time     // When the current retry group started
    AttemptsLaunched int           // Number of attempts in this group (primary + hedges)
    MaxHedges        int           // Configured max hedges
    Elapsed          time.Duration // Time since AttemptStart
    
    // LatencyStats will be added in Phase 2
}

type Trigger interface {
    // ShouldSpawnHedge returns true if a new hedge should be spawned.
    // nextCheckIn returns the duration to wait before checking again.
    // If nextCheckIn is 0, the executor uses a default enforcement interval (e.g., 25ms).
    ShouldSpawnHedge(state HedgeState) (should bool, nextCheckIn time.Duration)
}

// Registry manages named triggers for policy integration
type Registry struct { ... }
```

### 1.2 Policy Configuration (`policy`)

Extend `HedgeConfig` to support fixed-delay and trigger-based hedging.

```go
type HedgeConfig struct {
    Enabled               bool          
    MaxHedges             int           // Max parallel attempts per retry group
    HedgeDelay            time.Duration // Fallback delay if Trigger is empty
    Trigger               string        // Name of trigger in registry
    CancelOnFirstTerminal bool          // If true, first non-retryable failure cancels the group
    Budget                BudgetRef     // Budget for hedge attempts (separate from Retry budget)
}
```

### 1.3 Executor Logic (`retry`)

The executor's inner loop (`doOneAttempt`) transforms into a `doRetryGroup` coordination function.

**Coordination Logic**:
1.  **Primary Attempt**: Launch immediately (goroutine).
2.  **Timer Loop**:
    *   Check `Trigger.ShouldSpawnHedge`.
    *   If yes, check `Budget`.
    *   If allowed, launch **Hedge Attempt** (goroutine).
3.  **Result Aggregation**:
    *   **Success Wins**: The first successful result returns immediately.
        *   Action: Cancel other in-flight attempts (Winner Cancellation).
    *   **Fail Fast**: If `CancelOnFirstTerminal` is true and a *terminal* failure (non-retryable) occurs:
        *   Action: Return failure immediately, cancel others.
    *   **Fail Slow**: If `CancelOnFirstTerminal` is false, ignore terminal failures until all attempts complete or a success occurs.
4.  **Cancellation Safety**:
    *   Use `context.WithCancelCause` (Go 1.20+) to distinguish internal cancellation (winner found) from external cancellation (user deadline).
    *   Internal cancellation results (`context.Canceled`) should be ignored/filtered from modifying the retry state.

---

## Phase 2: Latency-Aware Hedging

**Objective**: Drive hedging using dynamic latency statistics (P90/P99) rather than static delays.

### 2.1 Latency Tracker (`hedge`)

A lock-free or mutex-protected ring buffer to track recent duration samples.

```go
type LatencyTracker interface {
    Observe(d time.Duration)
    Snapshot() LatencySnapshot
}

type LatencySnapshot struct {
    P50, P90, P99 time.Duration
}
```

**Implementation**:
*   Fixed-size ring buffer (e.g., `256` slots).
*   `Snapshot()` computes approximate quantiles.
*   Must be thread-safe.

### 2.2 Executor Integration

*   **State**: `Executor` maintains `map[policy.PolicyKey]LatencyTracker`.
*   **Feed**: Feed duration of *completed* attempts (success or failure) to the tracker.
*   **Guardrail**: Wrap the map in an LRU (max items ~1000) to prevent memory leaks from high-cardinality keys.

### 2.3 Latency Triggers

Implement standard triggers in `hedge` package:
*   `"p95"`: Spawn hedge if `elapsed > Snapshot.P95`.
*   `"p99"`: Spawn hedge if `elapsed > Snapshot.P99`.

---

## Phase 3: Circuit Breakers

**Objective**: Fail fast when a dependency is unhealthy, independent of specific error classification on individual attempts.

### 3.1 Primitives (`circuit`)

```go
package circuit

type State int
const (
    StateClosed   State = iota // Normal
    StateOpen                  // Failing fast
    StateHalfOpen              // Probing
)

type Decision struct {
    Allowed bool
    State   State
    Reason  string
}

type CircuitBreaker interface {
    Allow(ctx context.Context) Decision
    RecordSuccess(ctx context.Context)
    RecordFailure(ctx context.Context)
    State() State
}
```

### 3.2 Breaker Implementations

1.  **CountingBreaker**: Opens after `N` consecutive failures within `Window`.
2.  **RateBreaker**: Opens if failure rate > `X%` within `Window` (min requests `M`).

**State Machine**:
*   **Closed** -> **Open**: Threshold exceeded.
*   **Open** -> **HalfOpen**: `Cooldown` duration expired.
*   **HalfOpen**:
    *   Allows `MaxProbes` concurrent requests.
    *   If Success: Increment probe counter. If `ProbeSuccesses` met -> **Closed**.
    *   If Failure: Immediate -> **Open** (reset cooldown).

### 3.3 Registry (`circuit`)

*   `BreakerRegistry` manages breakers by `PolicyKey`.
*   **Updates**: Must handle configuration changes (policy updates). If config changes, the breaker should conceptually "reset" or migrate. Simple approach: Reset if config differs significantly.
*   **Eviction**: LRU eviction for unused breakers.

### 3.4 Executor Integration

Update `retry.Executor`:

1.  **Pre-Loop Check**:
    ```go
    cb := exec.circuitRegistry.Get(key, pol.Circuit)
    if decision := cb.Allow(ctx); !decision.Allowed {
        // Record "circuit_open" in timeline
        return CircuitOpenError{State: decision.State} // Fail fast
    }
    ```
2.  **Post-Attempt Reporting**:
    *   On `OutcomeSuccess`: `cb.RecordSuccess()`.
    *   On `OutcomeRetryable` / `OutcomeNonRetryable`: `cb.RecordFailure()`.
    *   On `OutcomeAbort`: Do not report (usually user cancel).
3.  **Half-Open Constraint**:
    *   If `cb.State() == StateHalfOpen`, **disable hedging**. Probes should be minimal.

### 3.5 Policy Options

Add functional options:
*   `policy.CircuitDefaults()`: Rate > 50%, 10s Window.
*   `policy.CircuitSensitive()`: Threshold 5 failures.

---

## Phase 4: Remote Control Plane

**Objective**: Dynamic policy management via HTTP.

### 4.1 RemoteProvider (`controlplane`)

**Config**:
*   `Endpoint`: URL template (e.g., `https://api.internal/policies/{namespace}/{name}`).
*   `RefreshInterval`: polling duration.
*   `TTL`: cache TTL.

**Logic**:
1.  **Fetch**: HTTP GET to endpoint.
2.  **Cache**: Shared `internal/cache` (LRU) to store `EffectivePolicy`.
3.  **Negative Caching**: Cache 404s (as "Policy Not Found") with short TTL to prevent hammering.
4.  **LKG Fallback**:
    *   If fetch fails (network/500), return cached policy (stale).
    *   If no cache, return error (Executor handles `MissingPolicyMode`).

---

## Phase 5: Hardening & Release

### 5.1 Benchmarks
*   **Fast Path Overhead**: Measure overhead of `Allow()` checks when functionality is disabled or defaults are used.
*   **Timeline Overhead**: Measure allocation cost of full timeline capture.

### 5.2 Concurrency Safety
*   Run `go test -race ./...` on strict mode.
*   Verify `LatencyTracker` ring buffer is race-free.

### 5.3 Extension Guide
*   Document how to write custom `HedgeTrigger` and `CircuitBreaker` implementations.
