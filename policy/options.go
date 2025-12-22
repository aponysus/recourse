package policy

import (
	"time"
)

// Option configures an EffectivePolicy.
type Option func(*EffectivePolicy)

// New creates an EffectivePolicy for the given key with applied options.
// The key is parsed from a string like "svc.Method".
// Options are applied in order, with later options overriding earlier ones.
//
// If normalization fails, New returns a safe default policy for the key,
// ensuring it never returns a zero EffectivePolicy.
func New(key string, opts ...Option) EffectivePolicy {
	return NewFromKey(ParseKey(key), opts...)
}

// NewFromKey creates an EffectivePolicy from a structured PolicyKey.
func NewFromKey(key PolicyKey, opts ...Option) EffectivePolicy {
	// Start with default policy for the key
	p := DefaultPolicyFor(key)

	for _, opt := range opts {
		opt(&p)
	}

	normalized, err := p.Normalize()
	if err != nil {
		// Fallback to default if normalization fails
		p = DefaultPolicyFor(key)
		normalized, _ = p.Normalize()
	}

	return normalized
}

// MaxAttempts sets the maximum number of retry attempts.
func MaxAttempts(n int) Option {
	return func(p *EffectivePolicy) {
		p.Retry.MaxAttempts = n
	}
}

// InitialBackoff sets the initial backoff duration.
func InitialBackoff(d time.Duration) Option {
	return func(p *EffectivePolicy) {
		p.Retry.InitialBackoff = d
	}
}

// MaxBackoff sets the maximum backoff duration.
func MaxBackoff(d time.Duration) Option {
	return func(p *EffectivePolicy) {
		p.Retry.MaxBackoff = d
	}
}

// BackoffMultiplier sets the exponential backoff multiplier.
func BackoffMultiplier(m float64) Option {
	return func(p *EffectivePolicy) {
		p.Retry.BackoffMultiplier = m
	}
}

// Backoff is a convenience option that sets initial, max, and multiplier together.
func Backoff(initial, max time.Duration, multiplier float64) Option {
	return func(p *EffectivePolicy) {
		p.Retry.InitialBackoff = initial
		p.Retry.MaxBackoff = max
		p.Retry.BackoffMultiplier = multiplier
	}
}

// Jitter sets the jitter strategy for backoff.
func Jitter(j JitterKind) Option {
	return func(p *EffectivePolicy) {
		p.Retry.Jitter = j
	}
}

// PerAttemptTimeout sets the timeout for each individual attempt.
func PerAttemptTimeout(d time.Duration) Option {
	return func(p *EffectivePolicy) {
		p.Retry.TimeoutPerAttempt = d
	}
}

// OverallTimeout sets the total timeout across all attempts.
func OverallTimeout(d time.Duration) Option {
	return func(p *EffectivePolicy) {
		p.Retry.OverallTimeout = d
	}
}

// Classifier sets the classifier name for this policy.
func Classifier(name string) Option {
	return func(p *EffectivePolicy) {
		p.Retry.ClassifierName = name
	}
}

// Budget sets the budget reference for retry attempts.
func Budget(name string) Option {
	return func(p *EffectivePolicy) {
		p.Retry.Budget = BudgetRef{Name: name, Cost: 1}
	}
}

// BudgetWithCost sets the budget reference with a custom cost.
func BudgetWithCost(name string, cost int) Option {
	return func(p *EffectivePolicy) {
		p.Retry.Budget = BudgetRef{Name: name, Cost: cost}
	}
}

// PolicyID sets an identifier for this policy (useful for observability).
func PolicyID(id string) Option {
	return func(p *EffectivePolicy) {
		p.ID = id
	}
}

// EnableHedging enables hedging with default settings.
// Note: Hedging logic might not be fully functional if hedge execution is unimplemented.
func EnableHedging() Option {
	return func(p *EffectivePolicy) {
		p.Hedge.Enabled = true
		if p.Hedge.MaxHedges == 0 {
			p.Hedge.MaxHedges = 2
		}
		if p.Hedge.HedgeDelay == 0 {
			p.Hedge.HedgeDelay = 200 * time.Millisecond
		}
	}
}

// HedgeMaxAttempts sets the maximum parallel attempts per retry group.
func HedgeMaxAttempts(n int) Option {
	return func(p *EffectivePolicy) {
		p.Hedge.Enabled = true
		p.Hedge.MaxHedges = n
	}
}

