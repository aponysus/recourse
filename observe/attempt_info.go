package observe

import "context"

type attemptInfoKey struct{}

// AttemptInfo is per-attempt metadata attached to the attempt context.
type AttemptInfo struct {
	RetryIndex int
	Attempt    int
	IsHedge    bool
	HedgeIndex int
	PolicyID   string
}

// WithAttemptInfo returns a context derived from ctx that carries info.
func WithAttemptInfo(ctx context.Context, info AttemptInfo) context.Context {
	return context.WithValue(ctx, attemptInfoKey{}, info)
}

// AttemptFromContext returns the AttemptInfo from ctx, if present.
func AttemptFromContext(ctx context.Context) (AttemptInfo, bool) {
	info, ok := ctx.Value(attemptInfoKey{}).(AttemptInfo)
	return info, ok
}
