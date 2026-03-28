# Ollama Backends DB Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move Ollama backends from YAML config to SQLite, with per-pipeline priority-ordered assignment, accessible via REST API, CLI, and UI.

**Architecture:** A new `BackendPool` (in `internal/inference`) owns all `*ollamaBackend` instances and runs one health-checker goroutine per physical URL. `OllamaPipeline` holds a `pool *BackendPool` and `assignedIDs []int64` instead of a local backend slice. Mutating API calls trigger `pool.Load()` + `actionCache.Load()` to hot-reload running state. The `BackendStatter` interface and per-pipeline health checker are removed and replaced by pool-level methods.

**Tech Stack:** Go 1.25.5, SQLite (modernc.org/sqlite), chi router, Vue 3 / TypeScript, Cobra CLI.

**Spec:** `docs/superpowers/specs/2026-03-28-ollama-backends-db-design.md`

---

## File Map

**New files:**
- `migrations/019_ollama_backends.sql` — DB schema for ollama_backends + pipeline_backends
- `internal/model/ollama_backend.go` — OllamaBackend + PipelineBackend domain types
- `internal/store/sqlite_ollama.go` — Store CRUD for ollama_backends + pipeline_backends
- `internal/store/sqlite_ollama_test.go` — Store CRUD tests

**Modified files (in dependency order):**
- `internal/store/store.go` — add 10 new Store interface methods
- `internal/inference/backend_pool.go` (new) — BackendPool + ollamaBackend ownership
- `internal/inference/ollama.go` — replace backends with pool+assignedIDs, remove per-pipeline health checker
- `internal/inference/pipeline.go` — remove BackendConfig + BackendStatter (replaced by pool methods)
- `internal/worker/action_cache.go` — accept *BackendPool, query pipeline_backends in resolvePipeline
- `internal/api/server.go` — replace ollamaBackends []inference.BackendConfig with pool *inference.BackendPool
- `internal/api/handlers_ollama.go` — rewrite CRUD handlers + add assignment handlers
- `internal/api/server.go` — register new routes
- `cli/cmd/serve.go` — remove ollamaBackendConfigs helper, create BackendPool from DB
- `internal/config/config.go` — remove Ollama.BaseURL/BaseURLs/Backends fields + OllamaBackends() + OllamaBaseURLs() methods
- `cli/cmd/ollama.go` — add backends add/update/remove/enable/disable subcommands
- `cli/cmd/pipeline.go` — add `pipeline backends` subcommands
- `web/src/api/ollama.ts` — new typed API functions
- `web/src/views/OllamaView.vue` — three panels: management, assignment, stats

---

## Task 1: Migration + Domain Types

**Files:**
- Create: `migrations/019_ollama_backends.sql`
- Create: `internal/model/ollama_backend.go`

- [ ] **Step 1: Create migration file**

```sql
-- migrations/019_ollama_backends.sql
CREATE TABLE ollama_backends (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT    NOT NULL,
    url        TEXT    NOT NULL UNIQUE,
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE pipeline_backends (
    pipeline_id INTEGER NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
    backend_id  INTEGER NOT NULL REFERENCES ollama_backends(id) ON DELETE CASCADE,
    priority    INTEGER NOT NULL DEFAULT 1,
    PRIMARY KEY (pipeline_id, backend_id)
);
```

- [ ] **Step 2: Create domain types**

```go
// internal/model/ollama_backend.go
package model

import "time"

// OllamaBackend is a stored Ollama server backend.
type OllamaBackend struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

// PipelineBackend is a priority-ordered assignment of a backend to a pipeline.
type PipelineBackend struct {
	PipelineID int64 `json:"pipeline_id"`
	BackendID  int64 `json:"backend_id"`
	Priority   int   `json:"priority"`
}
```

- [ ] **Step 3: Commit**

```bash
git add migrations/019_ollama_backends.sql internal/model/ollama_backend.go
git commit -m "feat: add ollama_backends + pipeline_backends schema and domain types"
```

---

## Task 2: Store Interface + SQLite Implementation

**Files:**
- Modify: `internal/store/store.go` (after `DeleteTwitterCredentials`)
- Create: `internal/store/sqlite_ollama.go`
- Create: `internal/store/sqlite_ollama_test.go`

- [ ] **Step 1: Write failing tests**

```go
// internal/store/sqlite_ollama_test.go
package store

import (
	"context"
	"testing"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

func TestOllamaBackends_CRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// Create
	b, err := s.CreateOllamaBackend(ctx, "local", "http://localhost:11434")
	if err != nil {
		t.Fatalf("CreateOllamaBackend: %v", err)
	}
	if b.ID == 0 || b.Name != "local" || b.URL != "http://localhost:11434" || !b.Enabled {
		t.Fatalf("unexpected backend: %+v", b)
	}

	// List
	all, err := s.ListOllamaBackends(ctx)
	if err != nil {
		t.Fatalf("ListOllamaBackends: %v", err)
	}
	if len(all) != 1 || all[0].ID != b.ID {
		t.Fatalf("expected 1 backend, got %d", len(all))
	}

	// Get
	got, err := s.GetOllamaBackend(ctx, b.ID)
	if err != nil {
		t.Fatalf("GetOllamaBackend: %v", err)
	}
	if got.Name != "local" {
		t.Fatalf("unexpected name: %s", got.Name)
	}

	// Update
	if err := s.UpdateOllamaBackend(ctx, b.ID, "remote", "http://remote:11434"); err != nil {
		t.Fatalf("UpdateOllamaBackend: %v", err)
	}
	got, _ = s.GetOllamaBackend(ctx, b.ID)
	if got.Name != "remote" || got.URL != "http://remote:11434" {
		t.Fatalf("update not reflected: %+v", got)
	}

	// Disable
	if err := s.SetOllamaBackendEnabled(ctx, b.ID, false); err != nil {
		t.Fatalf("SetOllamaBackendEnabled: %v", err)
	}
	got, _ = s.GetOllamaBackend(ctx, b.ID)
	if got.Enabled {
		t.Fatal("expected disabled")
	}

	// Delete
	if err := s.DeleteOllamaBackend(ctx, b.ID); err != nil {
		t.Fatalf("DeleteOllamaBackend: %v", err)
	}
	all, _ = s.ListOllamaBackends(ctx)
	if len(all) != 0 {
		t.Fatal("expected 0 backends after delete")
	}
}

func TestPipelineBackends_SetAndList(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// Need a pipeline to reference
	pipe := samplePipeline()
	if err := s.SavePipeline(ctx, pipe); err != nil {
		t.Fatalf("SavePipeline: %v", err)
	}
	b1, _ := s.CreateOllamaBackend(ctx, "b1", "http://b1:11434")
	b2, _ := s.CreateOllamaBackend(ctx, "b2", "http://b2:11434")

	assignments := []model.PipelineBackend{
		{PipelineID: pipe.ID, BackendID: b1.ID, Priority: 1},
		{PipelineID: pipe.ID, BackendID: b2.ID, Priority: 2},
	}
	if err := s.SetPipelineBackends(ctx, pipe.ID, assignments); err != nil {
		t.Fatalf("SetPipelineBackends: %v", err)
	}

	list, err := s.ListPipelineBackends(ctx, pipe.ID)
	if err != nil {
		t.Fatalf("ListPipelineBackends: %v", err)
	}
	if len(list) != 2 || list[0].Priority != 1 || list[1].Priority != 2 {
		t.Fatalf("unexpected list: %+v", list)
	}

	// Replace
	if err := s.SetPipelineBackends(ctx, pipe.ID, []model.PipelineBackend{
		{PipelineID: pipe.ID, BackendID: b2.ID, Priority: 1},
	}); err != nil {
		t.Fatalf("SetPipelineBackends replace: %v", err)
	}
	list, _ = s.ListPipelineBackends(ctx, pipe.ID)
	if len(list) != 1 || list[0].BackendID != b2.ID {
		t.Fatalf("expected only b2 after replace: %+v", list)
	}

	// Remove one
	b3, _ := s.CreateOllamaBackend(ctx, "b3", "http://b3:11434")
	_ = s.SetPipelineBackends(ctx, pipe.ID, []model.PipelineBackend{
		{PipelineID: pipe.ID, BackendID: b2.ID, Priority: 1},
		{PipelineID: pipe.ID, BackendID: b3.ID, Priority: 2},
	})
	if err := s.RemovePipelineBackend(ctx, pipe.ID, b3.ID); err != nil {
		t.Fatalf("RemovePipelineBackend: %v", err)
	}
	list, _ = s.ListPipelineBackends(ctx, pipe.ID)
	if len(list) != 1 || list[0].BackendID != b2.ID {
		t.Fatalf("expected only b2 after remove: %+v", list)
	}
}

func TestListAllPipelineBackendIDs(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	pipe := samplePipeline()
	_ = s.SavePipeline(ctx, pipe)
	b1, _ := s.CreateOllamaBackend(ctx, "b1", "http://b1:11434")
	b2, _ := s.CreateOllamaBackend(ctx, "b2", "http://b2:11434")
	_ = s.SetPipelineBackends(ctx, pipe.ID, []model.PipelineBackend{
		{PipelineID: pipe.ID, BackendID: b1.ID, Priority: 1},
		{PipelineID: pipe.ID, BackendID: b2.ID, Priority: 2},
	})

	m, err := s.ListAllPipelineBackendIDs(ctx)
	if err != nil {
		t.Fatalf("ListAllPipelineBackendIDs: %v", err)
	}
	ids, ok := m[pipe.ID]
	if !ok || len(ids) != 2 || ids[0] != b1.ID || ids[1] != b2.ID {
		t.Fatalf("unexpected map: %+v", m)
	}
}
```

