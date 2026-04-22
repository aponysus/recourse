package retry

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/aponysus/recourse/controlplane"
	"github.com/aponysus/recourse/observe"
	"github.com/aponysus/recourse/policy"
)

func TestDoValue_TimelineIncludesPolicyResolutionAttributes(t *testing.T) {
	key := policy.PolicyKey{Name: "policy_attrs"}

	fullPolicy := func() policy.EffectivePolicy {
		return policy.EffectivePolicy{
			Key: key,
			Retry: policy.RetryPolicy{
				MaxAttempts:       2,
				InitialBackoff:    10 * time.Millisecond,
				MaxBackoff:        20 * time.Millisecond,
				BackoffMultiplier: 2,
				Jitter:            policy.JitterNone,
				Budget:            policy.BudgetRef{Cost: 1},
			},
			Hedge: policy.HedgePolicy{
				Budget: policy.BudgetRef{Cost: 1},
			},
		}
	}

	allowNormalizedFields := []string{
		"hedge.budget.cost",
		"retry.backoff_multiplier",
		"retry.budget.cost",
		"retry.initial_backoff",
		"retry.jitter",
		"retry.max_backoff",
	}

	cases := []struct {
		name           string
		exec           *Executor
		wantMode       string
		wantSource     policy.PolicySource
		wantNormalized bool
		wantFields     []string
		wantCalls      int
		wantErr        func(error) bool
	}{
		{
			name: "standard_policy",
			exec: NewExecutorFromOptions(ExecutorOptions{
				Provider: &controlplane.StaticProvider{
					Policies: map[policy.PolicyKey]policy.EffectivePolicy{
						key: fullPolicy(),
					},
				},
			}),
			wantMode:       policyModeStandard,
			wantSource:     policy.PolicySourceStatic,
			wantNormalized: false,
			wantCalls:      1,
			wantErr: func(err error) bool {
				return err == nil
			},
		},
		{
			name: "standard_policy_normalized",
			exec: NewExecutorFromOptions(ExecutorOptions{
				Provider: &controlplane.StaticProvider{
					Policies: map[policy.PolicyKey]policy.EffectivePolicy{
						key: func() policy.EffectivePolicy {
							pol := fullPolicy()
							pol.Retry.MaxAttempts = 0
							return pol
						}(),
					},
				},
			}),
			wantMode:       policyModeStandard,
			wantSource:     policy.PolicySourceStatic,
			wantNormalized: true,
			wantFields:     []string{"retry.max_attempts"},
			wantCalls:      1,
			wantErr: func(err error) bool {
				return err == nil
			},
		},
		{
			name: "fallback_provider_policy",
			exec: NewExecutorFromOptions(ExecutorOptions{
				Provider: policyResolutionProviderStub{
					pol: func() policy.EffectivePolicy {
						pol := fullPolicy()
						pol.Meta.Source = policy.PolicySourceLKG
						return pol
					}(),
					err: controlplane.ErrProviderUnavailable,
				},
				MissingPolicyMode: FailureFallback,
			}),
			wantMode:       "fallback",
			wantSource:     policy.PolicySourceLKG,
			wantNormalized: false,
			wantCalls:      1,
			wantErr: func(err error) bool {
				return err == nil
			},
		},
		{
			name: "fallback_default_policy",
			exec: NewExecutorFromOptions(ExecutorOptions{
				Provider:          policyResolutionProviderStub{err: controlplane.ErrProviderUnavailable},
				MissingPolicyMode: FailureFallback,
			}),
			wantMode:       "fallback",
			wantSource:     policy.PolicySourceDefault,
			wantNormalized: false,
			wantCalls:      1,
			wantErr: func(err error) bool {
				return err == nil
			},
		},
		{
			name: "allow_mode",
			exec: NewExecutorFromOptions(ExecutorOptions{
				Provider:          policyResolutionProviderStub{err: controlplane.ErrProviderUnavailable},
				MissingPolicyMode: FailureAllow,
			}),
			wantMode:       "allow",
			wantSource:     policy.PolicySourceUnknown,
			wantNormalized: true,
			wantFields:     allowNormalizedFields,
			wantCalls:      1,
			wantErr: func(err error) bool {
				return err == nil
			},
		},
		{
			name: "deny_mode",
			exec: NewExecutorFromOptions(ExecutorOptions{
				Provider:          policyResolutionProviderStub{err: controlplane.ErrProviderUnavailable},
				MissingPolicyMode: FailureDeny,
			}),
			wantMode:       "deny",
			wantSource:     policy.PolicySourceUnknown,
			wantNormalized: false,
			wantCalls:      0,
			wantErr: func(err error) bool {
				return errors.Is(err, ErrNoPolicy)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, capture := observe.RecordTimeline(context.Background())

			calls := 0
			_, err := DoValue[int](ctx, tc.exec, key, func(context.Context) (int, error) {
				calls++
				return 42, nil
			})

			if !tc.wantErr(err) {
				t.Fatalf("err=%v did not match expectation", err)
			}
			if calls != tc.wantCalls {
				t.Fatalf("calls=%d, want %d", calls, tc.wantCalls)
			}

			tl := capture.Timeline()
			if tl == nil {
				t.Fatal("expected captured timeline")
			}
			assertPolicyResolutionAttrs(t, tl.Attributes, tc.wantMode, tc.wantSource, tc.wantNormalized, tc.wantFields...)
		})
	}
}

func assertPolicyResolutionAttrs(t *testing.T, attrs map[string]string, wantMode string, wantSource policy.PolicySource, wantNormalized bool, wantFields ...string) {
	t.Helper()

	if got := attrs[policyAttrMode]; got != wantMode {
		t.Fatalf("%s=%q, want %q", policyAttrMode, got, wantMode)
	}
	if got := attrs[policyAttrSource]; got != string(wantSource) {
		t.Fatalf("%s=%q, want %q", policyAttrSource, got, wantSource)
	}

	wantNormalizedValue := "false"
	if wantNormalized {
		wantNormalizedValue = "true"
	}
	if got := attrs[policyAttrNormalized]; got != wantNormalizedValue {
		t.Fatalf("%s=%q, want %q", policyAttrNormalized, got, wantNormalizedValue)
	}

	if len(wantFields) == 0 {
		if got, ok := attrs[policyAttrNormalizedFields]; ok {
			t.Fatalf("%s=%q, want omitted", policyAttrNormalizedFields, got)
		}
		return
	}

	fields := append([]string(nil), wantFields...)
	sort.Strings(fields)
	want := strings.Join(fields, ",")
	if got := attrs[policyAttrNormalizedFields]; got != want {
		t.Fatalf("%s=%q, want %q", policyAttrNormalizedFields, got, want)
	}
}

type policyResolutionProviderStub struct {
	pol policy.EffectivePolicy
	err error
}

func (p policyResolutionProviderStub) GetEffectivePolicy(context.Context, policy.PolicyKey) (policy.EffectivePolicy, error) {
	return p.pol, p.err
}
