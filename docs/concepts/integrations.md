# Integrations

`recourse` provides drop-in integrations to make using resilience policies with standard Go libraries ergonomic and correct.

## Philosophy

- **Standard Library First**: Integrations hook into standard interfaces (`http.Client`, `grpc.ClientConn`) rather than wrapping them in heavy custom types.
- **Opt-in Dependencies**: The core `recourse` library never imports heavy third-party packages (like gRPC). Integrations that require them are tied to separate modules or sub-packages to keep your dependency graph clean.
- **Correctness**: Helpers automatically handle subtleties like draining response bodies (HTTP) or classifying protocol-specific error codes (gRPC).

---

## HTTP Integration

The `integrations/http` package provides resilience for `net/http` calls.

### Usage

Use `DoHTTP` instead of `client.Do`. It handles request cloning (for deadline/retries) and response body management.

```go
package main

import (
    "context"
    "net/http"
    "time"

    integration "github.com/aponysus/recourse/integrations/http"
    "github.com/aponysus/recourse/retry"
    "github.com/aponysus/recourse/policy"
)

func main() {
    // 1. Setup Executor (or use recourse.Do)
    exec := retry.NewDefaultExecutor()

    // 2. Standard HTTP client
    client := &http.Client{Timeout: 5 * time.Second}
    req, _ := http.NewRequest("GET", "http://api.example.com/data", nil)

    // 3. Execute with resilience
    key := policy.PolicyKey{Name: "api.GetData"}
    resp, timeline, err := integration.DoHTTP(context.Background(), exec, key, client, req)
    
    if err != nil {
        // Handle error (timeline contains attempt details)
        return
    }
    defer resp.Body.Close()
    
    // consume body...
}
```

### Features

- **Status Classification**: Automatically retries 5xx errors (except 501) and respects `Retry-After` headers on 429/503 responses.
- **Resource Management**: Automatically drains and closes response bodies for failed attempts to ensure connection reuse.
- **Timeline Integration**: Captures HTTP-specific metadata (status code, method) in the `observe.Timeline`.

---

## gRPC Integration

The `integrations/grpc` package provides a `UnaryClientInterceptor` for `google.golang.org/grpc`.

> [!NOTE]
> This integration is a **separate module** to avoid forcing gRPC dependencies on all users.

### Installation

```bash
go get github.com/aponysus/recourse/integrations/grpc
```

### Usage

Add the interceptor to your gRPC connection options.

```go
package main

import (
    "google.golang.org/grpc"
    
    "github.com/aponysus/recourse/retry"
    integration "github.com/aponysus/recourse/integrations/grpc"
)

func main() {
    // 1. Setup Executor with gRPC capabilities
    // This allows the executor to understand gRPC status codes.
    exec := retry.NewDefaultExecutor(integration.WithClassifier())

    // 2. Create Interceptor
    // DefaultKeyFunc maps "/Service/Method" -> policy key "Service.Method"
    interceptor := integration.UnaryClientInterceptor(exec, nil)

    // 3. Dial with Interceptor
    conn, err := grpc.NewClient("localhost:50051",
        grpc.WithUnaryInterceptor(interceptor),
    )
    // ...
}
```

### Features

- **Status Classification**: Smart defaults for gRPC codes:
    - Retries: `Unavailable`, `ResourceExhausted`.
    - Aborts: `Canceled`.
    - Fails: `InvalidArgument`, `Unimplemented`, etc.
    - Context deadlines (`DeadlineExceeded`) are respected and annotated.
- **Metadata**: Maps usage of `grpc.Method` to `recourse` policy keys automatically.

---