- [ ] **Step 2: Run tests — expect compilation failure**

```bash
cd /mnt/shared/projects/personal/iran-watcher
go test ./internal/store/... 2>&1 | head -30
```
Expected: compilation errors about missing Store methods.

- [ ] **Step 3: Add 10 methods to Store interface in `internal/store/store.go`**

Add after the `DeleteTwitterCredentials` line (line 237):

```go
	// OllamaBackends
	ListOllamaBackends(ctx context.Context) ([]model.OllamaBackend, error)
	GetOllamaBackend(ctx context.Context, id int64) (model.OllamaBackend, error)
	CreateOllamaBackend(ctx context.Context, name, url string) (model.OllamaBackend, error)
	UpdateOllamaBackend(ctx context.Context, id int64, name, url string) error
	DeleteOllamaBackend(ctx context.Context, id int64) error
	SetOllamaBackendEnabled(ctx context.Context, id int64, enabled bool) error

	// PipelineBackends
	ListPipelineBackends(ctx context.Context, pipelineID int64) ([]model.PipelineBackend, error)
	SetPipelineBackends(ctx context.Context, pipelineID int64, assignments []model.PipelineBackend) error
	RemovePipelineBackend(ctx context.Context, pipelineID, backendID int64) error
	// ListAllPipelineBackendIDs returns a map of pipelineID → []backendID ordered by priority.
	// Used by ActionCache.Load to populate assignedIDs for each Ollama pipeline.
	ListAllPipelineBackendIDs(ctx context.Context) (map[int64][]int64, error)
```

- [ ] **Step 4: Create `internal/store/sqlite_ollama.go`**

```go
package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

func (s *SQLiteStore) ListOllamaBackends(ctx context.Context) ([]model.OllamaBackend, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, url, enabled, created_at FROM ollama_backends ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("store: list ollama backends: %w", err)
	}
	defer rows.Close()
	var out []model.OllamaBackend
	for rows.Next() {
		var b model.OllamaBackend
		var enabledInt int
		if err := rows.Scan(&b.ID, &b.Name, &b.URL, &enabledInt, &b.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: scan ollama backend: %w", err)
		}
		b.Enabled = enabledInt != 0
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) GetOllamaBackend(ctx context.Context, id int64) (model.OllamaBackend, error) {
	var b model.OllamaBackend
	var enabledInt int
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, url, enabled, created_at FROM ollama_backends WHERE id = ?`, id).
		Scan(&b.ID, &b.Name, &b.URL, &enabledInt, &b.CreatedAt)
	if err == sql.ErrNoRows {
		return b, fmt.Errorf("store: ollama backend %d not found", id)
	}
	if err != nil {
		return b, fmt.Errorf("store: get ollama backend: %w", err)
	}
	b.Enabled = enabledInt != 0
	return b, nil
}

func (s *SQLiteStore) CreateOllamaBackend(ctx context.Context, name, url string) (model.OllamaBackend, error) {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO ollama_backends (name, url, enabled, created_at) VALUES (?, ?, 1, ?)`,
		name, url, now)
	if err != nil {
		return model.OllamaBackend{}, fmt.Errorf("store: create ollama backend: %w", err)
	}
	id, _ := res.LastInsertId()
	return model.OllamaBackend{ID: id, Name: name, URL: url, Enabled: true, CreatedAt: now}, nil
}

func (s *SQLiteStore) UpdateOllamaBackend(ctx context.Context, id int64, name, url string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE ollama_backends SET name = ?, url = ? WHERE id = ?`, name, url, id)
	if err != nil {
		return fmt.Errorf("store: update ollama backend: %w", err)
	}
	return nil
}

func (s *SQLiteStore) DeleteOllamaBackend(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM ollama_backends WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete ollama backend: %w", err)
	}
	return nil
}

