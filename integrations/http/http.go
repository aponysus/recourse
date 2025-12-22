package http

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/aponysus/recourse/observe"
	"github.com/aponysus/recourse/policy"
	"github.com/aponysus/recourse/retry"
)

// DoHTTP executes an HTTP request with retries.
// It automatically handles request cloning, body draining/closing on retryable errors,
// and status code classification.
func DoHTTP(ctx context.Context, exec *retry.Executor, key policy.PolicyKey, client *http.Client, req *http.Request) (*http.Response, observe.Timeline, error) {
	if req.Body != nil && req.Body != http.NoBody && req.GetBody == nil {
		return nil, observe.Timeline{}, errors.New("recourse: request body is not replayable (GetBody is nil)")
	}

	op := func(ctx context.Context) (*http.Response, error) {
		// Clone request
		outReq := req.Clone(ctx)
		if req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			outReq.Body = body
		}

		resp, err := client.Do(outReq)
		if err != nil {
			// Transport error or context error.
			// We wrap it to interface with AutoClassifier if needed, but standard errors (context) might be wrapped.
			// However, AutoClassifier delegates to AlwaysRetry which checks errors.Is.
			// If we return raw error, AlwaysRetry works.
			// If we return StatusError with code 0, AutoClassifier sends to HTTPClassifier.
			// HTTPClassifier handles code 0 + idempotent check.

			// So, if we want idempotency check, we MUST use StatusError.
			return nil, &StatusError{
				Err:    err,
				Method: req.Method,
			}
		}

		// Check if successful (2xx)
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp, nil
		}

		// Failure: Drain and close to prevent leaks on retry
		// We limit drain to avoid hanging on large error bodies
		_, _ = io.CopyN(io.Discard, resp.Body, 4096)
		resp.Body.Close()

		return nil, &StatusError{
			Code:   resp.StatusCode,
			Method: req.Method,
			Header: resp.Header,
		}
	}

	// Wrap context to capture timeline
	ctx, capture := observe.RecordTimeline(ctx)

	val, err := retry.DoValue(ctx, exec, key, op)

	var tl observe.Timeline
	if t := capture.Timeline(); t != nil {
		tl = *t
	}

	return val, tl, err
}

// StatusError implements classify.HTTPError.
type StatusError struct {
	Code   int
	Method string
	Header http.Header
	Err    error
}

func (e *StatusError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return "http status " + strconv.Itoa(e.Code)
}

func (e *StatusError) Unwrap() error { return e.Err }

func (e *StatusError) HTTPStatusCode() int { return e.Code }
func (e *StatusError) HTTPMethod() string  { return e.Method }

func (e *StatusError) RetryAfter() (time.Duration, bool) {
	if e.Header == nil {
		return 0, false
	}
	s := e.Header.Get("Retry-After")
	if s == "" {
		return 0, false
	}

	// Try seconds
	if secs, err := strconv.Atoi(s); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second, true
	}

	// Try HTTP date
	if t, err := http.ParseTime(s); err == nil {
		d := time.Until(t)
		if d < 0 {
			d = 0
		}
		return d, true
	}

	return 0, false
}
