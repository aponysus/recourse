package otelrecourse

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aponysus/recourse/classify"
	"github.com/aponysus/recourse/observe"
	"github.com/aponysus/recourse/policy"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestObserver_OnSuccessRecordsSpanAndAttemptEvents(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	defer func() {
		_ = provider.Shutdown(context.Background())
	}()

	key := policy.PolicyKey{Namespace: "svc", Name: "method"}
	start := time.Unix(0, 0)
	observer := NewObserver(provider.Tracer("test"))
	observer.OnSuccess(context.Background(), key, observe.Timeline{
		Key:   key,
		Start: start,
		End:   start.Add(20 * time.Millisecond),
		Attributes: map[string]string{
			"policy_mode":   "standard",
			"policy_source": "default",
		},
		Attempts: []observe.AttemptRecord{
			{
				Attempt:       0,
				StartTime:     start,
				EndTime:       start.Add(5 * time.Millisecond),
				Outcome:       classify.Outcome{Reason: "retryable_error"},
				Err:           errors.New("temporary upstream error"),
				BudgetAllowed: true,
				BudgetReason:  "no_budget",
			},
			{
				Attempt:       1,
				StartTime:     start.Add(10 * time.Millisecond),
				EndTime:       start.Add(15 * time.Millisecond),
				Outcome:       classify.Outcome{Reason: "success"},
				BudgetAllowed: true,
				BudgetReason:  "no_budget",
			},
		},
	})

	stubs := spanStubs(recorder)
	if len(stubs) != 1 {
		t.Fatalf("expected 1 span, got %d", len(stubs))
	}
	span := stubs[0]
	if span.Name != "recourse.svc.method" {
		t.Fatalf("unexpected span name: %s", span.Name)
	}
	if span.Status.Code != codes.Ok {
		t.Fatalf("expected status OK, got %v", span.Status.Code)
	}
	assertStringAttr(t, span.Attributes, attrKey, "svc.method")
	assertIntAttr(t, span.Attributes, attrAttempts, 2)
	assertStringAttr(t, span.Attributes, "recourse.policy_source", "default")
	assertStringAttr(t, span.Attributes, "recourse.policy_mode", "standard")

	if len(span.Events) != 2 {
		t.Fatalf("expected 2 attempt events, got %d", len(span.Events))
	}
	if span.Events[0].Name != "recourse.attempt" {
		t.Fatalf("expected recourse.attempt event, got %s", span.Events[0].Name)
	}
	assertIntAttr(t, span.Events[0].Attributes, attrAttempt, 0)
	assertStringAttr(t, span.Events[0].Attributes, attrOutcome, "retryable_error")
	assertBoolAttr(t, span.Events[0].Attributes, attrError, true)
}

func TestObserver_OnFailureSetsErrorStatus(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	defer func() {
		_ = provider.Shutdown(context.Background())
	}()

	key := policy.PolicyKey{Name: "failure"}
	finalErr := errors.New("boom")
	observer := NewObserver(provider.Tracer("test"))
	observer.OnFailure(context.Background(), key, observe.Timeline{
		Key:      key,
		FinalErr: finalErr,
	})

	stubs := spanStubs(recorder)
	if len(stubs) != 1 {
		t.Fatalf("expected 1 span, got %d", len(stubs))
	}
	if stubs[0].Status.Code != codes.Error {
		t.Fatalf("expected status Error, got %v", stubs[0].Status.Code)
	}
	if stubs[0].Status.Description != finalErr.Error() {
		t.Fatalf("expected status description %q, got %q", finalErr.Error(), stubs[0].Status.Description)
	}
}

func TestObserver_SpanPerAttemptCreatesChildSpans(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	defer func() {
		_ = provider.Shutdown(context.Background())
	}()

	key := policy.PolicyKey{Namespace: "svc", Name: "method"}
	start := time.Unix(0, 0)
	observer := NewObserver(
		provider.Tracer("test"),
		WithRecordEvents(false),
		WithSpanPerAttempt(true),
	)
	observer.OnSuccess(context.Background(), key, observe.Timeline{
		Key:   key,
		Start: start,
		End:   start.Add(20 * time.Millisecond),
		Attempts: []observe.AttemptRecord{
			{
				Attempt:       0,
				StartTime:     start,
				EndTime:       start.Add(5 * time.Millisecond),
				Outcome:       classify.Outcome{Reason: "success"},
				BudgetAllowed: true,
			},
		},
	})

	stubs := spanStubs(recorder)
	if len(stubs) != 2 {
		t.Fatalf("expected operation and attempt spans, got %d", len(stubs))
	}
	op := findSpan(t, stubs, "recourse.svc.method")
	attempt := findSpan(t, stubs, "recourse.attempt")
	if attempt.Parent.SpanID() != op.SpanContext.SpanID() {
		t.Fatalf("attempt span parent=%s, want %s", attempt.Parent.SpanID(), op.SpanContext.SpanID())
	}
	if len(op.Events) != 0 {
		t.Fatalf("expected no attempt events when WithRecordEvents(false), got %d", len(op.Events))
	}
	assertIntAttr(t, attempt.Attributes, attrAttempt, 0)
	assertStringAttr(t, attempt.Attributes, attrOutcome, "success")
}

func spanStubs(recorder *tracetest.SpanRecorder) []tracetest.SpanStub {
	return tracetest.SpanStubsFromReadOnlySpans(recorder.Ended())
}

func findSpan(t *testing.T, stubs []tracetest.SpanStub, name string) tracetest.SpanStub {
	t.Helper()
	for _, stub := range stubs {
		if stub.Name == name {
			return stub
		}
	}
	t.Fatalf("span %q not found", name)
	return tracetest.SpanStub{}
}

func findAttr(attrs []attribute.KeyValue, key string) (attribute.Value, bool) {
	for _, attr := range attrs {
		if string(attr.Key) == key {
			return attr.Value, true
		}
	}
	return attribute.Value{}, false
}

func assertStringAttr(t *testing.T, attrs []attribute.KeyValue, key string, want string) {
	t.Helper()
	value, ok := findAttr(attrs, key)
	if !ok {
		t.Fatalf("attribute %s not found", key)
	}
	if got := value.AsString(); got != want {
		t.Fatalf("attribute %s=%q, want %q", key, got, want)
	}
}

func assertIntAttr(t *testing.T, attrs []attribute.KeyValue, key string, want int64) {
	t.Helper()
	value, ok := findAttr(attrs, key)
	if !ok {
		t.Fatalf("attribute %s not found", key)
	}
	if got := value.AsInt64(); got != want {
		t.Fatalf("attribute %s=%d, want %d", key, got, want)
	}
}

func assertBoolAttr(t *testing.T, attrs []attribute.KeyValue, key string, want bool) {
	t.Helper()
	value, ok := findAttr(attrs, key)
	if !ok {
		t.Fatalf("attribute %s not found", key)
	}
	if got := value.AsBool(); got != want {
		t.Fatalf("attribute %s=%v, want %v", key, got, want)
	}
}
