# ADR-003: Policy Normalization

## Status
Accepted

## Context
Policies are loaded from a provider and may be incomplete, invalid, or out of
safe operating bounds (for example, too many attempts or extreme backoff values).
The executor must be resilient to these issues while keeping behavior predictable.

## Decision
recourse normalizes every EffectivePolicy during resolution:
- Fill missing values with safe defaults.
- Clamp values to documented safe ranges.
- Reject invalid enum values (for example, unknown jitter mode).
- Record normalization metadata for observability.

Normalization happens centrally so that all execution paths (fast and timeline)
use the same validated policy.

## Consequences
- Caller-provided policies may be adjusted to keep the system safe.
- Observability can report when normalization occurred via timeline attributes such as `policy_source`, `policy_normalized`, and `policy_normalized_fields`.
- Policy authors should consult documented limits to avoid surprises.
- Tight limits may need to be revisited if valid use cases require higher bounds.
