package hedge

import (
	"sync"
)

// Registry manages named hedge triggers.
// It is safe for concurrent use.
type Registry struct {
	mu       sync.RWMutex
	triggers map[string]Trigger
}

// NewRegistry creates a new, empty registry.
func NewRegistry() *Registry {
	return &Registry{
		triggers: make(map[string]Trigger),
	}
}

// Register adds a trigger to the registry.
// Panics if name is empty or trigger is nil.
func (r *Registry) Register(name string, t Trigger) {
	if name == "" {
		panic("hedge: name cannot be empty")
	}
	if t == nil {
		panic("hedge: trigger cannot be nil")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.triggers[name] = t
}

// Get returns the trigger with the given name.
func (r *Registry) Get(name string) (Trigger, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.triggers[name]
	return t, ok
}
