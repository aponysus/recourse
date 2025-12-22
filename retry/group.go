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
	op OperationValue[any], // Generic machinery uses 'any' usually, or we use a closure? DoValue is generic T.
	// We need doRetryGroup to be generic or cast?
	// Methods on structs cannot have type parameters.
	// So doRetryGroup must be a function or we use 'any'.
	// Using 'any' and casting in caller is easier for internal method.
	pol policy.EffectivePolicy,
	retryIdx int,
	classifier classify.Classifier,
	cmeta classifierMeta,
	lastBackoff time.Duration,
	recordAttempt func(context.Context, observe.AttemptRecord),
) (any, error, classify.Outcome, bool) {

	// If hedging is disabled, run simpler logic (but same coordination to unify code paths?
	// Or explicitly optimize? Phase 1 says "Integrated as parallel attempts".
	// Integrating trivial case (0 hedges) into same logic is fine.

	maxHedges := 0
	if pol.Hedge.Enabled {
		maxHedges = pol.Hedge.MaxHedges
	}

	results := make(chan groupResult[any], 1+maxHedges)

	// Cancellation context for the group.
	// Use WithCancelCause if available? Go 1.20+.
	// Assuming modern Go.
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

			// For hedges, if we exceeded max hedges, should we stop?
			// The trigger logic handles timing, but we enforce hard limit here?
			// attemptsLaunched includes primary.
			// If isHedge=true, idx > 0.

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

				recordAttempt(groupCtx, rec) // Use groupCtx or parent ctx?
				// denied attempts don't have their own context really.

				// Send "failure" to channel so we don't hang?
				results <- groupResult[any]{
					err:     errors.New(decision.Reason), // Sentinel?
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

			// Annotate Context
			attemptCtx = observe.WithAttemptInfo(attemptCtx, observe.AttemptInfo{
				RetryIndex: retryIdx,
				Attempt:    retryIdx,
				// Standard meaning: RetryIndex is which retry we are on.
				// Attempt index in timeline is handled by recordAttempt usually appending.
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
	// We need a timer loop that checks the trigger.
	start := e.clock()

	// Assuming single threaded coordination for spawning
	go func() {
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
		ticker := time.NewTicker(25 * time.Millisecond) // Default check interval
		defer ticker.Stop()

		for {
			select {
			case <-groupCtx.Done():
				return
			case <-ticker.C:
				if hedgesLaunched >= maxHedges {
					return
				}

				state := hedge.HedgeState{
					AttemptStart:     start,
					AttemptsLaunched: 1 + hedgesLaunched, // Primary + previous hedges
					MaxHedges:        maxHedges,
					Elapsed:          e.clock().Sub(start), // Use wall clock usually? e.clock for tests.
				}

				should, nextCheck := trig.ShouldSpawnHedge(state)
				if should {
					hedgesLaunched++
					launch(hedgesLaunched, true)
				}

				if nextCheck > 0 {
					ticker.Reset(nextCheck)
				}
			}
		}
	}()

	// 3. Wait for Results
	// We wait until:
	// - Success
	// - All attempts fail
	// - FailFast triggers

	// Wait, activeAttempts is atomic.
	// But we don't know total attempts in advance due to dynamic spawning.

	// We collect failures.
	var lastRel groupResult[any]
	failures := 0

	// We need to know when "all attempts that WILL run have finished".
	// This covers:
	// 1. Primary finished.
	// 2. Hedges finished.
	// 3. No more hedges will naturally spawn (time constraint?) OR we cancel remaining.

	// Simplified logic:
	// We loop until `failures == attemptsLaunched` AND `no more hedges can spawn`?
	// Or we use the channel.

	// Problem: `attemptsLaunched` is dynamic.
	// We can loop endlessly on `results` channel?
	// But when do we stop if all fail?
	// We need to track "potential attempts".

	// If CancelOnFirstTerminal is set:
	// - On ANY terminal failure, we abort group (return failure).

	// If NO valid result yet:
	// - If successful, return immediately.
	// - If failure, increment failures.
	// - If failures == current_active_and_launched?

	// Workaround:
	// We only return when:
	// A) Success
	// B) CancelOnFirstTerminal && Ternimal Failure
	// C) All attempts failed. How to detect "All"?
	//    - Active attempts == 0 AND (hedging done OR timeout)

	// Let's use a simpler approach for Phase 1:
	// We don't strictly wait for "all hedges that MIGHT have spawned".
	// If primary fails, and we are waiting for hedge...
	// If we just return primary failure, subsequent hedge is wasted?
	// The point of hedging is to recover.
	// So we MUST wait if a hedge is *running* or *pending*.

	// This suggests we iterate `maxHedges + 1` times on the channel?
	// No, because we might not spawn all.

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
			// If no active attempts, AND (max hedges reached OR primary failed long ago?)
			// Actually, if active == 0, are we done?
			// Not necessarily. The timer might spawn a new one in 10ms.
			// But if Primary failed, and elapsed < HedgeDelay, active=0.
			// Should we exit?
			// If we exit, we retry (outer loop).
			// If we wait, we might spawn hedge.
			// This is "Retry vs Hedge".
			// If Primary fails FAST (before hedge delay), usually we just Retry immediately (next loop).
			// Hedging is for SLOW requests.
			// If Primary fails, it's not "slow", it's "failed".
			// So yes, if active==0, we should typically exit.
			// UNLESS: We want to hedge *failures*?
			// "Hedging" usually targets latency (timeout/slow).
			// "Retries" target failures.
			// So: If all current attempts failed, and we have no active attempts...
			// Should we wait for next hedge timer?
			// Usually NO. If primary failed, we go to next Retry attempt.

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
