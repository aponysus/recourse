package retry

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/aponysus/recourse/classify"
	"github.com/aponysus/recourse/controlplane"
	"github.com/aponysus/recourse/policy"
)

func TestFailureModeString(t *testing.T) {
	cases := []struct {
		mode FailureMode
		want string
	}{
		{mode: FailureDeny, want: "deny"},
		{mode: FailureAllow, want: "allow"},
		{mode: FailureFallback, want: "fallback"},
		{mode: FailureAllowUnsafe, want: "allow_unsafe"},
		{mode: FailureModeUnknown, want: "unknown"},
	}

	for _, tc := range cases {
		if got := failureModeString(tc.mode); got != tc.want {
			t.Fatalf("mode %v: got %q, want %q", tc.mode, got, tc.want)
		}
	}
}

func TestPolicyErrorKind(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{err: controlplane.ErrPolicyNotFound, want: "policy_not_found"},
		{err: controlplane.ErrProviderUnavailable, want: "provider_unavailable"},
		{err: controlplane.ErrPolicyFetchFailed, want: "policy_fetch_failed"},
		{err: errors.New("other"), want: "unknown_error"},
	}

	for _, tc := range cases {
		if got := policyErrorKind(tc.err); got != tc.want {
			t.Fatalf("err=%v: got %q, want %q", tc.err, got, tc.want)
		}
	}
}

func TestHandleMissingBudget(t *testing.T) {
	exec := &Executor{missingBudgetMode: FailureAllow}
	if d, ok := exec.handleMissingBudget(context.Background(), "missing"); !ok || !d.Allowed {
		t.Fatalf("expected allow for FailureAllow")
	}

	exec.missingBudgetMode = FailureAllowUnsafe
	if d, ok := exec.handleMissingBudget(context.Background(), "missing"); !ok || !d.Allowed {
		t.Fatalf("expected allow for FailureAllowUnsafe")
	}

	exec.missingBudgetMode = FailureDeny
	if d, ok := exec.handleMissingBudget(context.Background(), "missing"); ok || d.Allowed {
		t.Fatalf("expected deny for FailureDeny")
	}
}

