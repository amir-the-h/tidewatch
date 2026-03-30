package worker

import (
	"testing"
	"time"
)

func TestRateTracker_ZeroWithOneSample(t *testing.T) {
	var r rateTracker
	r.record()
	if got := r.rate(); got != 0 {
		t.Errorf("expected 0 with 1 sample, got %f", got)
	}
}

func TestRateTracker_NonZeroWithTwoSamples(t *testing.T) {
	var r rateTracker
	r.record()
	time.Sleep(10 * time.Millisecond)
	r.record()
	if got := r.rate(); got <= 0 {
		t.Errorf("expected rate > 0 with 2 samples, got %f", got)
	}
}

func TestRateTracker_StaleReturnsZero(t *testing.T) {
	var r rateTracker
	// Inject two old timestamps directly into the ring to simulate stale data.
	old := time.Now().Add(-(staleRateThreshold + time.Second))
	r.mu.Lock()
	r.ring[0] = old
	r.ring[1] = old.Add(100 * time.Millisecond)
	r.head = 2
	r.size = 2
	r.mu.Unlock()
	if got := r.rate(); got != 0 {
		t.Errorf("expected 0 for stale window, got %f", got)
	}
}
