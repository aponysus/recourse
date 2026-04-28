package otelrecourse

import (
	"context"
	"reflect"

	"github.com/aponysus/recourse/observe"
	"github.com/aponysus/recourse/policy"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	attrKey        = "recourse.key"
	attrPolicyID   = "recourse.policy_id"
	attrAttempts   = "recourse.attempts"
	attrAttempt    = "recourse.attempt"
	attrHedge      = "recourse.hedge"
	attrHedgeIndex = "recourse.hedge_index"
	attrOutcome    = "recourse.outcome"
	attrBudgetOK   = "recourse.budget_allowed"
	attrBudgetWhy  = "recourse.budget_reason"
	attrBackoffMS  = "recourse.backoff_ms"
	attrError      = "recourse.error"
)

var timelineAttributeAllowlist = map[string]struct{}{
	"classifier_error":         {},
	"classifier_name":          {},
	"circuit_state":            {},
	"policy_mode":              {},
	"policy_normalized":        {},
	"policy_normalized_fields": {},
	"policy_source":            {},
}

// Option configures an Observer.
type Option func(*config)

type config struct {
	recordEvents   bool
	spanPerAttempt bool
	spanKind       trace.SpanKind
}

// WithRecordEvents controls whether attempts are recorded as events on the
// operation span. It is enabled by default.
func WithRecordEvents(enabled bool) Option {
	return func(cfg *config) {
		cfg.recordEvents = enabled
	}
}

// WithSpanPerAttempt controls whether each attempt is recorded as a child span.
// It is disabled by default to keep traces compact.
func WithSpanPerAttempt(enabled bool) Option {
	return func(cfg *config) {
		cfg.spanPerAttempt = enabled
	}
}

// WithSpanKind sets the OpenTelemetry span kind for recourse operation spans.
// The default is trace.SpanKindClient.
func WithSpanKind(kind trace.SpanKind) Option {
	return func(cfg *config) {
		cfg.spanKind = kind
	}
}

// Observer emits OpenTelemetry spans for completed recourse calls.
type Observer struct {
	observe.BaseObserver
	tracer trace.Tracer
	cfg    config
}

// NewObserver creates an OpenTelemetry observer for recourse events.
func NewObserver(tracer trace.Tracer, opts ...Option) *Observer {
	cfg := config{
		recordEvents: true,
		spanKind:     trace.SpanKindClient,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return &Observer{tracer: tracer, cfg: cfg}
}

// OnSuccess records a successful recourse call.
func (o *Observer) OnSuccess(ctx context.Context, key policy.PolicyKey, tl observe.Timeline) {
	o.record(ctx, key, tl, nil)
}

// OnFailure records a failed recourse call.
func (o *Observer) OnFailure(ctx context.Context, key policy.PolicyKey, tl observe.Timeline) {
	o.record(ctx, key, tl, tl.FinalErr)
}

func (o *Observer) record(ctx context.Context, key policy.PolicyKey, tl observe.Timeline, err error) {
	if o == nil || o.tracer == nil {
		return
	}

	attrs := []attribute.KeyValue{
		attribute.String(attrKey, key.String()),
		attribute.Int(attrAttempts, len(tl.Attempts)),
	}
	if tl.PolicyID != "" {
		attrs = append(attrs, attribute.String(attrPolicyID, tl.PolicyID))
	}
	attrs = append(attrs, timelineAttributes(tl.Attributes)...)

	spanName := "recourse." + key.String()
	startOpts := []trace.SpanStartOption{
		trace.WithAttributes(attrs...),
		trace.WithSpanKind(o.cfg.spanKind),
	}
	if !tl.Start.IsZero() {
		startOpts = append(startOpts, trace.WithTimestamp(tl.Start))
	}

	opCtx, span := o.tracer.Start(ctx, spanName, startOpts...)
	for _, attempt := range tl.Attempts {
		attemptAttrs := attemptAttributes(attempt)
		if o.cfg.recordEvents {
			eventOpts := []trace.EventOption{trace.WithAttributes(attemptAttrs...)}
			if !attempt.EndTime.IsZero() {
				eventOpts = append(eventOpts, trace.WithTimestamp(attempt.EndTime))
			}
			span.AddEvent("recourse.attempt", eventOpts...)
		}
		if o.cfg.spanPerAttempt {
			o.recordAttemptSpan(opCtx, attemptAttrs, attempt)
		}
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

func (o *Observer) recordAttemptSpan(ctx context.Context, attrs []attribute.KeyValue, attempt observe.AttemptRecord) {
	opts := []trace.SpanStartOption{
		trace.WithAttributes(attrs...),
		trace.WithSpanKind(trace.SpanKindInternal),
	}
	if !attempt.StartTime.IsZero() {
		opts = append(opts, trace.WithTimestamp(attempt.StartTime))
	}

	_, span := o.tracer.Start(ctx, "recourse.attempt", opts...)
	if attempt.Err != nil {
		span.RecordError(attempt.Err)
		span.SetStatus(codes.Error, attempt.Outcome.Reason)
	} else {
		span.SetStatus(codes.Ok, attempt.Outcome.Reason)
	}

	if !attempt.EndTime.IsZero() {
		span.End(trace.WithTimestamp(attempt.EndTime))
		return
	}
	span.End()
}

func timelineAttributes(attrs map[string]string) []attribute.KeyValue {
	if len(attrs) == 0 {
		return nil
	}

	out := make([]attribute.KeyValue, 0, len(attrs))
	for key, value := range attrs {
		if value == "" {
			continue
		}
		if _, ok := timelineAttributeAllowlist[key]; !ok {
			continue
		}
		out = append(out, attribute.String("recourse."+key, value))
	}
	return out
}

func attemptAttributes(attempt observe.AttemptRecord) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.Int(attrAttempt, attempt.Attempt),
		attribute.Bool(attrHedge, attempt.IsHedge),
		attribute.Bool(attrBudgetOK, attempt.BudgetAllowed),
	}
	if attempt.IsHedge {
		attrs = append(attrs, attribute.Int(attrHedgeIndex, attempt.HedgeIndex))
	}
	if attempt.Outcome.Reason != "" {
		attrs = append(attrs, attribute.String(attrOutcome, attempt.Outcome.Reason))
	}
	if attempt.BudgetReason != "" {
		attrs = append(attrs, attribute.String(attrBudgetWhy, attempt.BudgetReason))
	}
	if attempt.Backoff > 0 {
		attrs = append(attrs, attribute.Int64(attrBackoffMS, attempt.Backoff.Milliseconds()))
	}
	if attempt.Err != nil {
		attrs = append(attrs, attribute.Bool(attrError, true))
		attrs = append(attrs, attribute.String(attrError+".type", errorType(attempt.Err)))
	}
	return attrs
}

func errorType(err error) string {
	if err == nil {
		return ""
	}
	t := reflect.TypeOf(err)
	if t == nil {
		return "error"
	}
	return t.String()
}
