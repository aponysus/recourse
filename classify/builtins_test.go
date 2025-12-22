package classify

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAlwaysRetryOnError(t *testing.T) {
	c := AlwaysRetryOnError{}

	if out := c.Classify(nil, nil); out.Kind != OutcomeSuccess {
		t.Fatalf("nil err: kind=%v want %v", out.Kind, OutcomeSuccess)
	}

	if out := c.Classify(nil, context.Canceled); out.Kind != OutcomeAbort {
		t.Fatalf("canceled: kind=%v want %v", out.Kind, OutcomeAbort)
	}

	if out := c.Classify(nil, context.DeadlineExceeded); out.Kind != OutcomeRetryable {
		t.Fatalf("deadline: kind=%v want %v", out.Kind, OutcomeRetryable)
	}

	if out := c.Classify(nil, errors.New("nope")); out.Kind != OutcomeRetryable {
		t.Fatalf("error: kind=%v want %v", out.Kind, OutcomeRetryable)
	}
}

type testHTTPError struct {
	status     int
	method     string
	retryAfter time.Duration
	hasRetry   bool
}

func (e testHTTPError) Error() string { return "http error" }

func (e testHTTPError) HTTPStatusCode() int { return e.status }

func (e testHTTPError) HTTPMethod() string { return e.method }

func (e testHTTPError) RetryAfter() (time.Duration, bool) { return e.retryAfter, e.hasRetry }

func TestHTTPClassifier_Success(t *testing.T) {
	c := HTTPClassifier{}
	out := c.Classify(nil, nil)
	if out.Kind != OutcomeSuccess {
		t.Fatalf("kind=%v want %v", out.Kind, OutcomeSuccess)
	}
}

func TestHTTPClassifier_5xx_Idempotent_Retryable(t *testing.T) {
	c := HTTPClassifier{}
	out := c.Classify(nil, testHTTPError{status: 500, method: "GET"})
	if out.Kind != OutcomeRetryable {
		t.Fatalf("kind=%v want %v", out.Kind, OutcomeRetryable)
	}
}

func TestHTTPClassifier_5xx_NonIdempotent_NonRetryable(t *testing.T) {
	c := HTTPClassifier{}
	out := c.Classify(nil, testHTTPError{status: 500, method: "POST"})
	if out.Kind != OutcomeNonRetryable {
		t.Fatalf("kind=%v want %v", out.Kind, OutcomeNonRetryable)
	}
}

func TestHTTPClassifier_404_NonRetryable(t *testing.T) {
	c := HTTPClassifier{}
	out := c.Classify(nil, testHTTPError{status: 404, method: "GET"})
	if out.Kind != OutcomeNonRetryable {
		t.Fatalf("kind=%v want %v", out.Kind, OutcomeNonRetryable)
	}
}

func TestHTTPClassifier_429_RetryAfter_Override(t *testing.T) {
	c := HTTPClassifier{}
	out := c.Classify(nil, testHTTPError{status: 429, method: "GET", retryAfter: 2 * time.Second, hasRetry: true})
	if out.Kind != OutcomeRetryable {
		t.Fatalf("kind=%v want %v", out.Kind, OutcomeRetryable)
	}
	if out.BackoffOverride != 2*time.Second {
		t.Fatalf("BackoffOverride=%v want 2s", out.BackoffOverride)
	}
	if got := out.Attributes["retry_after"]; got != "2s" {
		t.Fatalf("retry_after=%q want %q", got, "2s")
	}
}

func TestHTTPClassifier_TypeMismatch(t *testing.T) {
	c := HTTPClassifier{}
	out := c.Classify(nil, errors.New("nope"))
	if out.Kind != OutcomeNonRetryable || out.Reason != "classifier_type_mismatch" {
		t.Fatalf("out=%+v want nonretryable classifier_type_mismatch", out)
	}
	if out.Attributes["expected_type"] == "" || out.Attributes["got_type"] == "" {
		t.Fatalf("expected type mismatch attributes")
	}
}
