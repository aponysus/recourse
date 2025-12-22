package main

import (
	"context"
	"fmt"
	"log"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	integration "github.com/aponysus/recourse/integrations/grpc"
	"github.com/aponysus/recourse/retry"
)

func main() {
	// 1. Setup Executor with gRPC capabilities
	exec := retry.NewDefaultExecutor(integration.WithClassifier())

	// 2. Setup gRPC connection with Interceptor
	interceptor := integration.UnaryClientInterceptor(exec, nil) // use default key func

	// Note: grpc.Dial is deprecated in newer versions, assuming NewClient.
	// If standard library is used, we use NewClient.
	conn, err := grpc.NewClient("localhost:50051",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(interceptor),
	)
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()

	fmt.Println("gRPC client initialized. (This example requires a running server to execute real calls).")

	// 3. Simulate usage
	// To verify the retry logic without a server, we can call the interceptor directly
	// with a mock invoker.

	fmt.Println("Simulating call to /Greeter/SayHello...")

	attempts := 0
	mockInvoker := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		attempts++
		fmt.Printf(" - Attempt %d...", attempts)
		if attempts < 3 {
			fmt.Println(" Failed (Unavailable)")
			return status.Error(codes.Unavailable, "transient failure")
		}
		fmt.Println(" Success!")
		return nil
	}

	err = interceptor(context.Background(), "/Greeter/SayHello", "req", "resp", conn, mockInvoker)
	if err != nil {
		fmt.Printf("Final result: Failed (%v)\n", err)
	} else {
		fmt.Println("Final result: Success")
	}
}
