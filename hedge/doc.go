// Package hedge defines hedge triggers and latency tracking used by the retry
// executor.
//
// Hedging starts an additional attempt while an earlier attempt is still in
// flight, then races the attempts so the first successful result can win. A
// Trigger decides when another attempt should be spawned for the current
// HedgeState.
//
// FixedDelayTrigger uses a configured delay from policy. LatencyTrigger uses a
// per-key LatencyTracker, such as RingBufferTracker, to derive dynamic delays
// from recent latency percentiles.
package hedge
