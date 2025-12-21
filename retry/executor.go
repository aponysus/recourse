package retry

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"runtime/debug"
	"strings"
	"time"

	"github.com/aponysus/recourse/budget"
	"github.com/aponysus/recourse/classify"
	"github.com/aponysus/recourse/controlplane"
	"github.com/aponysus/recourse/observe"
	"github.com/aponysus/recourse/policy"
)

type Operation func(ctx context.Context) error

type OperationValue[T any] func(ctx context.Context) (T, error)

type FailureMode int

const (
	FailureFallback    FailureMode = iota // use safe defaults
	FailureAllow                          // proceed without constraint
	FailureDeny                           // fail fast
	FailureAllowUnsafe                    // proceed without constraint (explicit unsafe opt-in)
)

// ErrNoPolicy is returned when MissingPolicyMode=FailureDeny and the executor
// cannot obtain an authoritative policy.
var ErrNoPolicy = errors.New("recourse: no policy")

type NoPolicyError struct {
	Key policy.PolicyKey
	Err error
}

func (e *NoPolicyError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Err == nil {
		return ErrNoPolicy.Error()
	}
	return ErrNoPolicy.Error() + ": " + e.Err.Error()
}

func (e *NoPolicyError) Unwrap() error { return e.Err }

func (e *NoPolicyError) Is(target error) bool { return target == ErrNoPolicy }

// ErrNoClassifier is returned when MissingClassifierMode=FailureDeny and the executor
// cannot resolve the requested classifier by name.
var ErrNoClassifier = errors.New("recourse: classifier not found")

type NoClassifierError struct {
	Name string
}

func (e *NoClassifierError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if strings.TrimSpace(e.Name) == "" {
		return ErrNoClassifier.Error()
	}
	return ErrNoClassifier.Error() + ": " + e.Name
}

func (e *NoClassifierError) Is(target error) bool { return target == ErrNoClassifier }

type PanicError struct {
	Component string
	Key       policy.PolicyKey
	Value     any
	Stack     []byte
}

func (e *PanicError) Error() string {
	msg := "recourse: panic in " + e.Component
	if e.Value != nil {
		msg += fmt.Sprintf(": %v", e.Value)
	}
	return msg
}

type ExecutorOptions struct {
	Provider controlplane.PolicyProvider
	Observer observe.Observer // nil → NoopObserver
	Clock    func() time.Time // for tests

	Classifiers       *classify.Registry
	DefaultClassifier classify.Classifier

	Budgets *budget.Registry

	// Failure modes for missing components (used in later phases).
	MissingPolicyMode     FailureMode // default: FailureFallback
	MissingClassifierMode FailureMode // default: FailureFallback (Phase 3+)
	MissingBudgetMode     FailureMode // default: FailureDeny (fail-closed)
	MissingTriggerMode    FailureMode // default: FailureFallback/disable hedging (Phase 5+)

	// Panic isolation for user hooks (Phase 3+).
	RecoverPanics bool // default false
}

type Executor struct {
	provider controlplane.PolicyProvider
	observer observe.Observer
	clock    func() time.Time

	classifiers       *classify.Registry
	defaultClassifier classify.Classifier
	budgets           *budget.Registry

	missingPolicyMode     FailureMode
	missingClassifierMode FailureMode
	missingBudgetMode     FailureMode
	missingTriggerMode    FailureMode

	recoverPanics bool

	sleep func(ctx context.Context, d time.Duration) error
}

