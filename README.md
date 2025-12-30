# recourse

Policy-driven, observable resilience for Go services: retries, hedging, circuit breaking, and budgets.

[![Go Reference](https://pkg.go.dev/badge/github.com/aponysus/recourse.svg)](https://pkg.go.dev/github.com/aponysus/recourse)
[![Go Report Card](https://goreportcard.com/badge/github.com/aponysus/recourse)](https://goreportcard.com/report/github.com/aponysus/recourse)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

> recourse (n.): a source of help or strength.

Docs site: https://aponysus.github.io/recourse/
Changelog: [CHANGELOG.md](CHANGELOG.md)

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
- **Classifiers**: outcomes are protocol-aware instead of "retry on any error".
- **Backpressure**: budgets gate attempts to prevent load amplification.
- **Explainability**: timelines and observer hooks make behavior debuggable.

## Install

```bash
go get github.com/aponysus/recourse@latest
```

## Quick start

```go
package main

import (
	"context"

	"github.com/aponysus/recourse/recourse"
)

type User struct{ ID string }

func main() {
	ctx := context.Background()

	user, err := recourse.DoValue[User](ctx, "user-service.GetUser", func(ctx context.Context) (User, error) {
		return User{ID: "123"}, nil
	})
	_ = user
	_ = err
}
```

## Debugging story

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

For streaming logs/metrics/tracing, implement `observe.Observer`. See the observability docs for details.

## Docs

- Design overview: https://aponysus.github.io/recourse/design-overview/
- Getting started: https://aponysus.github.io/recourse/getting-started/
- Gotchas and safety checklist: https://aponysus.github.io/recourse/gotchas/
- Adoption guide: https://aponysus.github.io/recourse/adoption-guide/
- Incident debugging: https://aponysus.github.io/recourse/incident-debugging/
- Comparison: https://aponysus.github.io/recourse/comparison/
- Why recourse: https://aponysus.github.io/recourse/blog/why-recourse/
- Policy schema reference: https://aponysus.github.io/recourse/reference/policy-schema/
- Reason codes and timelines: https://aponysus.github.io/recourse/reference/reason-codes/

## Compatibility policy

- v1.x follows SemVer; exported APIs in the core packages are stable.
- Stable packages: `recourse`, `retry`, `policy`, `observe`, `classify`, `budget`, `controlplane`, `circuit`, `hedge`, `integrations/http`.
- `integrations/grpc` is a separate module with its own tags (intended to track root releases).
- `internal` and `examples` are not part of the API contract.
- Telemetry fields and reason codes are treated as stable and documented in the generated references.

## Versioning

- Current release tag: v0.1.0
- Go version: see `go.mod` (currently 1.23)

## Contributing

- See [CONTRIBUTING.md](CONTRIBUTING.md).
- Onboarding: `docs/onboarding.md`
- Extending: `docs/extending.md`

## License

Apache-2.0. See `LICENSE`.
