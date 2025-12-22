package grpc_test

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/aponysus/recourse/classify"
	integration "github.com/aponysus/recourse/integrations/grpc"
	"github.com/aponysus/recourse/retry"
)

func TestClassifier(t *testing.T) {
	c := integration.Classifier{}

	tests := []struct {
		err      error
		wantKind classify.OutcomeKind
	}{
		{nil, classify.OutcomeSuccess},
		{status.Error(codes.OK, "ok"), classify.OutcomeSuccess},
		{status.Error(codes.Unavailable, "unavailable"), classify.OutcomeRetryable},
		{status.Error(codes.ResourceExhausted, "exhausted"), classify.OutcomeRetryable},
		{status.Error(codes.DeadlineExceeded, "deadline"), classify.OutcomeRetryable},
		{status.Error(codes.Canceled, "canceled"), classify.OutcomeAbort},
		{status.Error(codes.InvalidArgument, "invalid"), classify.OutcomeNonRetryable},
		{errors.New("generic error"), classify.OutcomeRetryable}, // AutoClassifier fallback (AlwaysRetry)
	}

	for _, tt := range tests {
		got := c.Classify(nil, tt.err)
		if got.Kind != tt.wantKind {
			t.Errorf("Classify(%v) Kind = %v, want %v", tt.err, got.Kind, tt.wantKind)
		}
	}
}

func TestUnaryClientInterceptor_Success(t *testing.T) {
	exec := retry.NewDefaultExecutor(integration.WithClassifier())
	interceptor := integration.UnaryClientInterceptor(exec, nil)

	mockInvoker := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		return nil
	}

	err := interceptor(context.Background(), "/Service/Method", nil, nil, nil, mockInvoker)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUnaryClientInterceptor_Retry(t *testing.T) {
	exec := retry.NewDefaultExecutor(integration.WithClassifier())
	interceptor := integration.UnaryClientInterceptor(exec, nil)

	attempts := 0
	mockInvoker := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		attempts++
		if attempts < 3 {
			return status.Error(codes.Unavailable, "transient failure")
		}
		return nil
	}

	err := interceptor(context.Background(), "/Service/Method", nil, nil, nil, mockInvoker)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestUnaryClientInterceptor_NonRetryable(t *testing.T) {
	exec := retry.NewDefaultExecutor(integration.WithClassifier())
	interceptor := integration.UnaryClientInterceptor(exec, nil)

	attempts := 0
	mockInvoker := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		attempts++
		return status.Error(codes.InvalidArgument, "bad args")
	}

	err := interceptor(context.Background(), "/Service/Method", nil, nil, nil, mockInvoker)
	if err == nil {
		t.Fatal("expected error")
	}

	if attempts != 1 {
		t.Errorf("expected 1 attempt, got %d", attempts)
	}

	st, _ := status.FromError(err)
	if st.Code() != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument, got %v", st.Code())
	}
}

func TestUnaryClientInterceptor_ContextCanceled(t *testing.T) {
	exec := retry.NewDefaultExecutor(integration.WithClassifier())
	interceptor := integration.UnaryClientInterceptor(exec, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	attempts := 0
	mockInvoker := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		attempts++
		return nil
	}

	err := interceptor(ctx, "/Service/Method", nil, nil, nil, mockInvoker)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if attempts != 0 {
		t.Errorf("expected 0 attempts, got %d", attempts)
	}
}
