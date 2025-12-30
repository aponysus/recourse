# Integrations

This page describes the contracts and constraints for the recourse integrations. It focuses on what each helper does, what it does not do, and the conditions required for safe use.

## Design goals

- Use standard interfaces (for example, `http.Client` and gRPC interceptors).
- Keep heavy dependencies opt-in by isolating integrations in separate modules.
- Make retry behavior explicit and observable rather than hidden.

---

## HTTP integration (`integrations/http`)

### What it does

- Provides `DoHTTP`, a wrapper around `http.Client.Do` that runs through a recourse executor.
- Clones the request for each attempt and replays the body via `req.GetBody` when present.
- Converts non-2xx responses and transport errors into `StatusError`, which implements `classify.HTTPError`.
- Drains and closes failed response bodies (up to 4KB) to support connection reuse.
- Returns the response, a captured `observe.Timeline`, and an error.

### Constraints and safety

- **Request bodies must be replayable**: if `req.Body` is set and `req.GetBody` is nil, `DoHTTP` returns an error.
- **Non-idempotent methods should not be retried**: use appropriate policies or classifiers.
- **Streaming responses are not retried**: failed attempts are drained and closed.
- **Timeouts are still your responsibility**: use policy timeouts and context deadlines.

### Example

```go
package main

import (
    "context"
    "net/http"

    integration "github.com/aponysus/recourse/integrations/http"
    "github.com/aponysus/recourse/policy"
    "github.com/aponysus/recourse/retry"
)

func main() {
    exec := retry.NewExecutor()
    client := &http.Client{}
    req, _ := http.NewRequest("GET", "http://api.example.com/data", nil)

    key := policy.PolicyKey{Name: "api.GetData"}
    resp, timeline, err := integration.DoHTTP(context.Background(), exec, key, client, req)
    _ = timeline
    if err != nil {
        return
    }
    defer resp.Body.Close()
}
```

---

## gRPC integration (`integrations/grpc`)

### What it does

- Provides `UnaryClientInterceptor`, which wraps unary client calls with a recourse executor.
- Maps gRPC method strings to policy keys via `DefaultKeyFunc`:
  - `"/Service/Method"` -> `{Namespace: "Service", Name: "Method"}`
- Provides `Classifier`, which maps gRPC status codes to retry outcomes.
- Provides `WithClassifier`, which sets the gRPC classifier as the executor default.

### Constraints and safety

- **Unary only**: there is no streaming interceptor in this package.
- **Key mapping must remain low-cardinality**: method strings are stable, but avoid embedding IDs in custom key functions.
- **Retry behavior depends on your policy**: use a classifier appropriate for gRPC.

### Example

For a runnable example, see `integrations/grpc/example/main.go`.
