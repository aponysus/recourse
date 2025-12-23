package main

import (
	"context"
	"testing"
	"time"

	"github.com/aponysus/recourse/classify"
	"github.com/aponysus/recourse/observe"
	"github.com/aponysus/recourse/policy"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestPrometheusObserver_RecordsMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	obs := NewPrometheusObserver(reg)

	key := policy.PolicyKey{Namespace: "svc", Name: "method"}
	start := time.Unix(0, 0)
	attempt := observe.AttemptRecord{
		Attempt:   1,
		IsHedge:   false,
		Outcome:   classify.Outcome{Reason: "retryable"},
		StartTime: start,
		EndTime:   start.Add(10 * time.Millisecond),
	}

	obs.OnAttempt(context.Background(), key, attempt)
	obs.OnBudgetDecision(context.Background(), observe.BudgetDecisionEvent{
		Key:     key,
		Allowed: true,
		Reason:  "allowed",
	})
	obs.OnSuccess(context.Background(), key, observe.Timeline{
		Key:      key,
		Start:    start,
		End:      start.Add(20 * time.Millisecond),
		Attempts: []observe.AttemptRecord{attempt},
	})

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}

	if got := counterValue(t, mfs, "recourse_calls_total", map[string]string{
		"namespace": "svc",
		"name":      "method",
		"result":    "success",
	}); got != 1 {
		t.Fatalf("recourse_calls_total expected 1, got %v", got)
	}

	if got := counterValue(t, mfs, "recourse_attempts_total", map[string]string{
		"namespace": "svc",
		"name":      "method",
		"outcome":   "retryable",
		"hedge":     "false",
	}); got != 1 {
		t.Fatalf("recourse_attempts_total expected 1, got %v", got)
	}

	if got := counterValue(t, mfs, "recourse_budget_decisions_total", map[string]string{
		"namespace": "svc",
		"name":      "method",
		"allowed":   "true",
		"reason":    "allowed",
	}); got != 1 {
		t.Fatalf("recourse_budget_decisions_total expected 1, got %v", got)
	}

	if got := histogramCount(t, mfs, "recourse_call_latency_seconds", map[string]string{
		"namespace": "svc",
		"name":      "method",
		"result":    "success",
	}); got != 1 {
		t.Fatalf("recourse_call_latency_seconds count expected 1, got %v", got)
	}

	if got := histogramCount(t, mfs, "recourse_attempt_latency_seconds", map[string]string{
		"namespace": "svc",
		"name":      "method",
		"hedge":     "false",
	}); got != 1 {
		t.Fatalf("recourse_attempt_latency_seconds count expected 1, got %v", got)
	}
}

func counterValue(t *testing.T, mfs []*dto.MetricFamily, name string, labels map[string]string) float64 {
	metric := findMetric(t, mfs, name, labels)
	if metric == nil {
		t.Fatalf("metric %s with labels not found", name)
	}
	if metric.GetCounter() == nil {
		t.Fatalf("metric %s is not a counter", name)
	}
	return metric.GetCounter().GetValue()
}

func histogramCount(t *testing.T, mfs []*dto.MetricFamily, name string, labels map[string]string) uint64 {
	metric := findMetric(t, mfs, name, labels)
	if metric == nil {
		t.Fatalf("metric %s with labels not found", name)
	}
	if metric.GetHistogram() == nil {
		t.Fatalf("metric %s is not a histogram", name)
	}
	return metric.GetHistogram().GetSampleCount()
}

func findMetric(t *testing.T, mfs []*dto.MetricFamily, name string, labels map[string]string) *dto.Metric {
	t.Helper()
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, metric := range mf.GetMetric() {
			if labelsMatch(metric, labels) {
				return metric
			}
		}
	}
	return nil
}

func labelsMatch(metric *dto.Metric, labels map[string]string) bool {
	if len(metric.GetLabel()) != len(labels) {
		return false
	}
	for _, label := range metric.GetLabel() {
		value, ok := labels[label.GetName()]
		if !ok {
			return false
		}
		if label.GetValue() != value {
			return false
		}
	}
	return true
}