// HedgeDelay sets the delay before spawning hedge attempts.
func HedgeDelay(d time.Duration) Option {
	return func(p *EffectivePolicy) {
		p.Hedge.Enabled = true
		p.Hedge.HedgeDelay = d
	}
}

// HedgeTrigger sets a named trigger for hedge decisions.
func HedgeTrigger(name string) Option {
	return func(p *EffectivePolicy) {
		p.Hedge.Enabled = true
		p.Hedge.TriggerName = name
	}
}

// HedgeBudget sets the budget reference for hedge attempts.
func HedgeBudget(name string) Option {
	return func(p *EffectivePolicy) {
		p.Hedge.Budget = BudgetRef{Name: name, Cost: 1}
	}
}

// HedgeCancelOnTerminal configures fail-fast behavior for hedges.
func HedgeCancelOnTerminal(cancel bool) Option {
	return func(p *EffectivePolicy) {
		p.Hedge.CancelOnFirstTerminal = cancel
	}
}

// --- Presets ---

// ExponentialBackoff returns options for exponential backoff with equal jitter.
// This is the recommended default for most use cases.
func ExponentialBackoff(initial, max time.Duration) Option {
	return func(p *EffectivePolicy) {
		p.Retry.InitialBackoff = initial
		p.Retry.MaxBackoff = max
		p.Retry.BackoffMultiplier = 2.0
		p.Retry.Jitter = JitterEqual
	}
}

// ConstantBackoff returns options for constant-delay retries.
// Use when you need predictable timing (e.g., polling).
func ConstantBackoff(delay time.Duration) Option {
	return func(p *EffectivePolicy) {
		p.Retry.InitialBackoff = delay
		p.Retry.MaxBackoff = delay
		p.Retry.BackoffMultiplier = 1.0
		p.Retry.Jitter = JitterNone
	}
}

// HTTPDefaults returns options suitable for HTTP client calls.
// Sets reasonable timeouts, exponential backoff, and the HTTP classifier.
func HTTPDefaults() Option {
	return func(p *EffectivePolicy) {
		p.Retry.MaxAttempts = 3
		p.Retry.InitialBackoff = 100 * time.Millisecond
		p.Retry.MaxBackoff = 2 * time.Second
		p.Retry.BackoffMultiplier = 2.0
		p.Retry.Jitter = JitterFull
		p.Retry.TimeoutPerAttempt = 10 * time.Second
		p.Retry.OverallTimeout = 30 * time.Second
		p.Retry.ClassifierName = "http"
	}
}

// DatabaseDefaults returns options suitable for database calls.
// Uses conservative retries with longer backoff for transient failures.
func DatabaseDefaults() Option {
	return func(p *EffectivePolicy) {
		p.Retry.MaxAttempts = 3
		p.Retry.InitialBackoff = 100 * time.Millisecond
		p.Retry.MaxBackoff = 5 * time.Second
		p.Retry.BackoffMultiplier = 2.0
		p.Retry.Jitter = JitterEqual
		p.Retry.TimeoutPerAttempt = 30 * time.Second
		p.Retry.OverallTimeout = 60 * time.Second
	}
}

// BackgroundJobDefaults returns options suitable for background/async jobs.
// Allows more retries with longer backoff since latency is less critical.
func BackgroundJobDefaults() Option {
	return func(p *EffectivePolicy) {
		p.Retry.MaxAttempts = 5
		p.Retry.InitialBackoff = 1 * time.Second
		p.Retry.MaxBackoff = 30 * time.Second
		p.Retry.BackoffMultiplier = 2.0
		p.Retry.Jitter = JitterFull
		// No per-attempt timeout by default for long-running jobs
		p.Retry.OverallTimeout = 5 * time.Minute
	}
}

// LowLatencyDefaults returns options optimized for latency-sensitive calls.
// Uses hedging and aggressive timeouts.
func LowLatencyDefaults() Option {
	return func(p *EffectivePolicy) {
		p.Retry.MaxAttempts = 2
		p.Retry.InitialBackoff = 10 * time.Millisecond
		p.Retry.MaxBackoff = 50 * time.Millisecond
		p.Retry.BackoffMultiplier = 2.0
		p.Retry.Jitter = JitterEqual
		p.Retry.TimeoutPerAttempt = 500 * time.Millisecond
		p.Retry.OverallTimeout = 1 * time.Second
		// Enable hedging
		p.Hedge.Enabled = true
		p.Hedge.MaxHedges = 2
		p.Hedge.HedgeDelay = 100 * time.Millisecond
	}
}
