# API compatibility policy

This document describes the v1.x compatibility contract for recourse.

## SemVer policy

- The root module follows SemVer.
- For v1.x, exported APIs in the stable packages listed below will not change in a breaking way.
- Additive changes are allowed. Removals or breaking changes require a v2 release.
- Deprecations will be documented in release notes and code comments.
<!-- Claim-ID: CLM-020 -->

## Stable packages (root module)

These packages are part of the v1.x stability contract:

- `github.com/aponysus/recourse/recourse`
- `github.com/aponysus/recourse/retry`
- `github.com/aponysus/recourse/policy`
- `github.com/aponysus/recourse/observe`
- `github.com/aponysus/recourse/classify`
- `github.com/aponysus/recourse/budget`
- `github.com/aponysus/recourse/controlplane`
- `github.com/aponysus/recourse/circuit`
- `github.com/aponysus/recourse/hedge`
- `github.com/aponysus/recourse/integrations/http`
<!-- Claim-ID: CLM-020 -->

## Separate modules: gRPC and OpenTelemetry integrations

The gRPC and OpenTelemetry integrations are separate modules:

- `github.com/aponysus/recourse/integrations/grpc`
- `github.com/aponysus/recourse/integrations/otel`

They follow SemVer in their own modules. The intent is to version them in lockstep with the root module, but they are independently tagged.
<!-- Claim-ID: CLM-020 -->

## Not part of the API contract

- `internal/` packages
- `examples/` packages
- Test-only helpers
<!-- Claim-ID: CLM-020 -->

## Telemetry contract

The following are considered stable for v1.x:

- `observe.Timeline`, `observe.AttemptRecord`, and `observe.BudgetDecisionEvent` fields
- `Outcome.Reason` values and budget/circuit reason codes
<!-- Claim-ID: CLM-020 -->

See the generated references:

- [Policy schema reference](policy-schema.md)
- [Reason codes and timeline fields](reason-codes.md)

## Go version support

The supported Go version is defined by `go.mod` (currently 1.23).
<!-- Claim-ID: CLM-021 -->
