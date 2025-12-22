package retry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aponysus/recourse/classify"
	"github.com/aponysus/recourse/controlplane"
	"github.com/aponysus/recourse/observe"
	"github.com/aponysus/recourse/policy"
)

type stubHTTPError struct {
	status     int
	method     string
	retryAfter time.Duration
	hasRetry   bool
}

func (e stubHTTPError) Error() string { return "http error" }

func (e stubHTTPError) HTTPStatusCode() int { return e.status }

func (e stubHTTPError) HTTPMethod() string { return e.method }

func (e stubHTTPError) RetryAfter() (time.Duration, bool) { return e.retryAfter, e.hasRetry }

func TestExecutor_MissingClassifier_Fallback_RecordsAttributes(t *testing.T) {
	key := policy.PolicyKey{Name: "x"}
	exec := NewExecutorFromOptions(ExecutorOptions{
		Provider: &controlplane.StaticProvider{
			Policies: map[policy.PolicyKey]policy.EffectivePolicy{
				key: {
					Key: key,
					Retry: policy.RetryPolicy{
						MaxAttempts:       2,
						ClassifierName:    "does_not_exist",
						InitialBackoff:    1 * time.Millisecond,
						BackoffMultiplier: 2,
						MaxBackoff:        10 * time.Millisecond,
						Jitter:            policy.JitterNone,
					},
				},
			},
		},
	})
	exec.sleep = func(context.Context, time.Duration) error { return nil }

	calls := 0
	ctx, capture := observe.RecordTimeline(context.Background())
	_, err := DoValue[int](ctx, exec, key, func(context.Context) (int, error) {
		calls++
		return 0, errors.New("nope")
	})
	tl := capture.Timeline()
	if err == nil {
		t.Fatalf("expected error")
	}
	if calls != 2 {
		t.Fatalf("calls=%d, want 2", calls)
	}
	if len(tl.Attempts) != 2 {
		t.Fatalf("attempts=%d, want 2", len(tl.Attempts))
	}
	for i, rec := range tl.Attempts {
		if rec.Attempt != i {
			t.Fatalf("attempt[%d].Attempt=%d, want %d", i, rec.Attempt, i)
		}
		if rec.Outcome.Attributes["classifier_not_found"] != "true" {
			t.Fatalf("attempt[%d] missing classifier_not_found attribute", i)
		}
		if rec.Outcome.Attributes["classifier_name"] != "does_not_exist" {
			t.Fatalf("attempt[%d] classifier_name=%q, want %q", i, rec.Outcome.Attributes["classifier_name"], "does_not_exist")
		}
	}
}

func TestExecutor_MissingClassifier_Deny_FailsBeforeAttempts(t *testing.T) {
	key := policy.PolicyKey{Name: "x"}
	exec := NewExecutorFromOptions(ExecutorOptions{
		Provider: &controlplane.StaticProvider{
			Policies: map[policy.PolicyKey]policy.EffectivePolicy{
				key: {
					Key: key,
					Retry: policy.RetryPolicy{
						MaxAttempts:    3,
						ClassifierName: "does_not_exist",
					},
				},
			},
		},
		MissingClassifierMode: FailureDeny,
	})

	ctx, capture := observe.RecordTimeline(context.Background())
	_, err := DoValue[int](ctx, exec, key, func(context.Context) (int, error) {
		t.Fatalf("op should not be called")
		return 0, nil
	})
	tl := capture.Timeline()
	var nce *NoClassifierError
	if err == nil || !errors.As(err, &nce) {
		t.Fatalf("err=%v, want NoClassifierError", err)
	}
	if len(tl.Attempts) != 0 {
		t.Fatalf("attempts=%d, want 0", len(tl.Attempts))
	}
	if tl.Attributes["classifier_error"] != "classifier_not_found" {
		t.Fatalf("classifier_error=%q, want %q", tl.Attributes["classifier_error"], "classifier_not_found")
	}
	if tl.Attributes["classifier_name"] != "does_not_exist" {
		t.Fatalf("classifier_name=%q, want %q", tl.Attributes["classifier_name"], "does_not_exist")
	}
}

