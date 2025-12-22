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

	// If we reached the delay, should we spawn?
	// Only if we haven't already maxed out hedges.
	// But the *Executor* also checks MaxHedges. The Trigger just says "time-wise, we are ready".
	// However, if we already launched 1 hedge (and that's all we want?), we shouldn't keep saying YES.

	// Actually, FixedDelay usually means "Start 2nd attempt at T+Delay".
	// If we have launched 1 (primary) + 0 (hedges) = 1 attempt so far...
	// And we are at T > Delay...
	// Then yes, spawn.

	// But what if we want multiple hedges? FixedDelay usually implies ONE hedge delay.
	// Or is it "Every X ms"?
	// Phase 1 is "Fixed Delay" (singular). Usually implies "Hedge at 200ms".
	// If we want multiple, maybe "Hedge at 200ms, then 400ms"?
	// For now, let's assume it spawns *one* hedge after Delay.
	// So if AttemptsLaunched > 1, we stop.

	// Wait, internal/executor logic:
	// "If ShouldSpawnHedge says yes, and attempts < max, we spawn."
	// Providing a stateless trigger with FixedDelay...
	// If AttemptsLaunched == 1 (primary only), and elapsed > delay -> YES.
	// If AttemptsLaunched == 2 (primary + 1 hedge), should we spawn another?
	// If FixedDelay is "Hedge at 200ms", then NO. We already did.
	// So FixedDelay should logicaly apply to the *first* hedge.

	if state.AttemptsLaunched > 1 {
		return false, 0 // No more hedges from this trigger
	}

	return true, 0
}
