// Package circuit defines circuit breaker interfaces and the default
// consecutive-failure implementation used by recourse.
//
// A CircuitBreaker tracks dependency health for a policy key and returns a
// Decision before the executor starts an attempt. Breakers move through closed,
// open, and half-open states so persistent failures can fail fast while
// cooldown probes determine whether recovery has started.
//
// Use NewConsecutiveFailureBreaker for the built-in threshold and cooldown
// behavior. Register custom breakers in a Registry when a service needs
// different state-transition logic.
package circuit
