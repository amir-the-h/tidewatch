package inference

import (
	"context"
	"testing"
	"time"
)

func TestBackendPool_LoadAndGet(t *testing.T) {
	p := NewBackendPool()
	defer p.Close()

	p.Load([]BackendRecord{
		{ID: 1, Name: "local", URL: "http://localhost:11434", Enabled: true},
		{ID: 2, Name: "remote", URL: "http://remote:11434", Enabled: false},
	})

	b := p.Get(1)
	if b == nil || b.baseURL != "http://localhost:11434" {
		t.Fatalf("expected backend 1, got %v", b)
	}
	if p.Get(2) != nil {
		t.Fatal("expected nil for disabled backend")
	}
	if p.Get(99) != nil {
		t.Fatal("expected nil for unknown backend")
	}
}

func TestBackendPool_All(t *testing.T) {
	p := NewBackendPool()
	defer p.Close()

	p.Load([]BackendRecord{
		{ID: 1, Name: "a", URL: "http://a:11434", Enabled: true},
		{ID: 2, Name: "b", URL: "http://b:11434", Enabled: false},
		{ID: 3, Name: "c", URL: "http://c:11434", Enabled: true},
	})

	all := p.All()
	if len(all) != 2 {
		t.Fatalf("expected 2 enabled backends, got %d", len(all))
	}
}

func TestBackendPool_LoadPreservesExisting(t *testing.T) {
	p := NewBackendPool()
	defer p.Close()

	p.Load([]BackendRecord{{ID: 1, Name: "a", URL: "http://a:11434", Enabled: true}})
	b1 := p.Get(1)

	p.Load([]BackendRecord{{ID: 1, Name: "a-renamed", URL: "http://a:11434", Enabled: true}})
	b2 := p.Get(1)
	if b1 != b2 {
		t.Fatal("expected same pointer on reload with unchanged URL")
	}
	if b2.name != "a-renamed" {
		t.Fatalf("expected updated name, got %q", b2.name)
	}
}

func TestBackendPool_ReleaseAndMarkUnhealthy(t *testing.T) {
	p := NewBackendPool()
	defer p.Close()

	p.Load([]BackendRecord{{ID: 1, Name: "a", URL: "http://a:11434", Enabled: true}})
	b := p.Get(1)
	if b == nil {
		t.Fatal("backend not found")
	}

	p.mu.Lock()
	b.activeCount = 1
	p.mu.Unlock()

	p.Release(b)
	p.mu.Lock()
	active := b.activeCount
	p.mu.Unlock()
	if active != 0 {
		t.Fatal("expected activeCount=0 after Release")
	}

	p.MarkUnhealthy(b)
	p.mu.Lock()
	healthy := b.healthy
	p.mu.Unlock()
	if healthy {
		t.Fatal("expected healthy=false after MarkUnhealthy")
	}
}

func TestBackendPool_AcquireAny(t *testing.T) {
	p := NewBackendPool()
	defer p.Close()

	p.Load([]BackendRecord{{ID: 1, Name: "a", URL: "http://a:11434", Enabled: true}})
	b := p.Get(1)
	p.mu.Lock()
	b.healthy = true
	p.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	got, err := p.AcquireAny(ctx)
	if err != nil || got == nil {
		t.Fatalf("AcquireAny: %v", err)
	}
	p.Release(got)
}

func TestBackendPool_IsAnyAvailable_TrueWhenIdleBackend(t *testing.T) {
	p := NewBackendPool()
	defer p.Close()
	p.Load([]BackendRecord{{ID: 1, Name: "b1", URL: "http://localhost:11434", Enabled: true}})

	if !p.IsAnyAvailable(nil) {
		t.Fatal("expected backend to be available after Load")
	}
}

func TestBackendPool_IsAnyAvailable_FalseWhenAllInUse(t *testing.T) {
	p := NewBackendPool()
	defer p.Close()
	p.Load([]BackendRecord{{ID: 1, Name: "b1", URL: "http://localhost:11434", Enabled: true}})

	p.mu.Lock()
	b := p.backends[1]
	b.activeCount = b.concurrencyLimit() // fill to capacity
	p.mu.Unlock()

	if p.IsAnyAvailable(nil) {
		t.Fatal("expected no backend when all at capacity")
	}
}

