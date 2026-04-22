package retry

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aponysus/recourse/budget"
	"github.com/aponysus/recourse/classify"
	"github.com/aponysus/recourse/hedge"
	"github.com/aponysus/recourse/observe"
	"github.com/aponysus/recourse/policy"
)

type groupResult[T any] struct {
	val      T
	err      error
	outcome  classify.Outcome
	start    time.Time
	end      time.Time
	isHedge  bool
	idx      int
	panicErr error
}

var (
	errHedgeWinnerSuccess = errors.New("winner_success")
	errHedgeGroupFinished = errors.New("hedge_group_finished")
)

type hedgeAttemptState struct {
	mu             sync.Mutex
	ctx            context.Context
	rec            observe.AttemptRecord
	completed      bool
	cancelNotified bool
}

func newHedgeAttemptState(ctx context.Context, attempt int, hedgeIndex int, start time.Time) *hedgeAttemptState {
	return &hedgeAttemptState{
		ctx: ctx,
		rec: observe.AttemptRecord{
			Attempt:    attempt,
			StartTime:  start,
			IsHedge:    true,
			HedgeIndex: hedgeIndex,
		},
	}
}

func (h *hedgeAttemptState) markCompleted() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.completed = true
}

func (h *hedgeAttemptState) cancelRecord(end time.Time) (context.Context, observe.AttemptRecord, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.completed || h.cancelNotified {
		return nil, observe.AttemptRecord{}, false
	}

	h.cancelNotified = true
	rec := h.rec
	rec.EndTime = end
	return h.ctx, rec, true
}

