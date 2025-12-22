package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	integration "github.com/aponysus/recourse/integrations/http"
	"github.com/aponysus/recourse/policy"
	"github.com/aponysus/recourse/retry"
)

func main() {
	// 1. Create Default Executor
	exec := retry.NewDefaultExecutor()

	client := &http.Client{Timeout: 2 * time.Second}

	// We target a URL that returns 503 (httpbin) to demonstrate retries.
	req, _ := http.NewRequest("GET", "https://httpbin.org/status/503", nil)

	fmt.Println("Sending request (expecting retries)...")
	start := time.Now()
	resp, tl, err := integration.DoHTTP(context.Background(), exec, policy.PolicyKey{Name: "example"}, client, req)

	fmt.Printf("Duration: %v\n", time.Since(start))
	if tl.Attempts != nil {
		fmt.Printf("Attempts: %d\n", len(tl.Attempts))
		for _, a := range tl.Attempts {
			fmt.Printf(" - Attempt %d: %v (Duration: %v)\n", a.Attempt, a.Outcome.Reason, a.EndTime.Sub(a.StartTime))
		}
	}

	if err != nil {
		fmt.Printf("Request failed as expected: %v\n", err)
	} else {
		fmt.Printf("Success: %s\n", resp.Status)
	}
}
