// Package classify defines outcomes and classifier interfaces for retry
// decisions.
//
// A Classifier maps an attempt result, represented as a value and error, to an
// Outcome. The retry executor uses that outcome to decide whether the call
// succeeded, should retry after backoff, should stop as non-retryable, or should
// abort immediately.
//
// Built-ins include AlwaysRetryOnError for generic error-based retry behavior,
// HTTPClassifier for HTTP-like status and Retry-After semantics, and
// AutoClassifier for dispatching to protocol-aware behavior when possible.
// RegisterBuiltins installs the standard classifiers into a Registry; custom
// classifiers can be registered by name and selected from policy.
package classify
