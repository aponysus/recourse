package retry

import (
	"log"
	"sync"
)

var (
	globalExec *Executor
	globalOnce sync.Once
)

// DefaultExecutor returns the shared, lazy-initialized default executor.
// It uses NewDefaultExecutor() if SetGlobal has not been called.
func DefaultExecutor() *Executor {
	globalOnce.Do(func() {
		if globalExec == nil {
			globalExec = NewDefaultExecutor()
		}
	})
	return globalExec
}

// SetGlobal configures the default executor.
// It must be called before DefaultExecutor() is used (e.g. at startup).
// If called after initialization, it logs a warning and does nothing.
func SetGlobal(exec *Executor) {
	if exec == nil {
		return
	}

	// Check if already initialized to provide a warning.
	// Note: This check is not strictly race-free vs DefaultExecutor, but sufficient for startup-time verification.
	if globalExec != nil {
		log.Printf("retry: SetGlobal called after global executor already initialized; ignoring.")
		return
	}

	globalOnce.Do(func() {
		globalExec = exec
	})
}
