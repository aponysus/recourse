package hedge

import "time"

// FixedDelayTrigger spawns a hedge after a fixed delay.
type FixedDelayTrigger struct {
	Delay time.Duration
}

func (t FixedDelayTrigger) ShouldSpawnHedge(state HedgeState) (bool, time.Duration) {
	// We stop if we've reached the maximum number of attempts (Primary + MaxHedges).
	if state.AttemptsLaunched >= 1+state.MaxHedges {
		return false, 0
	}

	// For multiple hedges, we space them out by the delay.
	// Primary (1) -> Wait Delay -> Hedge 1 (2) -> Wait Delay -> Hedge 2 (3) ...
	// Target elapsed time for the *next* hedge is Delay * AttemptsLaunched.
	// Target elapsed time for the *next* hedge is Delay * AttemptsLaunched.
	delay := t.Delay
	if state.HedgeDelay > 0 {
		delay = state.HedgeDelay
	}

	target := delay * time.Duration(state.AttemptsLaunched)
	if state.Elapsed < target {
		return false, target - state.Elapsed
	}

	return true, 0
}
