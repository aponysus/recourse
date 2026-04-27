// Package policy defines policy keys and the policy schema used by recourse.
//
// A PolicyKey is the low-cardinality identifier a call site supplies to select
// resilience behavior. EffectivePolicy is the normalized policy envelope for a
// key, including retry settings, optional hedging, optional circuit breaking,
// classifier selection, and budget references.
//
// Use New or NewFromKey with Option helpers for programmatic policy creation.
// EffectivePolicy.Normalize applies documented safety bounds so invalid or
// overly aggressive policy input is clamped before execution.
package policy
