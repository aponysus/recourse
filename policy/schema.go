package policy

import (
	"time"
)

type JitterKind string

const (
	JitterNone  JitterKind = "none"
	JitterFull  JitterKind = "full"
	JitterEqual JitterKind = "equal"
)

type BudgetRef struct {
	Name string `json:"name"`          // Budget registry name.
	Cost int    `json:"cost,omitempty"` // Units consumed per attempt (min 1).
}

type RetryPolicy struct {
	MaxAttempts       int           `json:"max_attempts"`        // Maximum attempts per call.
	InitialBackoff    time.Duration `json:"initial_backoff"`     // Starting backoff before retries.
	MaxBackoff        time.Duration `json:"max_backoff"`         // Upper bound for backoff delays.
	BackoffMultiplier float64       `json:"backoff_multiplier"`  // Exponential backoff multiplier.
	Jitter            JitterKind    `json:"jitter"`              // Backoff jitter strategy.

	TimeoutPerAttempt time.Duration `json:"timeout_per_attempt"` // Per-attempt timeout (0 disables).
	OverallTimeout    time.Duration `json:"overall_timeout"`     // Total timeout for all attempts (0 disables).

	ClassifierName string    `json:"classifier_name,omitempty"` // Classifier registry name.
	Budget         BudgetRef `json:"budget,omitempty"`          // Budget gating for retry attempts.
}

type HedgePolicy struct {
	Enabled               bool          `json:"enabled"`                     // Enable hedging for this key.
	MaxHedges             int           `json:"max_hedges"`                  // Maximum additional hedged attempts.
	HedgeDelay            time.Duration `json:"hedge_delay"`                 // Delay before spawning a hedge.
	TriggerName           string        `json:"trigger_name,omitempty"`      // Optional dynamic trigger name.
	CancelOnFirstTerminal bool          `json:"cancel_on_first_terminal"`    // Cancel on any terminal outcome.
	Budget                BudgetRef     `json:"budget,omitempty"`            // Budget gating for hedged attempts.
}

type CircuitPolicy struct {
	Enabled   bool          `json:"enabled"`   // Enable circuit breaking for this key.
	Threshold int           `json:"threshold"` // Consecutive failures to open the circuit.
	Cooldown  time.Duration `json:"cooldown"`  // Cooldown before a half-open probe.
}

type PolicySource string

const (
	PolicySourceUnknown PolicySource = "unknown"
	PolicySourceStatic  PolicySource = "static"
	PolicySourceRemote  PolicySource = "remote"
	PolicySourceLKG     PolicySource = "lkg"
	PolicySourceDefault PolicySource = "default"
)

type NormalizationInfo struct {
	Changed       bool     `json:"-"` // Whether normalization changed any field.
	ChangedFields []string `json:"-"` // Dot-delimited field paths that were changed.
}

type Metadata struct {
	Source        PolicySource      `json:"-"` // Policy resolution source.
	Normalization NormalizationInfo `json:"-"` // Normalization metadata.
}

type EffectivePolicy struct {
	Key     PolicyKey     `json:"key"`           // Policy key this policy applies to.
	ID      string        `json:"id,omitempty"`  // Optional policy identifier.
	Retry   RetryPolicy   `json:"retry"`         // Retry envelope configuration.
	Hedge   HedgePolicy   `json:"hedge"`         // Hedging configuration.
	Circuit CircuitPolicy `json:"circuit"`       // Circuit breaker configuration.

	Meta Metadata `json:"-"` // Resolution metadata (source, normalization).
}

func DefaultPolicyFor(key PolicyKey) EffectivePolicy {
	return EffectivePolicy{
		Key: key,
		Retry: RetryPolicy{
			MaxAttempts:       3,
			InitialBackoff:    10 * time.Millisecond,
			MaxBackoff:        250 * time.Millisecond,
			BackoffMultiplier: 2,
			Jitter:            JitterNone,
			TimeoutPerAttempt: 0,
			OverallTimeout:    0,
			Budget: BudgetRef{
				Cost: 1,
			},
		},
		Hedge: HedgePolicy{
			Enabled:               false,
			MaxHedges:             0,
			HedgeDelay:            0,
			CancelOnFirstTerminal: false,
			Budget: BudgetRef{
				Cost: 1,
			},
		},
		Circuit: CircuitPolicy{
			Enabled:   false,
			Threshold: 0,
			Cooldown:  0,
		},
		Meta: Metadata{
			Source: PolicySourceDefault,
		},
	}
}

const (
	maxRetryAttempts = 10
	maxHedges        = 3

	minBackoffFloor      = 1 * time.Millisecond
	minHedgeDelayFloor   = 10 * time.Millisecond
	maxBackoffCeiling    = 30 * time.Second
	minTimeoutFloor      = 1 * time.Millisecond
	maxBackoffMultiplier = 10.0
	minCircuitThreshold  = 1
	minCircuitCooldown   = 100 * time.Millisecond
)

