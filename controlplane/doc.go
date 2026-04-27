// Package controlplane provides policy providers for recourse.
//
// A PolicyProvider resolves the effective policy for a policy key. The retry
// executor calls a provider before running an operation, then uses the returned
// policy to apply retry limits, backoff, timeouts, classifiers, budgets,
// hedging, and circuit breaking.
//
// StaticProvider is the in-process provider for local or embedded policies.
// RemoteProvider fetches policies from a Source and caches positive and
// negative lookups so a remote control plane can be introduced without forcing
// each call site to know where policies live.
package controlplane