func (s *SQLiteStore) SetOllamaBackendEnabled(ctx context.Context, id int64, enabled bool) error {
	v := 0
	if enabled {
		v = 1
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE ollama_backends SET enabled = ? WHERE id = ?`, v, id)
	if err != nil {
		return fmt.Errorf("store: set ollama backend enabled: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ListPipelineBackends(ctx context.Context, pipelineID int64) ([]model.PipelineBackend, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT pipeline_id, backend_id, priority FROM pipeline_backends
		 WHERE pipeline_id = ? ORDER BY priority`, pipelineID)
	if err != nil {
		return nil, fmt.Errorf("store: list pipeline backends: %w", err)
	}
	defer rows.Close()
	var out []model.PipelineBackend
	for rows.Next() {
		var pb model.PipelineBackend
		if err := rows.Scan(&pb.PipelineID, &pb.BackendID, &pb.Priority); err != nil {
			return nil, fmt.Errorf("store: scan pipeline backend: %w", err)
		}
		out = append(out, pb)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) SetPipelineBackends(ctx context.Context, pipelineID int64, assignments []model.PipelineBackend) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: set pipeline backends tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM pipeline_backends WHERE pipeline_id = ?`, pipelineID); err != nil {
		return fmt.Errorf("store: clear pipeline backends: %w", err)
	}
	for _, a := range assignments {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO pipeline_backends (pipeline_id, backend_id, priority) VALUES (?, ?, ?)`,
			pipelineID, a.BackendID, a.Priority); err != nil {
			return fmt.Errorf("store: insert pipeline backend: %w", err)
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) RemovePipelineBackend(ctx context.Context, pipelineID, backendID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM pipeline_backends WHERE pipeline_id = ? AND backend_id = ?`,
		pipelineID, backendID)
	if err != nil {
		return fmt.Errorf("store: remove pipeline backend: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ListAllPipelineBackendIDs(ctx context.Context) (map[int64][]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT pipeline_id, backend_id FROM pipeline_backends ORDER BY pipeline_id, priority`)
	if err != nil {
		return nil, fmt.Errorf("store: list all pipeline backend ids: %w", err)
	}
	defer rows.Close()
	out := make(map[int64][]int64)
	for rows.Next() {
		var pipelineID, backendID int64
		if err := rows.Scan(&pipelineID, &backendID); err != nil {
			return nil, fmt.Errorf("store: scan pipeline backend ids: %w", err)
		}
		out[pipelineID] = append(out[pipelineID], backendID)
	}
	return out, rows.Err()
}
```

- [ ] **Step 5: Add `samplePipeline` helper to `sqlite_test.go`** (if it doesn't already exist)

```go
// Add to internal/store/sqlite_test.go after sampleRule()
func samplePipeline() *model.Pipeline {
	return &model.Pipeline{
		Name:   "test-pipeline",
		Type:   model.PipelineTypeOllama,
		Active: true,
	}
}
```

- [ ] **Step 6: Run tests**

```bash
go test ./internal/store/... -v -run TestOllamaBackends 2>&1
go test ./internal/store/... -v -run TestPipelineBackends 2>&1
go test ./internal/store/... -v -run TestListAllPipelineBackendIDs 2>&1
```
Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/store/store.go internal/store/sqlite_ollama.go internal/store/sqlite_ollama_test.go internal/store/sqlite_test.go
git commit -m "feat: add OllamaBackend and PipelineBackend store methods"
```

---

## Task 3: BackendPool

**Files:**
- Create: `internal/inference/backend_pool.go`
- Create: `internal/inference/backend_pool_test.go`

- [ ] **Step 1: Write failing tests**

```go
// internal/inference/backend_pool_test.go
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
	// disabled backend returns nil
	if p.Get(2) != nil {
		t.Fatal("expected nil for disabled backend")
	}
	// unknown id returns nil
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

	// Reload with same URL — pointer should be preserved
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
```

- [ ] **Step 2: Run tests — expect compilation failure**

```bash
go test ./internal/inference/... -run TestBackendPool 2>&1 | head -20
```

- [ ] **Step 3: Create `internal/inference/backend_pool.go`**

```go
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
	go func() { <-ctx.Done(); p.cond.Broadcast() }()

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
	go func() { <-ctx.Done(); p.cond.Broadcast() }()

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

// Stats returns a snapshot of all backends for the /api/ollama/backends response.
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

// runHealthChecker runs two tickers: one to re-probe unhealthy backends (every 30s),
// one to verify healthy backends (every 60s).
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
		if pingBackend(b) {
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
		if !pingBackend(b) {
			p.MarkUnhealthy(b)
		}
	}
}

// pingBackend pings GET /api/tags with a 5s timeout. Returns true if reachable.
func pingBackend(b *ollamaBackend) bool {
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
```

- [ ] **Step 4: Add `Enabled` field to `BackendStat` and `ID` field in `internal/inference/pipeline.go`**

In `pipeline.go`, update `BackendStat`:
```go
// BackendStat is a runtime snapshot of one Ollama backend.
type BackendStat struct {
	ID           int64      `json:"id"`
	Name         string     `json:"name"`
	URL          string     `json:"url"`
	Enabled      bool       `json:"enabled"`
	Healthy      bool       `json:"healthy"`
	InUse        bool       `json:"in_use"`
	Score        float64    `json:"score"`
	RequestCount int64      `json:"request_count"`
	LastActiveAt *time.Time `json:"last_active_at,omitempty"`
}
```

Also remove `BackendConfig` struct and `BackendStatter` interface from `pipeline.go` (they will be unused after Task 4).

- [ ] **Step 5: Run tests**

```bash
go test ./internal/inference/... -run TestBackendPool -v 2>&1
```
Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/inference/backend_pool.go internal/inference/backend_pool_test.go internal/inference/pipeline.go
git commit -m "feat: add BackendPool for shared Ollama backend management"
```

---

## Task 4: Refactor OllamaPipeline

**Files:**
- Modify: `internal/inference/ollama.go`

The goal is to replace the per-pipeline `backends []*ollamaBackend` and health-checker with `pool *BackendPool + assignedIDs []int64`. The `ollamaBackend` struct itself moves to `backend_pool.go` (already done in Task 3 — move it from ollama.go if it's still there).

- [ ] **Step 1: Move `ollamaBackend` struct to `backend_pool.go` if still in `ollama.go`**

Remove this block from `ollama.go`:
```go
type ollamaBackend struct {
	name         string
	baseURL      string
	client       *http.Client
	healthy      bool
	inUse        bool
	score        float64
	requestCount int64
	lastActiveAt time.Time
}
```
And add the `enabled` field in `backend_pool.go` (already included in Task 3).

Also remove `pingBackend` from `ollama.go` (moved to `backend_pool.go` in Task 3).

- [ ] **Step 2: Update `OllamaPipeline` struct in `ollama.go`**

Replace the struct with:
```go
// OllamaPipeline executes prompts against an Ollama backend pool.
type OllamaPipeline struct {
	name            string
	model           string
	systemPrompt    string
	pool            *BackendPool
	assignedIDs     []int64 // priority-ordered; empty = use full pool
	fastWaitTimeout time.Duration
}
```

Remove fields: `backends`, `mu`, `cond`, `cancelHealth`.

- [ ] **Step 3: Update constructors**

Replace `NewOllamaPipeline` and `NewOllamaPipelineFromConfig`:

```go
// NewOllamaPipelineFromConfig creates an OllamaPipeline from a DB pipeline config map.
// pool is the shared BackendPool. assignedIDs are backend IDs in priority order (empty = use all).
func NewOllamaPipelineFromConfig(name string, cfg map[string]any, pool *BackendPool, assignedIDs []int64, fastWaitTimeout time.Duration) *OllamaPipeline {
	systemPrompt, _ := cfg["system_prompt"].(string)
	if fastWaitTimeout == 0 {
		fastWaitTimeout = 5 * time.Second
	}
	return &OllamaPipeline{
		name:            name,
		model:           stringVal(cfg, "model"),
		systemPrompt:    systemPrompt,
		pool:            pool,
		assignedIDs:     assignedIDs,
		fastWaitTimeout: fastWaitTimeout,
	}
}
```

Remove `NewOllamaPipeline` (it was the old config-based constructor — only `NewOllamaPipelineFromConfig` is used externally).

- [ ] **Step 4: Replace `acquire`, `acquireFastest`, `release`, `markUnhealthy` with pool delegation**

Remove the old `acquire`, `acquireFastest`, `release`, `markUnhealthy`, `bestHealthy`, `fastestIdle` methods.

Add the new acquire:
```go
// acquire gets a backend for this pipeline. If assignedIDs is set, uses priority order
// with per-backend fastWaitTimeout. Otherwise picks any available backend from the pool.
func (p *OllamaPipeline) acquire(ctx context.Context) (*ollamaBackend, error) {
	if len(p.assignedIDs) == 0 {
		return p.pool.AcquireAny(ctx)
	}
	return p.acquirePrioritized(ctx)
}

