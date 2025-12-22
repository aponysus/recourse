package retry

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/aponysus/recourse/budget"
	"github.com/aponysus/recourse/circuit"
	"github.com/aponysus/recourse/classify"
	"github.com/aponysus/recourse/controlplane"
	"github.com/aponysus/recourse/hedge"
	"github.com/aponysus/recourse/observe"
	"github.com/aponysus/recourse/policy"
)

var (
	// ErrNoPolicy is returned when no policy is found and missing policy mode is FailureDeny.
	ErrNoPolicy = errors.New("recourse: no policy found")

	// errHedgingRequiresTimeline is an internal sentinel used to switch from fast path to strict path.
	errHedgingRequiresTimeline = errors.New("recourse: hedging requires timeline")
)

// FailureMode controls behavior when a dependency is missing.
type FailureMode int

const (
	FailureModeUnknown FailureMode = iota
	FailureDeny
	FailureAllow
	FailureFallback
	FailureAllowUnsafe
)

func failureModeString(mode FailureMode) string {
	switch mode {
	case FailureDeny:
		return "deny"
	case FailureAllow:
		return "allow"
	case FailureFallback:
		return "fallback"
	case FailureAllowUnsafe:
		return "allow_unsafe"
	default:
		return "unknown"
	}
}

type Operation func(ctx context.Context) error
type OperationValue[T any] func(ctx context.Context) (T, error)

type Executor struct {
	provider              controlplane.PolicyProvider
	observer              observe.Observer
	clock                 func() time.Time
	sleep                 func(context.Context, time.Duration) error
	classifiers           *classify.Registry
	defaultClassifier     classify.Classifier
	budgets               *budget.Registry
	triggers              *hedge.Registry
	circuits              *circuit.Registry
	missingPolicyMode     FailureMode
	missingClassifierMode FailureMode
	missingBudgetMode     FailureMode
	missingTriggerMode    FailureMode
	recoverPanics         bool

	trackerMu sync.RWMutex
	trackers  map[policy.PolicyKey]hedge.LatencyTracker
}

type executorConfig struct {
	opts           ExecutorOptions
	staticPolicies map[policy.PolicyKey]policy.EffectivePolicy
}

// ExecutorOptions configures an Executor.
type ExecutorOptions struct {
	Provider              controlplane.PolicyProvider
	Observer              observe.Observer
	Clock                 func() time.Time
	Classifiers           *classify.Registry
	DefaultClassifier     classify.Classifier
	Budgets               *budget.Registry
	Triggers              *hedge.Registry
	Circuits              *circuit.Registry
	MissingPolicyMode     FailureMode
	MissingClassifierMode FailureMode
	MissingBudgetMode     FailureMode
	MissingTriggerMode    FailureMode
	RecoverPanics         bool
}

// NewExecutor creates an Executor with default options.
func NewExecutor(opts ...ExecutorOption) *Executor {
	cfg := &executorConfig{}

	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.opts.Provider == nil && len(cfg.staticPolicies) > 0 {
		cfg.opts.Provider = &controlplane.StaticProvider{
			Policies: cfg.staticPolicies,
		}
	}

	return NewExecutorFromOptions(cfg.opts)
}

