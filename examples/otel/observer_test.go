package main

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

func TestOTelObserver_OnSuccessCreatesSpan(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	defer func() {
		_ = provider.Shutdown(context.Background())
	}()

	observer := NewOTelObserver(provider.Tracer("test"))
	key := policy.PolicyKey{Namespace: "svc", Name: "method"}
	start := time.Unix(0, 0)
	attempt := observe.AttemptRecord{
		Attempt:   1,
		IsHedge:   false,
		Outcome:   classify.Outcome{Reason: "retryable"},
		StartTime: start,
		EndTime:   start.Add(5 * time.Millisecond),
	}
	observer.OnSuccess(context.Background(), key, observe.Timeline{
		Key:      key,
		Start:    start,
		End:      start.Add(10 * time.Millisecond),
		Attempts: []observe.AttemptRecord{attempt},
	})

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	stub := tracetest.SpanStubsFromReadOnlySpans(spans)[0]
	if stub.Name != "recourse.svc.method" {
		t.Fatalf("unexpected span name: %s", stub.Name)
	}
	if stub.Status.Code != codes.Ok {
		t.Fatalf("expected status OK, got %v", stub.Status.Code)
	}

	if value, ok := findAttr(stub.Attributes, "recourse.key"); !ok || value.AsString() != "svc.method" {
		t.Fatalf("expected recourse.key attribute")
	}
	if value, ok := findAttr(stub.Attributes, "recourse.attempts"); !ok || value.AsInt64() != 1 {
		t.Fatalf("expected recourse.attempts=1")
	}

	if len(stub.Events) != 1 {
		t.Fatalf("expected 1 attempt event, got %d", len(stub.Events))
	}
	event := stub.Events[0]
	if event.Name != "attempt" {
		t.Fatalf("expected attempt event, got %s", event.Name)
	}
	if value, ok := findAttr(event.Attributes, "recourse.attempt"); !ok || value.AsInt64() != 1 {
		t.Fatalf("expected recourse.attempt=1")
	}
	if value, ok := findAttr(event.Attributes, "recourse.hedge"); !ok || value.AsBool() {
		t.Fatalf("expected recourse.hedge=false")
	}
}

func TestOTelObserver_OnFailureSetsErrorStatus(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	defer func() {
		_ = provider.Shutdown(context.Background())
	}()

	observer := NewOTelObserver(provider.Tracer("test"))
	key := policy.PolicyKey{Name: "failure"}
	start := time.Unix(0, 0)
	finalErr := errors.New("boom")
	observer.OnFailure(context.Background(), key, observe.Timeline{
		Key:      key,
		Start:    start,
		End:      start.Add(5 * time.Millisecond),
		FinalErr: finalErr,
	})

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	stub := tracetest.SpanStubsFromReadOnlySpans(spans)[0]
	if stub.Status.Code != codes.Error {
		t.Fatalf("expected status Error, got %v", stub.Status.Code)
	}
	if stub.Status.Description != finalErr.Error() {
		t.Fatalf("expected status description %q, got %q", finalErr.Error(), stub.Status.Description)
	}
}

func findAttr(attrs []attribute.KeyValue, key string) (attribute.Value, bool) {
	for _, attr := range attrs {
		if string(attr.Key) == key {
			return attr.Value, true
		}
	}
	return attribute.Value{}, false
}
