package hedge

import (
	"testing"
	"time"
)

func TestLatencyTrigger_ShouldSpawnHedge(t *testing.T) {
	snap := LatencySnapshot{
		P50: 10 * time.Millisecond,
		P90: 50 * time.Millisecond,
		P99: 100 * time.Millisecond,
	}

	tests := []struct {
		name       string
		percentile string
		elapsed    time.Duration
		attempts   int
		maxHedges  int
		want       bool
		wantWait   time.Duration
	}{
		{
			name:       "P50 Trigger - Below Threshold",
			percentile: "p50",
			elapsed:    5 * time.Millisecond,
			attempts:   1,
			maxHedges:  1,
			want:       false,
			wantWait:   5 * time.Millisecond, // 10 - 5
		},
		{
			name:       "P50 Trigger - Above Threshold",
			percentile: "p50",
			elapsed:    11 * time.Millisecond,
			attempts:   1,
			maxHedges:  1,
			want:       true,
			wantWait:   0,
		},
		{
			name:       "P99 Trigger - Below Threshold",
			percentile: "p99",
			elapsed:    90 * time.Millisecond,
			attempts:   1,
			maxHedges:  1,
			want:       false,
			wantWait:   10 * time.Millisecond,
		},
		{
			name:       "P99 Trigger - Above Threshold",
			percentile: "p99",
			elapsed:    101 * time.Millisecond,
			attempts:   1,
			maxHedges:  1,
			want:       true,
			wantWait:   0,
		},
		{
			name:       "Already Hedged - Should Stop",
			percentile: "p50",
			elapsed:    20 * time.Millisecond,
			attempts:   2,
			maxHedges:  1,
			want:       false,
			wantWait:   0,
		},
		{
			name:       "Unknown Percentile",
			percentile: "pXX",
			elapsed:    1000 * time.Millisecond,
			attempts:   1,
			maxHedges:  1,
			want:       false,
			wantWait:   0,
		},
		{
			name:       "Zero Stats",
			percentile: "p50",
			elapsed:    100 * time.Millisecond,
			attempts:   1,
			maxHedges:  1,
			want:       false, // Threshold is 0, so <= 0 check returns false?
			// Logic: threshold <= 0 returns false.
			wantWait: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trigger := LatencyTrigger{Percentile: tt.percentile}
			state := HedgeState{
				Elapsed:          tt.elapsed,
				AttemptsLaunched: tt.attempts,
				MaxHedges:        tt.maxHedges,
				Snapshot:         snap,
			}
			if tt.name == "Zero Stats" {
				state.Snapshot = LatencySnapshot{}
			}

			got, gotWait := trigger.ShouldSpawnHedge(state)
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
			if gotWait != tt.wantWait {
				t.Errorf("gotWait %v, want %v", gotWait, tt.wantWait)
			}
		})
	}
}