// acquirePrioritized cycles through assignedIDs in priority order,
// waiting up to fastWaitTimeout per backend before trying the next.
func (p *OllamaPipeline) acquirePrioritized(ctx context.Context) (*ollamaBackend, error) {
	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		for _, id := range p.assignedIDs {
			b := p.pool.Get(id)
			if b == nil {
				continue // disabled or removed
			}
			ok, err := p.pool.AcquireWithTimeout(ctx, b, p.fastWaitTimeout)
			if err != nil {
				return nil, err
			}
			if ok {
				return b, nil
			}
		}
		// all assigned backends busy in this cycle; brief yield before retry
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}
```

- [ ] **Step 5: Replace `release` / `markUnhealthy` call sites in the run/chat methods**

Search `ollama.go` for calls to `p.release(b)` and replace with `p.pool.Release(b)`.
Search for `p.markUnhealthy(b)` and replace with `p.pool.MarkUnhealthy(b)`.

- [ ] **Step 6: Update `Load` method — remove health checker, remove benchmark seeding**

The health checker is now in BackendPool. The `Load` method becomes:
```go
// Load sets the model name. The pool handles backend health checking.
func (p *OllamaPipeline) Load(modelPath string) error {
	p.model = modelPath
	return nil
}
```

Remove `runHealthChecker`, `probeUnhealthy`, `probeHealthy` methods from `ollama.go` (they are now in `backend_pool.go`).

- [ ] **Step 7: Update `Close` method**

```go
// Close is a no-op; BackendPool manages backend lifecycle.
func (p *OllamaPipeline) Close() error {
	return nil
}
```

- [ ] **Step 8: Update `MaxConcurrency` method**

```go
func (p *OllamaPipeline) MaxConcurrency() int {
	return len(p.pool.All())
}
```

- [ ] **Step 9: Update `BackendStats` method — remove it (BackendPool.Stats() replaces it)**

Remove the `BackendStats()` method from `OllamaPipeline`.

- [ ] **Step 10: Build to verify compilation**

```bash
go build ./internal/inference/... 2>&1
```
Expected: no errors. Fix any remaining references.

- [ ] **Step 11: Commit**

```bash
git add internal/inference/ollama.go
git commit -m "refactor: OllamaPipeline uses BackendPool, removes per-pipeline health checker"
```

---

## Task 5: ActionCache Wiring

**Files:**
- Modify: `internal/worker/action_cache.go`

- [ ] **Step 1: Update struct and constructor**

Replace `backends []inference.BackendConfig` with `pool *inference.BackendPool`:

```go
type ActionCache struct {
	mu      sync.RWMutex
	entries map[int64]*CacheEntry

	store    store.Store
	infReg   *inference.Registry
	pipeReg  *pipeline.Registry
	pool     *inference.BackendPool  // replaces backends
	logger   *slog.Logger
}

func NewActionCache(
	st store.Store,
	infReg *inference.Registry,
	pipeReg *pipeline.Registry,
	pool *inference.BackendPool,
	logger *slog.Logger,
) *ActionCache {
	return &ActionCache{
		entries: make(map[int64]*CacheEntry),
		store:   st,
		infReg:  infReg,
		pipeReg: pipeReg,
		pool:    pool,
		logger:  logger,
	}
}
```

- [ ] **Step 2: Update `Load` to fetch pipeline backend IDs**

In `Load`, before building entries, fetch the assignment map:
```go
func (c *ActionCache) Load(ctx context.Context) error {
	// Load pipeline→[]backendID map for priority-ordered assignment.
	pipelineBackendIDs, err := c.store.ListAllPipelineBackendIDs(ctx)
	if err != nil {
		return fmt.Errorf("action_cache: list pipeline backend ids: %w", err)
	}

	active := true
	actions, err := c.store.ListActions(ctx, store.ActionFilter{Active: &active})
	if err != nil {
		return fmt.Errorf("action_cache: list actions: %w", err)
	}

	entries := make(map[int64]*CacheEntry, len(actions))
	for _, a := range actions {
		p, pErr := c.resolvePipeline(ctx, a.PipelineID, pipelineBackendIDs)
		if pErr != nil {
			if c.logger != nil {
				c.logger.Warn("action_cache: skip action (pipeline resolution failed)",
					"action_id", a.ID, "pipeline_id", a.PipelineID, "error", pErr)
			}
			continue
		}
		entries[a.ID] = &CacheEntry{Action: a, Pipeline: p}
	}

	c.mu.Lock()
	c.entries = entries
	c.mu.Unlock()
	return nil
}
```

- [ ] **Step 3: Update `resolvePipeline` signature and Ollama case**

```go
func (c *ActionCache) resolvePipeline(ctx context.Context, pipelineID int64, pipelineBackendIDs map[int64][]int64) (inference.Pipeline, error) {
	row, err := c.store.GetPipeline(ctx, pipelineID)
	if err != nil {
		return nil, fmt.Errorf("get pipeline row: %w", err)
	}

	var cfg map[string]any
	if len(row.Config) > 0 {
		if err := json.Unmarshal(row.Config, &cfg); err != nil {
			cfg = make(map[string]any)
		}
	} else {
		cfg = make(map[string]any)
	}

	switch row.Type {
	case model.PipelineTypeONNX:
		if p := c.infReg.Get(row.Name); p != nil {
			return p, nil
		}
		onnx, err := inference.NewONNXPipelineFromConfig(row.Name, cfg)
		if err != nil {
			return nil, fmt.Errorf("onnx: %w", err)
		}
		var p inference.Pipeline = onnx
		labels, _ := c.store.ListPipelineLabels(ctx, pipelineID)
		if len(labels) > 0 {
			if lp, ok := p.(inference.LabeledPipeline); ok {
				lp.SetLabels(labels)
			}
		}
		c.infReg.Register(p)
		return p, nil

	case model.PipelineTypeOllama:
		if p := c.infReg.Get(row.Name); p != nil {
			return p, nil
		}
		assignedIDs := pipelineBackendIDs[pipelineID] // nil = use full pool
		p := inference.NewOllamaPipelineFromConfig(row.Name, cfg, c.pool, assignedIDs, 0)
		c.infReg.Register(p)
		return p, nil

	case model.PipelineTypeInternal:
		return c.pipeReg.Build(row.Name, cfg)

	default:
		return nil, fmt.Errorf("unknown pipeline type: %s", row.Type)
	}
}
```

- [ ] **Step 4: Build**

```bash
go build ./internal/worker/... 2>&1
```
Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add internal/worker/action_cache.go
git commit -m "refactor: ActionCache uses BackendPool instead of []BackendConfig"
```

---

## Task 6: Config Cleanup + serve.go Wiring

**Files:**
- Modify: `internal/config/config.go`
- Modify: `cli/cmd/serve.go`

- [ ] **Step 1: Strip Ollama backend fields from config**

In `internal/config/config.go`:

