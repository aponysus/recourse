# Remote Configuration

Recourse supports dynamic policy configuration, allowing you to update retry policies, circuit breakers, and hedging settings at runtime without redeploying your application.

## RemoteProvider

The `controlplane.RemoteProvider` fetches policies from an external source (e.g., HTTP endpoint, database, or file) and caches them locally.

### Setup

```go
import (
    "github.com/aponysus/recourse/controlplane"
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
)

// Register with Executor
exec := retry.NewExecutor(
    retry.WithProvider(provider),
    // Fallback behavior if remote is unavailable/missing
    retry.WithMissingPolicyMode(retry.FailureFallback), 
)
```

## Caching

To prevent hammering the control plane, `RemoteProvider` implements robust caching:

1.  **TTL Cache**: Successfully fetched policies are cached for `CacheTTL` (default 1 min).
2.  **Negative Caching**: If a policy is not found (404), this result is cached for `NegativeCacheTTL` (default 10s) to prevent hot-spotting on missing keys.

## Resolution Logic

When `exec.Do(ctx, "key", op)` is called:
1.  **Cache Lookup**: The provider checks its local cache.
2.  **Fetch**: If missing/expired, it calls result `Source.GetPolicy`.
3.  **Fallback**: If the source errors (network down), the executor falls back based on `MissingPolicyMode` (e.g., using a static default or failing closed).
