package observe_test

import (
	"context"
	"testing"

	"github.com/aponysus/recourse/observe"
)

func TestAttemptFromContext_Present(t *testing.T) {
	info := observe.AttemptInfo{
		RetryIndex: 1,
		Attempt:    2,
		IsHedge:    true,
		HedgeIndex: 1,
		PolicyID:   "policy-id",
	}
	ctx := observe.WithAttemptInfo(context.Background(), info)
	got, ok := observe.AttemptFromContext(ctx)
	if !ok {
		t.Fatal("expected attempt info in context")
	}
	if got != info {
		t.Fatalf("expected %+v, got %+v", info, got)
	}
}

func TestAttemptFromContext_NotPresent(t *testing.T) {
	if _, ok := observe.AttemptFromContext(context.Background()); ok {
		t.Fatal("expected no attempt info in context")
	}
}

func TestWithAttemptInfo_Roundtrip(t *testing.T) {
	base := context.Background()
	info := observe.AttemptInfo{Attempt: 3, PolicyID: "policy-123"}
	ctx := observe.WithAttemptInfo(base, info)
	if ctx == base {
		t.Fatal("expected derived context")
	}
	got, ok := observe.AttemptFromContext(ctx)
	if !ok {
		t.Fatal("expected attempt info in context")
	}
	if got != info {
		t.Fatalf("expected %+v, got %+v", info, got)
	}
	if _, ok := observe.AttemptFromContext(base); ok {
		t.Fatal("expected base context to be unchanged")
	}
}
