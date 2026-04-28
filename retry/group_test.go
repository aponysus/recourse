package retry

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aponysus/recourse/classify"
	"github.com/aponysus/recourse/hedge"
	"github.com/aponysus/recourse/observe"
	"github.com/aponysus/recourse/policy"
)

type nonRetryableClassifier struct{}

func (nonRetryableClassifier) Classify(any, error) classify.Outcome {
	return classify.Outcome{Kind: classify.OutcomeNonRetryable, Reason: "non_retryable"}
}

type successClassifier struct{}

func (successClassifier) Classify(any, error) classify.Outcome {
	return classify.Outcome{Kind: classify.OutcomeSuccess, Reason: "success"}
}

type signalTrigger struct {
	calls  atomic.Int32
	signal chan struct{}
}

func (t *signalTrigger) ShouldSpawnHedge(hedge.HedgeState) (bool, time.Duration) {
	if t.calls.Add(1) == 1 && t.signal != nil {
		close(t.signal)
	}
	return false, 0
}

func TestDoRetryGroup_CancelOnFirstTerminal(t *testing.T) {
	exec := NewExecutorFromOptions(ExecutorOptions{})
	key := policy.PolicyKey{Name: "op"}
	pol := policy.EffectivePolicy{
		Retry: policy.RetryPolicy{MaxAttempts: 1},
		Hedge: policy.HedgePolicy{
			Enabled:               true,
			MaxHedges:             1,
			HedgeDelay:            50 * time.Millisecond,
			CancelOnFirstTerminal: true,
		},
	}

	recordAttempt := func(context.Context, observe.AttemptRecord) {}
	op := func(context.Context) (any, error) { return nil, errors.New("nope") }

	_, out, success, err := exec.doRetryGroup(
		context.Background(),
		key,
		op,
		pol,
		0,
		nonRetryableClassifier{},
		classifierMeta{},
		0,
		recordAttempt,
	)

	if success {
		t.Fatalf("expected failure")
	}
	if err == nil {
		t.Fatalf("expected error")
	}
	if out.Kind != classify.OutcomeNonRetryable {
		t.Fatalf("out=%+v, want non-retryable", out)
	}
}

func TestDoRetryGroup_TriggerNextCheckDefault(t *testing.T) {
	trigger := &signalTrigger{signal: make(chan struct{})}
	triggers := hedge.NewRegistry()
	triggers.Register("signal", trigger)

	exec := NewExecutorFromOptions(ExecutorOptions{Triggers: triggers})
	key := policy.PolicyKey{Name: "op"}
	pol := policy.EffectivePolicy{
		Retry: policy.RetryPolicy{MaxAttempts: 1},
		Hedge: policy.HedgePolicy{
			Enabled:     true,
			MaxHedges:   1,
			TriggerName: "signal",
			HedgeDelay:  10 * time.Millisecond,
		},
	}

	recordAttempt := func(context.Context, observe.AttemptRecord) {}
	op := func(context.Context) (any, error) {
		select {
		case <-trigger.signal:
			return "ok", nil
		case <-time.After(200 * time.Millisecond):
			return "", errors.New("trigger not called")
		}
	}

	val, out, success, err := exec.doRetryGroup(
		context.Background(),
		key,
		op,
		pol,
		0,
		successClassifier{},
		classifierMeta{},
		0,
		recordAttempt,
	)

	if !success || err != nil {
		t.Fatalf("success=%v err=%v, want success", success, err)
	}
	if val.(string) != "ok" {
		t.Fatalf("val=%v, want ok", val)
	}
	if out.Kind != classify.OutcomeSuccess {
		t.Fatalf("out=%+v, want success", out)
	}
	if trigger.calls.Load() == 0 {
		t.Fatalf("expected trigger to be consulted")
	}
}

func TestDoRetryGroup_ContextCanceled(t *testing.T) {
	exec := NewExecutorFromOptions(ExecutorOptions{})
	key := policy.PolicyKey{Name: "op"}
	pol := policy.EffectivePolicy{
		Retry: policy.RetryPolicy{MaxAttempts: 1},
	}

	started := make(chan struct{})
	unblock := make(chan struct{})

	op := func(context.Context) (any, error) {
		close(started)
		<-unblock
		return nil, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
	}()

	_, out, success, err := exec.doRetryGroup(
		ctx,
		key,
		op,
		pol,
		0,
		successClassifier{},
		classifierMeta{},
		0,
		func(context.Context, observe.AttemptRecord) {},
	)
	close(unblock)

	if success {
		t.Fatalf("expected failure")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v, want context canceled", err)
	}
	if out.Kind != classify.OutcomeAbort || out.Reason != "context_canceled" {
		t.Fatalf("out=%+v, want abort/context_canceled", out)
	}
}

