package controlplane

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aponysus/recourse/policy"
)

func TestRemoteProvider_LKGRefreshAndExpiry(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	key := policy.ParseKey("svc.lkg")
	oldPolicy := policy.DefaultPolicyFor(key)
	oldPolicy.ID = "old"
	oldPolicy.Meta.Source = policy.PolicySourceRemote
	newPolicy := oldPolicy
	newPolicy.ID = "new"
	newPolicy.Retry.MaxAttempts = 5
	sourcePolicy := oldPolicy
	var sourceErr error
	source := &MockSource{GetPolicyFunc: func(context.Context, policy.PolicyKey) (policy.EffectivePolicy, error) {
		return sourcePolicy, sourceErr
	}}
	provider := NewRemoteProvider(source, WithCacheTTL(10*time.Second), WithLKGTTL(20*time.Second))
	provider.cache.nowFn = clock.Now
	fetchErr := errors.New("source offline")

	steps := []struct {
		name       string
		advance    time.Duration
		remote     policy.EffectivePolicy
		err        error
		want       policy.EffectivePolicy
		wantSource policy.PolicySource
		wantErr    error
		wantCalls  int32
	}{
		{"initial", 0, oldPolicy, nil, oldPolicy, policy.PolicySourceRemote, nil, 1},
		{"fresh cache", 0, newPolicy, nil, oldPolicy, policy.PolicySourceRemote, nil, 1},
		{"stale on error", 10 * time.Second, policy.EffectivePolicy{}, fetchErr, oldPolicy, policy.PolicySourceLKG, nil, 2},
		{"refresh while stale", 5 * time.Second, newPolicy, nil, newPolicy, policy.PolicySourceRemote, nil, 3},
		{"refreshed cache", 0, policy.EffectivePolicy{}, fetchErr, newPolicy, policy.PolicySourceRemote, nil, 3},
		{"new LKG", 10 * time.Second, policy.EffectivePolicy{}, fetchErr, newPolicy, policy.PolicySourceLKG, nil, 4},
		{"repeated error", 19 * time.Second, policy.EffectivePolicy{}, fetchErr, newPolicy, policy.PolicySourceLKG, nil, 5},
		{"LKG expired", time.Second, policy.EffectivePolicy{}, fetchErr, policy.EffectivePolicy{}, "", fetchErr, 6},
	}
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			clock.Advance(step.advance)
			sourcePolicy, sourceErr = step.remote, step.err
			got, err := provider.GetEffectivePolicy(context.Background(), key)
			want := step.want
			want.Meta.Source = step.wantSource
			if !errors.Is(err, step.wantErr) || !reflect.DeepEqual(got, want) {
				t.Fatalf("got %+v, %v; want %+v, %v", got, err, want, step.wantErr)
			}
			if calls := atomic.LoadInt32(&source.Calls); calls != step.wantCalls {
				t.Fatalf("source calls=%d, want %d", calls, step.wantCalls)
			}
			if step.wantSource == policy.PolicySourceLKG {
				cached, _ := provider.cache.GetLKG(key)
				if cached.Meta.Source != policy.PolicySourceRemote {
					t.Fatal("serving LKG must not change stored source metadata")
				}
			}
		})
	}
}

func TestRemoteProvider_LKGDisabledOrUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []RemoteProviderOption
		warm bool
	}{
		{"default", nil, true},
		{"zero", []RemoteProviderOption{WithLKGTTL(0)}, true},
		{"negative", []RemoteProviderOption{WithLKGTTL(-time.Second)}, true},
		{"cold", []RemoteProviderOption{WithLKGTTL(time.Minute)}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key := policy.ParseKey("svc.lkg")
			fetchErr := errors.New("offline")
			var sourceErr error
			source := &MockSource{GetPolicyFunc: func(context.Context, policy.PolicyKey) (policy.EffectivePolicy, error) {
				return policy.DefaultPolicyFor(key), sourceErr
			}}
			opts := append([]RemoteProviderOption{WithCacheTTL(0)}, tc.opts...)
			provider := NewRemoteProvider(source, opts...)
			if tc.warm {
				if _, err := provider.GetEffectivePolicy(context.Background(), key); err != nil {
					t.Fatal(err)
				}
			}
			sourceErr = fetchErr
			if got, err := provider.GetEffectivePolicy(context.Background(), key); !errors.Is(err, fetchErr) || !reflect.DeepEqual(got, policy.EffectivePolicy{}) {
				t.Fatalf("got %+v, %v; want no policy and source error", got, err)
			}
		})
	}
}

