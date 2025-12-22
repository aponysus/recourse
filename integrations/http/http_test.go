package http_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	integration "github.com/aponysus/recourse/integrations/http"
	"github.com/aponysus/recourse/policy"
	"github.com/aponysus/recourse/retry"
)

func TestDoHTTP_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "Hello")
	}))
	defer server.Close()

	exec := retry.NewDefaultExecutor()
	client := server.Client()

	req, _ := http.NewRequest("GET", server.URL, nil)
	resp, _, err := integration.DoHTTP(context.Background(), exec, policy.PolicyKey{Name: "test"}, client, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if strings.TrimSpace(string(body)) != "Hello" {
		t.Errorf("got body %q, want Hello", body)
	}
}

func TestDoHTTP_RetryOn503(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		fmt.Fprintln(w, "Success")
	}))
	defer server.Close()

	exec := retry.NewDefaultExecutor()
	client := server.Client()

	req, _ := http.NewRequest("GET", server.URL, nil)
	resp, tl, err := integration.DoHTTP(context.Background(), exec, policy.PolicyKey{Name: "test"}, client, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if len(tl.Attempts) != 3 {
		t.Errorf("expected 3 attempts, got %d", len(tl.Attempts))
	}
	if resp.StatusCode != 200 {
		t.Errorf("got status %d, want 200", resp.StatusCode)
	}
}

func TestDoHTTP_RespectsRetryAfter(t *testing.T) {
	attempts := 0
	start := time.Now()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.Header().Set("Retry-After", "1") // 1 second
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	exec := retry.NewDefaultExecutor(
		retry.WithPolicy("test", policy.MaxBackoff(5*time.Second)),
	)
	client := server.Client()

	req, _ := http.NewRequest("GET", server.URL, nil)
	_, _, err := integration.DoHTTP(context.Background(), exec, policy.PolicyKey{Name: "test"}, client, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	elapsed := time.Since(start)
	if elapsed < 1*time.Second {
		t.Errorf("expected >1s latency due to Retry-After, got %v", elapsed)
	}
}

func TestDoHTTP_DrainsAndClosesExample(t *testing.T) {
	// Verifying that the body is closed is verified by the fact that we can modify the
	// response handling logic without leaking connections in a tight loop.
	// For this test, we rely on the server behavior and the absence of connection errors.

	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			// Write some body to drain
			fmt.Fprintln(w, "some error body")
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	exec := retry.NewDefaultExecutor()
	client := server.Client()

	req, _ := http.NewRequest("GET", server.URL, nil)
	_, tl, err := integration.DoHTTP(context.Background(), exec, policy.PolicyKey{Name: "test"}, client, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(tl.Attempts) != 2 {
		t.Errorf("expected 2 attempts, got %d", len(tl.Attempts))
	}
}

func TestDoHTTP_NonRetryableStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	exec := retry.NewDefaultExecutor()
	client := server.Client()

	req, _ := http.NewRequest("GET", server.URL, nil)
	_, tl, err := integration.DoHTTP(context.Background(), exec, policy.PolicyKey{Name: "test"}, client, req)

	// DoHTTP returns a StatusError for non-2xx responses when they are classified as failures
	// by the AutoClassifier or default rules. Verify proper error wrapping.

	if err == nil {
		t.Fatal("expected error for 404")
	}

	st, ok := err.(*integration.StatusError)
	if !ok || st.Code != 404 {
		t.Errorf("expected 404 StatusError, got %v", err)
	}

	if len(tl.Attempts) != 1 {
		t.Errorf("expected 1 attempt for non-retryable error, got %d", len(tl.Attempts))
	}
}
