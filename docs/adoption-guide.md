# Adoption guide (staged rollout)

This guide is a staged path for adopting recourse without surprising behavior changes. Treat each stage as a gate with clear exit criteria.

## Stage 1: Define keys and a review process

- Establish a key format (for example: `service.Operation`).
- Document what does not belong in a key (IDs, URLs, request IDs).
- Add a lightweight review step for new keys.

See [Policy keys](concepts/policy-keys.md).

## Stage 2: Wrap call sites with minimal behavior change

- Start with a small set of critical call sites.
- Use policies that mirror current behavior. If you want zero retry behavior at first, set `MaxAttempts(1)` and disable hedging for those keys.
- Confirm you can still deploy without a centralized provider.

See [Getting started](getting-started.md).

## Stage 3: Add observability

- Capture timelines on at least one path per service.
- If you already use logs or metrics, wire an `observe.Observer` to emit lifecycle and attempt events.
- Ensure keys are low-cardinality in all telemetry.

See [Observability](concepts/observability.md).

## Stage 4: Centralize policies

- Move policies into a shared provider (static to start).
- Ensure policy changes are reviewed and tracked.
- Add safe bounds and normalization reviews to avoid runaway configs.

See [Policies and providers](concepts/policies.md).

## Stage 5: Introduce budgets and backpressure

- Register budgets that match your resource model.
- Choose explicit behavior for missing budgets via `MissingBudgetMode`.
- Start with conservative limits and observe deny rates.

See [Budgets and backpressure](concepts/budgets.md).

## Stage 6: Add hedging selectively

- Enable hedging only for idempotent operations with clear tail latency pain.
- Use budgets for hedges if load is a concern.
- Verify cancellation behavior in staging before wider rollout.

See [Hedging](concepts/hedging.md).

## Stage 7: Remote configuration and governance

- Introduce remote policy providers only after you have governance in place.
- Define rollback and fallback behavior for policy fetch failures.
- Roll out changes gradually and monitor effects on retries and budgets.

See [Remote configuration](concepts/remote-configuration.md).

## Suggested exit criteria for each stage

- No unexpected retries or load spikes.
- Timelines are available and explainable.
- Keys and policies are reviewed and kept low-cardinality.
- Rollout can be reverted quickly if needed.
