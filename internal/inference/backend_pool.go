package inference

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// BackendRecord is DB data for a single Ollama backend, used to populate the pool.
type BackendRecord struct {
	ID      int64
	Name    string
	URL     string
	Enabled bool
}

// BackendPool owns all ollamaBackend instances and their health-checking goroutines.
// A single mutex and condition variable are shared so state changes broadcast
// to all waiting pipelines.
type BackendPool struct {
	mu       sync.Mutex
	cond     *sync.Cond
	backends map[int64]*ollamaBackend
	cancel   context.CancelFunc
}

// NewBackendPool creates an empty BackendPool. Call Load to populate it.
func NewBackendPool() *BackendPool {
	p := &BackendPool{
		backends: make(map[int64]*ollamaBackend),
	}
	p.cond = sync.NewCond(&p.mu)
	return p
}

// Load rebuilds the pool from DB records.
// Existing backends with unchanged URLs are preserved (health state kept).
// New backends are added. Removed backends are dropped.
// Restarts the health checker goroutine.
func (p *BackendPool) Load(records []BackendRecord) {
	if p.cancel != nil {
		p.cancel()
	}

	p.mu.Lock()
	newMap := make(map[int64]*ollamaBackend, len(records))
	for _, r := range records {
		if existing, ok := p.backends[r.ID]; ok && existing.baseURL == r.URL {
			existing.name = r.Name
			existing.enabled = r.Enabled
			newMap[r.ID] = existing
		} else {
			newMap[r.ID] = &ollamaBackend{
				name:    r.Name,
				baseURL: r.URL,
				enabled: r.Enabled,
				client:  &http.Client{Timeout: 30 * time.Second},
				healthy: true,
			}
		}
	}
	p.backends = newMap
	p.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	go p.runHealthChecker(ctx)
}

// Get returns the backend for id, or nil if not found or disabled.
func (p *BackendPool) Get(id int64) *ollamaBackend {
	p.mu.Lock()
	defer p.mu.Unlock()
	b, ok := p.backends[id]
	if !ok || !b.enabled {
		return nil
	}
	return b
}

// All returns all enabled backends.
func (p *BackendPool) All() []*ollamaBackend {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*ollamaBackend, 0, len(p.backends))
	for _, b := range p.backends {
		if b.enabled {
			out = append(out, b)
		}
	}
	return out
}

// AcquireAny blocks until any healthy idle enabled backend is available or ctx is cancelled.
// Picks the highest-score backend when multiple are available.
func (p *BackendPool) AcquireAny(ctx context.Context) (*ollamaBackend, error) {
	watchCtx, watchCancel := context.WithCancel(ctx)
	defer watchCancel()
	go func() { <-watchCtx.Done(); p.cond.Broadcast() }()

	p.mu.Lock()
	defer p.mu.Unlock()
	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		best := p.bestEnabledIdle()
		if best != nil {
			best.inUse = true
			return best, nil
		}
		p.cond.Wait()
	}
}

// AcquireWithTimeout tries to acquire b within timeout.
// Returns (true, nil) if acquired, (false, nil) on timeout, (false, err) if ctx cancelled.
func (p *BackendPool) AcquireWithTimeout(ctx context.Context, b *ollamaBackend, timeout time.Duration) (bool, error) {
	timer := time.AfterFunc(timeout, func() { p.cond.Broadcast() })
	defer timer.Stop()
	watchCtx, watchCancel := context.WithCancel(ctx)
	defer watchCancel()
	go func() { <-watchCtx.Done(); p.cond.Broadcast() }()

	deadline := time.Now().Add(timeout)
	p.mu.Lock()
	defer p.mu.Unlock()
	for {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		if time.Now().After(deadline) {
			return false, nil
		}
		if b.enabled && b.healthy && !b.inUse {
			b.inUse = true
			return true, nil
		}
		p.cond.Wait()
	}
}

// Release marks b as not in use and broadcasts to wake waiters.
func (p *BackendPool) Release(b *ollamaBackend) {
	p.mu.Lock()
	b.inUse = false
	p.mu.Unlock()
	p.cond.Broadcast()
}

// MarkUnhealthy marks b as unhealthy and not in use, then wakes waiters.
func (p *BackendPool) MarkUnhealthy(b *ollamaBackend) {
	p.mu.Lock()
	b.healthy = false
	b.inUse = false
	p.mu.Unlock()
	p.cond.Broadcast()
	slog.Warn("ollama: backend marked unhealthy", "url", b.baseURL)
}