// NewExecutorFromOptions creates an Executor from a config struct.
func NewExecutorFromOptions(opts ExecutorOptions) *Executor {
	e := &Executor{
		provider:              opts.Provider,
		observer:              opts.Observer,
		clock:                 opts.Clock,
		classifiers:           opts.Classifiers,
		defaultClassifier:     opts.DefaultClassifier,
		budgets:               opts.Budgets,
		triggers:              opts.Triggers,
		circuits:              opts.Circuits,
		missingPolicyMode:     normalizeFailureMode(opts.MissingPolicyMode, FailureDeny),
		missingClassifierMode: normalizeFailureMode(opts.MissingClassifierMode, FailureFallback),
		missingBudgetMode:     normalizeFailureMode(opts.MissingBudgetMode, FailureDeny),
		missingTriggerMode:    normalizeFailureMode(opts.MissingTriggerMode, FailureFallback),
		recoverPanics:         opts.RecoverPanics,
		trackers:              make(map[policy.PolicyKey]hedge.LatencyTracker),
	}

	if e.provider == nil {
		e.provider = &controlplane.StaticProvider{}
	}
	if e.observer == nil {
		e.observer = &observe.NoopObserver{}
	}
	if e.clock == nil {
		e.clock = time.Now
	}
	if e.sleep == nil {
		e.sleep = sleepWithContext
	}
	if e.classifiers == nil {
		e.classifiers = classify.NewRegistry()
		classify.RegisterBuiltins(e.classifiers)
	}
	if e.triggers == nil {
		e.triggers = hedge.NewRegistry()
	}
	if e.circuits == nil {
		e.circuits = circuit.NewRegistry()
	}
	if e.defaultClassifier == nil {
		e.defaultClassifier = classify.AlwaysRetryOnError{}
	}

	return e
}

// Validator for PanicError etc.
type PanicError struct {
	Component string
	Key       policy.PolicyKey
	Value     any
	Stack     []byte
}

func (e *PanicError) Error() string {
	return fmt.Sprintf("recourse: panic in %s for %s: %v", e.Component, e.Key, e.Value)
}

type NoPolicyError struct {
	Key policy.PolicyKey
	Err error
}

func (e *NoPolicyError) Error() string {
	return fmt.Sprintf("recourse: policy not found for %s: %v", e.Key, e.Err)
}

func (e *NoPolicyError) Unwrap() error {
	return e.Err
}

func (e *NoPolicyError) Is(target error) bool {
	return target == ErrNoPolicy
}

type NoClassifierError struct {
	Name string
}

func (e *NoClassifierError) Error() string {
	return fmt.Sprintf("recourse: classifier not found: %s", e.Name)
}

// CircuitOpenError is returned when a circuit breaker prevents execution.
type CircuitOpenError struct {
	State  circuit.State
	Reason string
}

func (e CircuitOpenError) Error() string {
	return fmt.Sprintf("recourse: circuit %s: %s", e.State, e.Reason)
}

// ExecutorOption configures an Executor.
type ExecutorOption func(*executorConfig)

// WithProvider sets the policy provider.
func WithProvider(p controlplane.PolicyProvider) ExecutorOption {
	return func(c *executorConfig) {
		c.opts.Provider = p
	}
}

// WithObserver sets the observer.
func WithObserver(o observe.Observer) ExecutorOption {
	return func(c *executorConfig) {
		c.opts.Observer = o
	}
}

// WithClock sets the clock function.
func WithClock(f func() time.Time) ExecutorOption {
	return func(c *executorConfig) {
		c.opts.Clock = f
	}
}

// WithClassifiers sets the classifier registry.
func WithClassifiers(r *classify.Registry) ExecutorOption {
	return func(c *executorConfig) {
		c.opts.Classifiers = r
	}
}

// WithDefaultClassifier sets the default classifier.
func WithDefaultClassifier(cls classify.Classifier) ExecutorOption {
	return func(c *executorConfig) {
		c.opts.DefaultClassifier = cls
	}
}

// WithClassifier adds or replaces a classifier in the registry.
func WithClassifier(name string, cls classify.Classifier) ExecutorOption {
	return func(c *executorConfig) {
		if c.opts.Classifiers == nil {
			c.opts.Classifiers = classify.NewRegistry()
		}
		c.opts.Classifiers.Register(name, cls)
	}
}

// WithBudgetRegistry sets the budget registry.
func WithBudgetRegistry(r *budget.Registry) ExecutorOption {
	return func(c *executorConfig) {
		c.opts.Budgets = r
	}
}

// WithHedgeTriggerRegistry sets the hedge trigger registry.
func WithHedgeTriggerRegistry(r *hedge.Registry) ExecutorOption {
	return func(c *executorConfig) {
		c.opts.Triggers = r
	}
}

// WithCircuitRegistry sets the circuit breaker registry.
func WithCircuitRegistry(r *circuit.Registry) ExecutorOption {
	return func(c *executorConfig) {
		c.opts.Circuits = r
	}
}

