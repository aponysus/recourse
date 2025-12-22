package policy

import (
	"testing"
	"time"
)

func TestNew_AppliesOptions(t *testing.T) {
	p := New("test.key",
		MaxAttempts(5),
		InitialBackoff(100*time.Millisecond),
		Classifier("custom"),
	)

	if p.Retry.MaxAttempts != 5 {
		t.Errorf("expected MaxAttempts 5, got %d", p.Retry.MaxAttempts)
	}
	if p.Retry.InitialBackoff != 100*time.Millisecond {
		t.Errorf("expected InitialBackoff 100ms, got %v", p.Retry.InitialBackoff)
	}
	if p.Retry.ClassifierName != "custom" {
		t.Errorf("expected ClassifierName 'custom', got %s", p.Retry.ClassifierName)
	}
	if p.Key.String() != "test.key" {
		t.Errorf("expected key 'test.key', got %s", p.Key.String())
	}
}

func TestNew_NormalizationFallback(t *testing.T) {
	// Provide invalid option that causes normalization failure (if possible)
	// Normalization fixes most things, but returns error for invalid Jitter.
	// We need to bypass the type safety of JitterKind to test this,
	// or use an option that sets it to something invalid.

	invalidJitter := func(p *EffectivePolicy) {
		p.Retry.Jitter = JitterKind("invalid-jitter")
	}

	p := New("test.broken", invalidJitter)

	// Should fall back to default
	if p.Retry.MaxAttempts != 3 { // default
		t.Errorf("expected default MaxAttempts 3, got %d", p.Retry.MaxAttempts)
	}
	if p.Retry.Jitter != JitterNone { // default
		t.Errorf("expected default JitterNone, got %v", p.Retry.Jitter)
	}
}

func TestPresets_HTTPDefaults(t *testing.T) {
	p := New("test.http", HTTPDefaults())

	if p.Retry.ClassifierName != "http" {
		t.Errorf("expected http classifier, got %s", p.Retry.ClassifierName)
	}
	if p.Retry.BackoffMultiplier != 2.0 {
		t.Errorf("expected multiplier 2.0, got %f", p.Retry.BackoffMultiplier)
	}
	if p.Retry.MaxAttempts != 3 {
		t.Errorf("expected 3 attempts, got %d", p.Retry.MaxAttempts)
	}
}

func TestPresets_LowLatencyDefaults(t *testing.T) {
	p := New("test.fast", LowLatencyDefaults())

	if !p.Hedge.Enabled {
		t.Error("expected hedging enabled")
	}
	if p.Retry.MaxAttempts != 2 {
		t.Errorf("expected 2 attempts, got %d", p.Retry.MaxAttempts)
	}
}

func TestExponentialBackoff(t *testing.T) {
	p := New("test.exp", ExponentialBackoff(50*time.Millisecond, 5*time.Second))

	if p.Retry.InitialBackoff != 50*time.Millisecond {
		t.Errorf("expected initial 50ms, got %v", p.Retry.InitialBackoff)
	}
	if p.Retry.MaxBackoff != 5*time.Second {
		t.Errorf("expected max 5s, got %v", p.Retry.MaxBackoff)
	}
	if p.Retry.Jitter != JitterEqual {
		t.Errorf("expected JitterEqual, got %v", p.Retry.Jitter)
	}
}
