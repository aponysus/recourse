package observe_test

import (
	"context"
	"testing"

	"github.com/aponysus/recourse/observe"
	"github.com/aponysus/recourse/policy"
)

func TestRecordTimeline_CapturesAllAttempts(t *testing.T) {
	ctx, capture := observe.RecordTimeline(context.Background())
	if capture == nil {
		t.Fatal("expected non-nil capture")
	}
	gotCapture, ok := observe.TimelineCaptureFromContext(ctx)
	if !ok {
		t.Fatal("expected capture in context")
	}
	if gotCapture != capture {
		t.Fatal("expected context capture to match returned capture")
	}

	tl := observe.Timeline{
		Key:      policy.PolicyKey{Name: "demo"},
		Attempts: []observe.AttemptRecord{{Attempt: 0}, {Attempt: 1}},
	}
	observe.StoreTimelineCapture(capture, &tl)

	got := capture.Timeline()
	if got == nil {
		t.Fatal("expected captured timeline")
	}
	if got.Key != tl.Key {
		t.Fatalf("expected key %v, got %v", tl.Key, got.Key)
	}
	if len(got.Attempts) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(got.Attempts))
	}
}

func TestTimelineCaptureFromContext_NotPresent(t *testing.T) {
	if _, ok := observe.TimelineCaptureFromContext(context.Background()); ok {
		t.Fatal("expected no capture in context")
	}
	if _, ok := observe.TimelineCaptureFromContext(nil); ok {
		t.Fatal("expected no capture for nil context")
	}
}

func TestWithoutTimelineCapture_SuppressesCapture(t *testing.T) {
	ctx, capture := observe.RecordTimeline(context.Background())
	if _, ok := observe.TimelineCaptureFromContext(ctx); !ok {
		t.Fatal("expected capture in base context")
	}
	noCapture := observe.WithoutTimelineCapture(ctx)
	if _, ok := observe.TimelineCaptureFromContext(noCapture); ok {
		t.Fatal("expected capture to be suppressed")
	}
	got, ok := observe.TimelineCaptureFromContext(ctx)
	if !ok || got != capture {
		t.Fatal("expected original capture to remain available")
	}
}