1. Delete `OllamaBackendConfig` struct (lines 14-17).
2. In `Config.Ollama` struct, remove `BaseURL`, `BaseURLs`, `Backends` fields. Keep only `FastWaitTimeout`:
```go
Ollama struct {
    FastWaitTimeout time.Duration `yaml:"fast_wait_timeout"`
} `yaml:"ollama"`
```
3. In `defaults()`, remove `cfg.Ollama.BaseURL = "http://localhost:11434"`.
4. In `applyEnv()`, remove the `IRAN_WATCHER_OLLAMA_BASE_URL` and `IRAN_WATCHER_OLLAMA_BASE_URLS` blocks (lines 215-224).
5. Delete `OllamaBackends()` method (lines 139-154).
6. Delete `OllamaBaseURLs()` method (lines 157-166).

- [ ] **Step 2: Update `cli/cmd/serve.go`**

Remove line 90: `ollamaBackends := ollamaBackendConfigs(cfg.OllamaBackends())`

Remove the `ollamaBackendConfigs` helper function (lines 253-261).

After the store is opened (after line 75), load backends from DB and create BackendPool:
```go
// Load Ollama backends from DB.
rawBackends, err := s.ListOllamaBackends(ctx)
if err != nil {
    return fmt.Errorf("failed to load ollama backends: %w", err)
}
pool := inference.NewBackendPool()
pool.Load(toBackendRecords(rawBackends))
defer pool.Close()
```

Add `toBackendRecords` helper at the bottom of serve.go:
```go
func toBackendRecords(backends []model.OllamaBackend) []inference.BackendRecord {
	out := make([]inference.BackendRecord, len(backends))
	for i, b := range backends {
		out[i] = inference.BackendRecord{ID: b.ID, Name: b.Name, URL: b.URL, Enabled: b.Enabled}
	}
	return out
}
```

Update the ActionCache construction (line 134):
```go
cache := worker.NewActionCache(s, infReg, pipeReg, pool, logger)
```

Update `api.NewServerWithConcreteRules` call — replace `ollamaBackends, cfg.Ollama.FastWaitTimeout` with `pool, cfg.Ollama.FastWaitTimeout`.

Add `model` import to serve.go imports if not already present.

- [ ] **Step 3: Build**

```bash
go build ./... 2>&1
```
Expected: no errors. Fix any remaining `BackendConfig` or `ollamaBackends` references.

- [ ] **Step 4: Commit**

```bash
git add internal/config/config.go cli/cmd/serve.go
git commit -m "refactor: remove Ollama backend config fields, load backends from DB at startup"
```

---

## Task 7: API Server — Update Struct + Backend CRUD Endpoints

**Files:**
- Modify: `internal/api/server.go`
- Modify: `internal/api/handlers_ollama.go`

- [ ] **Step 1: Update `Server` struct and constructor**

In `server.go`, replace:
```go
ollamaBackends        []inference.BackendConfig
ollamaFastWaitTimeout time.Duration
```
With:
```go
pool                  *inference.BackendPool
ollamaFastWaitTimeout time.Duration
```

Update `NewServer` parameter and body accordingly (replace `ollamaBackends []inference.BackendConfig` with `pool *inference.BackendPool`, and `srv.ollamaBackends = ollamaBackends` → `srv.pool = pool`).

- [ ] **Step 2: Update `ollamaPipelineForUsecase` in `handlers_ollama.go`**

Replace the old construction:
```go
p := inference.NewOllamaPipelineFromConfig(row.Name, cfg, s.ollamaBackends, s.ollamaFastWaitTimeout)
```
With:
```go
// Load pipeline_backends assignment for this specific pipeline
assignedIDs, err := s.loadAssignedIDs(ctx, row.ID)
if err != nil {
    return nil, fmt.Errorf("load assigned ids: %w", err)
}
p := inference.NewOllamaPipelineFromConfig(row.Name, cfg, s.pool, assignedIDs, s.ollamaFastWaitTimeout)
```

Add helper on `*Server`:
```go
func (s *Server) loadAssignedIDs(ctx context.Context, pipelineID int64) ([]int64, error) {
	pbs, err := s.store.ListPipelineBackends(ctx, pipelineID)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, len(pbs))
	for i, pb := range pbs {
		ids[i] = pb.BackendID
	}
	return ids, nil
}
```

- [ ] **Step 3: Rewrite `handlers_ollama.go` with CRUD + assignment handlers**

Replace the entire file:

```go
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/store"
	"github.com/go-chi/chi/v5"
)

// handleListOllamaBackends returns all backends with live stats.
// GET /api/ollama/backends
func (s *Server) handleListOllamaBackends(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.ListOllamaBackends(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	stats := s.pool.Stats()
	statsByID := make(map[int64]inference.BackendStat, len(stats))
	for _, st := range stats {
		statsByID[st.ID] = st
	}
	type backendRow struct {
		model.OllamaBackend
		inference.BackendStat
	}
	out := make([]map[string]any, 0, len(rows))
	for _, b := range rows {
		st := statsByID[b.ID]
		out = append(out, map[string]any{
			"id":            b.ID,
			"name":          b.Name,
			"url":           b.URL,
			"enabled":       b.Enabled,
			"created_at":    b.CreatedAt,
			"healthy":       st.Healthy,
			"in_use":        st.InUse,
			"score":         st.Score,
			"request_count": st.RequestCount,
			"last_active_at": st.LastActiveAt,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleCreateOllamaBackend creates a new backend.
// POST /api/ollama/backends
func (s *Server) handleCreateOllamaBackend(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.URL == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}
	if req.Name == "" {
		req.Name = req.URL
	}
	b, err := s.store.CreateOllamaBackend(r.Context(), req.Name, req.URL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.reloadPool(r.Context())
	writeJSON(w, http.StatusCreated, b)
}

// handleUpdateOllamaBackend updates name and URL.
// PUT /api/ollama/backends/{id}
func (s *Server) handleUpdateOllamaBackend(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	existing, err := s.store.GetOllamaBackend(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if req.Name == "" {
		req.Name = existing.Name
	}
	if req.URL == "" {
		req.URL = existing.URL
	}
	if err := s.store.UpdateOllamaBackend(r.Context(), id, req.Name, req.URL); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.reloadPool(r.Context())
	b, _ := s.store.GetOllamaBackend(r.Context(), id)
	writeJSON(w, http.StatusOK, b)
}

// handleDeleteOllamaBackend deletes a backend.
// DELETE /api/ollama/backends/{id}
func (s *Server) handleDeleteOllamaBackend(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.DeleteOllamaBackend(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.reloadPool(r.Context())
	w.WriteHeader(http.StatusNoContent)
}

// handleEnableOllamaBackend enables a backend.
// POST /api/ollama/backends/{id}/enable
func (s *Server) handleEnableOllamaBackend(w http.ResponseWriter, r *http.Request) {
	s.setOllamaBackendEnabled(w, r, true)
}

// handleDisableOllamaBackend disables a backend.
// POST /api/ollama/backends/{id}/disable
func (s *Server) handleDisableOllamaBackend(w http.ResponseWriter, r *http.Request) {
	s.setOllamaBackendEnabled(w, r, false)
}

func (s *Server) setOllamaBackendEnabled(w http.ResponseWriter, r *http.Request, enabled bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.store.SetOllamaBackendEnabled(r.Context(), id, enabled); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.reloadPool(r.Context())
	w.WriteHeader(http.StatusNoContent)
}

// handleListPipelineBackends returns a pipeline's assigned backends in priority order.
// GET /api/pipelines/{id}/backends
func (s *Server) handleListPipelineBackends(w http.ResponseWriter, r *http.Request) {
	pipelineID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	pbs, err := s.store.ListPipelineBackends(r.Context(), pipelineID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, pbs)
}

// handleSetPipelineBackends replaces a pipeline's full backend assignment list.
// PUT /api/pipelines/{id}/backends
func (s *Server) handleSetPipelineBackends(w http.ResponseWriter, r *http.Request) {
	pipelineID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req []struct {
		BackendID int64 `json:"backend_id"`
		Priority  int   `json:"priority"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	assignments := make([]model.PipelineBackend, len(req))
	for i, r := range req {
		assignments[i] = model.PipelineBackend{PipelineID: pipelineID, BackendID: r.BackendID, Priority: r.Priority}
	}
	if err := s.store.SetPipelineBackends(r.Context(), pipelineID, assignments); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.reloadCache(r.Context())
	pbs, _ := s.store.ListPipelineBackends(r.Context(), pipelineID)
	writeJSON(w, http.StatusOK, pbs)
}

