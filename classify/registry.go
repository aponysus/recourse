package classify

import (
	"strings"
	"sync"
)

// Registry is a thread-safe name → Classifier map.
type Registry struct {
	mu sync.RWMutex
	m  map[string]Classifier
}

func NewRegistry() *Registry {
	return &Registry{m: make(map[string]Classifier)}
}

// Register associates name with c. Empty names and nil classifiers are ignored.
func (r *Registry) Register(name string, c Classifier) {
	if r == nil {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" || c == nil {
		return
	}

	r.mu.Lock()
	if r.m == nil {
		r.m = make(map[string]Classifier)
	}
	r.m[name] = c
	r.mu.Unlock()
}

func (r *Registry) Get(name string) (Classifier, bool) {
	if r == nil {
		return nil, false
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, false
	}

	r.mu.RLock()
	c, ok := r.m[name]
	r.mu.RUnlock()
	return c, ok && c != nil
}
