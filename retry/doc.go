// Package retry implements the core retry and hedging executor for recourse.
//
// An Executor orchestrates retries, optional hedging, circuit breaking, and
// backpressure budgets for one operation. Construct an executor once at program
// startup with NewDefaultExecutor or NewExecutor, then share it across
// goroutines.
//
// Each call supplies a policy.PolicyKey. The executor resolves a
// policy.EffectivePolicy through a controlplane.PolicyProvider, classifies each
// attempt result with classify.Classifier, gates retry attempts after the base
// attempt through budget.Budget when configured, and emits observe.Observer
// events or an observe.Timeline when requested.
//
// Missing dependencies are controlled by FailureMode settings. Policies fail
// closed by default, while classifiers and hedge triggers can fall back to safe
// defaults depending on the configured mode.
package retry
