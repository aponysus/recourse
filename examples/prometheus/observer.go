package main

import (
	"context"

	"github.com/aponysus/recourse/observe"
	"github.com/aponysus/recourse/policy"
	"github.com/prometheus/client_golang/prometheus"
)

type PrometheusObserver struct {
	observe.BaseObserver

	calls          *prometheus.CounterVec
	callLatency    *prometheus.HistogramVec
	attempts       *prometheus.CounterVec
	attemptLatency *prometheus.HistogramVec
	budgets        *prometheus.CounterVec
}

func NewPrometheusObserver(reg prometheus.Registerer) *PrometheusObserver {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}

	obs := &PrometheusObserver{
		calls: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "recourse_calls_total",
				Help: "Total number of recourse calls.",
			},
			[]string{"namespace", "name", "result"},
		),
		callLatency: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "recourse_call_latency_seconds",
				Help:    "End-to-end latency per recourse call.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"namespace", "name", "result"},
		),
		attempts: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "recourse_attempts_total",
				Help: "Total number of recourse attempts.",
			},
			[]string{"namespace", "name", "outcome", "hedge"},
		),
		attemptLatency: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "recourse_attempt_latency_seconds",
				Help:    "Latency per recourse attempt.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"namespace", "name", "hedge"},
		),
		budgets: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "recourse_budget_decisions_total",
				Help: "Budget allow/deny decisions.",
			},
			[]string{"namespace", "name", "allowed", "reason"},
		),
	}

	reg.MustRegister(obs.calls, obs.callLatency, obs.attempts, obs.attemptLatency, obs.budgets)
	return obs
}

func (o *PrometheusObserver) OnAttempt(ctx context.Context, key policy.PolicyKey, rec observe.AttemptRecord) {
	hedge := boolLabel(rec.IsHedge)
	outcome := rec.Outcome.Reason
	if outcome == "" {
		outcome = "unknown"
	}
	if o.attempts != nil {
		o.attempts.WithLabelValues(key.Namespace, key.Name, outcome, hedge).Inc()
	}
	if o.attemptLatency != nil && !rec.StartTime.IsZero() && !rec.EndTime.IsZero() {
		o.attemptLatency.WithLabelValues(key.Namespace, key.Name, hedge).Observe(rec.EndTime.Sub(rec.StartTime).Seconds())
	}
}

func (o *PrometheusObserver) OnBudgetDecision(ctx context.Context, ev observe.BudgetDecisionEvent) {
	if o.budgets == nil {
		return
	}
	reason := ev.Reason
	if reason == "" {
		reason = "unknown"
	}
	o.budgets.WithLabelValues(ev.Key.Namespace, ev.Key.Name, boolLabel(ev.Allowed), reason).Inc()
}

func (o *PrometheusObserver) OnSuccess(ctx context.Context, key policy.PolicyKey, tl observe.Timeline) {
	o.observeCall(key, tl, "success")
}

func (o *PrometheusObserver) OnFailure(ctx context.Context, key policy.PolicyKey, tl observe.Timeline) {
	o.observeCall(key, tl, "failure")
}

func (o *PrometheusObserver) observeCall(key policy.PolicyKey, tl observe.Timeline, result string) {
	if o.calls != nil {
		o.calls.WithLabelValues(key.Namespace, key.Name, result).Inc()
	}
	if o.callLatency != nil && !tl.Start.IsZero() && !tl.End.IsZero() {
		o.callLatency.WithLabelValues(key.Namespace, key.Name, result).Observe(tl.End.Sub(tl.Start).Seconds())
	}
}

func boolLabel(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