// WithMissingPolicyMode sets the mode for handling missing policies.
func WithMissingPolicyMode(mode FailureMode) ExecutorOption {
	return func(c *executorConfig) {
		c.opts.MissingPolicyMode = mode
	}
}

// WithMissingClassifierMode sets the mode for handling missing classifiers.
func WithMissingClassifierMode(mode FailureMode) ExecutorOption {
	return func(c *executorConfig) {
		c.opts.MissingClassifierMode = mode
	}
}

// WithMissingBudgetMode sets the mode for handling missing budgets.
func WithMissingBudgetMode(mode FailureMode) ExecutorOption {
	return func(c *executorConfig) {
		c.opts.MissingBudgetMode = mode
	}
}

// WithRecoverPanics sets whether to capture and report panics in user code.
func WithRecoverPanics(recover bool) ExecutorOption {
	return func(c *executorConfig) {
		c.opts.RecoverPanics = recover
	}
}

// WithPolicy adds a static policy for a string key (e.g. "svc.Method").
func WithPolicy(key string, opts ...policy.Option) ExecutorOption {
	return func(c *executorConfig) {
		if c.staticPolicies == nil {
			c.staticPolicies = make(map[policy.PolicyKey]policy.EffectivePolicy)
		}
		p := policy.New(key, opts...)
		c.staticPolicies[p.Key] = p
	}
}

// WithPolicyKey adds a static policy for a structured key.
func WithPolicyKey(key policy.PolicyKey, opts ...policy.Option) ExecutorOption {
	return func(c *executorConfig) {
		if c.staticPolicies == nil {
			c.staticPolicies = make(map[policy.PolicyKey]policy.EffectivePolicy)
		}
		p := policy.NewFromKey(key, opts...)
		c.staticPolicies[p.Key] = p
	}
}

func normalizeFailureMode(mode FailureMode, defaultMode FailureMode) FailureMode {
	switch mode {
	case FailureFallback, FailureAllow, FailureDeny, FailureAllowUnsafe:
		return mode
	default:
		return defaultMode
	}
}

func (e *Executor) Do(ctx context.Context, key policy.PolicyKey, op Operation) error {
	_, err := DoValue[struct{}](ctx, e, key, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, op(ctx)
	})
	return err
}

func DoValue[T any](ctx context.Context, exec *Executor, key policy.PolicyKey, op OperationValue[T]) (T, error) {
	val, _, err := doValueInternal(ctx, exec, key, op, false)
	return val, err
}

func doValueInternal[T any](ctx context.Context, exec *Executor, key policy.PolicyKey, op OperationValue[T], wantTimeline bool) (T, observe.Timeline, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if exec == nil {
		exec = NewExecutor()
	} else if exec.provider == nil || exec.clock == nil || exec.sleep == nil || exec.observer == nil || exec.classifiers == nil || exec.defaultClassifier == nil {
		exec = NewExecutorFromOptions(ExecutorOptions{
			Provider:              exec.provider,
			Observer:              exec.observer,
			Clock:                 exec.clock,
			Classifiers:           exec.classifiers,
			DefaultClassifier:     exec.defaultClassifier,
			Budgets:               exec.budgets,
			Triggers:              exec.triggers,
			Circuits:              exec.circuits,
			MissingPolicyMode:     exec.missingPolicyMode,
			MissingBudgetMode:     exec.missingBudgetMode,
			MissingClassifierMode: exec.missingClassifierMode,
			MissingTriggerMode:    exec.missingTriggerMode,
			RecoverPanics:         exec.recoverPanics,
		})
	}

	capture, hasCapture := observe.TimelineCaptureFromContext(ctx)
	fullTimeline := wantTimeline || hasCapture || !isNoopObserver(exec.observer)

	if !fullTimeline {
		// Use a wrapped op that suppresses capture to prevent implicit capture in nested calls.
		fastOp := func(c context.Context) (T, error) {
			return op(observe.WithoutTimelineCapture(c))
		}
		val, err := doValueFast(ctx, exec, key, fastOp)

		// Fallback check
		if err == errHedgingRequiresTimeline {
			// Fall through to fullTimeline path
			fullTimeline = true
		} else {
			return val, observe.Timeline{}, err
		}
	}

	// For full timeline, we also suppress capture in the op.
	// Wrapping here provides consistency with the fast path.
	safeOp := func(c context.Context) (T, error) {
		return op(observe.WithoutTimelineCapture(c))
	}

	val, tl, err := doValueWithTimeline(ctx, exec, key, safeOp)
	if capture != nil {
		observe.StoreTimelineCapture(capture, &tl)
	}
	// Record latency if we have a valid policy key and tracking is enabled.
	return val, tl, err
}

