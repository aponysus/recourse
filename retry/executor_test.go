package retry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aponysus/recourse/controlplane"
	"github.com/aponysus/recourse/policy"
)

func TestExecutor_Do_Trivial(t *testing.T) {
	exec := NewExecutorFromOptions(ExecutorOptions{})
	called := false
	err := exec.Do(context.Background(), policy.PolicyKey{}, func(context.Context) error {
		called = true
		return nil
	})
	if err != nil || !called {
		t.Fatalf("unexpected result: err=%v called=%v", err, called)
	}
}

func TestExecutor_Do_MaxAttempts_One(t *testing.T) {
	key := policy.PolicyKey{Name: "x"}
	exec := newTestExecutor(t, key, policy.EffectivePolicy{
		Key: key,
		Retry: policy.RetryPolicy{
			MaxAttempts: 1,
		},
	})

	calls := 0
	err := exec.Do(context.Background(), key, func(context.Context) error {
		calls++
		return errors.New("nope")
	})
	if calls != 1 {
		t.Fatalf("calls=%d, want 1", calls)
	}
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestExecutor_Do_MaxAttempts_Three(t *testing.T) {
	key := policy.PolicyKey{Name: "x"}
	exec := newTestExecutor(t, key, policy.EffectivePolicy{
		Key: key,
		Retry: policy.RetryPolicy{
			MaxAttempts: 3,
		},
	})

	calls := 0
	err := exec.Do(context.Background(), key, func(context.Context) error {
		calls++
		return errors.New("nope")
	})
	if calls != 3 {
		t.Fatalf("calls=%d, want 3", calls)
	}
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestExecutor_Do_StopsOnSuccess(t *testing.T) {
	key := policy.PolicyKey{Name: "x"}
	exec := newTestExecutor(t, key, policy.EffectivePolicy{
		Key: key,
		Retry: policy.RetryPolicy{
			MaxAttempts: 5,
		},
	})

	calls := 0
	err := exec.Do(context.Background(), key, func(context.Context) error {
		calls++
		if calls < 3 {
			return errors.New("nope")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("err=%v, want nil", err)
	}
	if calls != 3 {
		t.Fatalf("calls=%d, want 3", calls)
	}
}

func TestExecutor_DoValue_StopsOnSuccess(t *testing.T) {
	key := policy.PolicyKey{Name: "x"}
	exec := newTestExecutor(t, key, policy.EffectivePolicy{
		Key: key,
		Retry: policy.RetryPolicy{
			MaxAttempts: 5,
		},
	})

	calls := 0
	val, err := DoValue(context.Background(), exec, key, func(context.Context) (int, error) {
		calls++
		if calls < 3 {
			return 0, errors.New("nope")
		}
		return 42, nil
	})
	if err != nil {
		t.Fatalf("err=%v, want nil", err)
	}
	if val != 42 {
		t.Fatalf("val=%d, want 42", val)
	}
	if calls != 3 {
		t.Fatalf("calls=%d, want 3", calls)
	}
}

func TestExecutor_Backoff_NoJitter_ExactSequence(t *testing.T) {
	key := policy.PolicyKey{Name: "x"}
	exec := newTestExecutor(t, key, policy.EffectivePolicy{
		Key: key,
		Retry: policy.RetryPolicy{
			MaxAttempts:       3,
			InitialBackoff:    10 * time.Millisecond,
			MaxBackoff:        250 * time.Millisecond,
			BackoffMultiplier: 2,
			Jitter:            policy.JitterNone,
		},
	})

	var sleeps []time.Duration
	exec.sleep = func(context.Context, time.Duration) error {
		t.Fatalf("sleep should be overridden")
		return nil
	}
	exec.sleep = func(_ context.Context, d time.Duration) error {
		sleeps = append(sleeps, d)
		return nil
	}

	calls := 0
	err := exec.Do(context.Background(), key, func(context.Context) error {
		calls++
		return errors.New("nope")
	})
	if calls != 3 {
		t.Fatalf("calls=%d, want 3", calls)
	}
	if err == nil {
		t.Fatalf("expected error")
	}
	if len(sleeps) != 2 {
		t.Fatalf("sleeps=%v, want 2 entries", sleeps)
	}
	if sleeps[0] != 10*time.Millisecond || sleeps[1] != 20*time.Millisecond {
		t.Fatalf("sleeps=%v, want [10ms 20ms]", sleeps)
	}
}

func TestExecutor_Backoff_JitterFull_Bounds(t *testing.T) {
	key := policy.PolicyKey{Name: "x"}
	exec := newTestExecutor(t, key, policy.EffectivePolicy{
		Key: key,
		Retry: policy.RetryPolicy{
			MaxAttempts:       3,
			InitialBackoff:    10 * time.Millisecond,
			MaxBackoff:        250 * time.Millisecond,
			BackoffMultiplier: 2,
			Jitter:            policy.JitterFull,
		},
	})

	var sleeps []time.Duration
	exec.sleep = func(_ context.Context, d time.Duration) error {
		sleeps = append(sleeps, d)
		return nil
	}

	_ = exec.Do(context.Background(), key, func(context.Context) error { return errors.New("nope") })

	if len(sleeps) != 2 {
		t.Fatalf("sleeps=%v, want 2 entries", sleeps)
	}
	if sleeps[0] < 0 || sleeps[0] > 10*time.Millisecond {
		t.Fatalf("sleeps[0]=%v, want in [0, 10ms]", sleeps[0])
	}
	if sleeps[1] < 0 || sleeps[1] > 20*time.Millisecond {
		t.Fatalf("sleeps[1]=%v, want in [0, 20ms]", sleeps[1])
	}
}

func TestExecutor_Backoff_JitterEqual_Bounds(t *testing.T) {
	key := policy.PolicyKey{Name: "x"}
	exec := newTestExecutor(t, key, policy.EffectivePolicy{
		Key: key,
		Retry: policy.RetryPolicy{
			MaxAttempts:       3,
			InitialBackoff:    10 * time.Millisecond,
			MaxBackoff:        250 * time.Millisecond,
			BackoffMultiplier: 2,
			Jitter:            policy.JitterEqual,
		},
	})

	var sleeps []time.Duration
	exec.sleep = func(_ context.Context, d time.Duration) error {
		sleeps = append(sleeps, d)
		return nil
	}

	_ = exec.Do(context.Background(), key, func(context.Context) error { return errors.New("nope") })

	if len(sleeps) != 2 {
		t.Fatalf("sleeps=%v, want 2 entries", sleeps)
	}
	if sleeps[0] < 5*time.Millisecond || sleeps[0] > 10*time.Millisecond {
		t.Fatalf("sleeps[0]=%v, want in [5ms, 10ms]", sleeps[0])
	}
	if sleeps[1] < 10*time.Millisecond || sleeps[1] > 20*time.Millisecond {
		t.Fatalf("sleeps[1]=%v, want in [10ms, 20ms]", sleeps[1])
	}
}

func TestExecutor_TimeoutPerAttempt_Retries(t *testing.T) {
	key := policy.PolicyKey{Name: "x"}
	exec := newTestExecutor(t, key, policy.EffectivePolicy{
		Key: key,
		Retry: policy.RetryPolicy{
			MaxAttempts:       3,
			TimeoutPerAttempt: 5 * time.Millisecond,
		},
	})

	calls := 0
	err := exec.Do(context.Background(), key, func(ctx context.Context) error {
		calls++
		<-ctx.Done()
		return ctx.Err()
	})
	if calls != 3 {
		t.Fatalf("calls=%d, want 3", calls)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v, want deadline exceeded", err)
	}
}

func TestExecutor_OverallTimeout_StopsLoop(t *testing.T) {
	key := policy.PolicyKey{Name: "x"}
	exec := newTestExecutor(t, key, policy.EffectivePolicy{
		Key: key,
		Retry: policy.RetryPolicy{
			MaxAttempts:    10,
			OverallTimeout: 5 * time.Millisecond,
			InitialBackoff: 100 * time.Millisecond,
		},
	})

	sleepCalled := make(chan struct{})
	exec.sleep = func(ctx context.Context, _ time.Duration) error {
		close(sleepCalled)
		<-ctx.Done()
		return ctx.Err()
	}

	calls := 0
	err := exec.Do(context.Background(), key, func(context.Context) error {
		calls++
		return errors.New("nope")
	})
	<-sleepCalled

	if calls != 1 {
		t.Fatalf("calls=%d, want 1", calls)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v, want deadline exceeded", err)
	}
}

func TestExecutor_ContextCanceledBeforeFirstAttempt_ZeroCalls(t *testing.T) {
	key := policy.PolicyKey{Name: "x"}
	exec := newTestExecutor(t, key, policy.EffectivePolicy{
		Key: key,
		Retry: policy.RetryPolicy{
			MaxAttempts: 3,
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	calls := 0
	err := exec.Do(ctx, key, func(context.Context) error {
		calls++
		return nil
	})
	if calls != 0 {
		t.Fatalf("calls=%d, want 0", calls)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v, want canceled", err)
	}
}

func TestExecutor_ContextCanceledDuringSleep(t *testing.T) {
	key := policy.PolicyKey{Name: "x"}
	exec := newTestExecutor(t, key, policy.EffectivePolicy{
		Key: key,
		Retry: policy.RetryPolicy{
			MaxAttempts:    3,
			InitialBackoff: 100 * time.Millisecond,
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	sleepCalled := make(chan struct{})
	exec.sleep = func(ctx context.Context, _ time.Duration) error {
		close(sleepCalled)
		<-ctx.Done()
		return ctx.Err()
	}

	calls := 0
	done := make(chan error, 1)
	go func() {
		done <- exec.Do(ctx, key, func(context.Context) error {
			calls++
			return errors.New("nope")
		})
	}()

	<-sleepCalled
	cancel()
	err := <-done

	if calls != 1 {
		t.Fatalf("calls=%d, want 1", calls)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v, want canceled", err)
	}
}

func TestExecutor_MissingPolicyMode_Allow(t *testing.T) {
	key := policy.PolicyKey{Name: "x"}
	exec := NewExecutorFromOptions(ExecutorOptions{
		Provider:          stubProvider{err: controlplane.ErrProviderUnavailable},
		MissingPolicyMode: FailureAllow,
	})
	exec.sleep = func(context.Context, time.Duration) error { return nil }

	calls := 0
	_ = exec.Do(context.Background(), key, func(context.Context) error {
		calls++
		return errors.New("nope")
	})
	if calls != 1 {
		t.Fatalf("calls=%d, want 1", calls)
	}
}

func TestExecutor_MissingPolicyMode_Deny(t *testing.T) {
	key := policy.PolicyKey{Name: "x"}
	exec := NewExecutorFromOptions(ExecutorOptions{
		Provider:          stubProvider{err: controlplane.ErrProviderUnavailable},
		MissingPolicyMode: FailureDeny,
	})
	exec.sleep = func(context.Context, time.Duration) error { return nil }

	calls := 0
	err := exec.Do(context.Background(), key, func(context.Context) error {
		calls++
		return nil
	})
	if calls != 0 {
		t.Fatalf("calls=%d, want 0", calls)
	}
	if !errors.Is(err, ErrNoPolicy) {
		t.Fatalf("err=%v, want ErrNoPolicy", err)
	}
}

func TestExecutor_MissingPolicyMode_Fallback_PrefersReturnedPolicy(t *testing.T) {
	key := policy.PolicyKey{Name: "x"}
	exec := NewExecutorFromOptions(ExecutorOptions{
		Provider: stubProvider{
			pol: policy.EffectivePolicy{
				Key: key,
				Retry: policy.RetryPolicy{
					MaxAttempts: 5,
				},
			},
			err: controlplane.ErrProviderUnavailable,
		},
		MissingPolicyMode: FailureFallback,
	})
	exec.sleep = func(context.Context, time.Duration) error { return nil }

	calls := 0
	_ = exec.Do(context.Background(), key, func(context.Context) error {
		calls++
		return errors.New("nope")
	})
	if calls != 5 {
		t.Fatalf("calls=%d, want 5", calls)
	}
}

func newTestExecutor(t *testing.T, key policy.PolicyKey, pol policy.EffectivePolicy) *Executor {
	t.Helper()

	exec := NewExecutorFromOptions(ExecutorOptions{
		Provider: &controlplane.StaticProvider{
			Policies: map[policy.PolicyKey]policy.EffectivePolicy{
				key: pol,
			},
		},
	})
	exec.sleep = func(context.Context, time.Duration) error { return nil }
	return exec
}

type stubProvider struct {
	pol policy.EffectivePolicy
	err error
}

func (p stubProvider) GetEffectivePolicy(context.Context, policy.PolicyKey) (policy.EffectivePolicy, error) {
	return p.pol, p.err
}
