// Package observe defines timelines, attempt records, and observer interfaces.
//
// Timelines are the pull-based debugging path: call RecordTimeline before a
// recourse operation, pass the returned context to the call, and read the
// TimelineCapture after the call returns. Timeline returns nil until the
// executor publishes a completed timeline.
//
// Observers are the push-based integration path for logs, metrics, and tracing.
// Implement Observer, or compose observers with MultiObserver, to receive
// lifecycle, attempt, budget, hedge, success, and failure events as calls run.
//
// Timeline, AttemptRecord, BudgetDecisionEvent, and reason-code fields are part
// of the v1 telemetry contract.
package observe
