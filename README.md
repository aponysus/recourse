# recourse

Policy-driven, observable resilience for Go services: retries, hedging, circuit breaking, and budgets.

[![Go Reference](https://pkg.go.dev/badge/github.com/aponysus/recourse/recourse.svg)](https://pkg.go.dev/github.com/aponysus/recourse/recourse)
[![Go Report Card](https://goreportcard.com/badge/github.com/aponysus/recourse)](https://goreportcard.com/report/github.com/aponysus/recourse)
[![Coverage](https://codecov.io/gh/aponysus/recourse/branch/main/graph/badge.svg)](https://codecov.io/gh/aponysus/recourse)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

> recourse (n.): a source of help or strength.

Docs site: https://aponysus.github.io/recourse/

Changelog: [CHANGELOG.md](CHANGELOG.md)

## Status

v1.x — API stable, with a documented [compatibility guarantee](https://aponysus.github.io/recourse/reference/compatibility/).

## When to use recourse

- You have multiple services and want consistent retry behavior.
- You need per-attempt visibility for incident debugging.
- You want explicit backpressure to avoid retry storms.
- You are willing to enforce low-cardinality policy keys and governance.

## When not to use recourse

- You only need a simple retry helper at one or two call sites.
- The operation is not safe to retry and you cannot add idempotency safeguards.
- You do not want to manage keys, policies, or rollout discipline.

## What makes it different

- **Policy keys**: call sites provide a stable key; policies define the retry envelope.
  <!-- Claim-ID: CLM-019 -->
- **Classifiers**: outcomes are protocol-aware instead of "retry on any error".
- **Backpressure**: budgets gate attempts to prevent load amplification.
  <!-- Claim-ID: CLM-010 -->
- **Explainability**: timelines and observer hooks make behavior debuggable.
  <!-- Claim-ID: CLM-013 -->
  <!-- Claim-ID: CLM-014 -->

## Install

```bash
go get github.com/aponysus/recourse@latest
```

## Quick start

### Zero-config facade

For the lowest-friction path, call the facade with a stable, low-cardinality key.
If you do not call `recourse.Init`, recourse lazily creates the default executor and uses `policy.DefaultPolicyFor(key)`.
That default policy gives you a bounded retry envelope, so you can see retry behavior immediately.

```go
package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/aponysus/recourse/observe"
	"github.com/aponysus/recourse/recourse"
)

type User struct{ ID string }

func main() {
	ctx, capture := observe.RecordTimeline(context.Background())
	attempts := 0

	user, err := recourse.DoValue[User](ctx, "user-service.GetUser", func(ctx context.Context) (User, error) {
		attempts++
		if attempts == 1 {
			return User{}, errors.New("temporary upstream error")
		}
		return User{ID: "123"}, nil
	})

	fmt.Printf("user=%s err=%v attempts=%d\n", user.ID, err, attempts)
	for _, attempt := range capture.Timeline().Attempts {
		fmt.Printf("attempt=%d reason=%s err=%v\n", attempt.Attempt, attempt.Outcome.Reason, attempt.Err)
	}
}
```

[Run a retry + timeline example in Go Playground](https://go.dev/play/p/AjNOC90vnpb)

### Explicit policies at startup

For application startup, wire explicit policies into an executor and install it before the first call:

```go
package main

import (
	"context"
	"time"

	"github.com/aponysus/recourse/controlplane"
	"github.com/aponysus/recourse/policy"
	"github.com/aponysus/recourse/recourse"
	"github.com/aponysus/recourse/retry"
)

type User struct{ ID string }

func main() {
	key := policy.ParseKey("user-service.GetUser")
	policies := map[policy.PolicyKey]policy.EffectivePolicy{
		key: policy.NewFromKey(key,
			policy.MaxAttempts(3),
			policy.ConstantBackoff(20*time.Millisecond),
		),
	}

	recourse.Init(retry.NewDefaultExecutor(
		retry.WithProvider(&controlplane.StaticProvider{Policies: policies}),
	))

	user, err := recourse.DoValue[User](context.Background(), key.String(), func(ctx context.Context) (User, error) {
		return User{ID: "123"}, nil
	})
	_ = user
	_ = err
}
```

Call `recourse.Init` only during startup, before any `recourse.Do` or `recourse.DoValue` call. If you prefer explicit dependency ownership, use `retry.DoValue(ctx, exec, key, op)` directly instead of the global facade.

## Debugging

Capture a timeline when you need to answer "what happened on each attempt":

```go
package main

import (
	"context"

	"github.com/aponysus/recourse/observe"
	"github.com/aponysus/recourse/recourse"
)

func main() {
	ctx, capture := observe.RecordTimeline(context.Background())

	_ = recourse.Do(ctx, "user-service.GetUser", func(ctx context.Context) error {
		return nil
	})

	if tl := capture.Timeline(); tl != nil {
		for _, a := range tl.Attempts {
			// a.Outcome, a.Err, a.Backoff, a.BudgetAllowed, ...
			_ = a
		}
	}
}
```
<!-- Claim-ID: CLM-013 -->

For streaming logs/metrics/tracing, implement `observe.Observer`. See the observability docs for details.
<!-- Claim-ID: CLM-014 -->

## Docs

- Design overview: https://aponysus.github.io/recourse/design-overview/
- Getting started: https://aponysus.github.io/recourse/getting-started/
- Gotchas and safety checklist: https://aponysus.github.io/recourse/gotchas/
- Key patterns and taxonomy: https://aponysus.github.io/recourse/concepts/key-patterns/
- Adoption guide: https://aponysus.github.io/recourse/adoption-guide/
- Incident debugging: https://aponysus.github.io/recourse/incident-debugging/
- Comparison: https://aponysus.github.io/recourse/comparison/
- Why recourse: https://aponysus.github.io/recourse/blog/why-recourse/
- Defaults and safety model: https://aponysus.github.io/recourse/reference/defaults-safety/
- Policy schema reference: https://aponysus.github.io/recourse/reference/policy-schema/
- Reason codes and timelines: https://aponysus.github.io/recourse/reference/reason-codes/

## Compatibility policy

- v1.x follows SemVer; exported APIs in the core packages are stable.
- Stable packages: `recourse`, `retry`, `policy`, `observe`, `classify`, `budget`, `controlplane`, `circuit`, `hedge`, `integrations/http`.
- `integrations/grpc` and `integrations/otel` are separate modules with their own tags (intended to track root releases).
- `internal` and `examples` are not part of the API contract.
- Telemetry fields and reason codes are treated as stable and documented in the generated references.
<!-- Claim-ID: CLM-020 -->

## Versioning

- Releases: see `CHANGELOG.md` or GitHub releases.
- Go version: see `go.mod` (currently 1.23)
<!-- Claim-ID: CLM-021 -->

## Contributing

- See [CONTRIBUTING.md](CONTRIBUTING.md).
- Onboarding: `docs/onboarding.md`
- Extending: `docs/extending.md`

## License

Apache-2.0. See `LICENSE`.