func NewExecutor(opts ExecutorOptions) *Executor {
	exec := &Executor{
		provider:          opts.Provider,
		observer:          opts.Observer,
		clock:             opts.Clock,
		classifiers:       opts.Classifiers,
		defaultClassifier: opts.DefaultClassifier,
		budgets:           opts.Budgets,
		recoverPanics:     opts.RecoverPanics,

		missingPolicyMode:     normalizeFailureMode(opts.MissingPolicyMode, FailureFallback),
		missingClassifierMode: normalizeFailureMode(opts.MissingClassifierMode, FailureFallback),
		missingBudgetMode:     normalizeFailureMode(opts.MissingBudgetMode, FailureDeny),
		missingTriggerMode:    normalizeFailureMode(opts.MissingTriggerMode, FailureFallback),

		sleep: sleepWithContext,
	}

	if exec.provider == nil {
		exec.provider = &controlplane.StaticProvider{}
	}
	if exec.observer == nil {
		exec.observer = observe.NoopObserver{}
	}
	if exec.classifiers == nil {
		exec.classifiers = classify.NewRegistry()
		classify.RegisterBuiltins(exec.classifiers)
	}
	if exec.defaultClassifier == nil {
		exec.defaultClassifier = classify.AlwaysRetryOnError{}
	}
	if exec.clock == nil {
		exec.clock = time.Now
	}
	return exec
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

func DoValueWithTimeline[T any](ctx context.Context, exec *Executor, key policy.PolicyKey, op OperationValue[T]) (T, observe.Timeline, error) {
	return doValueInternal(ctx, exec, key, op, true)
}

func (e *Executor) DoWithTimeline(ctx context.Context, key policy.PolicyKey, op Operation) (observe.Timeline, error) {
	_, tl, err := DoValueWithTimeline[struct{}](ctx, e, key, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, op(ctx)
	})
	return tl, err
}

func doValueInternal[T any](ctx context.Context, exec *Executor, key policy.PolicyKey, op OperationValue[T], wantTimeline bool) (T, observe.Timeline, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if exec == nil {
		exec = NewExecutor(ExecutorOptions{})
	} else if exec.provider == nil || exec.clock == nil || exec.sleep == nil || exec.observer == nil || exec.classifiers == nil || exec.defaultClassifier == nil {
		exec = NewExecutor(ExecutorOptions{
			Provider:              exec.provider,
			Observer:              exec.observer,
			Clock:                 exec.clock,
			Classifiers:           exec.classifiers,
			DefaultClassifier:     exec.defaultClassifier,
			Budgets:               exec.budgets,
			MissingPolicyMode:     exec.missingPolicyMode,
			MissingBudgetMode:     exec.missingBudgetMode,
			MissingClassifierMode: exec.missingClassifierMode,
			MissingTriggerMode:    exec.missingTriggerMode,
			RecoverPanics:         exec.recoverPanics,
		})
	}

	fullTimeline := wantTimeline || !isNoopObserver(exec.observer)
	if !fullTimeline {
		val, err := doValueFast(ctx, exec, key, op)
		return val, observe.Timeline{}, err
	}
	return doValueWithTimeline(ctx, exec, key, op)
}

