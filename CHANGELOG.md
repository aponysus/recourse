# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- TBD

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
