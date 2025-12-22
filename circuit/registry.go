package circuit

import (
	"sync"

	"github.com/aponysus/recourse/policy"
)

// Registry manages circuit breakers for different policies.
type Registry struct {
	mu       sync.RWMutex
	breakers map[policy.PolicyKey]CircuitBreaker
}

// NewRegistry creates a new circuit breaker registry.
func NewRegistry() *Registry {
	return &Registry{
		breakers: make(map[policy.PolicyKey]CircuitBreaker),
	}
}

// Get returns an existing breaker or creates a new one for the given policy.
func (r *Registry) Get(key policy.PolicyKey, config policy.CircuitPolicy) CircuitBreaker {
	if !config.Enabled {
		return nil
	}

	r.mu.RLock()
	cb, ok := r.breakers[key]
	r.mu.RUnlock()

	if ok {
		return cb
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Double check
	if cb, ok := r.breakers[key]; ok {
		return cb
	}

	// Create new breaker
	cb = NewConsecutiveFailureBreaker(config.Threshold, config.Cooldown)
	r.breakers[key] = cb
	return cb
}
