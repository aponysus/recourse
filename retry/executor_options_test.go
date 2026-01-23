package retry

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aponysus/recourse/budget"
	"github.com/aponysus/recourse/circuit"
	"github.com/aponysus/recourse/classify"
	"github.com/aponysus/recourse/controlplane"
	"github.com/aponysus/recourse/hedge"
	"github.com/aponysus/recourse/observe"
	"github.com/aponysus/recourse/policy"
)

type optionObserver struct{}

func (optionObserver) OnStart(context.Context, policy.PolicyKey, policy.EffectivePolicy)     {}
func (optionObserver) OnAttempt(context.Context, policy.PolicyKey, observe.AttemptRecord)    {}
func (optionObserver) OnHedgeSpawn(context.Context, policy.PolicyKey, observe.AttemptRecord) {}
func (optionObserver) OnHedgeCancel(context.Context, policy.PolicyKey, observe.AttemptRecord, string) {
}
func (optionObserver) OnBudgetDecision(context.Context, observe.BudgetDecisionEvent) {}
func (optionObserver) OnSuccess(context.Context, policy.PolicyKey, observe.Timeline) {}
func (optionObserver) OnFailure(context.Context, policy.PolicyKey, observe.Timeline) {}

type testClassifier struct{}

func (testClassifier) Classify(any, error) classify.Outcome {
	return classify.Outcome{Kind: classify.OutcomeSuccess}
}

func TestNewExecutorFromOptions_Defaults(t *testing.T) {
	exec := NewExecutorFromOptions(ExecutorOptions{})
	if exec.provider == nil || exec.observer == nil || exec.clock == nil || exec.sleep == nil {
		t.Fatal("expected defaults to be set")
	}
	if exec.classifiers == nil || exec.defaultClassifier == nil || exec.triggers == nil || exec.circuits == nil {
		t.Fatal("expected registries to be initialized")
	}
}

func TestNewExecutor_OptionWiring(t *testing.T) {
	obs := &optionObserver{}
	classReg := classify.NewRegistry()
	budgetReg := budget.NewRegistry()
	triggerReg := hedge.NewRegistry()
	circuitReg := circuit.NewRegistry()

	exec := NewExecutor(
		WithObserver(obs),
		WithClassifiers(classReg),
		WithDefaultClassifier(testClassifier{}),
		WithClassifier("custom", testClassifier{}),
		WithBudgetRegistry(budgetReg),
		WithHedgeTriggerRegistry(triggerReg),
		WithCircuitRegistry(circuitReg),
		WithMissingClassifierMode(FailureAllow),
		WithMissingBudgetMode(FailureAllowUnsafe),
	)

	if exec.observer != obs {
		t.Fatalf("observer not set")
	}
	if exec.classifiers != classReg || exec.budgets != budgetReg || exec.triggers != triggerReg || exec.circuits != circuitReg {
		t.Fatalf("expected registries to be wired")
	}
	if exec.missingClassifierMode != FailureAllow || exec.missingBudgetMode != FailureAllowUnsafe {
		t.Fatalf("unexpected missing modes: %v/%v", exec.missingClassifierMode, exec.missingBudgetMode)
	}
	if _, ok := exec.defaultClassifier.(testClassifier); !ok {
		t.Fatalf("expected default classifier to be testClassifier, got %T", exec.defaultClassifier)
	}
	if _, ok := exec.classifiers.Get("custom"); !ok {
		t.Fatalf("expected custom classifier to be registered")
	}
}

func TestWithClassifier_CreatesRegistry(t *testing.T) {
	exec := NewExecutor(WithClassifier("custom", testClassifier{}))
	if exec.classifiers == nil {
		t.Fatal("expected classifiers registry to be initialized")
	}
	if _, ok := exec.classifiers.Get("custom"); !ok {
		t.Fatalf("expected custom classifier to be registered")
	}
}

func TestNewExecutor_WithPolicyKeyCreatesStaticProvider(t *testing.T) {
	key := policy.PolicyKey{Namespace: "svc", Name: "op"}
	exec := NewExecutor(WithPolicyKey(key, policy.MaxAttempts(2)))

	sp, ok := exec.provider.(*controlplane.StaticProvider)
	if !ok {
		t.Fatalf("expected StaticProvider, got %T", exec.provider)
	}
	pol, ok := sp.Policies[key]
	if !ok {
		t.Fatalf("expected policy for key")
	}
	if pol.Retry.MaxAttempts != 2 {
		t.Fatalf("maxAttempts=%d, want 2", pol.Retry.MaxAttempts)
	}
}

func TestExecutorErrorTypes(t *testing.T) {
	key := policy.PolicyKey{Name: "op"}
	pe := &PanicError{Component: "executor", Key: key, Value: "boom"}
	if !strings.Contains(pe.Error(), "panic") {
		t.Fatalf("unexpected panic error: %q", pe.Error())
	}

	root := errors.New("missing")
	npe := &NoPolicyError{Key: key, Err: root}
	if !errors.Is(npe, ErrNoPolicy) {
		t.Fatalf("expected ErrNoPolicy to match")
	}
	if !errors.Is(npe, root) || npe.Unwrap() != root {
		t.Fatalf("expected unwrap to return root")
	}

	nce := &NoClassifierError{Name: "custom"}
	if !strings.Contains(nce.Error(), "custom") {
		t.Fatalf("unexpected classifier error: %q", nce.Error())
	}

	ce := CircuitOpenError{State: circuit.StateOpen, Reason: circuit.ReasonCircuitOpen}
	if !strings.Contains(ce.Error(), "circuit") || !strings.Contains(ce.Error(), "open") {
		t.Fatalf("unexpected circuit error: %q", ce.Error())
	}
}
