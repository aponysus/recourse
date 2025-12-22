package recourse_test

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aponysus/recourse/controlplane"
	"github.com/aponysus/recourse/observe"
	"github.com/aponysus/recourse/policy"
	"github.com/aponysus/recourse/recourse"
	"github.com/aponysus/recourse/retry"
)

func TestMain(m *testing.M) {
	recourse.Init(newTestExecutor())
	os.Exit(m.Run())
}

func newTestExecutor() *retry.Executor {
	policies := map[policy.PolicyKey]policy.EffectivePolicy{
		policy.ParseKey("recourse.success"):  testPolicy(2),
		policy.ParseKey("recourse.retry"):    testPolicy(2),
		policy.ParseKey("recourse.timeline"): testPolicy(2),
	}
	provider := &controlplane.StaticProvider{Policies: policies}
	return retry.NewExecutor(retry.WithProvider(provider))
}

func testPolicy(maxAttempts int) policy.EffectivePolicy {
	return policy.EffectivePolicy{
		Retry: policy.RetryPolicy{
			MaxAttempts:       maxAttempts,
			InitialBackoff:    time.Millisecond,
			MaxBackoff:        time.Millisecond,
			BackoffMultiplier: 1,
			Jitter:            policy.JitterNone,
		},
	}
}

func TestDoValue_SimpleSuccess(t *testing.T) {
	ctx := context.Background()
	got, err := recourse.DoValue(ctx, "recourse.success", func(ctx context.Context) (int, error) {
		return 7, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 7 {
		t.Fatalf("expected 7, got %d", got)
	}
}

func TestDoValue_RetriesOnError(t *testing.T) {
	ctx := context.Background()
	var attempts int32
	got, err := recourse.DoValue(ctx, "recourse.retry", func(ctx context.Context) (int, error) {
		if atomic.AddInt32(&attempts, 1) == 1 {
			return 0, errors.New("retry me")
		}
		return 99, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 99 {
		t.Fatalf("expected 99, got %d", got)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
}

func TestDoValue_WithTimeline(t *testing.T) {
	ctx, capture := observe.RecordTimeline(context.Background())
	var attempts int32
	_, err := recourse.DoValue(ctx, "recourse.timeline", func(ctx context.Context) (int, error) {
		if atomic.AddInt32(&attempts, 1) == 1 {
			return 0, errors.New("retry once")
		}
		return 42, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
	if capture == nil {
		t.Fatal("expected timeline capture to be non-nil")
	}
	if tl := capture.Timeline(); tl == nil {
		t.Fatal("expected timeline to be captured")
	} else {
		if tl.Key != policy.ParseKey("recourse.timeline") {
			t.Fatalf("expected timeline key %v, got %v", policy.ParseKey("recourse.timeline"), tl.Key)
		}
		if len(tl.Attempts) != 2 {
			t.Fatalf("expected 2 attempts in timeline, got %d", len(tl.Attempts))
		}
	}
}

func TestParseKey_VariousFormats(t *testing.T) {
	cases := []struct {
		input string
		want  policy.PolicyKey
	}{
		{input: "service.method", want: policy.PolicyKey{Namespace: "service", Name: "method"}},
		{input: "method", want: policy.PolicyKey{Name: "method"}},
		{input: " service.method ", want: policy.PolicyKey{Namespace: "service", Name: "method"}},
		{input: "service.", want: policy.PolicyKey{Name: "service."}},
		{input: "", want: policy.PolicyKey{}},
	}

	for _, tc := range cases {
		got := recourse.ParseKey(tc.input)
		if got != tc.want {
			t.Fatalf("ParseKey(%q) = %+v, want %+v", tc.input, got, tc.want)
		}
	}
}