// doRetryGroup executes a primary attempt and optional hedged attempts.
// It returns the result of the "winning" attempt.
func (e *Executor) doRetryGroup(
	ctx context.Context,
	key policy.PolicyKey,
	// Generic helper for concurrent operations.
	op OperationValue[any],
	pol policy.EffectivePolicy,
	retryIdx int,
	classifier classify.Classifier,
	cmeta classifierMeta,
	lastBackoff time.Duration,
	recordAttempt func(context.Context, observe.AttemptRecord),
) (any, error, classify.Outcome, bool) {

	// Check if hedging is enabled.

	maxHedges := 0
	if pol.Hedge.Enabled {
		maxHedges = pol.Hedge.MaxHedges
	}

	results := make(chan groupResult[any], 1+maxHedges)

	// Group-level context for cancellation.
	groupCtx, cancelGroup := context.WithCancelCause(ctx)

	// Track active attempts
	var activeAttempts atomic.Int32
	var attemptsLaunched atomic.Int32
	var hedgeStatesMu sync.Mutex
	hedgeStates := make([]*hedgeAttemptState, 0, maxHedges)
	var finishOnce sync.Once
	finishGroup := func(cause error) {
		finishOnce.Do(func() {
			if cause == nil {
				if ctx.Err() != nil {
					cause = context.Cause(ctx)
				} else {
					cause = errHedgeGroupFinished
				}
			}

			cancelGroup(cause)
			reason := hedgeCancelReason(cause)
			if reason == "" {
				return
			}

			hedgeStatesMu.Lock()
			states := append([]*hedgeAttemptState(nil), hedgeStates...)
			hedgeStatesMu.Unlock()

			for _, state := range states {
				cancelCtx, rec, ok := state.cancelRecord(e.clock())
				if !ok {
					continue
				}
				rec.Err = hedgeCancelError(reason)
				e.observer.OnHedgeCancel(cancelCtx, key, rec, reason)
			}
		})
	}
	defer finishGroup(nil)

	// Helper to launch attempt
	launch := func(idx int, isHedge bool) {
		activeAttempts.Add(1)
		attemptsLaunched.Add(1)

		go func() {
			defer activeAttempts.Add(-1)

			start := e.clock()
			var hedgeState *hedgeAttemptState

			// Budget Check
			budgetKind := budget.KindRetry
			budgetRef := pol.Retry.Budget
			if isHedge {
				budgetKind = budget.KindHedge
				budgetRef = pol.Hedge.Budget
			}

			// Check budget for this attempt.

			// AllowAttempt
			decision, allowed := e.allowAttempt(groupCtx, key, budgetRef, retryIdx, budgetKind) // retryIdx is constant for group
			if !allowed {
				// Record budget denial
				rec := observe.AttemptRecord{
					Attempt:       retryIdx,
					StartTime:     start,
					EndTime:       e.clock(),
					IsHedge:       isHedge,
					HedgeIndex:    idx, // 0 for primary, 1..N for hedges
					Outcome:       classify.Outcome{Kind: classify.OutcomeAbort, Reason: decision.Reason},
					BudgetAllowed: false,
					BudgetReason:  decision.Reason,
					Backoff:       lastBackoff, // For primary only?
				}
				if isHedge {
					rec.Backoff = 0 // Hedges don't strictly have "backoff" from previous retry
				}

				recordAttempt(groupCtx, rec)
				results <- groupResult[any]{
					err:     errors.New(decision.Reason),
					outcome: classify.Outcome{Kind: classify.OutcomeAbort, Reason: decision.Reason},
					start:   start,
					end:     e.clock(),
					isHedge: isHedge,
					idx:     idx,
				}
				return
			}

			release := decision.Release
			defer func() {
				if release != nil {
					release()
				}
			}()

			// Attempt Context
			attemptCtx := groupCtx
			var cancelAttempt context.CancelFunc
			if pol.Retry.TimeoutPerAttempt > 0 {
				attemptCtx, cancelAttempt = context.WithTimeout(groupCtx, pol.Retry.TimeoutPerAttempt)
			} else {
				cancelAttempt = func() {}
			}
			defer cancelAttempt()

			attemptCtx = observe.WithAttemptInfo(attemptCtx, observe.AttemptInfo{
				RetryIndex: retryIdx,
				Attempt:    retryIdx,
				IsHedge:    isHedge,
				HedgeIndex: idx,
				PolicyID:   pol.ID,
			})

			if isHedge {
				hedgeStatesMu.Lock()
				if groupCtx.Err() != nil {
					hedgeStatesMu.Unlock()
					return
				}
				hedgeState = newHedgeAttemptState(attemptCtx, retryIdx, idx, start)
				hedgeStates = append(hedgeStates, hedgeState)
				hedgeStatesMu.Unlock()

				e.observer.OnHedgeSpawn(attemptCtx, key, hedgeState.rec)
			}

			// Execute
			var val any
			var err error
			val, err = op(attemptCtx)
			if hedgeState != nil {
				hedgeState.markCompleted()
			}

			end := e.clock()

			// Classify
			outcome, panicErr := classifyWithRecovery(e.recoverPanics, classifier, val, err, key)
			annotateClassifierFallback(&outcome, cmeta)

			// Record
			rec := observe.AttemptRecord{
				Attempt:       retryIdx,
				StartTime:     start,
				EndTime:       end,
				Outcome:       outcome,
				Err:           err,
				Backoff:       lastBackoff, // Only meaningful for primary
				BudgetAllowed: true,
				BudgetReason:  decision.Reason,
				IsHedge:       isHedge,
				HedgeIndex:    idx,
			}
			if isHedge {
				rec.Backoff = 0
			}
			recordAttempt(attemptCtx, rec)

			res := groupResult[any]{
				val:      val,
				err:      err,
				outcome:  outcome,
				start:    start,
				end:      end,
				isHedge:  isHedge,
				idx:      idx,
				panicErr: panicErr,
			}

			results <- res
		}()
	}

	// 1. Launch Primary
	launch(0, false)

	// 2. Hedge Loop
	start := e.clock()
	go func() {
		// Assuming single threaded coordination for spawning
		if !pol.Hedge.Enabled {
			return
		}

		// Find trigger
		var trig hedge.Trigger
		if pol.Hedge.TriggerName != "" {
			var ok bool
			if e.triggers != nil {
				trig, ok = e.triggers.Get(pol.Hedge.TriggerName)
			}
			if !ok {
				switch e.missingTriggerMode {
				case FailureFallback:
					trig = hedge.FixedDelayTrigger{Delay: pol.Hedge.HedgeDelay}
				case FailureAllow, FailureAllowUnsafe, FailureDeny:
					return
				default:
					return
				}
			}
		}

		// Fallback to fixed delay if no trigger found or Logic
		if trig == nil {
			trig = hedge.FixedDelayTrigger{Delay: pol.Hedge.HedgeDelay}
		}

		// Loop
		hedgesLaunched := 0
		// Use a Timer based on nextCheck values from the trigger.
		// Start with immediate check.
		timer := time.NewTimer(0)
		defer timer.Stop()

		for {
			select {
			case <-groupCtx.Done():
				return
			case <-timer.C:
				if hedgesLaunched >= maxHedges {
					return
				}

				state := hedge.HedgeState{
					AttemptStart:     start,
					AttemptsLaunched: 1 + hedgesLaunched, // Primary + previous hedges
					MaxHedges:        maxHedges,
					Elapsed:          e.clock().Sub(start),
					Snapshot:         e.getTracker(key).Snapshot(),
					HedgeDelay:       pol.Hedge.HedgeDelay,
				}

				should, nextCheck := trig.ShouldSpawnHedge(state)
				if should {
					hedgesLaunched++
					launch(hedgesLaunched, true)

					// Re-check immediately to allow back-to-back hedges.
					if hedgesLaunched < maxHedges {
						timer.Reset(0)
					}
					continue
				}

				// If we shouldn't spawn yet, wait using the returned nextCheck.
				if nextCheck <= 0 {
					// Trigger didn't return a wait time (e.g. waiting for stats or invalid).
					// Poll to avoid stalling if stats might appear.
					nextCheck = 25 * time.Millisecond
				}
				timer.Reset(nextCheck)
			}
		}
	}()

	// Wait for results. We return as soon as:
	// 1. A success is received (wins).
	// 2. Cancellation occurs.
	// 3. All attempts fail.
	// 4. Fail-fast threshold is reached.

	var lastRel groupResult[any]
	failures := 0

	for {
		select {
		case res := <-results:
			if res.outcome.Kind == classify.OutcomeSuccess {
				finishGroup(errHedgeWinnerSuccess)
				return res.val, nil, res.outcome, true
			}

			// It's a failure
			lastRel = res
			failures++

			// Fail Fast check
			if pol.Hedge.CancelOnFirstTerminal {
				if res.outcome.Kind == classify.OutcomeNonRetryable || res.outcome.Kind == classify.OutcomeAbort {
					return res.val, res.err, res.outcome, false
				}
			}

			// Check if we are done
			active := activeAttempts.Load()
			// Check if all active attempts have finished.
			// If active=0, it means all launched attempts (primary + any hedges so far) have failed.
			// While valid hedges might spawn later if we waited, failure of the Primary
			// usually suggests we should proceed to the next Retry step rather than waiting
			// for speculative hedges, unless we strictly hedge failures (which is not this mode).

			if active == 0 {
				// All launched attempts failed.
				return lastRel.val, lastRel.err, lastRel.outcome, false
			}

		// If active > 0, we have hope. Continue waiting.

		case <-ctx.Done(): // Outer context cancelled
			return nil, ctx.Err(), classify.Outcome{Kind: classify.OutcomeAbort, Reason: "context_canceled"}, false
		}
	}
}

func hedgeCancelReason(cause error) string {
	switch {
	case errors.Is(cause, errHedgeWinnerSuccess):
		return "winner_success"
	case errors.Is(cause, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(cause, context.Canceled):
		return "context_canceled"
	default:
		return ""
	}
}

func hedgeCancelError(reason string) error {
	switch reason {
	case "deadline_exceeded":
		return context.DeadlineExceeded
	case "winner_success", "context_canceled":
		return context.Canceled
	default:
		return nil
	}
}
