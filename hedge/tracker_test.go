package hedge

import (
	"testing"
	"time"
)

func TestRingBufferTracker_Empty(t *testing.T) {
	tracker := NewRingBufferTracker(10)
	snap := tracker.Snapshot()

	if snap.P50 != 0 || snap.P90 != 0 || snap.P99 != 0 {
		t.Errorf("expected zero stats for empty tracker, got %+v", snap)
	}
}

func TestRingBufferTracker_Simple(t *testing.T) {
	tracker := NewRingBufferTracker(100)

	// Add 100 samples: 1ms, 2ms, ... 100ms
	for i := 1; i <= 100; i++ {
		tracker.Observe(time.Duration(i) * time.Millisecond)
	}

	snap := tracker.Snapshot()

	// P50 should be ~50ms
	if snap.P50 != 50*time.Millisecond {
		t.Errorf("expected P50=50ms, got %v", snap.P50)
	}
	// P90 should be ~90ms
	if snap.P90 != 90*time.Millisecond {
		t.Errorf("expected P90=90ms, got %v", snap.P90)
	}
	// P99 should be ~99ms
	if snap.P99 != 99*time.Millisecond {
		t.Errorf("expected P99=99ms, got %v", snap.P99)
	}
}

func TestRingBufferTracker_Rollover(t *testing.T) {
	tracker := NewRingBufferTracker(5)

	// Fill with low values
	for i := 0; i < 5; i++ {
		tracker.Observe(1 * time.Millisecond)
	}

	// Overwrite with high values
	tracker.Observe(100 * time.Millisecond)
	tracker.Observe(100 * time.Millisecond)

	// Buffer: [100, 100, 1, 1, 1] (sorted: 1, 1, 1, 100, 100)
	// P50 (index 2) = 1ms
	// P90 (index 4) = 100ms

	snap := tracker.Snapshot()
	if snap.P50 != 1*time.Millisecond {
		t.Errorf("expected P50=1ms, got %v", snap.P50)
	}
	if snap.P90 != 100*time.Millisecond {
		t.Errorf("expected P90=100ms, got %v", snap.P90)
	}
}

func TestRingBufferTracker_Concurrent(t *testing.T) {
	tracker := NewRingBufferTracker(1000)
	done := make(chan bool)

	go func() {
		for i := 0; i < 1000; i++ {
			tracker.Observe(time.Duration(i) * time.Millisecond)
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			tracker.Snapshot()
		}
		done <- true
	}()

	<-done
	<-done
}
