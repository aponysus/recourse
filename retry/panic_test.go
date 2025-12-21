package retry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aponysus/recourse/classify"
	"github.com/aponysus/recourse/controlplane"
	"github.com/aponysus/recourse/policy"
)

type panicProvider struct{}

func (panicProvider) GetEffectivePolicy(_ context.Context, _ policy.PolicyKey) (policy.EffectivePolicy, error) {
	panic("provider panic")
}

func TestExecutor_RecoverPanics_PolicyProvider(t *testing.T) {
	key := policy.PolicyKey{Name: "panic-policy"}
	exec := NewExecutor(ExecutorOptions{
		Provider:          panicProvider{},
		RecoverPanics:     true,
		MissingPolicyMode: FailureDeny,
	})

	_, err := DoValue[int](context.Background(), exec, key, func(ctx context.Context) (int, error) {
		return 1, nil
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Verify error chain
	var noPol *NoPolicyError
	if !errors.As(err, &noPol) {
		t.Fatalf("expected NoPolicyError, got %T: %v", err, err)
	}

	var panicErr *PanicError
	if !errors.As(err, &panicErr) {
		t.Fatalf("expected PanicError in chain, got %v", err)
	}
	if panicErr.Component != "policy_provider" {
		t.Errorf("expected component policy_provider, got %s", panicErr.Component)
	}
}

type panicClassifierRegistry struct{}

func (panicClassifierRegistry) Get(name string) (classify.Classifier, bool) {
	panic("registry panic")
}

// We can't easily mock Registry since it's a struct, not interface.
// But we can rely on integration test or modify the ExecutorOptions to specific usage?
// Ah, ExecutorOptions takes *classify.Registry. We can't mock it easily unless we hack internal state
// or if we rely on a classifier that panics?
// Wait, resolveClassifier calls exec.classifiers.Get(name).
// classify.Registry is a struct with a map and mutex. It shouldn't panic unless we pass nil or something?
// Actually if `Get` method panics... but it's a concrete type method.
// So we can't mock `Get` unless we change `Executor` to use an interface for registry?
// Or we just test `classify` panic, which calls `Classifier.Classify`.

type panicClassifier struct{}

func (panicClassifier) Classify(value any, err error) classify.Outcome {
	panic("classifier panic")
}

func TestExecutor_RecoverPanics_Classifier(t *testing.T) {
	key := policy.PolicyKey{Name: "panic-classifier"}

	// Create a real registry and register a panicking classifier
	reg := classify.NewRegistry()
	reg.Register("panic-cls", panicClassifier{})

	exec := NewExecutor(ExecutorOptions{
		Provider: &controlplane.StaticProvider{
			Policies: map[policy.PolicyKey]policy.EffectivePolicy{
				key: {
					Key: key,
					Retry: policy.RetryPolicy{
						MaxAttempts:    2,
						ClassifierName: "panic-cls",
					},
				},
			},
		},
		Classifiers:   reg,
		RecoverPanics: true,
	})
	exec.sleep = func(context.Context, time.Duration) error { return nil }

	_, err := DoValue[int](context.Background(), exec, key, func(ctx context.Context) (int, error) {
		return 0, nil
	})

	if err == nil {
		t.Fatal("expected error")
	}
	// Now we expect a PanicError in the chain
	var panicErr *PanicError
	if !errors.As(err, &panicErr) {
		t.Fatalf("expected PanicError in chain, got %T: %v", err, err)
	}
	if panicErr.Component != "classifier" {
		t.Errorf("expected component classifier, got %s", panicErr.Component)
	}
}