func (e *Executor) getTracker(key policy.PolicyKey) hedge.LatencyTracker {
	e.trackerMu.RLock()
	t, ok := e.trackers[key]
	e.trackerMu.RUnlock()
	if ok {
		return t
	}

	e.trackerMu.Lock()
	defer e.trackerMu.Unlock()
	// Double check
	if t, ok = e.trackers[key]; ok {
		return t
	}
	t = hedge.NewRingBufferTracker(256)
	e.trackers[key] = t
	return t
}

func doValueFast[T any](ctx context.Context, exec *Executor, key policy.PolicyKey, op OperationValue[T]) (T, error) {
	var zero T

	pol, err := resolvePolicyFast(ctx, exec, key)
	if err != nil {
		return zero, err
	}

	if pol.Hedge.Enabled {
		return zero, errHedgingRequiresTimeline
	}
	if pol.Circuit.Enabled {
		return zero, errHedgingRequiresTimeline // Reuse sentinel for now to force full path
	}

	classifier, _, err := resolveClassifier(exec, pol)
	if err != nil {
		return zero, err
	}

	if pol.Retry.OverallTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, pol.Retry.OverallTimeout)
		defer cancel()
	}

	maxAttempts := pol.Retry.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 1
	}

	backoff := pol.Retry.InitialBackoff

	var last T
	var lastErr error

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return last, err
		}

		decision, ok := exec.allowAttempt(ctx, key, pol.Retry.Budget, attempt, budget.KindRetry)
		// Check if attempt is allowed by budget.
		if !ok {
			return last, errors.New(decision.Reason)
		}

		release := decision.Release

		attemptCtx := ctx
		cancelAttempt := func() {}
		if pol.Retry.TimeoutPerAttempt > 0 {
			attemptCtx, cancelAttempt = context.WithTimeout(ctx, pol.Retry.TimeoutPerAttempt)
		}

		// Inject attempt info for observability.
		attemptCtx = observe.WithAttemptInfo(attemptCtx, observe.AttemptInfo{
			RetryIndex: attempt,
			Attempt:    attempt,
			IsHedge:    false,
			PolicyID:   pol.ID,
		})

		var val T
		var err error

		func() {
			defer cancelAttempt()
			if release != nil {
				defer release()
			}
			start := exec.clock()
			val, err = op(attemptCtx)
			// Feed latency tracker
			exec.getTracker(key).Observe(exec.clock().Sub(start))
		}()

		last = val
		lastErr = err

		out, panicErr := classifyWithRecovery(exec.recoverPanics, classifier, val, err, key)
		if panicErr != nil {
			return last, panicErr
		}

		if out.Kind == classify.OutcomeSuccess {
			return val, nil
		}

		switch out.Kind {
		case classify.OutcomeRetryable:
			// continue
		case classify.OutcomeNonRetryable, classify.OutcomeAbort, classify.OutcomeUnknown:
			return last, terminalError(ctx, lastErr, out)
		default:
			return last, terminalError(ctx, lastErr, out)
		}

		if attempt == maxAttempts-1 {
			return last, terminalError(ctx, lastErr, out)
		}

		sleepFor := computeSleep(backoff, pol.Retry, out)
		if sleepFor > 0 {
			if err := exec.sleep(ctx, sleepFor); err != nil {
				return last, err
			}
		}

		backoff = nextBackoff(backoff, pol.Retry.BackoffMultiplier, pol.Retry.MaxBackoff)
	}

	return last, lastErr
}