func TestRemoteProvider_MissingDiscardsLKG(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	key := policy.ParseKey("svc.deleted")
	var sourceErr error
	source := &MockSource{GetPolicyFunc: func(context.Context, policy.PolicyKey) (policy.EffectivePolicy, error) {
		return policy.DefaultPolicyFor(key), sourceErr
	}}
	provider := NewRemoteProvider(source, WithCacheTTL(time.Second), WithNegativeCacheTTL(time.Second), WithLKGTTL(time.Minute))
	provider.cache.nowFn = clock.Now
	if _, err := provider.GetEffectivePolicy(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Second)
	sourceErr = fmt.Errorf("deleted: %w", ErrPolicyNotFound)
	for i := 0; i < 2; i++ {
		if _, err := provider.GetEffectivePolicy(context.Background(), key); !errors.Is(err, ErrPolicyNotFound) {
			t.Fatalf("expected missing policy, got %v", err)
		}
	}
	if calls := atomic.LoadInt32(&source.Calls); calls != 2 {
		t.Fatalf("source calls=%d, want 2 (negative cache hit)", calls)
	}
	clock.Advance(time.Second)
	sourceErr = errors.New("offline after deletion")
	if _, err := provider.GetEffectivePolicy(context.Background(), key); !errors.Is(err, sourceErr) {
		t.Fatalf("deleted LKG must not return after negative cache expires, got %v", err)
	}
}

func TestRemoteProvider_NormalizationFailurePreservesLKG(t *testing.T) {
	key := policy.ParseKey("svc.invalid")
	sourcePolicy := policy.EffectivePolicy{ID: "good"}
	var sourceErr error
	source := &MockSource{GetPolicyFunc: func(context.Context, policy.PolicyKey) (policy.EffectivePolicy, error) {
		return sourcePolicy, sourceErr
	}}
	provider := NewRemoteProvider(source, WithCacheTTL(0), WithLKGTTL(time.Minute))
	good, err := provider.GetEffectivePolicy(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if !good.Meta.Normalization.Changed {
		t.Fatal("expected initial policy to be normalized")
	}
	sourcePolicy.ID = "invalid"
	sourcePolicy.Retry.Jitter = "invalid"
	_, err = provider.GetEffectivePolicy(context.Background(), key)
	var normErr *policy.NormalizeError
	if !errors.As(err, &normErr) {
		t.Fatalf("normalization failures must remain errors, got %v", err)
	}
	sourceErr = errors.New("offline")
	got, err := provider.GetEffectivePolicy(context.Background(), key)
	good.Meta.Source = policy.PolicySourceLKG
	if err != nil || !reflect.DeepEqual(got, good) {
		t.Fatalf("invalid policy must not replace normalized LKG, got %+v, %v", got, err)
	}
}

func TestRemoteProvider_SourceTimeoutUsesLKG(t *testing.T) {
	key := policy.ParseKey("svc.timeout")
	var sourceErr error
	source := &MockSource{GetPolicyFunc: func(context.Context, policy.PolicyKey) (policy.EffectivePolicy, error) {
		return policy.EffectivePolicy{}, sourceErr
	}}
	provider := NewRemoteProvider(source, WithCacheTTL(0), WithLKGTTL(time.Minute))
	if _, err := provider.GetEffectivePolicy(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	sourceErr = fmt.Errorf("source's own timeout: %w", context.DeadlineExceeded)
	got, err := provider.GetEffectivePolicy(context.Background(), key)
	if err != nil || got.Meta.Source != policy.PolicySourceLKG {
		t.Fatalf("active caller should receive LKG on source timeout, got %+v, %v", got, err)
	}
}

func TestRemoteProvider_LKGExpiresDuringFetch(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	key := policy.ParseKey("svc.slow")
	fetchErr := errors.New("slow source failure")
	var fail bool
	source := &MockSource{GetPolicyFunc: func(context.Context, policy.PolicyKey) (policy.EffectivePolicy, error) {
		if fail {
			clock.Advance(time.Second)
			return policy.EffectivePolicy{}, fetchErr
		}
		return policy.DefaultPolicyFor(key), nil
	}}
	provider := NewRemoteProvider(source, WithCacheTTL(time.Second), WithLKGTTL(time.Second))
	provider.cache.nowFn = clock.Now
	if _, err := provider.GetEffectivePolicy(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Second)
	fail = true
	if got, err := provider.GetEffectivePolicy(context.Background(), key); !errors.Is(err, fetchErr) || got.Meta.Source == policy.PolicySourceLKG {
		t.Fatalf("LKG expired while fetching must not be served, got %+v, %v", got, err)
	}
}
