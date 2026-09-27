# Remote Configuration

Recourse supports dynamic policy configuration, allowing you to update retry policies, circuit breakers, and hedging settings at runtime without redeploying your application.

## RemoteProvider

The `controlplane.RemoteProvider` fetches policies from an external source (e.g., HTTP endpoint, database, or file) and caches them locally.

### Setup

```go
import (
    "context"
    "time"

    "github.com/aponysus/recourse/controlplane"
    "github.com/aponysus/recourse/policy"
    "github.com/aponysus/recourse/retry"
)

// Define a source
type MyHTTPSource struct { ... }
func (s *MyHTTPSource) GetPolicy(ctx context.Context, key policy.PolicyKey) (policy.EffectivePolicy, error) {
    // Fetch from URL, JSON unmarshal...
}

// Create the provider
provider := controlplane.NewRemoteProvider(
    &MyHTTPSource{},
    controlplane.WithCacheTTL(1 * time.Minute),
    controlplane.WithNegativeCacheTTL(10 * time.Second),
    controlplane.WithLKGTTL(5 * time.Minute),
)

// Register with Executor
exec := retry.NewExecutor(
    retry.WithProvider(provider),
    // Fallback behavior when no usable remote or LKG policy is available.
    retry.WithMissingPolicyMode(retry.FailureFallback),
)
```

## Caching

Each `RemoteProvider` maintains its own cache and coalesces concurrent fetches:

1. **Fresh policies**: Successfully fetched and normalized policies are cached for `CacheTTL` (default 1 minute). Non-positive values disable fresh caching.
2. **Missing policies**: `Source.GetPolicy` must return `controlplane.ErrPolicyNotFound` (possibly wrapped) for a confirmed missing policy. This result is cached for `NegativeCacheTTL` (default 10 seconds). Sources using HTTP should translate an authoritative 404 into this error. Non-positive values disable negative caching.
3. **Last-known-good (LKG)**: `WithLKGTTL` retains the last successfully normalized policy for an additional duration after the fresh TTL expires. LKG is disabled by default; zero or negative values disable it explicitly.
4. **Concurrent fetches**: Overlapping misses or refreshes for the same complete `policy.PolicyKey` share one source call and its result, including errors and missing results. Different keys fetch independently.
<!-- Claim-ID: CLM-018 -->

With a 1-minute fresh TTL and a 5-minute LKG TTL, a policy fetched at 12:00 is fresh until 12:01 and eligible for LKG fallback until 12:06. A failed refresh does not move either deadline. A successful refresh replaces the policy and starts both windows again. The LKG deadline is checked after a failed fetch returns, so a slow fetch cannot extend eligibility.

`PolicyCache.Get` returns only fresh entries. `SetWithLKG` and `GetLKG` provide explicit retention and retrieval for cache users; `Set` keeps its original signature and disables LKG for that entry. `GetLKG` returns the retained policy with its stored metadata, including while fresh. `SetMissing` and `Invalidate` discard retained LKG. Expired entries are not automatically evicted from memory; continue to use low-cardinality keys.

## Resolution Logic

When the executor asks the provider for a policy:

1. Check caller cancellation, then the fresh positive or negative cache.
2. On a cache miss, join an existing fetch for the key or start one. Recheck the cache before calling `Source.GetPolicy`.
3. On success, normalize the policy, cache it, and return it. Normalization failures remain errors and do not replace the previous good policy.
4. On `ErrPolicyNotFound`, discard any LKG and cache the missing result. A confirmed deletion must not later reappear as LKG.
5. On any other source error, return eligible LKG if the fetch context is still active. This includes source-owned timeouts; the `Source` interface has no transient/permanent error classification. Caller cancellation and deadline expiry remain errors and do not trigger LKG.
6. Without eligible LKG, return the source error. The executor then applies `MissingPolicyMode`.

When serving LKG, the provider returns a copy with `Meta.Source = policy.PolicySourceLKG` and a **nil error**. It does not change the stored policy's metadata. The executor records `policy_source=lkg` and `policy_mode=standard`, and uses the policy even under the default `FailureDeny` mode. Returning an error alongside LKG would instead invoke `MissingPolicyMode` and fail under `FailureDeny`.

LKG is used only after a failed fetch. Subsequent calls still attempt refresh once no fetch is in progress. Coalescing reduces overlapping source calls; it does not introduce a retry cooldown, cache source errors, or throttle sequential failed refreshes.

## Cancellation and shared fetches

The first caller's context, including its deadline and values, drives the shared `Source.GetPolicy` call. Sources must respect context cancellation. Other callers can cancel their own waits promptly without canceling the shared fetch.

If the first caller cancels or its deadline expires, the shared fetch fails and waiting callers receive that context error, even when LKG exists. A subsequent caller can start a new fetch with its own context. Source calls remain synchronous in the first caller; source panics propagate to callers so the executor's existing `RecoverPanics` option can handle them.
