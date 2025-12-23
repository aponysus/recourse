package main

import (
	"context"

	"github.com/aponysus/recourse/observe"
	"github.com/aponysus/recourse/policy"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type OTelObserver struct {
	observe.BaseObserver
	tracer trace.Tracer
}

func NewOTelObserver(tracer trace.Tracer) *OTelObserver {
	return &OTelObserver{tracer: tracer}
}

func (o *OTelObserver) OnSuccess(ctx context.Context, key policy.PolicyKey, tl observe.Timeline) {
	o.record(ctx, key, tl, nil)
}

func (o *OTelObserver) OnFailure(ctx context.Context, key policy.PolicyKey, tl observe.Timeline) {
	o.record(ctx, key, tl, tl.FinalErr)
}

func (o *OTelObserver) record(ctx context.Context, key policy.PolicyKey, tl observe.Timeline, err error) {
	if o == nil || o.tracer == nil {
		return
	}

	spanName := "recourse." + key.String()
	startOpts := []trace.SpanStartOption{trace.WithSpanKind(trace.SpanKindClient)}
	if !tl.Start.IsZero() {
		startOpts = append(startOpts, trace.WithTimestamp(tl.Start))
	}
	ctx, span := o.tracer.Start(ctx, spanName, startOpts...)
	span.SetAttributes(
		attribute.String("recourse.key", key.String()),
		attribute.Int("recourse.attempts", len(tl.Attempts)),
	)

	for _, attempt := range tl.Attempts {
		attrs := []attribute.KeyValue{
			attribute.Int("recourse.attempt", attempt.Attempt),
			attribute.Bool("recourse.hedge", attempt.IsHedge),
		}
		if attempt.Outcome.Reason != "" {
			attrs = append(attrs, attribute.String("recourse.outcome", attempt.Outcome.Reason))
		}
		if attempt.Err != nil {
			attrs = append(attrs, attribute.String("recourse.error", attempt.Err.Error()))
		}
		eventOpts := []trace.EventOption{trace.WithAttributes(attrs...)}
		if !attempt.EndTime.IsZero() {
			eventOpts = append(eventOpts, trace.WithTimestamp(attempt.EndTime))
		}
		span.AddEvent("attempt", eventOpts...)
	}

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else {
		span.SetStatus(codes.Ok, "success")
	}

	if !tl.End.IsZero() {
		span.End(trace.WithTimestamp(tl.End))
		return
	}
	span.End()
}
