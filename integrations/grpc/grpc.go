package grpc

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/aponysus/recourse/classify"
	"github.com/aponysus/recourse/policy"
	"github.com/aponysus/recourse/retry"
)

// DefaultKeyFunc maps methods to policy keys.
// "/Service/Method" -> {Namespace: "Service", Name: "Method"}
func DefaultKeyFunc(method string) policy.PolicyKey {
	// method is typically "/package.Service/Method"
	method = strings.TrimPrefix(method, "/")
	parts := strings.Split(method, "/")
	if len(parts) == 2 {
		return policy.PolicyKey{Namespace: parts[0], Name: parts[1]}
	}
	return policy.PolicyKey{Name: method}
}

// UnaryClientInterceptor returns a gRPC interceptor that retries calls using the executor.
func UnaryClientInterceptor(exec *retry.Executor, keyFunc func(method string) policy.PolicyKey) grpc.UnaryClientInterceptor {
	if keyFunc == nil {
		keyFunc = DefaultKeyFunc
	}
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		key := keyFunc(method)
		op := func(ctx context.Context) error {
			return invoker(ctx, method, req, reply, cc, opts...)
		}
		// retry.Do handles the retry loop.
		return exec.Do(ctx, key, op)
	}
}

// Classifier implements classify.Classifier for gRPC status codes.
type Classifier struct{}

func (Classifier) Classify(val any, err error) classify.Outcome {
	if err == nil {
		return classify.Outcome{Kind: classify.OutcomeSuccess, Reason: "success"}
	}
	if status.Code(err) == codes.OK {
		return classify.Outcome{Kind: classify.OutcomeSuccess, Reason: "success"}
	}

	// If status.FromError(err) returns !ok, it means the error is not a gRPC Status error.
	// In this case, we delegate to the AutoClassifier to handle standard errors.
	if _, ok := status.FromError(err); !ok {
		return classify.AutoClassifier{}.Classify(val, err)
	}

	// At this point, we know it's a gRPC error (or wraps one).

	st, _ := status.FromError(err)
	code := st.Code()
	outcome := classify.Outcome{
		Kind:       classify.OutcomeNonRetryable,
		Reason:     "grpc_" + code.String(),
		Attributes: map[string]string{"grpc_code": code.String()},
	}

	switch code {
	case codes.Unavailable, codes.ResourceExhausted:
		outcome.Kind = classify.OutcomeRetryable
	case codes.DeadlineExceeded:
		outcome.Kind = classify.OutcomeRetryable
		outcome.Reason = "context_deadline_exceeded"
	case codes.Canceled:
		outcome.Kind = classify.OutcomeAbort
		outcome.Reason = "context_canceled"
	case codes.Unknown:
		// Unknown often typically maps to internal application errors, so we treat it as non-retryable
		// unless a specific policy overrides it.
	}

	return outcome
}

// WithClassifier returns an option to register the gRPC classifier as the default.
// This ensures that policies without a specific classifier name will use this logic,
// which handles gRPC codes and delegates non-gRPC errors to AutoClassifier.
func WithClassifier() retry.DefaultOption {
	return retry.WithDefaultClassifier(Classifier{})
}
