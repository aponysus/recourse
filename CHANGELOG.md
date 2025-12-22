# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- TBD

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
