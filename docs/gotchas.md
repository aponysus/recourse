# Gotchas and safety checklist

Retries, hedges, and circuit breakers change system behavior. This checklist is meant to prevent the most common failure modes before they happen.

## Quick checklist

- The operation is idempotent, or you have idempotency keys and dedupe in place.
- Request payloads are replayable and safe to re-send.
- Keys are low-cardinality and stable across releases.
- Per-attempt and overall timeouts are set and aligned with upstream deadlines.
- Missing policy and missing budget modes are explicitly chosen.
- Budgets are in place before scaling retries or hedging.
- Timeline capture or observer events are wired for at least one critical path.
- You have a rollback plan for policy changes and hedging rollouts.

## Idempotency and side effects

Retries can create duplicates. If the operation is not idempotent, you can still use retries, but you must add safeguards such as idempotency keys, dedupe tables, or server-side replay protection. If that is not possible, do not retry the operation or set `MaxAttempts(1)` for that key.

## Timeouts and cancellation

Align timeouts so you do not accidentally exceed upstream deadlines:

- Use an overall timeout to bound total time spent.
- Use per-attempt timeouts to prevent a single attempt from stalling the entire call.
- Ensure parent context deadlines are larger than the retry envelope you intend to allow.

recourse respects `context.Context` cancellation for attempts, backoff sleeps, and hedges.

## Key cardinality

Keys are used for policy resolution and observability dimensions. If a key can take on millions of values, it will break caches and metrics. Use stable operation names and bucket any dynamic attributes into low-cardinality categories. See [Policy keys](concepts/policy-keys.md).

## Budgets and backpressure

Retries and hedges multiply load. Budgets are the primary tool to prevent retry storms. Decide up front:

- What your budget units represent (requests, concurrency, tokens).
- How to handle missing budgets via `MissingBudgetMode`.
- Which services should use strict backpressure and which can fail open.

Record budget decisions so you can see denials and allow rates during incidents.

## Hedging hazards

Hedging can reduce tail latency, but it can also increase load:

- Only hedge idempotent operations.
- Budget hedges separately if needed.
- Validate cancellation behavior so in-flight attempts are stopped when a winner completes.

See [Hedging](concepts/hedging.md) for configuration details.

## Remote configuration hazards

Remote policy control adds a dependency on the control plane:

- Ensure cache TTLs and negative caching match your control plane capacity.
- Define explicit fallback modes for missing or failed policy fetches.
- Roll out policy changes gradually and monitor their effect.

See [Remote configuration](concepts/remote-configuration.md).

## Integration constraints

Integrations are helpers, not a replacement for correctness:

- Ensure HTTP request bodies are replayable if retries are enabled.
- Avoid retrying streaming calls without a clear replay strategy.
- For gRPC, the provided interceptor targets unary calls; streaming requires custom handling.

See [Integrations](concepts/integrations.md).
