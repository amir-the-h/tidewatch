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
	b.inUse = true
	p.mu.Unlock()

	p.Release(b)
	p.mu.Lock()
	inUse := b.inUse
	p.mu.Unlock()
	if inUse {
		t.Fatal("expected inUse=false after Release")
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
