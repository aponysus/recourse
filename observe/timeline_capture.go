package observe

import (
	"context"
	"sync/atomic"
)

// TimelineCapture holds a captured timeline after execution completes.
//
// Timeline() returns nil until the call completes (or if capture is not used).
type TimelineCapture struct {
	tl atomic.Pointer[Timeline]
}

// Timeline returns the captured timeline, or nil if not yet populated.
// It is thread-safe.
func (c *TimelineCapture) Timeline() *Timeline {
	if c == nil {
		return nil
	}
	return c.tl.Load()
}

// store is used by the retry executor to publish the finished timeline.
// It is intentionally unexported to discourage direct mutation by end-users.
// Use StoreTimelineCapture to set this from other packages.
func (c *TimelineCapture) store(tl *Timeline) {
	if c == nil || tl == nil {
		return
	}
	c.tl.Store(tl)
}

type timelineCaptureKey struct{}

// RecordTimeline returns a derived context that requests timeline capture for the next call,
// plus a holder for retrieving the completed timeline.
func RecordTimeline(ctx context.Context) (context.Context, *TimelineCapture) {
	if ctx == nil {
		ctx = context.Background()
	}
	capture := &TimelineCapture{}
	return context.WithValue(ctx, timelineCaptureKey{}, capture), capture
}

// TimelineCaptureFromContext returns the capture (if requested).
//
// This is primarily used by the retry executor.
func TimelineCaptureFromContext(ctx context.Context) (*TimelineCapture, bool) {
	if ctx == nil {
		return nil, false
	}
	switch v := ctx.Value(timelineCaptureKey{}).(type) {
	case *TimelineCapture:
		return v, v != nil
	default:
		return nil, false
	}
}

type disabledTimelineCapture struct{}

// WithoutTimelineCapture disables timeline capture in derived contexts.
//
// The retry executor should use this when constructing the per-attempt context passed to op,
// to prevent nested calls from accidentally reusing the same capture.
func WithoutTimelineCapture(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, timelineCaptureKey{}, disabledTimelineCapture{})
}

// StoreTimelineCapture publishes the finished timeline into the capture.
//
// This is primarily used by the retry executor.
func StoreTimelineCapture(capture *TimelineCapture, tl *Timeline) {
	if capture == nil {
		return
	}
	capture.store(tl)
}