// handleRemovePipelineBackend removes one backend from a pipeline's assignment list.
// DELETE /api/pipelines/{id}/backends/{backend_id}
func (s *Server) handleRemovePipelineBackend(w http.ResponseWriter, r *http.Request) {
	pipelineID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	backendID, err := strconv.ParseInt(chi.URLParam(r, "backend_id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid backend_id")
		return
	}
	if err := s.store.RemovePipelineBackend(r.Context(), pipelineID, backendID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.reloadCache(r.Context())
	w.WriteHeader(http.StatusNoContent)
}

// ollamaPipelineForUsecase finds the single active Ollama pipeline for the given usecase.
func (s *Server) ollamaPipelineForUsecase(ctx context.Context, usecase string) (inference.Pipeline, error) {
	pt := model.PipelineTypeOllama
	active := true
	rows, err := s.store.ListPipelines(ctx, store.PipelineFilter{
		Type:    &pt,
		Active:  &active,
		Usecase: &usecase,
	})
	if err != nil {
		return nil, fmt.Errorf("list pipelines: %w", err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("no active %s pipeline — create an Ollama pipeline with usecase=%s", usecase, usecase)
	}
	row := rows[0]
	if p := s.registry.Get(row.Name); p != nil {
		return p, nil
	}
	var cfg map[string]any
	_ = json.Unmarshal(row.Config, &cfg)
	assignedIDs, err := s.loadAssignedIDs(ctx, row.ID)
	if err != nil {
		return nil, fmt.Errorf("load assigned ids: %w", err)
	}
	p := inference.NewOllamaPipelineFromConfig(row.Name, cfg, s.pool, assignedIDs, s.ollamaFastWaitTimeout)
	s.registry.Register(p)
	return p, nil
}

// loadAssignedIDs returns backend IDs in priority order for a pipeline.
func (s *Server) loadAssignedIDs(ctx context.Context, pipelineID int64) ([]int64, error) {
	pbs, err := s.store.ListPipelineBackends(ctx, pipelineID)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, len(pbs))
	for i, pb := range pbs {
		ids[i] = pb.BackendID
	}
	return ids, nil
}

// reloadPool reloads BackendPool from DB then reloads ActionCache.
func (s *Server) reloadPool(ctx context.Context) {
	backends, err := s.store.ListOllamaBackends(ctx)
	if err != nil {
		s.logger.Error("api: reload pool: list backends", "error", err)
		return
	}
	records := make([]inference.BackendRecord, len(backends))
	for i, b := range backends {
		records[i] = inference.BackendRecord{ID: b.ID, Name: b.Name, URL: b.URL, Enabled: b.Enabled}
	}
	s.pool.Load(records)
	s.reloadCache(ctx)
}

// reloadCache reloads the ActionCache from DB.
func (s *Server) reloadCache(ctx context.Context) {
	if err := s.actionCache.Load(ctx); err != nil {
		s.logger.Error("api: reload action cache", "error", err)
	}
}
```

- [ ] **Step 4: Register new routes in `server.go`**

Find the route registration block in `NewServer`. Add under the existing ollama route:

```go
// Ollama backend management (admin)
r.Group(func(r chi.Router) {
    r.Use(adminOnly)
    r.Get("/api/ollama/backends", srv.handleListOllamaBackends)
    r.Post("/api/ollama/backends", srv.handleCreateOllamaBackend)
    r.Put("/api/ollama/backends/{id}", srv.handleUpdateOllamaBackend)
    r.Delete("/api/ollama/backends/{id}", srv.handleDeleteOllamaBackend)
    r.Post("/api/ollama/backends/{id}/enable", srv.handleEnableOllamaBackend)
    r.Post("/api/ollama/backends/{id}/disable", srv.handleDisableOllamaBackend)
    // Pipeline-backend assignments
    r.Get("/api/pipelines/{id}/backends", srv.handleListPipelineBackends)
    r.Put("/api/pipelines/{id}/backends", srv.handleSetPipelineBackends)
    r.Delete("/api/pipelines/{id}/backends/{backend_id}", srv.handleRemovePipelineBackend)
})
```

Replace the old single route `r.Get("/api/ollama/backends", srv.handleOllamaBackends)` with the new group.

- [ ] **Step 5: Build**

```bash
go build ./internal/api/... 2>&1
go build ./... 2>&1
```
Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add internal/api/server.go internal/api/handlers_ollama.go
git commit -m "feat: Ollama backend CRUD and pipeline-backend assignment API endpoints"
```

---

## Task 8: CLI — Ollama Backends Commands

**Files:**
- Modify: `cli/cmd/ollama.go`

- [ ] **Step 1: Read `cli/cmd/ollama.go`** to understand current structure before editing.

- [ ] **Step 2: Add CRUD subcommands to `ollamaCmd`**

The existing `ollama backends` subcommand should become the `list` sub-subcommand. Add `add`, `update`, `remove`, `enable`, `disable`:

```go
var ollamaBackendsCmd = &cobra.Command{
    Use:   "backends",
    Short: "Manage Ollama backends",
}

var ollamaBackendsListCmd = &cobra.Command{
    Use:   "list",
    Short: "List all Ollama backends with live stats",
    RunE:  runOllamaBackendsList,
}

var (
    addBackendName string
    addBackendURL  string
)

var ollamaBackendsAddCmd = &cobra.Command{
    Use:   "add",
    Short: "Add a new Ollama backend",
    RunE:  runOllamaBackendsAdd,
}

var ollamaBackendsUpdateCmd = &cobra.Command{
    Use:   "update <id>",
    Short: "Update an Ollama backend's name or URL",
    Args:  cobra.ExactArgs(1),
    RunE:  runOllamaBackendsUpdate,
}

var ollamaBackendsRemoveCmd = &cobra.Command{
    Use:   "remove <id>",
    Short: "Delete an Ollama backend",
    Args:  cobra.ExactArgs(1),
    RunE:  runOllamaBackendsRemove,
}

var ollamaBackendsEnableCmd = &cobra.Command{
    Use:   "enable <id>",
    Short: "Enable an Ollama backend",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        return runOllamaBackendsSetEnabled(cmd, args, true)
    },
}

var ollamaBackendsDisableCmd = &cobra.Command{
    Use:   "disable <id>",
    Short: "Disable an Ollama backend",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        return runOllamaBackendsSetEnabled(cmd, args, false)
    },
}
```

Wire them in `init()`:
```go
func init() {
    ollamaBackendsAddCmd.Flags().StringVar(&addBackendName, "name", "", "display name (defaults to URL)")
    ollamaBackendsAddCmd.Flags().StringVar(&addBackendURL, "url", "", "Ollama server URL (required)")
    _ = ollamaBackendsAddCmd.MarkFlagRequired("url")

    ollamaBackendsUpdateCmd.Flags().StringVar(&addBackendName, "name", "", "new display name")
    ollamaBackendsUpdateCmd.Flags().StringVar(&addBackendURL, "url", "", "new URL")

    ollamaBackendsCmd.AddCommand(ollamaBackendsListCmd, ollamaBackendsAddCmd, ollamaBackendsUpdateCmd,
        ollamaBackendsRemoveCmd, ollamaBackendsEnableCmd, ollamaBackendsDisableCmd)
    ollamaCmd.AddCommand(ollamaBackendsCmd)
    rootCmd.AddCommand(ollamaCmd)
}
```

Implement run functions using the existing `apiClient()` / `mustGet` / `mustPost` / `mustPut` / `mustDelete` helpers from the CLI:

```go
func runOllamaBackendsList(cmd *cobra.Command, _ []string) error {
    var backends []map[string]any
    if err := apiGet("/api/ollama/backends", &backends); err != nil {
        return err
    }
    w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
    fmt.Fprintln(w, "ID\tNAME\tURL\tENABLED\tHEALTHY\tIN_USE\tSCORE\tREQUESTS\tLAST_ACTIVE")
    for _, b := range backends {
        fmt.Fprintf(w, "%v\t%v\t%v\t%v\t%v\t%v\t%.1f\t%v\t%v\n",
            b["id"], b["name"], b["url"], b["enabled"], b["healthy"], b["in_use"],
            b["score"], b["request_count"], b["last_active_at"])
    }
    return w.Flush()
}

func runOllamaBackendsAdd(_ *cobra.Command, _ []string) error {
    body := map[string]any{"name": addBackendName, "url": addBackendURL}
    var result any
    return apiPost("/api/ollama/backends", body, &result)
}

func runOllamaBackendsUpdate(_ *cobra.Command, args []string) error {
    body := map[string]any{"name": addBackendName, "url": addBackendURL}
    var result any
    return apiPut("/api/ollama/backends/"+args[0], body, &result)
}

func runOllamaBackendsRemove(_ *cobra.Command, args []string) error {
    return apiDelete("/api/ollama/backends/" + args[0])
}

func runOllamaBackendsSetEnabled(_ *cobra.Command, args []string, enabled bool) error {
    action := "enable"
    if !enabled {
        action = "disable"
    }
    return apiPost("/api/ollama/backends/"+args[0]+"/"+action, nil, nil)
}
```

- [ ] **Step 3: Build**

```bash
go build ./cli/... 2>&1
```
Expected: no errors. Adapt `apiGet`/`apiPost`/`apiPut`/`apiDelete` calls to match the actual helper names in the codebase.

- [ ] **Step 4: Commit**

```bash
git add cli/cmd/ollama.go
git commit -m "feat: CLI commands for Ollama backend CRUD (add/update/remove/enable/disable)"
```

---

## Task 9: CLI — Pipeline Backends Commands

**Files:**
- Modify: `cli/cmd/pipeline.go`

- [ ] **Step 1: Read `cli/cmd/pipeline.go`** to understand existing structure.

- [ ] **Step 2: Add `pipeline backends` subcommands**

```go
var pipelineBackendsCmd = &cobra.Command{
    Use:   "backends",
    Short: "Manage pipeline backend assignments",
}

var pipelineBackendsListCmd = &cobra.Command{
    Use:   "list <pipeline-id>",
    Short: "List backends assigned to a pipeline in priority order",
    Args:  cobra.ExactArgs(1),
    RunE:  runPipelineBackendsList,
}

var pipelineBackendsSetCmd = &cobra.Command{
    Use:   "set <pipeline-id> <backend-id>:<priority> [<backend-id>:<priority> ...]",
    Short: "Replace the full backend assignment list for a pipeline",
    Long: `Replaces all backend assignments for the pipeline atomically.
Each argument is backend-id:priority, e.g.: set 3 1:1 2:2`,
    Args: cobra.MinimumNArgs(2),
    RunE: runPipelineBackendsSet,
}

var pipelineBackendsUnassignCmd = &cobra.Command{
    Use:   "unassign <pipeline-id> <backend-id>",
    Short: "Remove one backend from a pipeline's assignment list",
    Args:  cobra.ExactArgs(2),
    RunE:  runPipelineBackendsUnassign,
}
```

Wire in `init()` (add to the existing pipeline command setup):
```go
pipelineBackendsCmd.AddCommand(pipelineBackendsListCmd, pipelineBackendsSetCmd, pipelineBackendsUnassignCmd)
pipelineCmd.AddCommand(pipelineBackendsCmd)
```

Implementations:
```go
func runPipelineBackendsList(cmd *cobra.Command, args []string) error {
    var pbs []map[string]any
    if err := apiGet("/api/pipelines/"+args[0]+"/backends", &pbs); err != nil {
        return err
    }
    w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
    fmt.Fprintln(w, "PIPELINE_ID\tBACKEND_ID\tPRIORITY")
    for _, pb := range pbs {
        fmt.Fprintf(w, "%v\t%v\t%v\n", pb["pipeline_id"], pb["backend_id"], pb["priority"])
    }
    return w.Flush()
}

func runPipelineBackendsSet(_ *cobra.Command, args []string) error {
    type assignment struct {
        BackendID int64 `json:"backend_id"`
        Priority  int   `json:"priority"`
    }
    pipelineID := args[0]
    assignments := make([]assignment, 0, len(args)-1)
    for _, pair := range args[1:] {
        parts := strings.SplitN(pair, ":", 2)
        if len(parts) != 2 {
            return fmt.Errorf("invalid pair %q: expected backend-id:priority", pair)
        }
        bid, err := strconv.ParseInt(parts[0], 10, 64)
        if err != nil {
            return fmt.Errorf("invalid backend-id %q: %w", parts[0], err)
        }
        pri, err := strconv.Atoi(parts[1])
        if err != nil {
            return fmt.Errorf("invalid priority %q: %w", parts[1], err)
        }
        assignments = append(assignments, assignment{BackendID: bid, Priority: pri})
    }
    var result any
    return apiPut("/api/pipelines/"+pipelineID+"/backends", assignments, &result)
}

func runPipelineBackendsUnassign(_ *cobra.Command, args []string) error {
    return apiDelete("/api/pipelines/" + args[0] + "/backends/" + args[1])
}
```

- [ ] **Step 3: Build**

```bash
go build ./cli/... 2>&1
```

- [ ] **Step 4: Commit**

```bash
git add cli/cmd/pipeline.go
git commit -m "feat: CLI commands for pipeline backend assignment (list/set/unassign)"
```

---

## Task 10: UI — OllamaView + API Client

**Files:**
- Modify: `web/src/api/ollama.ts`
- Modify: `web/src/views/OllamaView.vue`

- [ ] **Step 1: Update `web/src/api/ollama.ts`**

Read the current file, then replace/extend its contents with typed functions for the new endpoints:

```typescript
import { apiClient } from './client'  // use existing API client pattern

export interface OllamaBackend {
  id: number
  name: string
  url: string
  enabled: boolean
  created_at: string
  healthy: boolean
  in_use: boolean
  score: number
  request_count: number
  last_active_at: string | null
}

export interface PipelineBackend {
  pipeline_id: number
  backend_id: number
  priority: number
}

export const listOllamaBackends = () =>
  apiClient.get<OllamaBackend[]>('/api/ollama/backends')

export const createOllamaBackend = (name: string, url: string) =>
  apiClient.post<OllamaBackend>('/api/ollama/backends', { name, url })

export const updateOllamaBackend = (id: number, name: string, url: string) =>
  apiClient.put<OllamaBackend>(`/api/ollama/backends/${id}`, { name, url })

export const deleteOllamaBackend = (id: number) =>
  apiClient.delete(`/api/ollama/backends/${id}`)

export const enableOllamaBackend = (id: number) =>
  apiClient.post(`/api/ollama/backends/${id}/enable`, {})

export const disableOllamaBackend = (id: number) =>
  apiClient.post(`/api/ollama/backends/${id}/disable`, {})

export const listPipelineBackends = (pipelineId: number) =>
  apiClient.get<PipelineBackend[]>(`/api/pipelines/${pipelineId}/backends`)

export const setPipelineBackends = (pipelineId: number, assignments: { backend_id: number; priority: number }[]) =>
  apiClient.put<PipelineBackend[]>(`/api/pipelines/${pipelineId}/backends`, assignments)

export const removePipelineBackend = (pipelineId: number, backendId: number) =>
  apiClient.delete(`/api/pipelines/${pipelineId}/backends/${backendId}`)
```

Adapt the exact import path and `apiClient` usage to match the existing pattern in the file.

- [ ] **Step 2: Rewrite `web/src/views/OllamaView.vue`**

Read the current file to understand existing patterns (component setup, polling, auth handling). Then restructure into three sections/tabs:

**Tab 1 — Backends** (management):
- Table columns: Name, URL, Enabled toggle, Healthy (indicator), Score, Requests, Last Active, Actions (edit/delete)
- "Add Backend" button → inline form with Name + URL fields → calls `createOllamaBackend`
- Enabled toggle calls `enableOllamaBackend` / `disableOllamaBackend`
- Edit icon opens inline edit row for Name + URL → calls `updateOllamaBackend`
- Delete icon calls `deleteOllamaBackend` (with confirmation)
- Poll `listOllamaBackends` every 5s (keep existing polling pattern)

**Tab 2 — Assignments**:
- Dropdown to select a pipeline (list of Ollama pipelines from `GET /api/pipelines?type=ollama`)
- On selection, load that pipeline's assigned backends via `listPipelineBackends`
- Show assigned backends as an ordered list (priority 1 at top):
  - Each row: backend name/URL, priority number, remove button
  - "Add backend" dropdown with available backends, "Priority" number input → calls `setPipelineBackends` (full replace)
- Unassigned pipelines show "uses any available backend" label
- Drag-to-reorder is optional (a simple up/down arrow approach is acceptable)

**Tab 3 — Live Stats** (read-only):
- Keep existing stats view (one card per backend, health, score, request count, last active)
- Sources data from Tab 1's poll (same `listOllamaBackends` data)

Structure the component with `ref` for active tab, backend list, selected pipeline, assignment list. Use `onMounted` and a polling interval. Follow existing patterns in the file for auth token, error handling, and API calls.

- [ ] **Step 3: Build the frontend**

```bash
cd /mnt/shared/projects/personal/iran-watcher/web
npm run build 2>&1 | tail -20
```
Expected: build succeeds with no errors.

- [ ] **Step 4: Commit**

```bash
git add web/src/api/ollama.ts web/src/views/OllamaView.vue
git commit -m "feat: OllamaView management, assignment, and live stats panels"
```

---

## Task 11: CLAUDE.md + Docker Build Verification

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Update CLAUDE.md**

In the **Project Structure** section, the `internal/store/` line should mention `sqlite_ollama.go`.

In **Key Design Decisions**, under the Ollama integration bullet:
- Remove references to `ollama.base_url`, `ollama.base_urls`, `IRAN_WATCHER_OLLAMA_BASE_URLS`
- Replace with: backends stored in `ollama_backends` table; per-pipeline priority assignment in `pipeline_backends` table; `BackendPool` (`internal/inference/backend_pool.go`) owns health checking and acquisition; `OllamaPipeline` holds `assignedIDs []int64` in priority order; mutating API calls trigger `pool.Load()` + `actionCache.Load()` for hot-reload

Add new API routes to Key Design Decisions (under the existing route list):
- `GET|POST /api/ollama/backends`, `PUT|DELETE /api/ollama/backends/{id}`, `POST /api/ollama/backends/{id}/{enable,disable}`
- `GET /api/pipelines/{id}/backends`, `PUT /api/pipelines/{id}/backends`, `DELETE /api/pipelines/{id}/backends/{backend_id}`

- [ ] **Step 2: Docker build verification**

```bash
cd /mnt/shared/projects/personal/iran-watcher
docker compose build --build-arg BUILDKIT_INLINE_CACHE=1 iran-watcher 2>&1 | tail -30
```
Expected: `Successfully built` / `FINISHED`. Fix any compile errors surfaced by the Docker build.

- [ ] **Step 3: Final commit**

```bash
git add CLAUDE.md
git commit -m "docs: update CLAUDE.md for DB-stored Ollama backends with per-pipeline priority assignment"
```

---

## Self-Review Checklist

- [x] **Spec coverage:**
  - DB schema (migration 019) ✓ Task 1
  - BackendPool with shared health checker ✓ Task 3
  - `enabled` field on backend ✓ Task 1 + Task 3
  - OllamaPipeline pool+assignedIDs replace backends slice ✓ Task 4
  - Acquire algorithm: priority order with per-backend timeout ✓ Task 4
  - Fallback to full pool when assignedIDs empty ✓ Task 4
  - Config cleanup (remove base_url/base_urls/backends) ✓ Task 6
  - Backend CRUD API endpoints ✓ Task 7
  - Pipeline-backend assignment API endpoints ✓ Task 7
  - reload sequence (pool.Load + actionCache.Load) ✓ Task 7
  - CLI ollama backends CRUD ✓ Task 8
  - CLI pipeline backends set/list/unassign ✓ Task 9
  - UI three panels ✓ Task 10
  - CLAUDE.md updates ✓ Task 11
  - Docker build verification ✓ Task 11

- [x] **Type consistency:**
  - `BackendRecord` used in Tasks 3, 5, 6, 7 — matches definition in Task 3
  - `NewOllamaPipelineFromConfig(name, cfg, pool, assignedIDs, fastWaitTimeout)` — 5 params used consistently in Tasks 4, 5, 7
  - `NewActionCache(store, infReg, pipeReg, pool, logger)` — used in Tasks 5, 6
  - `pool *inference.BackendPool` on Server — used in Tasks 7, 6
  - `ListAllPipelineBackendIDs` returns `map[int64][]int64` — used in Tasks 2, 5
  - `BackendStat.ID int64` added in Task 3, consumed in Task 7

- [x] **No placeholders:** All steps have exact code, exact commands, and expected outputs.
