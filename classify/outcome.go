package classify

import "time"

// OutcomeKind describes the executor's decision about an attempt result.
type OutcomeKind int

const (
	// OutcomeUnknown indicates an invalid or missing classification.
	OutcomeUnknown OutcomeKind = iota
	OutcomeSuccess
	OutcomeRetryable
	OutcomeNonRetryable
	OutcomeAbort
)

// Outcome describes the classification of an attempt.
type Outcome struct {
	Kind   OutcomeKind
	Reason string

	// Attributes are optional classifier-provided metadata (low-cardinality).
	Attributes map[string]string

	// BackoffOverride, when set, overrides the policy backoff before the next attempt.
	BackoffOverride time.Duration
}

// Classifier determines whether an attempt result is success, retryable, terminal,
// or should abort immediately.
type Classifier interface {
	Classify(value any, err error) Outcome
}
