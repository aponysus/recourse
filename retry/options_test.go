package retry

import (
	"context"
	"testing"
	"time"

	"github.com/aponysus/recourse/classify"
	"github.com/aponysus/recourse/policy"
)

func TestNewExecutor_FunctionalOptions(t *testing.T) {
	// Test basic options
	exec := NewExecutor(
		WithMissingPolicyMode(FailureDeny),
		WithRecoverPanics(true),
		WithClock(time.Now),
	)

	if exec.missingPolicyMode != FailureDeny {
		t.Errorf("expected missingPolicyMode FailureDeny, got %v", exec.missingPolicyMode)
	}
	if !exec.recoverPanics {
		t.Error("expected recoverPanics true")
	}
	if exec.clock == nil {
		t.Error("expected clock to be set")
	}
}

func TestNewExecutor_StaticPolicies(t *testing.T) {
	keyStr := "test.static"

	exec := NewExecutor(
		WithPolicy(keyStr,
			policy.MaxAttempts(5),
			policy.Classifier("custom"),
		),
		WithMissingPolicyMode(FailureDeny),
	)

	// Try to execute with this key - should succeed because policy is known
	// We need a dummy op
	op := func(ctx context.Context) (int, error) { return 1, nil }

	// We also need "custom" classifier or it will fail if we check?
	// The policy will be resolved. Use DoValue.
	// We didn't register "custom" classifier, so resolveClassifier might fail depending on mode?
	// MissingClassifierMode default is Fallback. So it should work and use default classifier (AlwaysRetry).

	val, err := DoValue(context.Background(), exec, policy.ParseKey(keyStr), op)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != 1 {
		t.Errorf("expected 1, got %d", val)
	}

	// Verify policy content (indirectly)
	pol, err := exec.provider.GetEffectivePolicy(context.Background(), policy.ParseKey(keyStr))
	if err != nil {
		t.Fatal(err)
	}
	if pol.Retry.MaxAttempts != 5 {
		t.Errorf("expected 5 attempts, got %d", pol.Retry.MaxAttempts)
	}
}

func TestNewExecutor_StaticPolicies_WithClassifierRegistry(t *testing.T) {
	reg := classify.NewRegistry()
	reg.Register("custom", classify.AlwaysRetryOnError{})

	exec := NewExecutor(
		WithClassifiers(reg),
		WithPolicy("test.reg", policy.Classifier("custom")),
	)

	// This ensures classifier is resolved correctly using the registry we passed
	if exec.classifiers != reg {
		t.Error("expected registry to be set")
	}
}
