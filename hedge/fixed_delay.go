package hedge

import "time"

// FixedDelayTrigger spawns a hedge after a fixed delay.
type FixedDelayTrigger struct {
	Delay time.Duration
}

func (t FixedDelayTrigger) ShouldSpawnHedge(state HedgeState) (bool, time.Duration) {
	// If we haven't reached the delay yet, wait until we do.
	if state.Elapsed < t.Delay {
		return false, t.Delay - state.Elapsed
	}

	// FixedDelay spawns a single hedge after the specified delay.
	// If we've already launched more than 1 attempt (primary), we stop.

	if state.AttemptsLaunched > 1 {
		return false, 0 // No more hedges from this trigger
	}

	return true, 0
}
