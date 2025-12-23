package main

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/aponysus/recourse/policy"
	"github.com/aponysus/recourse/retry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func main() {
	ctx := context.Background()

	provider, err := newTracerProvider(ctx)
	if err != nil {
		log.Fatalf("init tracer: %v", err)
	}
	defer func() {
		_ = provider.Shutdown(ctx)
	}()
	otel.SetTracerProvider(provider)

	observer := NewOTelObserver(otel.Tracer("recourse-otel-example"))
	exec := retry.NewExecutor(
		retry.WithObserver(observer),
		retry.WithPolicy("example.otel", policy.MaxAttempts(2)),
	)

	key := policy.ParseKey("example.otel")
	attempt := 0
	_, err = retry.DoValue(ctx, exec, key, func(context.Context) (string, error) {
		attempt++
		if attempt == 1 {
			return "", errors.New("transient failure")
		}
		return "ok", nil
	})
	if err != nil {
		log.Printf("call failed: %v", err)
	}

	fmt.Println("otel spans emitted to stdout")
}

func newTracerProvider(ctx context.Context) (*sdktrace.TracerProvider, error) {
	exporter, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
	if err != nil {
		return nil, err
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			attribute.String("service.name", "recourse-otel-example"),
		),
	)
	if err != nil {
		return nil, err
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	return provider, nil
}
