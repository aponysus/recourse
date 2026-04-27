// Package budget defines per-attempt backpressure gates for recourse.
//
// A Budget decides whether a retry or hedge attempt should be allowed before
// the executor runs it. Budgets are looked up by name through a Registry and
// return a Decision with a stable reason code, which lets callers see budget
// denials in timelines and observer events.
//
// Use UnlimitedBudget when you want retry behavior without load limiting. Use
// TokenBucketBudget when retry or hedge attempts should consume capacity from a
// shared token bucket to reduce load amplification during dependency failures.
package budget
