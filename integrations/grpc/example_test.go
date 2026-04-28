package grpc_test

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	integration "github.com/aponysus/recourse/integrations/grpc"
	"github.com/aponysus/recourse/policy"
	"github.com/aponysus/recourse/retry"
)

func ExampleDefaultKeyFunc() {
	key := integration.DefaultKeyFunc("/payments.Service/Charge")
	fmt.Println(key.String())

	// Output:
	// payments.Service.Charge
}

func ExampleUnaryClientInterceptor() {
	exec := retry.NewDefaultExecutor(
		integration.WithClassifier(),
		retry.WithPolicy("payments.Service.Charge",
			policy.MaxAttempts(3),
			policy.ConstantBackoff(time.Millisecond),
		),
	)
	interceptor := integration.UnaryClientInterceptor(exec, nil)

	attempts := 0
	invoker := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		attempts++
		if attempts == 1 {
			return status.Error(codes.Unavailable, "temporary outage")
		}
		return nil
	}

	err := interceptor(context.Background(), "/payments.Service/Charge", nil, nil, nil, invoker)
	fmt.Println(attempts, err)

	// Output:
	// 2 <nil>
}
