package circuit

import (
	"context"
	"testing"
	"time"
)

func TestConsecutiveFailureBreaker_Transitions(t *testing.T) {
	threshold := 3
	cooldown := 50 * time.Millisecond
	cb := NewConsecutiveFailureBreaker(threshold, cooldown)

	ctx := context.Background()

	// Initial State: Closed
	if cb.State() != StateClosed {
		t.Fatalf("expected state Closed, got %v", cb.State())
	}
	if d := cb.Allow(ctx); !d.Allowed {
		t.Fatalf("expected allowed=true in Closed state")
	}

	// 1. Failures < Threshold
	cb.RecordFailure(ctx)
	cb.RecordFailure(ctx)
	if cb.State() != StateClosed {
		t.Fatalf("expected state Closed after 2 failures (threshold 3)")
	}

	// Success resets count
	cb.RecordSuccess(ctx)

	// 2. Failures >= Threshold
	cb.RecordFailure(ctx)
	cb.RecordFailure(ctx)
	cb.RecordFailure(ctx)
	if cb.State() != StateOpen {
		t.Fatalf("expected state Open after 3 consecutive failures")
	}

	// 3. Open State Rejection
	d := cb.Allow(ctx)
	if d.Allowed {
		t.Fatalf("expected allowed=false in Open state")
	}
	if d.Reason != ReasonCircuitOpen {
		t.Fatalf("expected reason %s, got %v", ReasonCircuitOpen, d.Reason)
	}

	// 4. Cooldown Wait
	time.Sleep(cooldown + 10*time.Millisecond)

	// 5. Half-Open Transition (on Allow)
	d = cb.Allow(ctx)
	if !d.Allowed {
		t.Fatalf("expected allowed=true for probe in Half-Open state")
	}
	if d.State != StateHalfOpen {
		t.Fatalf("expected state Half-Open, got %v", d.State)
	}
	if cb.State() != StateHalfOpen {
		t.Fatalf("expected reported state Half-Open")
	}

	// 6. Max Probes (Default 1)
	d2 := cb.Allow(ctx)
	if d2.Allowed {
		t.Fatalf("expected allowed=false for second request in Half-Open (max probes 1)")
	}

	// 7. Probe Failure -> Open
	cb.RecordFailure(ctx)
	if cb.State() != StateOpen {
		t.Fatalf("expected state Open after probe failure")
	}

	// Wait again
	time.Sleep(cooldown + 10*time.Millisecond)
	cb.Allow(ctx) // Transition to Half-Open
	if cb.State() != StateHalfOpen {
		t.Fatalf("expected state Half-Open")
	}

	// 8. Probe Success -> Closed
	cb.RecordSuccess(ctx)
	if cb.State() != StateClosed {
		t.Fatalf("expected state Closed after probe success")
	}
	if d := cb.Allow(ctx); !d.Allowed {
		t.Fatalf("expected allowed=true after closing")
	}
}
