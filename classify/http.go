package classify

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// HTTPError is a classify-owned interface that allows HTTP classifiers to
// recognize retry semantics without importing integration packages.
//
// Implementations should use status code 0 for transport errors.
type HTTPError interface {
	HTTPStatusCode() int
	HTTPMethod() string
	RetryAfter() (time.Duration, bool)
}

// HTTPClassifier classifies outcomes for HTTP-like operations based on an HTTPError.
//
// If the provided error does not implement HTTPError, it returns a non-retryable
// outcome with reason "classifier_type_mismatch".
type HTTPClassifier struct {
	// Retryable4xx is an optional set of additional retryable 4xx status codes.
	// If nil, defaults to {408, 429}.
	Retryable4xx map[int]struct{}
}

func (c HTTPClassifier) Classify(_ any, err error) Outcome {
	if err == nil {
		return Outcome{Kind: OutcomeSuccess, Reason: "success"}
	}
	if errors.Is(err, context.Canceled) {
		return Outcome{Kind: OutcomeAbort, Reason: "context_canceled"}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return Outcome{Kind: OutcomeRetryable, Reason: "context_deadline_exceeded"}
	}

	he, ok := err.(HTTPError)
	if !ok {
		return Outcome{
			Kind:   OutcomeNonRetryable,
			Reason: "classifier_type_mismatch",
			Attributes: map[string]string{
				"expected_type": "classify.HTTPError",
				"got_type":      typeString(err),
			},
		}
	}

	status := he.HTTPStatusCode()
	method := strings.ToUpper(strings.TrimSpace(he.HTTPMethod()))
	idempotent := isIdempotentMethod(method)

	out := Outcome{
		Kind:   OutcomeNonRetryable,
		Reason: "http_non_retryable_status",
		Attributes: map[string]string{
			"status": strconv.Itoa(status),
			"method": method,
		},
	}

	if status >= 200 && status < 300 {
		out.Kind = OutcomeSuccess
		out.Reason = "success"
		return out
	}

	if status == 0 {
		if idempotent {
			out.Kind = OutcomeRetryable
			out.Reason = "http_transport_error"
		} else {
			out.Kind = OutcomeNonRetryable
			out.Reason = "http_non_idempotent"
		}
		return out
	}

	if status >= 500 && status <= 599 {
		if idempotent {
			out.Kind = OutcomeRetryable
			out.Reason = "http_5xx"
		} else {
			out.Kind = OutcomeNonRetryable
			out.Reason = "http_non_idempotent"
		}
		return out
	}

	if status == 408 || status == 429 || c.retryable4xx(status) {
		if idempotent {
			out.Kind = OutcomeRetryable
			out.Reason = "http_" + strconv.Itoa(status)
			if d, ok := he.RetryAfter(); ok && d > 0 {
				out.BackoffOverride = d
				out.Attributes["retry_after"] = d.String()
			}
		} else {
			out.Kind = OutcomeNonRetryable
			out.Reason = "http_non_idempotent"
		}
		return out
	}

	// All other 4xx are treated as terminal by default.
	return out
}

func (c HTTPClassifier) retryable4xx(status int) bool {
	if c.Retryable4xx == nil {
		return false
	}
	_, ok := c.Retryable4xx[status]
	return ok
}

func isIdempotentMethod(method string) bool {
	switch method {
	case "GET", "HEAD", "PUT", "DELETE", "OPTIONS", "TRACE":
		return true
	default:
		return false
	}
}

func typeString(err error) string {
	t := reflect.TypeOf(err)
	if t == nil {
		return "<nil>"
	}
	return t.String()
}