func doValueWithTimeline[T any](ctx context.Context, exec *Executor, key policy.PolicyKey, op OperationValue[T]) (T, observe.Timeline, error) {
	var zero T

	start := exec.clock()

	// 1. Resolve Policy
	pol, attrs, err := resolvePolicyWithAttributes(ctx, exec, key)
	if err != nil {
		tl := observe.Timeline{
			Key:        key,
			PolicyID:   pol.ID,
			Start:      start,
			End:        exec.clock(),
			Attributes: attrs,
			Attempts:   nil,
			FinalErr:   err,
		}
		exec.observer.OnStart(ctx, key, pol)
		exec.observer.OnFailure(ctx, key, tl)
		return zero, tl, err
	}

	// 2. Check Circuit Breaker
	var cb circuit.CircuitBreaker
	if pol.Circuit.Enabled {
		cb = exec.circuits.Get(key, pol.Circuit)
		if cb != nil {
			decision := cb.Allow(ctx)
			if !decision.Allowed {
				tl := observe.Timeline{
					Key:        key,
					PolicyID:   pol.ID,
					Start:      start,
					End:        exec.clock(),
					Attributes: attrs,
					Attempts:   nil,
					FinalErr:   CircuitOpenError{State: decision.State, Reason: decision.Reason},
				}
				tl.Attributes["circuit_state"] = decision.State.String()
				exec.observer.OnStart(ctx, key, pol)
				exec.observer.OnFailure(ctx, key, tl)
				// Record failure? IP says: "On OutcomeAbort: Do not report".
				// Circuit rejection is arguably a failure of availability, but we didn't attempt.
				// Usually we don't record failure to the breaker if the breaker itself rejected it
				// (preventing feedback loop).
				return zero, tl, tl.FinalErr
			}
			// If allowed, we proceed.
			// Half-open state might affect hedging later.
			if decision.State == circuit.StateHalfOpen {
				pol.Hedge.Enabled = false
			}
		}
	}

	classifier, cmeta, err := resolveClassifier(exec, pol)
	if err != nil {
		tl := observe.Timeline{
			Key:        key,
			PolicyID:   pol.ID,
			Start:      start,
			End:        exec.clock(),
			Attributes: attrs,
			Attempts:   nil,
			FinalErr:   err,
		}
		if cmeta.requested != "" {
			tl.Attributes["classifier_name"] = cmeta.requested
		}
		tl.Attributes["classifier_error"] = "classifier_not_found"
		exec.observer.OnStart(ctx, key, pol)
		exec.observer.OnFailure(ctx, key, tl)
		return zero, tl, err
	}

	if pol.Retry.OverallTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, pol.Retry.OverallTimeout)
		defer cancel()
	}

	maxAttempts := pol.Retry.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 1
	}

	tl := observe.Timeline{
		Key:        key,
		PolicyID:   pol.ID,
		Start:      start,
		Attributes: attrs,
		Attempts:   make([]observe.AttemptRecord, 0, maxAttempts),
	}
	exec.observer.OnStart(ctx, key, pol)

	backoff := pol.Retry.InitialBackoff

	var last T
	var lastErr error
	var lastBackoff time.Duration

	var tlMu sync.Mutex
	var done bool
	recordAttempt := func(ctx context.Context, rec observe.AttemptRecord) {
		tlMu.Lock()
		defer tlMu.Unlock()
		if done {
			return
		}
		tl.Attempts = append(tl.Attempts, rec)
		exec.observer.OnAttempt(ctx, key, rec)

		// Feed latency tracker
		tracker := exec.getTracker(key)
		tracker.Observe(rec.EndTime.Sub(rec.StartTime))
	}

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			tlMu.Lock()
			done = true
			tl.End = exec.clock()
			tl.FinalErr = err
			tlMu.Unlock()
			exec.observer.OnFailure(ctx, key, tl)
			// Context canceled before attempt.
			// Should we report this to breaker?
			// Usually Context Canceled is OutcomeAbort, which we don't report.
			return last, tl, err
		}

		opAny := func(c context.Context) (any, error) { return op(c) }

		valAny, err, outcome, success := exec.doRetryGroup(
			ctx,
			key,
			opAny,
			pol,
			attempt,
			classifier,
			cmeta,
			lastBackoff,
			recordAttempt,
		)

		if success {
			// Record success to circuit breaker
			if cb != nil {
				cb.RecordSuccess(ctx)
			}

			tlMu.Lock()
			done = true
			tl.End = exec.clock()
			tl.FinalErr = nil
			tlMu.Unlock()
			exec.observer.OnSuccess(ctx, key, tl)
			return valAny.(T), tl, nil
		}

		prevErr := lastErr
		lastErr = err

		isTerminal := false
		if outcome.Kind == classify.OutcomeAbort || outcome.Kind == classify.OutcomeNonRetryable {
			isTerminal = true
		}

		if isTerminal {
			// Record failure to circuit breaker (unless it's an abort/cancellation).
			if cb != nil && outcome.Kind != classify.OutcomeAbort {
				cb.RecordFailure(ctx)
			}

			terr := terminalError(ctx, lastErr, outcome)
			if attempt > 0 && prevErr != nil && outcome.Reason == "budget_denied" {
				terr = prevErr
			}

			tlMu.Lock()
			done = true
			tl.End = exec.clock()
			tl.FinalErr = terr
			tlMu.Unlock()
			exec.observer.OnFailure(ctx, key, tl)

			return last, tl, terr
		}
		if attempt == maxAttempts-1 {
			// Max attempts reached, still failing.
			if cb != nil && outcome.Kind != classify.OutcomeAbort {
				cb.RecordFailure(ctx)
			}

			terr := terminalError(ctx, lastErr, outcome)
			tlMu.Lock()
			done = true
			tl.End = exec.clock()
			tl.FinalErr = terr
			tlMu.Unlock()
			exec.observer.OnFailure(ctx, key, tl)
			return last, tl, terr
		}

		sleepFor := computeSleep(backoff, pol.Retry, outcome)
		lastBackoff = sleepFor
		if sleepFor > 0 {
			if err := exec.sleep(ctx, sleepFor); err != nil {
				tlMu.Lock()
				done = true
				tl.End = exec.clock()
				tl.FinalErr = err
				tlMu.Unlock()
				exec.observer.OnFailure(ctx, key, tl)
				return last, tl, err
			}
		}

		backoff = nextBackoff(backoff, pol.Retry.BackoffMultiplier, pol.Retry.MaxBackoff)
	}

	tlMu.Lock()
	done = true
	tl.End = exec.clock()
	tl.FinalErr = lastErr
	tlMu.Unlock()
	exec.observer.OnFailure(ctx, key, tl)
	return last, tl, lastErr
}

