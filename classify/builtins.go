package classify

import (
	"context"
	"errors"
)

// Built-in classifier registry names.
const (
	ClassifierAlwaysRetryOnError = "always"
	ClassifierHTTP               = "http"
)

// RegisterBuiltins registers core classifiers into reg.
func RegisterBuiltins(reg *Registry) {
	if reg == nil {
		return
	}
	reg.Register(ClassifierAlwaysRetryOnError, AlwaysRetryOnError{})
	reg.Register(ClassifierHTTP, HTTPClassifier{})
	reg.Register("auto", AutoClassifier{})
}

// AlwaysRetryOnError classifies nil errors as success and all other errors as retryable,
// except for context cancellation which aborts immediately.
type AlwaysRetryOnError struct{}

func (AlwaysRetryOnError) Classify(_ any, err error) Outcome {
	if err == nil {
		return Outcome{Kind: OutcomeSuccess, Reason: "success"}
	}
	if errors.Is(err, context.Canceled) {
		return Outcome{Kind: OutcomeAbort, Reason: "context_canceled"}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		// Per-attempt timeouts and dependency timeouts are often retryable; the executor
		// still respects overall context cancellation/deadlines separately.
		return Outcome{Kind: OutcomeRetryable, Reason: "context_deadline_exceeded"}
	}
	return Outcome{Kind: OutcomeRetryable, Reason: "retryable_error"}
}