// Stats returns a snapshot of all backends for API responses.
func (p *BackendPool) Stats() []BackendStat {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]BackendStat, 0, len(p.backends))
	for id, b := range p.backends {
		var lat *time.Time
		if !b.lastActiveAt.IsZero() {
			t := b.lastActiveAt
			lat = &t
		}
		out = append(out, BackendStat{
			ID:           id,
			Name:         b.name,
			URL:          b.baseURL,
			Enabled:      b.enabled,
			Healthy:      b.healthy,
			InUse:        b.inUse,
			Score:        b.score,
			RequestCount: b.requestCount,
			LastActiveAt: lat,
		})
	}
	return out
}

// Close stops the health checker.
func (p *BackendPool) Close() {
	if p.cancel != nil {
		p.cancel()
	}
}

// bestEnabledIdle returns the enabled healthy idle backend with the highest score.
// Must be called with p.mu held.
func (p *BackendPool) bestEnabledIdle() *ollamaBackend {
	var best *ollamaBackend
	for _, b := range p.backends {
		if !b.enabled || !b.healthy || b.inUse {
			continue
		}
		if best == nil || b.score > best.score {
			best = b
		}
	}
	return best
}

// isAnyAvailable reports whether any backend matching assignedIDs is healthy,
// enabled, and not in use. Empty/nil assignedIDs checks the full pool.
// Caller must hold p.mu.
func (p *BackendPool) isAnyAvailable(assignedIDs []int64) bool {
	if len(assignedIDs) == 0 {
		return p.bestEnabledIdle() != nil
	}
	for _, id := range assignedIDs {
		b, ok := p.backends[id]
		if ok && b.enabled && b.healthy && !b.inUse {
			return true
		}
	}
	return false
}

// IsAnyAvailable reports whether any backend matching assignedIDs is
// immediately acquirable (non-blocking). Empty/nil assignedIDs checks
// the full pool. Uses the same priority logic as AcquireAny / acquirePrioritized.
func (p *BackendPool) IsAnyAvailable(assignedIDs []int64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.isAnyAvailable(assignedIDs)
}

// WaitForAvailability blocks until a backend matching assignedIDs is
// available, the timeout elapses, or ctx is cancelled.
// Empty/nil assignedIDs waits for any backend in the full pool.
func (p *BackendPool) WaitForAvailability(ctx context.Context, timeout time.Duration, assignedIDs []int64) {
	deadline := time.Now().Add(timeout)
	timer := time.AfterFunc(timeout, func() { p.cond.Broadcast() })
	defer timer.Stop()
	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() { <-watchCtx.Done(); p.cond.Broadcast() }()

	p.mu.Lock()
	defer p.mu.Unlock()
	for {
		if ctx.Err() != nil || time.Now().After(deadline) {
			return
		}
		if p.isAnyAvailable(assignedIDs) {
			return
		}
		p.cond.Wait()
	}
}

// runHealthChecker runs two tickers: re-probe unhealthy backends (30s) and
// verify healthy backends (60s).
func (p *BackendPool) runHealthChecker(ctx context.Context) {
	unhealthyTick := time.NewTicker(30 * time.Second)
	healthyTick := time.NewTicker(60 * time.Second)
	defer unhealthyTick.Stop()
	defer healthyTick.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-unhealthyTick.C:
			p.probeUnhealthy()
		case <-healthyTick.C:
			p.probeHealthy()
		}
	}
}

func (p *BackendPool) probeUnhealthy() {
	p.mu.Lock()
	toProbe := make([]*ollamaBackend, 0)
	for _, b := range p.backends {
		if !b.healthy {
			toProbe = append(toProbe, b)
		}
	}
	p.mu.Unlock()

	for _, b := range toProbe {
		if poolPingBackend(b) {
			p.mu.Lock()
			b.healthy = true
			p.mu.Unlock()
			p.cond.Broadcast()
			slog.Info("ollama: backend recovered", "url", b.baseURL)
		}
	}
}

func (p *BackendPool) probeHealthy() {
	p.mu.Lock()
	toProbe := make([]*ollamaBackend, 0)
	for _, b := range p.backends {
		if b.healthy {
			toProbe = append(toProbe, b)
		}
	}
	p.mu.Unlock()

	for _, b := range toProbe {
		if !poolPingBackend(b) {
			p.MarkUnhealthy(b)
		}
	}
}

// poolPingBackend pings GET /api/tags with a 5s timeout. Returns true if reachable.
// Named poolPingBackend to avoid conflict with the pingBackend method on OllamaPipeline.
func poolPingBackend(b *ollamaBackend) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.baseURL+"/api/tags", nil)
	if err != nil {
		return false
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
