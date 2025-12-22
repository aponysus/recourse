package circuit

import (
	"context"
)

// State represents the state of a circuit breaker.
type State int

const (
	StateClosed   State = iota // Normal operation, requests allowed.
	StateOpen                  // Circuit open, requests fast-failed.
	StateHalfOpen              // Probing mode, limited requests allowed.
)

const (
	ReasonCircuitOpen               = "circuit_open"
	ReasonCircuitHalfOpenProbeLimit = "circuit_half_open_probe_limit"
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// Decision represents the result of checking a circuit breaker.
type Decision struct {
	Allowed bool
	State   State
	Reason  string
}

// CircuitBreaker defines the interface for a circuit breaker.
type CircuitBreaker interface {
	// Allow checks if a request should be allowed.
	Allow(ctx context.Context) Decision

	// RecordSuccess records a successful request execution.
	RecordSuccess(ctx context.Context)

	// RecordFailure records a failed request execution.
	RecordFailure(ctx context.Context)

	// State returns the current state of the breaker.
	State() State
}