func resolvePolicyWithAttributes(ctx context.Context, exec *Executor, key policy.PolicyKey) (policy.EffectivePolicy, map[string]string, error) {
	attrs := make(map[string]string)

	var pol policy.EffectivePolicy
	var err error

	func() {
		if exec.recoverPanics {
			defer func() {
				if r := recover(); r != nil {
					err = &PanicError{
						Component: "policy_provider",
						Key:       key,
						Value:     r,
						Stack:     debug.Stack(),
					}
				}
			}()
		}
		pol, err = exec.provider.GetEffectivePolicy(ctx, key)
	}()

	if err != nil {
		switch exec.missingPolicyMode {
		case FailureDeny:
			return policy.EffectivePolicy{}, attrs, &NoPolicyError{Key: key, Err: err}
		case FailureAllow:
			pol = policy.EffectivePolicy{Key: key, Retry: policy.RetryPolicy{MaxAttempts: 1}}
		case FailureFallback:
			if isZeroEffectivePolicy(pol) {
				pol = policy.DefaultPolicyFor(key)
			}
		}
	}
	if isZeroEffectivePolicy(pol) {
		pol = policy.DefaultPolicyFor(key)
	}
	pol.Key = key

	pol, normErr := pol.Normalize()
	if normErr != nil {
		switch exec.missingPolicyMode {
		case FailureDeny:
			return policy.EffectivePolicy{}, attrs, &NoPolicyError{Key: key, Err: normErr}
		case FailureAllow:
			pol = policy.EffectivePolicy{Key: key, Retry: policy.RetryPolicy{MaxAttempts: 1}}
			pol, _ = pol.Normalize()
		case FailureFallback:
			pol = policy.DefaultPolicyFor(key)
			pol, _ = pol.Normalize()
		}
		attrs["policy_error"] = fmt.Sprintf("normalization_failed: %v", normErr)
	}

	return pol, attrs, nil
}

