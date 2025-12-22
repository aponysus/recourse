package hedge

import "time"

// HedgeState describes the current state of a retry group for hedging decisions.
type HedgeState struct {
	// CallStart is when the overall operation (call) started.
	CallStart time.Time // Not strictly needed for fixed delay but good for future.
	// AttemptStart is when the current retry group (attempt 0 of this group) started.
	AttemptStart time.Time
	// AttemptsLaunched is the number of attempts already launched in this group.
	AttemptsLaunched int
	// MaxHedges is the maximum number of additional managed attempts (hedges) allowed.
	// Note: Total attempts in group = 1 (primary) + MaxHedges.
	MaxHedges int
	// Elapsed is the time elapsed since AttemptStart.
	Elapsed time.Duration
	// Snapshot contains the current latency statistics for the operation.
	Snapshot LatencySnapshot
}

// Trigger decides when to spawn a hedged attempt.
type Trigger interface {
	// ShouldSpawnHedge returns true if a new hedge should be spawned.
	// nextCheckIn returns the duration to wait before checking again.
	// If nextCheckIn is 0, the executor uses a default enforcement interval.
	ShouldSpawnHedge(state HedgeState) (should bool, nextCheckIn time.Duration)
}
