package resilience

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/aponysus/recourse/classify"
)

type fakeAWSAPIError struct{ code string }

func (e fakeAWSAPIError) Error() string     { return e.code }
func (e fakeAWSAPIError) ErrorCode() string { return e.code }

type fakeSQLError struct{ number int32 }

func (e fakeSQLError) Error() string         { return "sql error" }
func (e fakeSQLError) SQLErrorNumber() int32 { return e.number }

type fakeNetError struct{}

func (fakeNetError) Error() string   { return "i/o timeout" }
func (fakeNetError) Timeout() bool   { return true }
func (fakeNetError) Temporary() bool { return true }

func TestAWSReadClassifier(t *testing.T) {
	c := AWSReadClassifier{}
	cases := []struct {
		name   string
		err    error
		kind   classify.OutcomeKind
		reason string
	}{
		{"success", nil, classify.OutcomeSuccess, "success"},
		{"canceled", context.Canceled, classify.OutcomeAbort, "context_canceled"},
		{"throttled", fakeAWSAPIError{"ThrottlingException"}, classify.OutcomeRetryable, "aws_throttled"},
		{"access denied", fakeAWSAPIError{"AccessDeniedException"}, classify.OutcomeNonRetryable, "aws_read_terminal"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := c.Classify(nil, tc.err)
			if out.Kind != tc.kind || out.Reason != tc.reason {
				t.Fatalf("out=%+v want kind=%v reason=%q", out, tc.kind, tc.reason)
			}
		})
	}
}

func TestAWSWriteSafeClassifier(t *testing.T) {
	c := AWSWriteSafeClassifier{}
	out := c.Classify(nil, fakeAWSAPIError{code: "ValidationException"})
	if out.Kind != classify.OutcomeNonRetryable || out.Reason != "aws_write_terminal" {
		t.Fatalf("out=%+v want terminal aws_write_terminal", out)
	}
}

func TestAWSCallbackClassifier(t *testing.T) {
	c := AWSCallbackClassifier{}
	cases := []struct {
		name   string
		err    error
		kind   classify.OutcomeKind
		reason string
	}{
		{"kms throttled", fakeAWSAPIError{"KmsThrottlingException"}, classify.OutcomeRetryable, "sfn_throttled"},
		{"invalid token", fakeAWSAPIError{"InvalidToken"}, classify.OutcomeNonRetryable, "sfn_invalid_token"},
		{"task timed out", fakeAWSAPIError{"TaskTimedOut"}, classify.OutcomeNonRetryable, "sfn_task_timed_out"},
		{"transport", fakeNetError{}, classify.OutcomeRetryable, "sfn_transport"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := c.Classify(nil, tc.err)
			if out.Kind != tc.kind || out.Reason != tc.reason {
				t.Fatalf("out=%+v want kind=%v reason=%q", out, tc.kind, tc.reason)
			}
		})
	}
}

func TestSQLConnectClassifier(t *testing.T) {
	c := SQLConnectClassifier{}
	cases := []struct {
		name   string
		err    error
		kind   classify.OutcomeKind
		reason string
	}{
		{"bad conn", driver.ErrBadConn, classify.OutcomeRetryable, "sql_bad_conn"},
		{"auth", fakeSQLError{18456}, classify.OutcomeNonRetryable, "sql_auth"},
		{"transient engine", fakeSQLError{40501}, classify.OutcomeRetryable, "sql_transient_engine"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := c.Classify(nil, tc.err)
			if out.Kind != tc.kind || out.Reason != tc.reason {
				t.Fatalf("out=%+v want kind=%v reason=%q", out, tc.kind, tc.reason)
			}
		})
	}
}

func TestSQLQueryClassifier(t *testing.T) {
	c := SQLQueryClassifier{}
	cases := []struct {
		name   string
		err    error
		kind   classify.OutcomeKind
		reason string
	}{
		{"deadlock", fakeSQLError{1205}, classify.OutcomeRetryable, "sql_deadlock"},
		{"lock timeout", fakeSQLError{1222}, classify.OutcomeRetryable, "sql_transient_engine"},
		{"non retryable", fakeSQLError{207}, classify.OutcomeNonRetryable, "sql_non_retryable"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := c.Classify(nil, tc.err)
			if out.Kind != tc.kind || out.Reason != tc.reason {
				t.Fatalf("out=%+v want kind=%v reason=%q", out, tc.kind, tc.reason)
			}
		})
	}
}

func TestEventingClassifier(t *testing.T) {
	c := EventingClassifier{}

	out := c.Classify(nil, fakeNetError{})
	if out.Kind != classify.OutcomeRetryable || out.Reason != "eventing_transport" {
		t.Fatalf("out=%+v want retryable eventing_transport", out)
	}

	out = c.Classify(nil, fakeAWSAPIError{code: "ThrottlingException"})
	if out.Kind != classify.OutcomeRetryable || out.Reason != "eventing_throttled" {
		t.Fatalf("out=%+v want retryable eventing_throttled", out)
	}

	out = c.Classify(nil, errors.New("boom"))
	if out.Kind != classify.OutcomeNonRetryable || out.Reason != "eventing_unknown" {
		t.Fatalf("out=%+v want terminal eventing_unknown", out)
	}
}