func TestDoRetryGroup_MissingTriggerModeFallbackSpawnsHedge(t *testing.T) {
	runMissingTriggerModeTest(t, FailureFallback, true)
}

func TestDoRetryGroup_MissingTriggerModeSkipsHedge(t *testing.T) {
	modes := []FailureMode{FailureAllow, FailureAllowUnsafe, FailureDeny}
	for _, mode := range modes {
		t.Run(failureModeString(mode), func(t *testing.T) {
			runMissingTriggerModeTest(t, mode, false)
		})
	}
}

func TestDoRetryGroup_MissingTriggerModeUnknownSkipsHedge(t *testing.T) {
	runMissingTriggerModeTest(t, FailureModeUnknown, false)
}

func TestDoRetryGroup_EmitsOnHedgeCancel_WhenPrimaryWins(t *testing.T) {
	obs := &hedgeCancelObserver{}
	exec := NewExecutorFromOptions(ExecutorOptions{Observer: obs})
	setImmediateTrigger(exec)

	key := policy.PolicyKey{Name: "op"}
	pol := policy.EffectivePolicy{
		Retry: policy.RetryPolicy{MaxAttempts: 1},
		Hedge: policy.HedgePolicy{
			Enabled:     true,
			MaxHedges:   1,
			HedgeDelay:  0,
			TriggerName: "immediate",
		},
	}

	hedgeStarted := make(chan struct{})
	unblockHedge := make(chan struct{})
	var hedgeOnce sync.Once
	errHedgeNotStarted := errors.New("hedge not started")

	op := func(ctx context.Context) (any, error) {
		info, _ := observe.AttemptFromContext(ctx)
		if info.IsHedge {
			hedgeOnce.Do(func() { close(hedgeStarted) })
			<-unblockHedge
			return nil, ctx.Err()
		}

		select {
		case <-hedgeStarted:
			return "primary", nil
		case <-time.After(time.Second):
			return nil, errHedgeNotStarted
		}
	}

	val, out, success, err := exec.doRetryGroup(
		context.Background(),
		key,
		op,
		pol,
		0,
		successClassifier{},
		classifierMeta{},
		0,
		func(context.Context, observe.AttemptRecord) {},
	)
	close(unblockHedge)

	if errors.Is(err, errHedgeNotStarted) {
		t.Fatal("expected hedge to start before primary won")
	}
	if !success || err != nil {
		t.Fatalf("success=%v err=%v out=%+v, want success", success, err, out)
	}
	if val != "primary" {
		t.Fatalf("val=%v, want primary", val)
	}
	if len(obs.spawns) != 1 {
		t.Fatalf("spawns=%d, want 1", len(obs.spawns))
	}
	if len(obs.cancels) != 1 {
		t.Fatalf("cancels=%d, want 1", len(obs.cancels))
	}

	cancel := obs.cancels[0]
	if cancel.reason != "winner_success" {
		t.Fatalf("reason=%q, want winner_success", cancel.reason)
	}
	if !cancel.rec.IsHedge || cancel.rec.HedgeIndex != 1 || cancel.rec.Attempt != 0 {
		t.Fatalf("cancel rec=%+v, want hedge attempt 0 index 1", cancel.rec)
	}
	if cancel.rec.StartTime.IsZero() || cancel.rec.EndTime.IsZero() || cancel.rec.EndTime.Before(cancel.rec.StartTime) {
		t.Fatalf("cancel timing invalid: start=%v end=%v", cancel.rec.StartTime, cancel.rec.EndTime)
	}
	if !errors.Is(cancel.rec.Err, context.Canceled) {
		t.Fatalf("cancel err=%v, want context.Canceled", cancel.rec.Err)
	}
}