func TestExecutor_HTTPClassifier_5xx_RetriesToMaxAttempts(t *testing.T) {
	key := policy.PolicyKey{Name: "x"}
	exec := NewExecutorFromOptions(ExecutorOptions{
		Provider: &controlplane.StaticProvider{
			Policies: map[policy.PolicyKey]policy.EffectivePolicy{
				key: {
					Key: key,
					Retry: policy.RetryPolicy{
						MaxAttempts:    3,
						ClassifierName: classify.ClassifierHTTP,
						Jitter:         policy.JitterNone,
					},
				},
			},
		},
	})
	exec.sleep = func(context.Context, time.Duration) error { return nil }

	calls := 0
	ctx, capture := observe.RecordTimeline(context.Background())
	_, err := DoValue[int](ctx, exec, key, func(context.Context) (int, error) {
		calls++
		return 0, stubHTTPError{status: 500, method: "GET"}
	})
	tl := capture.Timeline()
	if calls != 3 {
		t.Fatalf("calls=%d, want 3", calls)
	}
	if err == nil {
		t.Fatalf("expected error")
	}
	if len(tl.Attempts) != 3 {
		t.Fatalf("attempts=%d, want 3", len(tl.Attempts))
	}
	if tl.Attempts[0].Outcome.Kind != classify.OutcomeRetryable {
		t.Fatalf("kind=%v, want retryable", tl.Attempts[0].Outcome.Kind)
	}
}

func TestExecutor_HTTPClassifier_404_StopsImmediately(t *testing.T) {
	key := policy.PolicyKey{Name: "x"}
	exec := NewExecutorFromOptions(ExecutorOptions{
		Provider: &controlplane.StaticProvider{
			Policies: map[policy.PolicyKey]policy.EffectivePolicy{
				key: {
					Key: key,
					Retry: policy.RetryPolicy{
						MaxAttempts:    5,
						ClassifierName: classify.ClassifierHTTP,
						Jitter:         policy.JitterNone,
					},
				},
			},
		},
	})
	exec.sleep = func(context.Context, time.Duration) error { return nil }

	calls := 0
	_, err := DoValue[int](context.Background(), exec, key, func(context.Context) (int, error) {
		calls++
		return 0, stubHTTPError{status: 404, method: "GET"}
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if calls != 1 {
		t.Fatalf("calls=%d, want 1", calls)
	}
}

type abortClassifier struct{}

func (abortClassifier) Classify(any, error) classify.Outcome {
	return classify.Outcome{Kind: classify.OutcomeAbort, Reason: "abort"}
}

func TestExecutor_AbortOutcome_DoesNotSleep(t *testing.T) {
	key := policy.PolicyKey{Name: "x"}
	reg := classify.NewRegistry()
	classify.RegisterBuiltins(reg)
	reg.Register("abort", abortClassifier{})

	exec := NewExecutorFromOptions(ExecutorOptions{
		Classifiers: reg,
		Provider: &controlplane.StaticProvider{
			Policies: map[policy.PolicyKey]policy.EffectivePolicy{
				key: {
					Key: key,
					Retry: policy.RetryPolicy{
						MaxAttempts:    3,
						ClassifierName: "abort",
						InitialBackoff: 10 * time.Millisecond,
					},
				},
			},
		},
	})

	sleepCalls := 0
	exec.sleep = func(context.Context, time.Duration) error {
		sleepCalls++
		return nil
	}

	calls := 0
	err := exec.Do(context.Background(), key, func(context.Context) error {
		calls++
		return errors.New("nope")
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if calls != 1 {
		t.Fatalf("calls=%d, want 1", calls)
	}
	if sleepCalls != 0 {
		t.Fatalf("sleepCalls=%d, want 0", sleepCalls)
	}
}

func TestExecutor_BackoffOverride_UsesRetryAfter(t *testing.T) {
	key := policy.PolicyKey{Name: "x"}
	exec := NewExecutorFromOptions(ExecutorOptions{
		Provider: &controlplane.StaticProvider{
			Policies: map[policy.PolicyKey]policy.EffectivePolicy{
				key: {
					Key: key,
					Retry: policy.RetryPolicy{
						MaxAttempts:    2,
						ClassifierName: classify.ClassifierHTTP,
						InitialBackoff: 10 * time.Millisecond,
						MaxBackoff:     250 * time.Millisecond,
						Jitter:         policy.JitterFull,
					},
				},
			},
		},
	})

	var sleeps []time.Duration
	exec.sleep = func(_ context.Context, d time.Duration) error {
		sleeps = append(sleeps, d)
		return nil
	}

	_, _ = DoValue[int](context.Background(), exec, key, func(context.Context) (int, error) {
		return 0, stubHTTPError{status: 429, method: "GET", retryAfter: 200 * time.Millisecond, hasRetry: true}
	})
	if len(sleeps) != 1 {
		t.Fatalf("sleeps=%v, want 1 entry", sleeps)
	}
	if sleeps[0] != 200*time.Millisecond {
		t.Fatalf("sleep=%v, want 200ms", sleeps[0])
	}
}