func (p EffectivePolicy) Normalize() (EffectivePolicy, error) {
	normalized := p
	norm := &normalized.Meta.Normalization

	markChanged := func(field string) {
		norm.Changed = true
		for _, f := range norm.ChangedFields {
			if f == field {
				return
			}
		}
		norm.ChangedFields = append(norm.ChangedFields, field)
	}

	if normalized.Retry.MaxAttempts == 0 {
		normalized.Retry.MaxAttempts = 3
		markChanged("retry.max_attempts")
	}
	if normalized.Retry.MaxAttempts < 1 {
		normalized.Retry.MaxAttempts = 1
		markChanged("retry.max_attempts")
	} else if normalized.Retry.MaxAttempts > maxRetryAttempts {
		normalized.Retry.MaxAttempts = maxRetryAttempts
		markChanged("retry.max_attempts")
	}

	if normalized.Retry.InitialBackoff <= 0 {
		normalized.Retry.InitialBackoff = 10 * time.Millisecond
		markChanged("retry.initial_backoff")
	}
	if normalized.Retry.InitialBackoff < minBackoffFloor {
		normalized.Retry.InitialBackoff = minBackoffFloor
		markChanged("retry.initial_backoff")
	}

	if normalized.Retry.MaxBackoff <= 0 {
		normalized.Retry.MaxBackoff = 250 * time.Millisecond
		markChanged("retry.max_backoff")
	}
	if normalized.Retry.MaxBackoff > maxBackoffCeiling {
		normalized.Retry.MaxBackoff = maxBackoffCeiling
		markChanged("retry.max_backoff")
	}
	if normalized.Retry.MaxBackoff < normalized.Retry.InitialBackoff {
		normalized.Retry.MaxBackoff = normalized.Retry.InitialBackoff
		markChanged("retry.max_backoff")
	}

	if normalized.Retry.BackoffMultiplier == 0 {
		normalized.Retry.BackoffMultiplier = 2
		markChanged("retry.backoff_multiplier")
	}
	if normalized.Retry.BackoffMultiplier < 1 {
		normalized.Retry.BackoffMultiplier = 1
		markChanged("retry.backoff_multiplier")
	} else if normalized.Retry.BackoffMultiplier > maxBackoffMultiplier {
		normalized.Retry.BackoffMultiplier = maxBackoffMultiplier
		markChanged("retry.backoff_multiplier")
	}

	switch normalized.Retry.Jitter {
	case "":
		normalized.Retry.Jitter = JitterNone
		markChanged("retry.jitter")
	case JitterNone, JitterFull, JitterEqual:
	default:
		return EffectivePolicy{}, &NormalizeError{Field: "retry.jitter", Value: string(normalized.Retry.Jitter)}
	}

	if normalized.Retry.TimeoutPerAttempt < 0 {
		normalized.Retry.TimeoutPerAttempt = 0
		markChanged("retry.timeout_per_attempt")
	}
	if normalized.Retry.TimeoutPerAttempt > 0 && normalized.Retry.TimeoutPerAttempt < minTimeoutFloor {
		normalized.Retry.TimeoutPerAttempt = minTimeoutFloor
		markChanged("retry.timeout_per_attempt")
	}

	if normalized.Retry.OverallTimeout < 0 {
		normalized.Retry.OverallTimeout = 0
		markChanged("retry.overall_timeout")
	}
	if normalized.Retry.OverallTimeout > 0 && normalized.Retry.OverallTimeout < minTimeoutFloor {
		normalized.Retry.OverallTimeout = minTimeoutFloor
		markChanged("retry.overall_timeout")
	}

	if normalized.Retry.Budget.Cost == 0 {
		normalized.Retry.Budget.Cost = 1
		markChanged("retry.budget.cost")
	}
	if normalized.Retry.Budget.Cost < 1 {
		normalized.Retry.Budget.Cost = 1
		markChanged("retry.budget.cost")
	}

	if normalized.Hedge.Budget.Cost == 0 {
		normalized.Hedge.Budget.Cost = 1
		markChanged("hedge.budget.cost")
	}
	if normalized.Hedge.Budget.Cost < 1 {
		normalized.Hedge.Budget.Cost = 1
		markChanged("hedge.budget.cost")
	}

	if !normalized.Hedge.Enabled {
		return normalized, nil
	}

	if normalized.Hedge.MaxHedges == 0 {
		normalized.Hedge.MaxHedges = 2
		markChanged("hedge.max_hedges")
	}
	if normalized.Hedge.MaxHedges < 1 {
		normalized.Hedge.MaxHedges = 1
		markChanged("hedge.max_hedges")
	} else if normalized.Hedge.MaxHedges > maxHedges {
		normalized.Hedge.MaxHedges = maxHedges
		markChanged("hedge.max_hedges")
	}

	if normalized.Hedge.HedgeDelay <= 0 {
		normalized.Hedge.HedgeDelay = 200 * time.Millisecond
		markChanged("hedge.hedge_delay")
	}
	if normalized.Hedge.HedgeDelay < minHedgeDelayFloor {
		normalized.Hedge.HedgeDelay = minHedgeDelayFloor
		markChanged("hedge.hedge_delay")
	}

	if !normalized.Circuit.Enabled {
		return normalized, nil
	}

	if normalized.Circuit.Threshold <= 0 {
		normalized.Circuit.Threshold = 5
		markChanged("circuit.threshold")
	}
	if normalized.Circuit.Threshold < minCircuitThreshold {
		normalized.Circuit.Threshold = minCircuitThreshold
		markChanged("circuit.threshold")
	}

	if normalized.Circuit.Cooldown <= 0 {
		normalized.Circuit.Cooldown = 10 * time.Second
		markChanged("circuit.cooldown")
	}
	if normalized.Circuit.Cooldown < minCircuitCooldown {
		normalized.Circuit.Cooldown = minCircuitCooldown
		markChanged("circuit.cooldown")
	}

	return normalized, nil
}
