package retry

import (
	"github.com/aponysus/recourse/budget"
	"github.com/aponysus/recourse/classify"
	"github.com/aponysus/recourse/hedge"
	"github.com/aponysus/recourse/observe"
)

// DefaultOption allows customizing the default executor.
// It is an alias for ExecutorOption for ergonomics.
type DefaultOption = ExecutorOption

// NewDefaultExecutor creates an Executor with conservative "happy path" defaults.
//
// Defaults:
// - Provider: StaticProvider (empty).
// - Classifiers: Built-in (Generic, HTTP) registered.
// - Budgets: "unlimited" budget registered.
// - Triggers: "fixed_delay", "p90", "p95", "p99" triggers registered.
// - Observer: NoopObserver.
func NewDefaultExecutor(opts ...DefaultOption) *Executor {
	// 1. Base defaults
	defaultOpts := []ExecutorOption{

		WithObserver(&observe.NoopObserver{}),
	}

	// 2. Registries (constructed fresh to avoid shared mutable state defaults)

	// Classifiers
	classifierReg := classify.NewRegistry()
	classify.RegisterBuiltins(classifierReg)
	defaultOpts = append(defaultOpts, WithClassifiers(classifierReg))
	defaultOpts = append(defaultOpts, WithDefaultClassifier(classify.AutoClassifier{}))

	// Budgets
	budgetReg := budget.NewRegistry()
	budgetReg.Register("unlimited", &budget.UnlimitedBudget{})
	defaultOpts = append(defaultOpts, WithBudgetRegistry(budgetReg))

	// Triggers
	triggerReg := hedge.NewRegistry()
	triggerReg.Register("fixed_delay", &hedge.FixedDelayTrigger{}) // Delay comes from policy
	triggerReg.Register("p90", &hedge.LatencyTrigger{Percentile: "p90"})
	triggerReg.Register("p95", &hedge.LatencyTrigger{Percentile: "p95"})
	triggerReg.Register("p99", &hedge.LatencyTrigger{Percentile: "p99"})
	defaultOpts = append(defaultOpts, WithHedgeTriggerRegistry(triggerReg))

	// 3. User overrides
	defaultOpts = append(defaultOpts, opts...)

	return NewExecutor(defaultOpts...)
}