func resolvePolicyFast(ctx context.Context, exec *Executor, key policy.PolicyKey) (policy.EffectivePolicy, error) {
	// Fast path avoids attributes map and defer overhead if possible.
	// But provider might panic.

	var pol policy.EffectivePolicy
	var err error

	func() {
		if exec.recoverPanics {
			defer func() {
				if r := recover(); r != nil {
					err = &PanicError{
						Component: "policy_provider",
						Key:       key,
						Value:     r,
						Stack:     debug.Stack(),
					}
				}
			}()
		}
		pol, err = exec.provider.GetEffectivePolicy(ctx, key)
	}()

	if err != nil {
		switch exec.missingPolicyMode {
		case FailureDeny:
			return policy.EffectivePolicy{}, &NoPolicyError{Key: key, Err: err}
		case FailureAllow:
			pol = policy.EffectivePolicy{Key: key, Retry: policy.RetryPolicy{MaxAttempts: 1}}
		case FailureFallback:
			if isZeroEffectivePolicy(pol) {
				pol = policy.DefaultPolicyFor(key)
			}
		}
	}
	if isZeroEffectivePolicy(pol) {
		pol = policy.DefaultPolicyFor(key)
	}
	pol.Key = key

	pol, normErr := pol.Normalize()
	if normErr != nil {
		switch exec.missingPolicyMode {
		case FailureDeny:
			return policy.EffectivePolicy{}, &NoPolicyError{Key: key, Err: normErr}
		case FailureAllow:
			pol = policy.EffectivePolicy{Key: key, Retry: policy.RetryPolicy{MaxAttempts: 1}}
			pol, _ = pol.Normalize()
		case FailureFallback:
			pol = policy.DefaultPolicyFor(key)
			pol, _ = pol.Normalize()
		}
	}

	return pol, nil
}

func policyErrorKind(err error) string {
	switch {
	case errors.Is(err, controlplane.ErrPolicyNotFound):
		return "policy_not_found"
	case errors.Is(err, controlplane.ErrProviderUnavailable):
		return "provider_unavailable"
	case errors.Is(err, controlplane.ErrPolicyFetchFailed):
		return "policy_fetch_failed"
	default:
		return "unknown_error"
	}
}

func isNoopObserver(obs observe.Observer) bool {
	switch obs.(type) {
	case observe.NoopObserver, *observe.NoopObserver:
		return true
	default:
		return false
	}
}

func isZeroEffectivePolicy(pol policy.EffectivePolicy) bool {
	return pol.Key == (policy.PolicyKey{}) &&
		pol.ID == "" &&
		pol.Retry == (policy.RetryPolicy{}) &&
		pol.Hedge == (policy.HedgePolicy{})
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)

	// defensive cleanup. This avoids subtle leaks/incorrect behavior patters
	defer func() {
		if !timer.Stop() {
			select {
			case <-timer.C: // drain any pending tick so the channel doesn't retain value
			default:
			}
		}
	}()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func nextBackoff(current time.Duration, multiplier float64, max time.Duration) time.Duration {
	next := time.Duration(float64(current) * multiplier)
	if next < 0 {
		next = 0
	}
	if max > 0 && next > max {
		return max
	}
	return next
}

func applyJitter(backoff time.Duration, kind policy.JitterKind) time.Duration {
	switch kind {
	case policy.JitterNone, "":
		return backoff
	case policy.JitterFull:
		return time.Duration(rand.Float64() * float64(backoff))
	case policy.JitterEqual:
		half := float64(backoff) / 2
		return time.Duration(half + rand.Float64()*half)
	default:
		return backoff
	}
}

