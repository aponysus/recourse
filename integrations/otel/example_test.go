package otelrecourse_test

import (
	"context"
	"errors"
	"fmt"
	"time"

	otelrecourse "github.com/aponysus/recourse/integrations/otel"
	"github.com/aponysus/recourse/policy"
	"github.com/aponysus/recourse/retry"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func ExampleNewObserver() {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	defer func() {
		_ = provider.Shutdown(context.Background())
	}()

	exec := retry.NewDefaultExecutor(
		retry.WithObserver(otelrecourse.NewObserver(provider.Tracer("example"))),
		retry.WithPolicy("svc.FetchUser",
			policy.MaxAttempts(2),
			policy.ConstantBackoff(time.Millisecond),
		),
	)

	attempts := 0
	err := exec.Do(context.Background(), policy.ParseKey("svc.FetchUser"), func(ctx context.Context) error {
		attempts++
		if attempts == 1 {
			return errors.New("temporary upstream error")
		}
		return nil
	})

	spans := recorder.Ended()
	fmt.Println(err)
	fmt.Println(len(spans))
	fmt.Println(spans[0].Name())
	fmt.Println(len(spans[0].Events()))

	// Output:
	// <nil>
	// 1
	// recourse.svc.FetchUser
	// 2
}