func doValueFast[T any](ctx context.Context, exec *Executor, key policy.PolicyKey, op OperationValue[T]) (T, error) {
	var zero T

	pol, err := resolvePolicyFast(ctx, exec, key)
	if err != nil {
		return zero, err
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

	backoff := pol.Retry.InitialBackoff
	maxAttempts := pol.Retry.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 1
	}

	var last T
	var lastErr error

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return last, err
		}

		decision, ok := exec.allowAttempt(ctx, key, pol.Retry.Budget, attempt, budget.KindRetry)
		if !ok {
			if attempt == 0 {
				return zero, terminalError(ctx, nil, classify.Outcome{Kind: classify.OutcomeAbort, Reason: decision.Reason})
			}
			return last, lastErr
		}

		release := decision.Release

		attemptCtx := ctx
		cancelAttempt := func() {}
		if pol.Retry.TimeoutPerAttempt > 0 {
			attemptCtx, cancelAttempt = context.WithTimeout(ctx, pol.Retry.TimeoutPerAttempt)
		}
		attemptCtx = observe.WithAttemptInfo(attemptCtx, observe.AttemptInfo{
			RetryIndex: attempt,
			Attempt:    attempt,
			IsHedge:    false,
			HedgeIndex: 0,
			PolicyID:   pol.ID,
		})

		var val T
		var err error
		func() {
			defer cancelAttempt()
			if release != nil {
				defer release()
			}
			val, err = op(attemptCtx)
		}()

		last = val
		lastErr = err

		out, panicErr := classifyWithRecovery(exec.recoverPanics, classifier, val, err, key)
		if panicErr != nil {
			return last, terminalError(ctx, panicErr, out)
		}
		if out.Kind == classify.OutcomeSuccess {
			return val, nil
		}

		switch out.Kind {
		case classify.OutcomeRetryable:
			// continue
		case classify.OutcomeNonRetryable, classify.OutcomeAbort, classify.OutcomeUnknown:
			return last, terminalError(ctx, err, out)
		default:
			return last, terminalError(ctx, err, classify.Outcome{Kind: classify.OutcomeAbort, Reason: "unknown_outcome"})
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

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			tl.End = exec.clock()
			tl.FinalErr = err
			exec.observer.OnFailure(ctx, key, tl)
			return last, tl, err
		}

		attemptStart := exec.clock()

		decision, ok := exec.allowAttempt(ctx, key, pol.Retry.Budget, attempt, budget.KindRetry)
		if !ok {
			attemptCtx := observe.WithAttemptInfo(ctx, observe.AttemptInfo{
				RetryIndex: attempt,
				Attempt:    attempt,
				IsHedge:    false,
				HedgeIndex: 0,
				PolicyID:   pol.ID,
			})

			outcome := classify.Outcome{Kind: classify.OutcomeAbort, Reason: decision.Reason}
			rec := observe.AttemptRecord{
				Attempt:       attempt,
				StartTime:     attemptStart,
				EndTime:       exec.clock(),
				Outcome:       outcome,
				Err:           nil,
				Backoff:       lastBackoff,
				BudgetAllowed: false,
				BudgetReason:  decision.Reason,
			}
			tl.Attempts = append(tl.Attempts, rec)
			exec.observer.OnAttempt(attemptCtx, key, rec)

			terr := terminalError(ctx, nil, outcome)
			if attempt > 0 && lastErr != nil {
				terr = lastErr
			}
			tl.End = exec.clock()
			tl.FinalErr = terr
			exec.observer.OnFailure(ctx, key, tl)
			return last, tl, terr
		}

		release := decision.Release

		attemptCtx := ctx
		cancelAttempt := func() {}
		if pol.Retry.TimeoutPerAttempt > 0 {
			attemptCtx, cancelAttempt = context.WithTimeout(ctx, pol.Retry.TimeoutPerAttempt)
		}
		attemptCtx = observe.WithAttemptInfo(attemptCtx, observe.AttemptInfo{
			RetryIndex: attempt,
			Attempt:    attempt,
			IsHedge:    false,
			HedgeIndex: 0,
			PolicyID:   pol.ID,
		})

		var val T
		var err error
		func() {
			defer cancelAttempt()
			if release != nil {
				defer release()
			}
			val, err = op(attemptCtx)
		}()

		attemptEnd := exec.clock()

		last = val
		lastErr = err

		outcome, panicErr := classifyWithRecovery(exec.recoverPanics, classifier, val, err, key)
		annotateClassifierFallback(&outcome, cmeta)

		rec := observe.AttemptRecord{
			Attempt:       attempt,
			StartTime:     attemptStart,
			EndTime:       attemptEnd,
			Outcome:       outcome,
			Err:           err,
			Backoff:       lastBackoff,
			BudgetAllowed: decision.Allowed,
			BudgetReason:  decision.Reason,
		}
		tl.Attempts = append(tl.Attempts, rec)
		exec.observer.OnAttempt(attemptCtx, key, rec)

		if panicErr != nil {
			terr := terminalError(ctx, panicErr, outcome)
			tl.End = exec.clock()
			tl.FinalErr = terr
			exec.observer.OnFailure(ctx, key, tl)
			return last, tl, terr
		}

		if outcome.Kind == classify.OutcomeSuccess {
			tl.End = exec.clock()
			tl.FinalErr = nil
			exec.observer.OnSuccess(ctx, key, tl)
			return val, tl, nil
		}

		switch outcome.Kind {
		case classify.OutcomeRetryable:
			// continue
		case classify.OutcomeNonRetryable, classify.OutcomeAbort, classify.OutcomeUnknown:
			terr := terminalError(ctx, err, outcome)
			tl.End = exec.clock()
			tl.FinalErr = terr
			exec.observer.OnFailure(ctx, key, tl)
			return last, tl, terr
		default:
			terr := terminalError(ctx, err, classify.Outcome{Kind: classify.OutcomeAbort, Reason: "unknown_outcome"})
			tl.End = exec.clock()
			tl.FinalErr = terr
			exec.observer.OnFailure(ctx, key, tl)
			return last, tl, terr
		}
		if attempt == maxAttempts-1 {
			terr := terminalError(ctx, lastErr, outcome)
			tl.End = exec.clock()
			tl.FinalErr = terr
			exec.observer.OnFailure(ctx, key, tl)
			return last, tl, terr
		}

		sleepFor := computeSleep(backoff, pol.Retry, outcome)
		lastBackoff = sleepFor
		if sleepFor > 0 {
			if err := exec.sleep(ctx, sleepFor); err != nil {
				tl.End = exec.clock()
				tl.FinalErr = err
				exec.observer.OnFailure(ctx, key, tl)
				return last, tl, err
			}
		}

		backoff = nextBackoff(backoff, pol.Retry.BackoffMultiplier, pol.Retry.MaxBackoff)
	}

	tl.End = exec.clock()
	tl.FinalErr = lastErr
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
		attrs["policy_error"] = policyErrorKind(err)
		attrs["missing_policy_mode"] = failureModeString(exec.missingPolicyMode)

		switch exec.missingPolicyMode {
		case FailureDeny:
			if isZeroEffectivePolicy(pol) {
				pol = policy.EffectivePolicy{Key: key}
			} else {
				pol.Key = key
			}
			// If it's a PanicError, wrapping it in NoPolicyError might hide it?
			// NoPolicyError wraps the error, so Is(ErrNoPolicy) works.
			// But maybe we want the panic to surface more loudly?
			// The contract says ResolvePolicy returns error if failed.
			return pol, attrs, &NoPolicyError{Key: key, Err: err}
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

	var normErr error
	pol, normErr = pol.Normalize()
	if normErr != nil {
		attrs["policy_error"] = "policy_normalize_error"
		attrs["missing_policy_mode"] = failureModeString(exec.missingPolicyMode)
		switch exec.missingPolicyMode {
		case FailureDeny:
			return pol, attrs, &NoPolicyError{Key: key, Err: normErr}
		case FailureAllow:
			pol = policy.EffectivePolicy{Key: key, Retry: policy.RetryPolicy{MaxAttempts: 1}}
			pol, _ = pol.Normalize()
		case FailureFallback:
			pol = policy.DefaultPolicyFor(key)
			pol, _ = pol.Normalize()
		}
	}

	if pol.Meta.Source != "" {
		attrs["policy_source"] = string(pol.Meta.Source)
	}
	if pol.Meta.Normalization.Changed {
		attrs["policy_normalized"] = "true"
		if len(pol.Meta.Normalization.ChangedFields) > 0 {
			attrs["policy_clamped_fields"] = strings.Join(pol.Meta.Normalization.ChangedFields, ",")
		}
	}

	return pol, attrs, nil
}

func failureModeString(mode FailureMode) string {
	switch mode {
	case FailureFallback:
		return "fallback"
	case FailureAllow:
		return "allow"
	case FailureDeny:
		return "deny"
	case FailureAllowUnsafe:
		return "allow_unsafe"
	default:
		return "unknown"
	}
}

func resolvePolicyFast(ctx context.Context, exec *Executor, key policy.PolicyKey) (policy.EffectivePolicy, error) {
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
		// Treat panic as not found or error?
		// If registry panics, we probably can't get the classifier.
		// We treat it as an error resolving classifier.
		// If missingClassifierMode is Deny, we return error.
		// If Fallback, we use default.

		// Let's use the missingClassifierMode logic but with the panic error.
		meta.notFound = true
		// Actually panic in registry is distinct from "not found".
		// But for resilience, falling back is good if configured.

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
