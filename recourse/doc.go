// Package recourse is the facade package that re-exports key types and provides helpers.
//
// To capture timelines for debugging or observability:
//
//	ctx, capture := observe.RecordTimeline(ctx)
//	result, err := recourse.DoValue(ctx, "svc.Method", op)
//	tl := capture.Timeline() // Safe to access after call completes
package recourse