type classifierMeta struct {
	requested string
	notFound  bool
}

func resolveClassifier(exec *Executor, pol policy.EffectivePolicy) (classify.Classifier, classifierMeta, error) {
	meta := classifierMeta{requested: strings.TrimSpace(pol.Retry.ClassifierName)}

	classifier := exec.defaultClassifier
	if meta.requested == "" {
		return classifier, meta, nil
	}

	var c classify.Classifier
	var ok bool
	var panicErr error

	func() {
		if exec.recoverPanics {
			defer func() {
				if r := recover(); r != nil {
					panicErr = &PanicError{
						Component: "classifier_registry",
						Key:       pol.Key, // Best effort key
						Value:     r,
						Stack:     debug.Stack(),
					}
				}
			}()
		}
		c, ok = exec.classifiers.Get(meta.requested)
	}()

	if panicErr != nil {
		meta.notFound = true
		switch exec.missingClassifierMode {
		case FailureDeny:
			return nil, meta, panicErr
		default:
			// Fallback
			return classifier, meta, nil
		}
	}

	if ok {
		return c, meta, nil
	}

	meta.notFound = true
	switch exec.missingClassifierMode {
	case FailureDeny:
		return nil, meta, &NoClassifierError{Name: meta.requested}
	default:
		return classifier, meta, nil
	}
}

func annotateClassifierFallback(out *classify.Outcome, meta classifierMeta) {
	if out == nil || !meta.notFound || meta.requested == "" {
		return
	}
	if out.Attributes == nil {
		out.Attributes = make(map[string]string, 3)
	}
	out.Attributes["classifier_not_found"] = "true"
	out.Attributes["classifier_name"] = meta.requested
	out.Attributes["classifier_fallback"] = "default"
}

func classifyWithRecovery(recoverPanics bool, classifier classify.Classifier, value any, err error, key policy.PolicyKey) (out classify.Outcome, panicErr error) {
	if recoverPanics {
		defer func() {
			if r := recover(); r != nil {
				out = classify.Outcome{Kind: classify.OutcomeAbort, Reason: "panic_in_classifier"}
				panicErr = &PanicError{
					Component: "classifier",
					Key:       key,
					Value:     r,
					Stack:     debug.Stack(),
				}
			}
		}()
	}
	out = classifier.Classify(value, err)
	if out.Kind == classify.OutcomeUnknown {
		if out.Reason == "" {
			out.Reason = "unknown_outcome"
		}
		out.Kind = classify.OutcomeAbort
	}
	if out.Reason == "" {
		switch out.Kind {
		case classify.OutcomeSuccess:
			out.Reason = "success"
		case classify.OutcomeRetryable:
			out.Reason = "retryable_error"
		case classify.OutcomeNonRetryable:
			out.Reason = "non_retryable_error"
		case classify.OutcomeAbort:
			out.Reason = "abort"
		default:
			out.Reason = "unknown_outcome"
		}
	}
	return out, nil
}

func terminalError(ctx context.Context, opErr error, out classify.Outcome) error {
	if ctx != nil {
		if ctxErr := ctx.Err(); ctxErr != nil && (errors.Is(opErr, context.Canceled) || errors.Is(opErr, context.DeadlineExceeded)) {
			return ctxErr
		}
	}
	if opErr != nil {
		return opErr
	}
	if out.Reason != "" {
		return errors.New("recourse: " + out.Reason)
	}
	return errors.New("recourse: operation failed")
}

func computeSleep(backoff time.Duration, pol policy.RetryPolicy, out classify.Outcome) time.Duration {
	if out.BackoffOverride > 0 {
		return capBackoff(out.BackoffOverride, pol.MaxBackoff)
	}
	return capBackoff(applyJitter(backoff, pol.Jitter), pol.MaxBackoff)
}

func capBackoff(d, max time.Duration) time.Duration {
	if d < 0 {
		return 0
	}
	if max > 0 && d > max {
		return max
	}
	return d
}
