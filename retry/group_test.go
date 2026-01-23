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

	_, err, out, success := exec.doRetryGroup(
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

	val, err, out, success := exec.doRetryGroup(
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

	_, err, out, success := exec.doRetryGroup(
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

	_, err, out, success := exec.doRetryGroup(
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