func TestSleepWithContext(t *testing.T) {
	if err := sleepWithContext(context.Background(), 0); err != nil {
		t.Fatalf("expected nil for zero duration, got %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepWithContext(ctx, 10*time.Millisecond); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}

func TestNoPolicyError_Error(t *testing.T) {
	err := &NoPolicyError{
		Key: policy.PolicyKey{Namespace: "svc", Name: "op"},
		Err: errors.New("missing"),
	}
	msg := err.Error()
	if !strings.Contains(msg, "svc.op") || !strings.Contains(msg, "missing") {
		t.Fatalf("unexpected error string: %q", msg)
	}
}

func TestTerminalError_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := terminalError(ctx, context.Canceled, classify.Outcome{Reason: "ignored"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}

func TestTerminalError_Fallbacks(t *testing.T) {
	root := errors.New("boom")
	if err := terminalError(context.Background(), root, classify.Outcome{}); err != root {
		t.Fatalf("expected root error, got %v", err)
	}

	err := terminalError(context.Background(), nil, classify.Outcome{Reason: "budget_denied"})
	if err == nil || err.Error() != "recourse: budget_denied" {
		t.Fatalf("unexpected error: %v", err)
	}

	err = terminalError(context.Background(), nil, classify.Outcome{})
	if err == nil || err.Error() != "recourse: operation failed" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNextBackoff(t *testing.T) {
	if got := nextBackoff(-1*time.Second, 2, 0); got != 0 {
		t.Fatalf("negative next backoff = %v, want 0", got)
	}
	if got := nextBackoff(100*time.Millisecond, 2, 150*time.Millisecond); got != 150*time.Millisecond {
		t.Fatalf("capped backoff = %v, want 150ms", got)
	}
	if got := nextBackoff(100*time.Millisecond, 2, 0); got != 200*time.Millisecond {
		t.Fatalf("next backoff = %v, want 200ms", got)
	}
}

func TestApplyJitterRanges(t *testing.T) {
	backoff := 100 * time.Millisecond

	if got := applyJitter(backoff, policy.JitterNone); got != backoff {
		t.Fatalf("no jitter = %v, want %v", got, backoff)
	}
	if got := applyJitter(backoff, ""); got != backoff {
		t.Fatalf("empty jitter = %v, want %v", got, backoff)
	}

	got := applyJitter(backoff, policy.JitterFull)
	if got < 0 || got > backoff {
		t.Fatalf("full jitter out of range: %v", got)
	}

	got = applyJitter(backoff, policy.JitterEqual)
	if got < backoff/2 || got > backoff {
		t.Fatalf("equal jitter out of range: %v", got)
	}

	if got = applyJitter(backoff, policy.JitterKind("odd")); got != backoff {
		t.Fatalf("unknown jitter = %v, want %v", got, backoff)
	}
}

func TestCapBackoff(t *testing.T) {
	if got := capBackoff(-1*time.Second, 0); got != 0 {
		t.Fatalf("negative backoff = %v, want 0", got)
	}
	if got := capBackoff(2*time.Second, 500*time.Millisecond); got != 500*time.Millisecond {
		t.Fatalf("capped backoff = %v, want 500ms", got)
	}
	if got := capBackoff(200*time.Millisecond, 0); got != 200*time.Millisecond {
		t.Fatalf("uncapped backoff = %v, want 200ms", got)
	}
}

func TestComputeSleep(t *testing.T) {
	pol := policy.RetryPolicy{MaxBackoff: 200 * time.Millisecond, Jitter: policy.JitterNone}

	out := classify.Outcome{BackoffOverride: 500 * time.Millisecond}
	if got := computeSleep(100*time.Millisecond, pol, out); got != 200*time.Millisecond {
		t.Fatalf("override capped = %v, want 200ms", got)
	}

	out.BackoffOverride = 50 * time.Millisecond
	if got := computeSleep(100*time.Millisecond, pol, out); got != 50*time.Millisecond {
		t.Fatalf("override = %v, want 50ms", got)
	}

	out.BackoffOverride = 0
	if got := computeSleep(300*time.Millisecond, pol, out); got != 200*time.Millisecond {
		t.Fatalf("backoff capped = %v, want 200ms", got)
	}
}

type classifierUnknownOutcome struct{}

func (classifierUnknownOutcome) Classify(any, error) classify.Outcome {
	return classify.Outcome{Kind: classify.OutcomeUnknown}
}

type classifierRetryable struct{}

func (classifierRetryable) Classify(any, error) classify.Outcome {
	return classify.Outcome{Kind: classify.OutcomeRetryable}
}

type classifierPanics struct{}

func (classifierPanics) Classify(any, error) classify.Outcome {
	panic("boom")
}

type classifierSuccess struct{}

func (classifierSuccess) Classify(any, error) classify.Outcome {
	return classify.Outcome{Kind: classify.OutcomeSuccess}
}

type classifierUnknownWithReason struct{}

func (classifierUnknownWithReason) Classify(any, error) classify.Outcome {
	return classify.Outcome{Kind: classify.OutcomeUnknown, Reason: "custom_reason"}
}

func TestClassifyWithRecovery(t *testing.T) {
	key := policy.PolicyKey{Name: "op"}

	out, panicErr := classifyWithRecovery(false, classifierUnknownOutcome{}, nil, errors.New("err"), key)
	if panicErr != nil {
		t.Fatalf("unexpected panic error: %v", panicErr)
	}
	if out.Kind != classify.OutcomeAbort || out.Reason != "unknown_outcome" {
		t.Fatalf("out=%+v, want abort/unknown_outcome", out)
	}

	out, panicErr = classifyWithRecovery(false, classifierRetryable{}, nil, errors.New("err"), key)
	if panicErr != nil {
		t.Fatalf("unexpected panic error: %v", panicErr)
	}
	if out.Kind != classify.OutcomeRetryable || out.Reason != "retryable_error" {
		t.Fatalf("out=%+v, want retryable/retryable_error", out)
	}

	out, panicErr = classifyWithRecovery(true, classifierPanics{}, nil, errors.New("err"), key)
	if panicErr == nil {
		t.Fatalf("expected panic error")
	}
	if out.Kind != classify.OutcomeAbort || out.Reason != "panic_in_classifier" {
		t.Fatalf("out=%+v, want abort/panic_in_classifier", out)
	}

	out, panicErr = classifyWithRecovery(false, classifierUnknownWithReason{}, nil, errors.New("err"), key)
	if panicErr != nil {
		t.Fatalf("unexpected panic error: %v", panicErr)
	}
	if out.Kind != classify.OutcomeAbort || out.Reason != "custom_reason" {
		t.Fatalf("out=%+v, want abort/custom_reason", out)
	}

	out, panicErr = classifyWithRecovery(false, classifierSuccess{}, nil, nil, key)
	if panicErr != nil {
		t.Fatalf("unexpected panic error: %v", panicErr)
	}
	if out.Kind != classify.OutcomeSuccess || out.Reason != "success" {
		t.Fatalf("out=%+v, want success/success", out)
	}
}

func TestResolveClassifier(t *testing.T) {
	key := policy.PolicyKey{Name: "op"}
	reg := classify.NewRegistry()
	reg.Register("custom", classifierRetryable{})

	exec := &Executor{
		classifiers:           reg,
		defaultClassifier:     classifierSuccess{},
		missingClassifierMode: FailureFallback,
	}

	pol := policy.EffectivePolicy{Key: key, Retry: policy.RetryPolicy{ClassifierName: "custom"}}
	cls, meta, err := resolveClassifier(exec, pol)
	if err != nil || meta.notFound {
		t.Fatalf("unexpected resolve error: %v meta=%+v", err, meta)
	}
	if _, ok := cls.(classifierRetryable); !ok {
		t.Fatalf("expected custom classifier, got %T", cls)
	}

	pol.Retry.ClassifierName = " missing "
	cls, meta, err = resolveClassifier(exec, pol)
	if err != nil || !meta.notFound || meta.requested != "missing" {
		t.Fatalf("unexpected resolve result: err=%v meta=%+v", err, meta)
	}
	if _, ok := cls.(classifierSuccess); !ok {
		t.Fatalf("expected default classifier, got %T", cls)
	}

	exec.missingClassifierMode = FailureDeny
	_, meta, err = resolveClassifier(exec, pol)
	if err == nil {
		t.Fatalf("expected error for missing classifier")
	}
	var nce *NoClassifierError
	if !errors.As(err, &nce) || meta.requested != "missing" {
		t.Fatalf("unexpected error/meta: %v meta=%+v", err, meta)
	}
}

type policyProviderStub struct {
	pol policy.EffectivePolicy
	err error
}

func (p policyProviderStub) GetEffectivePolicy(context.Context, policy.PolicyKey) (policy.EffectivePolicy, error) {
	return p.pol, p.err
}

func TestResolvePolicyWithAttributes(t *testing.T) {
	key := policy.PolicyKey{Name: "op"}

	exec := &Executor{
		provider:          policyProviderStub{err: controlplane.ErrPolicyNotFound},
		missingPolicyMode: FailureDeny,
	}

	_, attrs, err := resolvePolicyWithAttributes(context.Background(), exec, key)
	var npe *NoPolicyError
	if !errors.As(err, &npe) {
		t.Fatalf("expected NoPolicyError, got %v", err)
	}
	assertPolicyResolutionAttrs(t, attrs, "deny", policy.PolicySourceUnknown, false)

	exec = &Executor{
		provider: policyProviderStub{pol: policy.EffectivePolicy{
			Retry: policy.RetryPolicy{Jitter: policy.JitterKind("bogus")},
		}},
		missingPolicyMode: FailureAllow,
	}

	pol, attrs, err := resolvePolicyWithAttributes(context.Background(), exec, key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attrs["policy_error"] == "" {
		t.Fatalf("expected policy_error attribute to be set")
	}
	assertPolicyResolutionAttrs(t, attrs, "allow", policy.PolicySourceUnknown, true,
		"hedge.budget.cost",
		"retry.backoff_multiplier",
		"retry.budget.cost",
		"retry.initial_backoff",
		"retry.jitter",
		"retry.max_backoff",
	)
	if pol.Retry.MaxAttempts != 1 {
		t.Fatalf("maxAttempts=%d, want 1", pol.Retry.MaxAttempts)
	}
}

func TestResolvePolicyWithAttributes_FallbackUsesReturnedPolicy(t *testing.T) {
	key := policy.PolicyKey{Name: "op"}
	exec := &Executor{
		provider: policyProviderStub{
			pol: policy.EffectivePolicy{
				Retry: policy.RetryPolicy{
					MaxAttempts:       4,
					InitialBackoff:    10 * time.Millisecond,
					MaxBackoff:        20 * time.Millisecond,
					BackoffMultiplier: 2,
					Jitter:            policy.JitterNone,
					Budget:            policy.BudgetRef{Cost: 1},
				},
				Hedge: policy.HedgePolicy{
					Budget: policy.BudgetRef{Cost: 1},
				},
				Meta: policy.Metadata{
					Source: policy.PolicySourceLKG,
				},
			},
			err: controlplane.ErrProviderUnavailable,
		},
		missingPolicyMode: FailureFallback,
	}

	pol, attrs, err := resolvePolicyWithAttributes(context.Background(), exec, key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pol.Retry.MaxAttempts != 4 {
		t.Fatalf("maxAttempts=%d, want 4", pol.Retry.MaxAttempts)
	}
	assertPolicyResolutionAttrs(t, attrs, "fallback", policy.PolicySourceLKG, false)
}

func TestResolvePolicyWithAttributes_NormalizationErrorFallback(t *testing.T) {
	key := policy.PolicyKey{Name: "op"}
	exec := &Executor{
		provider: policyProviderStub{
			pol: policy.EffectivePolicy{
				Retry: policy.RetryPolicy{Jitter: policy.JitterKind("bad")},
			},
		},
		missingPolicyMode: FailureFallback,
	}

	pol, attrs, err := resolvePolicyWithAttributes(context.Background(), exec, key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attrs["policy_error"] == "" {
		t.Fatalf("expected policy_error attribute")
	}
	assertPolicyResolutionAttrs(t, attrs, "fallback", policy.PolicySourceDefault, false)
	if pol.Retry.MaxAttempts != 3 {
		t.Fatalf("maxAttempts=%d, want 3", pol.Retry.MaxAttempts)
	}
}

func TestResolvePolicyFast_Fallback(t *testing.T) {
	key := policy.PolicyKey{Name: "op"}
	exec := &Executor{
		provider:          policyProviderStub{err: controlplane.ErrProviderUnavailable},
		missingPolicyMode: FailureFallback,
	}

	pol, err := resolvePolicyFast(context.Background(), exec, key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pol.Key != key {
		t.Fatalf("key=%v, want %v", pol.Key, key)
	}
	if pol.Retry.MaxAttempts != 3 {
		t.Fatalf("maxAttempts=%d, want 3", pol.Retry.MaxAttempts)
	}
}

func TestResolvePolicyFast_NormalizationErrorDeny(t *testing.T) {
	key := policy.PolicyKey{Name: "op"}
	exec := &Executor{
		provider: policyProviderStub{pol: policy.EffectivePolicy{
			Retry: policy.RetryPolicy{Jitter: policy.JitterKind("bad")},
		}},
		missingPolicyMode: FailureDeny,
	}

	_, err := resolvePolicyFast(context.Background(), exec, key)
	var npe *NoPolicyError
	if !errors.As(err, &npe) {
		t.Fatalf("expected NoPolicyError, got %v", err)
	}
}

const spinWaitIterations = 10000

func spinWait(check func() bool) bool {
	for i := 0; i < spinWaitIterations; i++ {
		if check() {
			return true
		}
		runtime.Gosched()
	}
	return check()
}

func waitForSignal(ch <-chan struct{}) bool {
	return spinWait(func() bool {
		select {
		case <-ch:
			return true
		default:
			return false
		}
	})
}
