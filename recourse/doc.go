// Package recourse is the facade package for the common recourse API.
//
// The package exposes string-keyed Do and DoValue helpers backed by a shared
// global retry executor. If Init is not called, the first call lazily uses the
// default executor and the built-in default policy for the provided key. Call
// Init during startup when you want to install a custom executor with your own
// policy provider, observer, classifiers, budgets, or hedge triggers.
//
// Applications that want explicit dependency ownership can use the retry
// package directly and pass a *retry.Executor to each call instead of relying on
// the global facade.
//
// To capture timelines for debugging or observability:
//
//	ctx, capture := observe.RecordTimeline(ctx)
//	result, err := recourse.DoValue(ctx, "svc.Method", op)
//	tl := capture.Timeline() // Safe to access after call completes
package recourse
