# Design overview

## What it is

recourse is a policy-driven resilience library for Go services. Call sites supply a stable key, policies define the retry envelope, and the executor records structured timelines and observer events so behavior is explainable in production.

## Who it is for

- Teams running multiple services that need consistent retry behavior.
- Platform or reliability engineers who want centralized policy control.
- Services that need incident-friendly, per-attempt observability.
- Teams willing to enforce low-cardinality keys and policy discipline.

## Who it is not for

- One-off scripts or small apps where manual retries are enough.
- Workloads that cannot tolerate duplicate attempts without safeguards.
- Teams that do not want to manage policy governance or key conventions.
- Situations where retries are simply the wrong tool.

## Mental model

A key chooses a policy, and the policy defines the envelope:

```
key -> policy -> executor -> attempts -> timeline/observer events
```

In more detail:

1. Call sites provide a low-cardinality policy key such as "payments.Charge".
2. A policy provider resolves the effective policy for that key.
3. The executor runs attempts using classification, backoff, budgets, hedging, and circuit breaking as configured.
4. Observability artifacts are emitted (timeline records and observer callbacks).

## The operational contract

- Attempts, backoff, and timeouts are bounded by policy.
- Classifiers decide whether an outcome should retry, stop, or abort.
- Budgets provide backpressure so retries do not amplify outages.
- Behavior is explainable through structured timelines and observer events.
- Context cancellation is respected across attempts, sleeps, and hedges.

## Tradeoffs and organizational cost

- Keys must be stable and low-cardinality, which requires governance and review.
- Policies need ownership and careful rollout to avoid surprises.
- Backpressure and hedging can change load patterns and must be introduced intentionally.
- Remote configuration adds a control plane dependency that must be operated reliably.

## How to start

1. Pick a key convention and enforce low-cardinality rules. See [Policy keys](concepts/policy-keys.md) and [Gotchas and safety checklist](gotchas.md).
2. Integrate via the facade in one service. See [Getting started](getting-started.md) and [Adoption guide](adoption-guide.md).
3. Capture timelines for critical calls to validate behavior. See [Observability](concepts/observability.md) and [Incident debugging](incident-debugging.md).

## Where to go deeper

- [Gotchas and safety checklist](gotchas.md)
- [Adoption guide](adoption-guide.md)
- [Incident debugging](incident-debugging.md)
- [Policies and providers](concepts/policies.md)
- [Classifiers](concepts/classifiers.md)
- [Budgets and backpressure](concepts/budgets.md)
- [Hedging](concepts/hedging.md)
- [Circuit breaking](concepts/circuit-breaking.md)
- [Remote configuration](concepts/remote-configuration.md)
- [Integrations](concepts/integrations.md)
