package hedge

import (
	"sync/atomic"
	"testing"
	"time"
)

func BenchmarkRingBufferTracker_Observe(b *testing.B) {
	tracker := NewRingBufferTracker(1024)
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		tracker.Observe(time.Duration(i))
	}
}

func BenchmarkRingBufferTracker_Snapshot(b *testing.B) {
	tracker := NewRingBufferTracker(1024)
	for i := 0; i < 1024; i++ {
		tracker.Observe(time.Duration(i))
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = tracker.Snapshot()
	}
}

func BenchmarkRingBufferTracker_ConcurrentObserve(b *testing.B) {
	tracker := NewRingBufferTracker(1024)
	var counter int64
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			tracker.Observe(time.Duration(atomic.AddInt64(&counter, 1)))
		}
	})
}
