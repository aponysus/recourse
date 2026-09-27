// Package controlplane provides policy providers for recourse.
//
// A PolicyProvider resolves the effective policy for a policy key. The retry
// executor calls a provider before running an operation, then uses the returned
// policy to apply retry limits, backoff, timeouts, classifiers, budgets,
// hedging, and circuit breaking.
//
// StaticProvider is the in-process provider for local or embedded policies.
// RemoteProvider fetches policies from a Source and caches positive and
// negative lookups. Concurrent lookups for the same key share a source fetch.
// WithLKGTTL optionally retains a last-known-good policy beyond the fresh TTL
// for use on source errors, with policy source metadata set to "lkg".
package controlplane
