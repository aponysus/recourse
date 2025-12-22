package classify

// AutoClassifier delegates to a specific classifier based on the error type,
// or falls back to a generic default.
//
// Behavior:
// - If error implements HTTPError: uses HTTPClassifier.
// - Otherwise: uses AlwaysRetryOnError.
type AutoClassifier struct{}

func (AutoClassifier) Classify(val any, err error) Outcome {
	if _, ok := err.(HTTPError); ok {
		return HTTPClassifier{}.Classify(val, err)
	}
	return AlwaysRetryOnError{}.Classify(val, err)
}
