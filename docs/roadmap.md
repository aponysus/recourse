# Roadmap

`recourse` is being built in phases to keep the public surface small while the internals evolve.

## Current status

Implemented today:

- Basic retry executor (bounded attempts, backoff/jitter, per-attempt + overall timeouts)
- Policy schema + normalization/clamping
- Static in-process policy provider
- Timelines (`observe.Timeline`) and observer callbacks (`observe.Observer`)
- Outcome classifiers (`classify.Classifier`) selected by policy
- Budgets/backpressure (per-attempt gating via `budget` + `ExecutorOptions.Budgets`)
- Facade helpers that accept string keys (`recourse.Do*`)
- API Ergonomics: Functional options for policies/executors and timeline capture context

## Planned phases (high level)

### Budgets

Budgets/backpressure are implemented for retries; next is extending budgets to cover hedges and richer backpressure models.

### Hedging

Add fixed-delay hedging, then latency-aware triggers, with strict concurrency/cancellation guarantees.

### Remote control plane

Add a remote policy provider with caching, last-known-good fallback, and negative caching for missing keys.

### Integrations and ergonomics

Add common integrations (HTTP/gRPC helpers) and stable zero-config wiring while preserving an explicit-executor path for advanced users.

### Hardening and v1 docs

Add race/leak hardening, broader tests, examples, and documentation polish leading to a v1.0 API freeze.
