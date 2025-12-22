package controlplane

import (
	"context"

	"github.com/aponysus/recourse/policy"
)

// PolicyProvider supplies an EffectivePolicy for a PolicyKey.
type PolicyProvider interface {
	// GetEffectivePolicy returns the policy for key.
	//
	// Providers may return a non-zero policy alongside a non-nil error to
	// communicate that the policy was obtained via a fallback path (for example,
	// last-known-good).
	GetEffectivePolicy(ctx context.Context, key policy.PolicyKey) (policy.EffectivePolicy, error)
}

// StaticProvider is an in-process PolicyProvider backed by a map and an optional default.
type StaticProvider struct {
	Policies map[policy.PolicyKey]policy.EffectivePolicy
	Default  policy.EffectivePolicy
}

func (p *StaticProvider) GetEffectivePolicy(_ context.Context, key policy.PolicyKey) (policy.EffectivePolicy, error) {
	if p != nil && p.Policies != nil {
		if pol, ok := p.Policies[key]; ok {
			pol.Key = key
			if pol.Meta.Source == "" || pol.Meta.Source == policy.PolicySourceUnknown {
				pol.Meta.Source = policy.PolicySourceStatic
			}
			return pol.Normalize()
		}
	}

	if p != nil && !isZeroEffectivePolicy(p.Default) {
		pol := p.Default
		pol.Key = key
		if pol.Meta.Source == "" || pol.Meta.Source == policy.PolicySourceUnknown {
			pol.Meta.Source = policy.PolicySourceStatic
		}
		return pol.Normalize()
	}

	return policy.DefaultPolicyFor(key).Normalize()
}

func isZeroEffectivePolicy(pol policy.EffectivePolicy) bool {
	return pol.Key == (policy.PolicyKey{}) &&
		pol.ID == "" &&
		pol.Retry == (policy.RetryPolicy{}) &&
		pol.Hedge == (policy.HedgePolicy{})
}
