package worker

import (
	"sync"
	"time"
)

// rateWindowSize is the number of recent completions used to estimate throughput.
const rateWindowSize = 50

// staleRateThreshold is the maximum age of the most recent completion before the
// rate is considered stale and reported as 0 (pipeline appears idle).
const staleRateThreshold = 30 * time.Second

// rateTracker measures per-pipeline task throughput using a sliding window of
// the last rateWindowSize completion timestamps.
type rateTracker struct {
	mu   sync.Mutex
	ring [rateWindowSize]time.Time
	head int // next write index (circular)
	size int // number of recorded completions (capped at rateWindowSize)
}

// record marks one task completion at the current time.
func (r *rateTracker) record() {
	now := time.Now()
	r.mu.Lock()
	r.ring[r.head] = now
	r.head = (r.head + 1) % rateWindowSize
	if r.size < rateWindowSize {
		r.size++
	}
	r.mu.Unlock()
}

// rate returns the estimated throughput in tasks per second.
// Returns 0 when fewer than 2 data points are available or the most recent
// completion is older than staleRateThreshold (pipeline appears idle).
func (r *rateTracker) rate() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.size < 2 {
		return 0
	}

	// newest = the entry written most recently (head-1 wrapping).
	newestIdx := (r.head - 1 + rateWindowSize) % rateWindowSize
	newest := r.ring[newestIdx]
	if time.Since(newest) > staleRateThreshold {
		return 0 // pipeline has gone idle
	}

	// oldest = the entry written longest ago within the current window.
	oldestIdx := (r.head - r.size + rateWindowSize) % rateWindowSize
	oldest := r.ring[oldestIdx]

	elapsed := newest.Sub(oldest).Seconds()
	if elapsed <= 0 {
		return 0
	}
	// (size-1) intervals span from oldest to newest.
	return float64(r.size-1) / elapsed
}
