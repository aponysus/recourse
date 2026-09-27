package controlplane

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aponysus/recourse/policy"
)

// waitingContext signals when a follower starts selecting on its context.
// This ensures callers have joined a blocked fetch before the test releases it.
type waitingContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (c *waitingContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

type policyResult struct {
	pol        policy.EffectivePolicy
	err        error
	panicValue any
}

func startPolicyLookup(p *RemoteProvider, ctx context.Context, key policy.PolicyKey) <-chan policyResult {
	result := make(chan policyResult, 1)
	go func() {
		var r policyResult
		defer func() {
			r.panicValue = recover()
			result <- r
		}()
		r.pol, r.err = p.GetEffectivePolicy(ctx, key)
	}()
	return result
}

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for lookup synchronization")
		var zero T
		return zero
	}
}

func TestRemoteProvider_CoalescesFetches(t *testing.T) {
	fetchErr := errors.New("offline")
	for _, warm := range []bool{false, true} {
		for _, outcome := range []string{"success", "error", "missing", "invalid"} {
			t.Run(fmt.Sprintf("warm=%v/%s", warm, outcome), func(t *testing.T) {
				key := policy.ParseKey("svc.shared")
				clock := &fakeClock{now: time.Unix(0, 0)}
				oldPolicy := policy.DefaultPolicyFor(key)
				oldPolicy.ID = "old"
				oldPolicy.Meta.Source = policy.PolicySourceRemote
				newPolicy := oldPolicy
				newPolicy.ID = "new"
				newPolicy.Retry.MaxAttempts = 5
				started := make(chan struct{}, 32)
				release := make(chan struct{})
				releaseFetch := sync.OnceFunc(func() { close(release) })
				t.Cleanup(releaseFetch)
				var source *MockSource
				source = &MockSource{GetPolicyFunc: func(ctx context.Context, _ policy.PolicyKey) (policy.EffectivePolicy, error) {
					if warm && atomic.LoadInt32(&source.Calls) == 1 {
						return oldPolicy, nil
					}
					started <- struct{}{}
					select {
					case <-ctx.Done():
						return policy.EffectivePolicy{}, ctx.Err()
					case <-release:
					}
					switch outcome {
					case "error":
						return policy.EffectivePolicy{}, fetchErr
					case "missing":
						return policy.EffectivePolicy{}, fmt.Errorf("missing: %w", ErrPolicyNotFound)
					case "invalid":
						invalid := newPolicy
						invalid.Retry.Jitter = "invalid"
						return invalid, nil
					default:
						return newPolicy, nil
					}
				}}
				provider := NewRemoteProvider(source, WithCacheTTL(time.Second), WithLKGTTL(time.Minute))
				provider.cache.nowFn = clock.Now
				wantCalls := int32(1)
				if warm {
					if _, err := provider.GetEffectivePolicy(context.Background(), key); err != nil {
						t.Fatal(err)
					}
					clock.Advance(time.Second)
					wantCalls++
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				t.Cleanup(cancel)
				results := []<-chan policyResult{startPolicyLookup(provider, ctx, key)}
				receive(t, started)
				for i := 0; i < 15; i++ {
					follower := &waitingContext{Context: ctx, waiting: make(chan struct{})}
					results = append(results, startPolicyLookup(provider, follower, key))
					receive(t, follower.waiting)
				}
				releaseFetch()
				for _, result := range results {
					r := receive(t, result)
					if r.panicValue != nil {
						t.Fatalf("unexpected panic: %v", r.panicValue)
					}
					switch {
					case outcome == "error" && !warm:
						if !errors.Is(r.err, fetchErr) {
							t.Fatalf("expected shared source error, got %v", r.err)
						}
					case outcome == "missing":
						if !errors.Is(r.err, ErrPolicyNotFound) {
							t.Fatalf("expected shared missing result, got %v", r.err)
						}
					case outcome == "invalid":
						var normErr *policy.NormalizeError
						if !errors.As(r.err, &normErr) {
							t.Fatalf("expected shared normalization error, got %v", r.err)
						}
					default:
						want := newPolicy
						if outcome == "error" {
							want = oldPolicy
							want.Meta.Source = policy.PolicySourceLKG
						}
						if r.err != nil || r.pol.ID != want.ID || r.pol.Meta.Source != want.Meta.Source || r.pol.Retry.MaxAttempts != want.Retry.MaxAttempts {
							t.Fatalf("got %+v, %v; want %+v", r.pol, r.err, want)
						}
					}
				}
				if calls := atomic.LoadInt32(&source.Calls); calls != wantCalls {
					t.Fatalf("source calls=%d, want %d", calls, wantCalls)
				}
			})
		}
	}
}

func TestRemoteProvider_DifferentKeysFetchIndependently(t *testing.T) {
	// String() is identical for these distinct keys; coalescing must use the
	// complete PolicyKey, as the policy cache does.
	keys := []policy.PolicyKey{{Namespace: "svc", Name: "op"}, {Name: "svc.op"}}
	started := make(chan policy.PolicyKey, 2)
	release := make(chan struct{})
	releaseFetch := sync.OnceFunc(func() { close(release) })
	t.Cleanup(releaseFetch)
	source := &MockSource{GetPolicyFunc: func(ctx context.Context, key policy.PolicyKey) (policy.EffectivePolicy, error) {
		started <- key
		select {
		case <-ctx.Done():
			return policy.EffectivePolicy{}, ctx.Err()
		case <-release:
			return policy.DefaultPolicyFor(key), nil
		}
	}}
	provider := NewRemoteProvider(source)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var results []<-chan policyResult
	for _, key := range keys {
		results = append(results, startPolicyLookup(provider, ctx, key))
		if got := receive(t, started); got != key {
			t.Fatalf("source key=%v, want %v", got, key)
		}
	}
	releaseFetch()
	for i, result := range results {
		r := receive(t, result)
		if r.err != nil || r.pol.Key != keys[i] || r.panicValue != nil {
			t.Fatalf("unexpected result: %+v", r)
		}
	}
}

func TestRemoteProvider_WaiterCancellation(t *testing.T) {
	key := policy.ParseKey("svc.cancel")
	started := make(chan struct{})
	release := make(chan struct{})
	releaseFetch := sync.OnceFunc(func() { close(release) })
	t.Cleanup(releaseFetch)
	source := &MockSource{GetPolicyFunc: func(ctx context.Context, _ policy.PolicyKey) (policy.EffectivePolicy, error) {
		close(started)
		select {
		case <-ctx.Done():
			return policy.EffectivePolicy{}, ctx.Err()
		case <-release:
			return policy.DefaultPolicyFor(key), nil
		}
	}}
	provider := NewRemoteProvider(source)
	leaderCtx, cancelLeader := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelLeader()
	leader := startPolicyLookup(provider, leaderCtx, key)
	receive(t, started)
	waiterCtx, cancelWaiter := context.WithCancel(leaderCtx)
	defer cancelWaiter()
	waiter := &waitingContext{Context: waiterCtx, waiting: make(chan struct{})}
	canceled := startPolicyLookup(provider, waiter, key)
	receive(t, waiter.waiting)
	cancelWaiter()
	if r := receive(t, canceled); !errors.Is(r.err, context.Canceled) {
		t.Fatalf("waiter should cancel before source completes, got %+v", r)
	}
	healthy := &waitingContext{Context: leaderCtx, waiting: make(chan struct{})}
	follower := startPolicyLookup(provider, healthy, key)
	receive(t, healthy.waiting)
	releaseFetch()
	for _, result := range []<-chan policyResult{leader, follower} {
		if r := receive(t, result); r.err != nil || r.panicValue != nil || r.pol.Key != key {
			t.Fatalf("canceling waiter must not cancel shared fetch, got %+v", r)
		}
	}
	if calls := atomic.LoadInt32(&source.Calls); calls != 1 {
		t.Fatalf("source calls=%d, want 1", calls)
	}
}

func TestRemoteProvider_LeaderCancellationDoesNotServeLKG(t *testing.T) {
	key := policy.ParseKey("svc.cancel")
	started := make(chan struct{})
	var source *MockSource
	source = &MockSource{GetPolicyFunc: func(ctx context.Context, _ policy.PolicyKey) (policy.EffectivePolicy, error) {
		if atomic.LoadInt32(&source.Calls) != 2 {
			return policy.DefaultPolicyFor(key), nil
		}
		close(started)
		<-ctx.Done()
		// Even a source that reports not-found after cancellation must not
		// discard a good policy or turn cancellation into LKG success.
		return policy.EffectivePolicy{}, ErrPolicyNotFound
	}}
	provider := NewRemoteProvider(source, WithCacheTTL(0), WithLKGTTL(time.Minute))
	if _, err := provider.GetEffectivePolicy(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	leaderCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	leader := startPolicyLookup(provider, leaderCtx, key)
	receive(t, started)
	followerCtx := &waitingContext{Context: context.Background(), waiting: make(chan struct{})}
	follower := startPolicyLookup(provider, followerCtx, key)
	receive(t, followerCtx.waiting)
	cancel()
	for _, result := range []<-chan policyResult{leader, follower} {
		if r := receive(t, result); !errors.Is(r.err, context.Canceled) || r.pol.Meta.Source == policy.PolicySourceLKG {
			t.Fatalf("shared cancellation must remain an error, got %+v", r)
		}
	}
	if _, ok := provider.cache.GetLKG(key); !ok {
		t.Fatal("cancellation must not discard LKG")
	}
	if _, err := provider.GetEffectivePolicy(context.Background(), key); err != nil {
		t.Fatalf("next caller must be able to fetch again: %v", err)
	}
}

func TestRemoteProvider_AlreadyCanceled(t *testing.T) {
	for _, ttl := range []time.Duration{0, time.Minute} {
		for _, deadline := range []bool{false, true} {
			t.Run(fmt.Sprintf("ttl=%v/deadline=%v", ttl, deadline), func(t *testing.T) {
				key := policy.ParseKey("svc.cancel")
				source := &MockSource{GetPolicyFunc: func(context.Context, policy.PolicyKey) (policy.EffectivePolicy, error) {
					return policy.DefaultPolicyFor(key), nil
				}}
				provider := NewRemoteProvider(source, WithCacheTTL(ttl), WithLKGTTL(time.Minute))
				if _, err := provider.GetEffectivePolicy(context.Background(), key); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				if deadline {
					ctx, cancel = context.WithDeadline(context.Background(), time.Unix(0, 0))
					defer cancel()
				}
				if _, err := provider.GetEffectivePolicy(ctx, key); !errors.Is(err, ctx.Err()) {
					t.Fatalf("got %v, want %v", err, ctx.Err())
				}
				if calls := atomic.LoadInt32(&source.Calls); calls != 1 {
					t.Fatalf("canceled caller must not fetch, source calls=%d", calls)
				}
			})
		}
	}
}

func TestRemoteProvider_AbnormalSourceReleasesWaiters(t *testing.T) {
	for _, goexit := range []bool{false, true} {
		t.Run(fmt.Sprintf("goexit=%v", goexit), func(t *testing.T) {
			key := policy.ParseKey("svc.panic")
			started := make(chan struct{})
			release := make(chan struct{})
			releaseFetch := sync.OnceFunc(func() { close(release) })
			t.Cleanup(releaseFetch)
			var source *MockSource
			source = &MockSource{GetPolicyFunc: func(context.Context, policy.PolicyKey) (policy.EffectivePolicy, error) {
				if atomic.LoadInt32(&source.Calls) == 1 {
					close(started)
					<-release
					if goexit {
						runtime.Goexit()
					}
					panic("source panic")
				}
				return policy.DefaultPolicyFor(key), nil
			}}
			provider := NewRemoteProvider(source)
			leader := startPolicyLookup(provider, context.Background(), key)
			receive(t, started)
			ctx := &waitingContext{Context: context.Background(), waiting: make(chan struct{})}
			follower := startPolicyLookup(provider, ctx, key)
			receive(t, ctx.waiting)
			releaseFetch()
			leaderResult, followerResult := receive(t, leader), receive(t, follower)
			if goexit {
				if !errors.Is(followerResult.err, ErrPolicyFetchFailed) {
					t.Fatalf("expected failure after source Goexit, got %+v", followerResult)
				}
			} else if leaderResult.panicValue != "source panic" || followerResult.panicValue != "source panic" {
				t.Fatalf("expected panic propagation, got %+v, %+v", leaderResult, followerResult)
			}
			if _, err := provider.GetEffectivePolicy(context.Background(), key); err != nil {
				t.Fatalf("abnormal source must not block future fetches: %v", err)
			}
		})
	}
}
