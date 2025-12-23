package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/aponysus/recourse/budget"
	"github.com/aponysus/recourse/policy"
	"github.com/aponysus/recourse/retry"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	reg := prometheus.NewRegistry()
	obs := NewPrometheusObserver(reg)

	budgets := budget.NewRegistry()
	budgets.Register("example", budget.NewTokenBucketBudget(5, 2))

	exec := retry.NewExecutor(
		retry.WithObserver(obs),
		retry.WithBudgetRegistry(budgets),
		retry.WithPolicy("example.prometheus",
			policy.MaxAttempts(3),
			policy.Budget("example"),
		),
	)

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	go func() {
		log.Println("metrics available at http://localhost:2112/metrics")
		if err := http.ListenAndServe(":2112", mux); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("metrics server: %v", err)
		}
	}()

	ctx := context.Background()
	key := policy.ParseKey("example.prometheus")
	log.Println("issuing sample calls (one retry per call)")

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		var attempt int32
		op := func(context.Context) (string, error) {
			if atomic.AddInt32(&attempt, 1) == 1 {
				return "", errors.New("transient failure")
			}
			return "ok", nil
		}
		if _, err := retry.DoValue(ctx, exec, key, op); err != nil {
			log.Printf("call failed: %v", err)
		}
	}
}
