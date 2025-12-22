package retry

import (
	"context"
	"testing"

	"github.com/aponysus/recourse/observe"
	"github.com/aponysus/recourse/policy"
)

func TestTimelineCapture_CapturesCorrectly(t *testing.T) {
	key := policy.ParseKey("test.capture")
	exec := NewExecutorFromOptions(ExecutorOptions{})

	ctx, capture := observe.RecordTimeline(context.Background())

	val, err := DoValue[int](ctx, exec, key, func(ctx context.Context) (int, error) {
		return 42, nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != 42 {
		t.Errorf("expected 42, got %d", val)
	}

	tl := capture.Timeline()
	if tl == nil {
		t.Fatal("expected timeline to be captured, got nil")
	}
	if tl.Key != key {
		t.Errorf("expected key %v, got %v", key, tl.Key)
	}
	if len(tl.Attempts) != 1 {
		t.Errorf("expected 1 attempt, got %d", len(tl.Attempts))
	}
}

func TestTimelineCapture_DoesNotLeakToNestedCalls(t *testing.T) {
	parentKey := policy.ParseKey("parent")
	childKey := policy.ParseKey("child")
	exec := NewExecutorFromOptions(ExecutorOptions{})

	ctx, capture := observe.RecordTimeline(context.Background())

	_, err := DoValue[int](ctx, exec, parentKey, func(ctx context.Context) (int, error) {
		// Verify capture is NOT present in this context
		if _, ok := observe.TimelineCaptureFromContext(ctx); ok {
			t.Error("capture leaked into operation context")
		}

		// Performing a nested call - should NOT contribute to parent capture
		val, err := DoValue[int](ctx, exec, childKey, func(ctx context.Context) (int, error) {
			return 100, nil
		})
		return val, err
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tl := capture.Timeline()
	if tl == nil {
		t.Fatal("expected timeline, got nil")
	}
	if tl.Key != parentKey {
		t.Errorf("expected parent key, got %v", tl.Key)
	}

	// The parent timeline should NOT have attempts from the child
	// (Though currently we don't merge them anyway, the concern is valid for race conditions or pollution)
	// The main check was `if _, ok := observe.TimelineCaptureFromContext(ctx); ok` inside the op.
}

func TestTimelineCapture_ExplicitNestedCapture(t *testing.T) {
	exec := NewExecutorFromOptions(ExecutorOptions{})

	ctx, parentCap := observe.RecordTimeline(context.Background())

	_, err := DoValue[struct{}](ctx, exec, policy.ParseKey("parent"), func(ctx context.Context) (struct{}, error) {
		// Start a new capture for child
		childCtx, childCap := observe.RecordTimeline(ctx)

		_, err := DoValue[struct{}](childCtx, exec, policy.ParseKey("child"), func(ctx context.Context) (struct{}, error) {
			return struct{}{}, nil
		})

		if childCap.Timeline() == nil {
			t.Error("child capture failed")
		}

		return struct{}{}, err
	})

	if err != nil {
		t.Fatal(err)
	}

	if parentCap.Timeline() == nil {
		t.Error("parent capture failed")
	}
}
