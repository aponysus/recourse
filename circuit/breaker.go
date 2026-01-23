package circuit

import (
	"context"
	"sync"
	"time"
)

// ConsecutiveFailureBreaker implements a circuit breaker that opens after N consecutive failures.
type ConsecutiveFailureBreaker struct {
	mu sync.Mutex

	state State

	// Config
	threshold int
	cooldown  time.Duration
	maxProbes int // Number of requests allowed in Half-Open state (usually 1)

	// State variables
	consecutiveFailures int
	openTime            time.Time
	probesSent          int
	probesSuccessful    int
	probesRequired      int // Number of consecutive successes needed to close

	nowFn func() time.Time
}

// NewConsecutiveFailureBreaker creates a new breaker.
// threshold: Number of consecutive failures to open.
// cooldown: Duration to stay open.
func NewConsecutiveFailureBreaker(threshold int, cooldown time.Duration) *ConsecutiveFailureBreaker {
	if threshold <= 0 {
		threshold = 5 // Default
	}
	if cooldown <= 0 {
		cooldown = 10 * time.Second // Default
	}
	return &ConsecutiveFailureBreaker{
		state:          StateClosed,
		threshold:      threshold,
		cooldown:       cooldown,
		maxProbes:      1, // Single probe by default
		probesRequired: 1, // Close after 1 success
	}
}

func (cb *ConsecutiveFailureBreaker) State() State {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.updateStateLocked()
}

func (cb *ConsecutiveFailureBreaker) Allow(ctx context.Context) Decision {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	state := cb.updateStateLocked()

	if state == StateOpen {
		return Decision{Allowed: false, State: StateOpen, Reason: ReasonCircuitOpen}
	}

	if state == StateHalfOpen {
		if cb.probesSent >= cb.maxProbes {
			return Decision{Allowed: false, State: StateHalfOpen, Reason: ReasonCircuitHalfOpenProbeLimit}
		}
		cb.probesSent++
		return Decision{Allowed: true, State: StateHalfOpen}
	}

	return Decision{Allowed: true, State: StateClosed}
}

func (cb *ConsecutiveFailureBreaker) RecordSuccess(ctx context.Context) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	state := cb.updateStateLocked()

	if state == StateClosed {
		cb.consecutiveFailures = 0
	} else if state == StateHalfOpen {
		cb.probesSuccessful++
		// If we met requirements, close it
		if cb.probesSuccessful >= cb.probesRequired {
			cb.transitionTo(StateClosed)
		} else {
			// Free a probe slot until required successes are met.
			cb.probesSent--
		}
	}
}

func (cb *ConsecutiveFailureBreaker) RecordFailure(ctx context.Context) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	state := cb.updateStateLocked()

	if state == StateClosed {
		cb.consecutiveFailures++
		if cb.consecutiveFailures >= cb.threshold {
			cb.transitionTo(StateOpen)
		}
	} else if state == StateHalfOpen {
		// Failure in Half-Open -> Open immediately
		cb.transitionTo(StateOpen)
	}
}

func (cb *ConsecutiveFailureBreaker) updateStateLocked() State {
	if cb.state == StateOpen {
		if cb.now().Sub(cb.openTime) >= cb.cooldown {
			cb.transitionTo(StateHalfOpen)
		}
	}
	return cb.state
}

func (cb *ConsecutiveFailureBreaker) transitionTo(newState State) {
	cb.state = newState
	switch newState {
	case StateClosed:
		cb.consecutiveFailures = 0
		cb.probesSent = 0
		cb.probesSuccessful = 0
	case StateOpen:
		cb.openTime = cb.now()
		cb.consecutiveFailures = 0
	case StateHalfOpen:
		cb.probesSent = 0
		cb.probesSuccessful = 0
	}
}

func (cb *ConsecutiveFailureBreaker) now() time.Time {
	if cb.nowFn != nil {
		return cb.nowFn()
	}
	return time.Now()
}

// SetClock overrides the breaker clock, primarily for tests.
func (cb *ConsecutiveFailureBreaker) SetClock(f func() time.Time) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.nowFn = f
}
