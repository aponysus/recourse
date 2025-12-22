package retry

import (
	"context"
	"errors"
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
	groupCtx, cancelGroup := context.WithCancel(ctx)
	defer cancelGroup()

	// Track active attempts
	var activeAttempts atomic.Int32
	var attemptsLaunched atomic.Int32

	// Helper to launch attempt
	launch := func(idx int, isHedge bool) {
		activeAttempts.Add(1)
		attemptsLaunched.Add(1)

		go func() {
			defer activeAttempts.Add(-1)

			start := e.clock()

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
				// Ensure we can cancel this specific attempt if needed?
				// groupCtx handles it.
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
				e.observer.OnHedgeSpawn(attemptCtx, key, observe.AttemptRecord{
					Attempt:    retryIdx,
					IsHedge:    true,
					HedgeIndex: idx,
				})
			}

			// Execute
			var val any
			var err error

			// Safe execution with panic recovery is handled inside... wait, we need to call op.
			// op expects T. We have `OperationValue[any]` forced cast wrapper?
			// Caller will wrap op to return `any`.
			val, err = op(attemptCtx)

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

			// Send result
			// Non-blocking send? No, buffered channel.
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
		if pol.Hedge.TriggerName != "" && e.triggers != nil {
			var ok bool
			trig, ok = e.triggers.Get(pol.Hedge.TriggerName)
			_ = ok // If not found, fall back to FixedDelay? Or just rely on loop?
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

					// If we spawned, we might need to spawn another immediately
					// or calculate the delay for the next one.
					// Check again immediately (but respect maxHedges loop check).
					// Drain channel if needed before Reset?
					// NewTimer(0) fires immediately.
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
			// While valid hedges *might* spawn later if we waited, failure of the Primary
			// usually suggests we should proceed to the next Retry step rather than waiting
			// for speculative hedges, unless we strictly hedge *failures* (which is not this mode).

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