func TestBackendPool_IsAnyAvailable_RespectAssignedIDs(t *testing.T) {
	p := NewBackendPool()
	defer p.Close()
	p.Load([]BackendRecord{
		{ID: 1, Name: "b1", URL: "http://b1:11434", Enabled: true},
		{ID: 2, Name: "b2", URL: "http://b2:11434", Enabled: true},
	})

	p.mu.Lock()
	b1 := p.backends[1]
	b1.activeCount = b1.concurrencyLimit() // fill b1 to capacity
	p.mu.Unlock()

	// Full pool: backend 2 is still available.
	if !p.IsAnyAvailable(nil) {
		t.Fatal("expected full pool to have an available backend")
	}
	// Assigned to backend 1 only: unavailable.
	if p.IsAnyAvailable([]int64{1}) {
		t.Fatal("expected assigned backend 1 to be unavailable")
	}
	// Assigned to backend 2 only: available.
	if !p.IsAnyAvailable([]int64{2}) {
		t.Fatal("expected assigned backend 2 to be available")
	}
}

func TestBackendPool_WaitForAvailability_ReturnsWhenBackendReleased(t *testing.T) {
	p := NewBackendPool()
	defer p.Close()
	p.Load([]BackendRecord{{ID: 1, Name: "b1", URL: "http://localhost:11434", Enabled: true}})

	b := p.Get(1)
	p.mu.Lock()
	b.activeCount = b.concurrencyLimit() // fill to capacity
	p.mu.Unlock()

	// Release the backend after 50ms.
	go func() {
		time.Sleep(50 * time.Millisecond)
		p.Release(b)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	start := time.Now()
	p.WaitForAvailability(ctx, 500*time.Millisecond, nil)
	elapsed := time.Since(start)

	if elapsed < 40*time.Millisecond {
		t.Errorf("returned too fast: %v (want ≥40ms)", elapsed)
	}
	if elapsed > 300*time.Millisecond {
		t.Errorf("returned too slow: %v (want ≤300ms)", elapsed)
	}
}

func TestBackendPool_WaitForAvailability_RespectsTimeout(t *testing.T) {
	p := NewBackendPool()
	defer p.Close()
	p.Load([]BackendRecord{{ID: 1, Name: "b1", URL: "http://localhost:11434", Enabled: true}})

	b := p.Get(1)
	p.mu.Lock()
	b.activeCount = b.concurrencyLimit() // fill to capacity
	p.mu.Unlock()

	start := time.Now()
	p.WaitForAvailability(context.Background(), 100*time.Millisecond, nil)
	elapsed := time.Since(start)

	if elapsed < 80*time.Millisecond {
		t.Errorf("returned too fast: %v (want ≥80ms)", elapsed)
	}
	if elapsed > 400*time.Millisecond {
		t.Errorf("returned too slow: %v (want ≤400ms)", elapsed)
	}
}

func TestBackendPool_TotalCapacity(t *testing.T) {
	p := NewBackendPool()
	defer p.Close()

	// Empty pool: capacity is 0.
	if got := p.TotalCapacity(); got != 0 {
		t.Errorf("empty pool TotalCapacity = %d, want 0", got)
	}

	p.Load([]BackendRecord{
		{ID: 1, Name: "a", URL: "http://a:11434", Enabled: true, MaxConcurrent: 2},
		{ID: 2, Name: "b", URL: "http://b:11434", Enabled: true, MaxConcurrent: 3},
		{ID: 3, Name: "c", URL: "http://c:11434", Enabled: false, MaxConcurrent: 5}, // disabled: excluded
	})

	if got := p.TotalCapacity(); got != 5 {
		t.Errorf("TotalCapacity = %d, want 5 (2+3, disabled excluded)", got)
	}
}

func TestBackendPool_TotalCapacity_UnhealthyExcluded(t *testing.T) {
	p := NewBackendPool()
	defer p.Close()

	p.Load([]BackendRecord{
		{ID: 1, Name: "healthy", URL: "http://a:11434", Enabled: true, MaxConcurrent: 4},
		{ID: 2, Name: "unhealthy", URL: "http://b:11434", Enabled: true, MaxConcurrent: 2},
	})

	// Mark backend 2 unhealthy.
	p.mu.Lock()
	p.backends[2].healthy = false
	p.mu.Unlock()

	if got := p.TotalCapacity(); got != 4 {
		t.Errorf("TotalCapacity = %d, want 4 (unhealthy backend excluded)", got)
	}
}
