package hedge

import (
	"sort"
	"sync"
	"time"
)

// LatencySnapshot contains latency quantiles.
type LatencySnapshot struct {
	P50 time.Duration
	P90 time.Duration
	P95 time.Duration
	P99 time.Duration
}

// LatencyTracker tracks recent latency samples and calculates quantiles.
type LatencyTracker interface {
	// Observe records a duration sample.
	Observe(d time.Duration)
	// Snapshot returns the current latency snapshot.
	Snapshot() LatencySnapshot
}

// RingBufferTracker implements LatencyTracker using a fixed-size ring buffer.
// It is safe for concurrent use.
type RingBufferTracker struct {
	mu      sync.RWMutex
	samples []time.Duration
	idx     int
	full    bool
}

// NewRingBufferTracker creates a new tracker with the specified size.
// Size must be greater than 0.
func NewRingBufferTracker(size int) *RingBufferTracker {
	if size <= 0 {
		size = 256 // Default safe size
	}
	return &RingBufferTracker{
		samples: make([]time.Duration, size),
	}
}

// Observe records a duration sample.
func (t *RingBufferTracker) Observe(d time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.samples[t.idx] = d
	t.idx++
	if t.idx >= len(t.samples) {
		t.idx = 0
		t.full = true
	}
}

// Snapshot returns the current latency snapshot.
func (t *RingBufferTracker) Snapshot() LatencySnapshot {
	t.mu.RLock()
	defer t.mu.RUnlock()

	count := t.idx
	if t.full {
		count = len(t.samples)
	}

	if count == 0 {
		return LatencySnapshot{}
	}

	// Copy samples to avoid holding lock during sort
	// We only copy valid samples
	sorted := make([]time.Duration, count)
	if t.full {
		copy(sorted, t.samples)
	} else {
		copy(sorted, t.samples[:count])
	}

	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	return LatencySnapshot{
		P50: quantile(sorted, 0.50),
		P90: quantile(sorted, 0.90),
		P95: quantile(sorted, 0.95),
		P99: quantile(sorted, 0.99),
	}
}

func quantile(sorted []time.Duration, q float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	// Use (N-1)*q to interpret index in 0-based array.
	// For N=100 (indices 0-99):
	// q=0.5 -> 49.5 -> 49 (Value 50)
	// q=0.99 -> 98.01 -> 98 (Value 99)
	idx := int(float64(len(sorted)-1) * q)
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	if idx < 0 {
		idx = 0
	}
	return sorted[idx]
}