func TestDoRetryGroup_EmitsOnHedgeCancel_WhenOuterContextCanceled(t *testing.T) {
	obs := &hedgeCancelObserver{}
	exec := NewExecutorFromOptions(ExecutorOptions{Observer: obs})
	setImmediateTrigger(exec)

	key := policy.PolicyKey{Name: "op"}
	pol := policy.EffectivePolicy{
		Retry: policy.RetryPolicy{MaxAttempts: 1},
		Hedge: policy.HedgePolicy{
			Enabled:     true,
			MaxHedges:   1,
			HedgeDelay:  0,
			TriggerName: "immediate",
		},
	}

	hedgeStarted := make(chan struct{})
	unblock := make(chan struct{})
	var hedgeOnce sync.Once

	op := func(ctx context.Context) (any, error) {
		info, _ := observe.AttemptFromContext(ctx)
		if info.IsHedge {
			hedgeOnce.Do(func() { close(hedgeStarted) })
		}
		<-unblock
		return nil, ctx.Err()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-hedgeStarted:
			cancel()
		case <-time.After(time.Second):
			cancel()
		}
	}()

	_, out, success, err := exec.doRetryGroup(
		ctx,
		key,
		op,
		pol,
		0,
		successClassifier{},
		classifierMeta{},
		0,
		func(context.Context, observe.AttemptRecord) {},
	)
	close(unblock)

	if success {
		t.Fatalf("expected failure")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v, want context.Canceled", err)
	}
	if out.Kind != classify.OutcomeAbort || out.Reason != "context_canceled" {
		t.Fatalf("out=%+v, want abort/context_canceled", out)
	}
	if len(obs.spawns) != 1 {
		t.Fatalf("spawns=%d, want 1", len(obs.spawns))
	}
	if len(obs.cancels) != 1 {
		t.Fatalf("cancels=%d, want 1", len(obs.cancels))
	}

	cancelEvent := obs.cancels[0]
	if cancelEvent.reason != "context_canceled" {
		t.Fatalf("reason=%q, want context_canceled", cancelEvent.reason)
	}
	if !cancelEvent.rec.IsHedge || cancelEvent.rec.HedgeIndex != 1 || cancelEvent.rec.Attempt != 0 {
		t.Fatalf("cancel rec=%+v, want hedge attempt 0 index 1", cancelEvent.rec)
	}
	if cancelEvent.rec.StartTime.IsZero() || cancelEvent.rec.EndTime.IsZero() || cancelEvent.rec.EndTime.Before(cancelEvent.rec.StartTime) {
		t.Fatalf("cancel timing invalid: start=%v end=%v", cancelEvent.rec.StartTime, cancelEvent.rec.EndTime)
	}
	if !errors.Is(cancelEvent.rec.Err, context.Canceled) {
		t.Fatalf("cancel err=%v, want context.Canceled", cancelEvent.rec.Err)
	}
}

func TestHedgeCancelReason(t *testing.T) {
	cases := []struct {
		name  string
		cause error
		want  string
	}{
		{name: "winner", cause: errHedgeWinnerSuccess, want: "winner_success"},
		{name: "deadline", cause: context.DeadlineExceeded, want: "deadline_exceeded"},
		{name: "canceled", cause: context.Canceled, want: "context_canceled"},
		{name: "internal", cause: errHedgeGroupFinished, want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hedgeCancelReason(tc.cause); got != tc.want {
				t.Fatalf("reason=%q, want %q", got, tc.want)
			}
		})
	}
}

func runMissingTriggerModeTest(t *testing.T, mode FailureMode, expectHedge bool) {
	t.Helper()

	exec := NewExecutorFromOptions(ExecutorOptions{})
	exec.missingTriggerMode = mode

	key := policy.PolicyKey{Name: "op"}
	pol := policy.EffectivePolicy{
		Retry: policy.RetryPolicy{MaxAttempts: 1},
		Hedge: policy.HedgePolicy{
			Enabled:     true,
			MaxHedges:   1,
			HedgeDelay:  0,
			TriggerName: "missing",
		},
	}

	hedgeStarted := make(chan struct{})
	var hedgeOnce sync.Once
	op := func(ctx context.Context) (any, error) {
		info, _ := observe.AttemptFromContext(ctx)
		if info.IsHedge {
			hedgeOnce.Do(func() { close(hedgeStarted) })
			return "hedge", nil
		}

		timer := time.NewTimer(50 * time.Millisecond)
		defer func() {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}()

		select {
		case <-hedgeStarted:
		case <-timer.C:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return "primary", nil
	}

	_, out, success, err := exec.doRetryGroup(
		context.Background(),
		key,
		op,
		pol,
		0,
		successClassifier{},
		classifierMeta{},
		0,
		func(context.Context, observe.AttemptRecord) {},
	)

	if !success || err != nil {
		t.Fatalf("success=%v err=%v out=%+v, want success", success, err, out)
	}

	if expectHedge {
		if !waitForSignal(hedgeStarted) {
			t.Fatalf("expected hedge attempt to start")
		}
		return
	}

	if waitForSignal(hedgeStarted) {
		t.Fatalf("did not expect hedge attempt to start")
	}
}

type hedgeCancelObserver struct {
	observe.BaseObserver

	mu      sync.Mutex
	spawns  []observe.AttemptRecord
	cancels []hedgeCancelEvent
}

type hedgeCancelEvent struct {
	rec    observe.AttemptRecord
	reason string
}

func (o *hedgeCancelObserver) OnHedgeSpawn(_ context.Context, _ policy.PolicyKey, rec observe.AttemptRecord) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.spawns = append(o.spawns, rec)
}

func (o *hedgeCancelObserver) OnHedgeCancel(_ context.Context, _ policy.PolicyKey, rec observe.AttemptRecord, reason string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.cancels = append(o.cancels, hedgeCancelEvent{rec: rec, reason: reason})
}
