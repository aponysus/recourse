# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.2.0] - 2026-04-28

### Added
- Executable pkg.go.dev examples for `retry`, `recourse`, `observe`, `classify`, `circuit`, `integrations/grpc`, and `integrations/otel`.
- First-class OpenTelemetry observer integration as the separate `integrations/otel` module.
- Migration guide from `cenkalti/backoff` with a compiling recourse example.
- Security policy, support guidance, and GitHub issue templates.

### Changed
- Expanded package-level documentation across public packages for clearer pkg.go.dev overviews.
- Clarified README positioning, default quick-start behavior, production status, and Go Reference badge target.
- Added a Go Playground link demonstrating retries with timeline capture.

## [1.1.0] - 2026-04-22

### Added
- Deterministic clock injection for `TokenBucketBudget` via `SetClock` and constructor options for test control.
- Stable policy-resolution timeline attributes: `policy_mode`, `policy_source`, `policy_normalized`, and `policy_normalized_fields`.
- `observe.Observer.OnHedgeCancel` emission for in-flight hedge cancellations with low-cardinality reasons.

### Changed
- Refreshed dependency versions in `integrations/grpc` and the OpenTelemetry example module.
- Updated example module requirements to reference the `v1.1.0` root release.

### Fixed
- Skip Codecov uploads on Dependabot pull requests where `CODECOV_TOKEN` is unavailable.
- Re-pin the docs toolchain to a known-good Pygments version so GitHub Pages builds remain stable.

## [1.0.1] - 2026-01-23

### Fixed
- Respect MissingTriggerMode when a named hedge trigger is not found.

## [1.0.0] - 2026-01-05

### Added
- API compatibility policy and telemetry contract documentation.
- Generated reference docs for policy schema, defaults/safety model, and reason codes/timeline fields.
- Docs generator and Makefile targets for reference docs.
- CI guard to keep generated references in sync.
- Expanded docs: design overview, gotchas, adoption guide, incident debugging, and key patterns/taxonomy.

### Changed
- Go version baseline set to 1.23 across modules.
- Docs navigation and landing copy aligned with the v1 framing.

## [0.1.0] - 2025-12-22

### Added
- Retry executor with bounded attempts, backoff/jitter, and timeouts.
- Policy keys with static and remote policy providers.
- Outcome classifiers (generic, HTTP, gRPC) and registry support.
- Budgets/backpressure with token bucket and unlimited budgets.
- Observability via timelines and observer callbacks.
- Hedging triggers with latency tracking.
- Circuit breaker with consecutive failure logic.
- HTTP and gRPC client integrations.
