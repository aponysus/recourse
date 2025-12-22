package hedge

import (
	"strings"
	"time"
)

// LatencyTrigger spawns a hedge if the elapsed time exceeds a dynamic threshold.
type LatencyTrigger struct {
	Percentile string // "p50", "p90", "p95", "p99"
}

// ShouldSpawnHedge checks if the hedge should be spawned based on latency stats.
func (t LatencyTrigger) ShouldSpawnHedge(state HedgeState) (bool, time.Duration) {
	threshold := time.Duration(0)

	// Since we haven't updated HedgeState yet, I will write the logic assuming state has Snapshot.
	// We will update types.go in the next step.

	switch strings.ToLower(t.Percentile) {
	case "p50":
		threshold = state.Snapshot.P50
	case "p90":
		threshold = state.Snapshot.P90
	case "p95":
		threshold = state.Snapshot.P95
	case "p99":
		threshold = state.Snapshot.P99
	default:
		// Default to P95 or similar if unknown? Or invalid config.
		// Safe fallback: 0 means never (since elapsed always > 0).
		return false, 0
	}

	if threshold <= 0 {
		return false, 0
	}

	if state.Elapsed > threshold {
		// Spawn!
		// But only if we haven't already reached the limit.
		if state.AttemptsLaunched >= 1+state.MaxHedges {
			return false, 0
		}
		return true, 0
	}

	// Wait remaining
	remaining := threshold - state.Elapsed
	return false, remaining
}
