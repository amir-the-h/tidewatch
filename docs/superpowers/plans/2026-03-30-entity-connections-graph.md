# Entity Connections Graph Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a "Connections" tab to the Tracker page that visualizes co-occurrence relationships between tracked entities as a force-directed graph.

**Architecture:** A new `entity_cooccurrence` internal pipeline reads `matched_ids` from the entity_matcher context and upserts weight-incremented rows into a new `entity_connections` table for each co-occurring pair. A `GET /api/entity-connections` endpoint serves all edges with entity names joined. The frontend Connections tab renders a vis-network force-directed graph from those edges.

**Tech Stack:** Go (internal pipeline + store + API handler), SQLite (new migration), Vue 3 + Naive UI (tab), vis-network (graph library), Pinia (store), TypeScript (API client)

---

## File Map

| File | Create / Modify | Purpose |
|------|-----------------|---------|
| `migrations/024_entity_connections.sql` | Create | New table + indexes |
| `internal/model/entity.go` | Modify | Add `EntityConnection` struct |
| `internal/store/store.go` | Modify | Add 3 interface methods |
| `internal/store/sqlite.go` | Modify | Implement the 3 store methods |
| `internal/store/sqlite_test.go` | Modify | Tests for entity connection store methods |
| `internal/pipeline/entity_cooccurrence.go` | Create | Pipeline logic |
| `internal/pipeline/entity_cooccurrence_test.go` | Create | Pipeline unit tests |
| `cli/cmd/serve.go` | Modify | Register pipeline handler |
| `internal/api/handlers_entity_connections.go` | Create | GET + DELETE handlers |
| `internal/api/api_test.go` | Modify | Add mock store methods |
| `internal/api/server.go` | Modify | Register 2 routes |
| `web/package.json` | Modify | Add `vis-network` dependency |
| `web/src/api/connections.ts` | Create | API client |
| `web/src/stores/connections.ts` | Create | Pinia store |
| `web/src/views/TrackerView.vue` | Modify | Add Connections tab with graph |
| `CLAUDE.md` | Modify | Document new pipeline, table, endpoints |
| `docs/superpowers/specs/2026-03-30-entity-connections-graph-design.md` | Modify | Mark as implemented |

---

## Task 1: Migration

**Files:**
- Create: `migrations/024_entity_connections.sql`

- [ ] **Step 1: Write the migration**

```sql
CREATE TABLE IF NOT EXISTS entity_connections (
    from_id    INTEGER NOT NULL REFERENCES tracked_entities(id) ON DELETE CASCADE,
    to_id      INTEGER NOT NULL REFERENCES tracked_entities(id) ON DELETE CASCADE,
    weight     INTEGER NOT NULL DEFAULT 1,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (from_id, to_id),
    CHECK (from_id < to_id)
);

CREATE INDEX IF NOT EXISTS idx_entity_connections_from ON entity_connections(from_id);
CREATE INDEX IF NOT EXISTS idx_entity_connections_to   ON entity_connections(to_id);
```

- [ ] **Step 2: Verify migration picks up automatically**

The store uses embedded migrations from `migrations/FS`. New `.sql` files in that directory are applied in filename order. Confirm `migrations/embed.go` uses `//go:embed *.sql` (open it and check — it should already cover `024_entity_connections.sql`).

- [ ] **Step 3: Commit**

```bash
git add migrations/024_entity_connections.sql
git commit -m "feat: add entity_connections migration"
```

---

## Task 2: Model

**Files:**
- Modify: `internal/model/entity.go`

- [ ] **Step 1: Add `EntityConnection` struct**

Append to the bottom of `internal/model/entity.go`:

```go
// EntityConnection records the co-occurrence weight between two tracked entities.
// from_id is always less than to_id (enforced by the DB CHECK constraint).
type EntityConnection struct {
	FromID    int64     `json:"from_id"`
	ToID      int64     `json:"to_id"`
	Weight    int       `json:"weight"`
	UpdatedAt time.Time `json:"updated_at"`
	// Joined fields populated by ListEntityConnections.
	FromName string `json:"from_name,omitempty"`
	FromType string `json:"from_type,omitempty"`
	ToName   string `json:"to_name,omitempty"`
	ToType   string `json:"to_type,omitempty"`
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/model/entity.go
git commit -m "feat: add EntityConnection model"
```

---

## Task 3: Store Interface

**Files:**
- Modify: `internal/store/store.go`

- [ ] **Step 1: Add interface methods**

Find the `// EntityCandidates` section in `store.go` and add a new section after the `DeleteEntityCandidate` line:

```go
// EntityConnections
UpsertEntityConnection(ctx context.Context, fromID, toID int64) error
ListEntityConnections(ctx context.Context) ([]*model.EntityConnection, error)
DeleteAllEntityConnections(ctx context.Context) error
```

- [ ] **Step 2: Commit**

```bash
git add internal/store/store.go
git commit -m "feat: add EntityConnection store interface methods"
```

---

## Task 4: Store Implementation + Tests

**Files:**
- Modify: `internal/store/sqlite.go`
- Modify: `internal/store/sqlite_test.go`

- [ ] **Step 1: Write the failing tests**

Add to the bottom of `internal/store/sqlite_test.go`:

```go
func seedTrackedEntity(t *testing.T, s Store, name string) *model.TrackedEntity {
	t.Helper()
	e := &model.TrackedEntity{Name: name, Type: "PER", Aliases: []string{}, Active: true}
	if err := s.SaveTrackedEntity(context.Background(), e); err != nil {
		t.Fatalf("SaveTrackedEntity(%q): %v", name, err)
	}
	return e
}

func TestUpsertEntityConnection_NewPair(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	a := seedTrackedEntity(t, s, "Alpha")
	b := seedTrackedEntity(t, s, "Beta")

	if err := s.UpsertEntityConnection(ctx, a.ID, b.ID); err != nil {
		t.Fatalf("UpsertEntityConnection: %v", err)
	}

	conns, err := s.ListEntityConnections(ctx)
	if err != nil {
		t.Fatalf("ListEntityConnections: %v", err)
	}
	if len(conns) != 1 {
		t.Fatalf("want 1 connection, got %d", len(conns))
	}
	if conns[0].Weight != 1 {
		t.Errorf("want weight=1, got %d", conns[0].Weight)
	}
}

func TestUpsertEntityConnection_IncrementsWeight(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	a := seedTrackedEntity(t, s, "Alpha")
	b := seedTrackedEntity(t, s, "Beta")

	for i := 0; i < 3; i++ {
		if err := s.UpsertEntityConnection(ctx, a.ID, b.ID); err != nil {
			t.Fatalf("UpsertEntityConnection (iter %d): %v", i, err)
		}
	}

	conns, _ := s.ListEntityConnections(ctx)
	if conns[0].Weight != 3 {
		t.Errorf("want weight=3, got %d", conns[0].Weight)
	}
}

func TestUpsertEntityConnection_NormalizesOrder(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	a := seedTrackedEntity(t, s, "Alpha")
	b := seedTrackedEntity(t, s, "Beta")

	// Insert with reversed order — should still produce one row.
	_ = s.UpsertEntityConnection(ctx, b.ID, a.ID)
	_ = s.UpsertEntityConnection(ctx, a.ID, b.ID)

	conns, _ := s.ListEntityConnections(ctx)
	if len(conns) != 1 {
		t.Fatalf("want 1 connection (deduped), got %d", len(conns))
	}
	if conns[0].Weight != 2 {
		t.Errorf("want weight=2, got %d", conns[0].Weight)
	}
}

func TestListEntityConnections_JoinsNames(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	a := seedTrackedEntity(t, s, "Alpha")
	b := seedTrackedEntity(t, s, "Beta")
	_ = s.UpsertEntityConnection(ctx, a.ID, b.ID)

	conns, _ := s.ListEntityConnections(ctx)
	if conns[0].FromName == "" || conns[0].ToName == "" {
		t.Errorf("want joined names, got from=%q to=%q", conns[0].FromName, conns[0].ToName)
	}
}

func TestDeleteAllEntityConnections(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	a := seedTrackedEntity(t, s, "Alpha")
	b := seedTrackedEntity(t, s, "Beta")
	_ = s.UpsertEntityConnection(ctx, a.ID, b.ID)

	if err := s.DeleteAllEntityConnections(ctx); err != nil {
		t.Fatalf("DeleteAllEntityConnections: %v", err)
	}

	conns, _ := s.ListEntityConnections(ctx)
	if len(conns) != 0 {
		t.Errorf("want 0 connections after delete, got %d", len(conns))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/store/... -run "TestUpsertEntityConnection|TestListEntityConnections|TestDeleteAllEntityConnections" -v 2>&1 | head -30
```

Expected: FAIL with `*mockStore does not implement store.Store` or similar compile error (methods not yet implemented).

- [ ] **Step 3: Implement store methods**

Append to the bottom of `internal/store/sqlite.go` (before or after the candidate methods, in the entity section):

```go
// UpsertEntityConnection inserts a new connection or increments its weight.
// Always normalises pair order so from_id < to_id.
func (s *sqliteStore) UpsertEntityConnection(ctx context.Context, fromID, toID int64) error {
	if fromID > toID {
		fromID, toID = toID, fromID
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO entity_connections (from_id, to_id, weight, updated_at)
		 VALUES (?, ?, 1, ?)
		 ON CONFLICT(from_id, to_id) DO UPDATE SET
		   weight     = weight + 1,
		   updated_at = excluded.updated_at`,
		fromID, toID, now,
	)
	if err != nil {
		return fmt.Errorf("store: upsert entity connection: %w", err)
	}
	return nil
}

// ListEntityConnections returns all connection edges with entity names joined.
func (s *sqliteStore) ListEntityConnections(ctx context.Context) ([]*model.EntityConnection, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT ec.from_id, ec.to_id, ec.weight, ec.updated_at,
		        f.name, f.type, t.name, t.type
		 FROM entity_connections ec
		 JOIN tracked_entities f ON f.id = ec.from_id
		 JOIN tracked_entities t ON t.id = ec.to_id
		 ORDER BY ec.weight DESC, ec.from_id, ec.to_id`,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list entity connections: %w", err)
	}
	defer rows.Close()

	var out []*model.EntityConnection
	for rows.Next() {
		var c model.EntityConnection
		var updatedAt string
		if err := rows.Scan(
			&c.FromID, &c.ToID, &c.Weight, &updatedAt,
			&c.FromName, &c.FromType, &c.ToName, &c.ToType,
		); err != nil {
			return nil, fmt.Errorf("store: list entity connections: scan: %w", err)
		}
		c.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		out = append(out, &c)
	}
	if out == nil {
		out = []*model.EntityConnection{}
	}
	return out, rows.Err()
}

// DeleteAllEntityConnections removes every row from entity_connections.
func (s *sqliteStore) DeleteAllEntityConnections(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM entity_connections`)
	if err != nil {
		return fmt.Errorf("store: delete all entity connections: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/store/... -run "TestUpsertEntityConnection|TestListEntityConnections|TestDeleteAllEntityConnections" -v 2>&1 | tail -20
```

Expected: all 5 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/sqlite.go internal/store/sqlite_test.go
git commit -m "feat: implement EntityConnection store methods"
```

---

## Task 5: Pipeline + Registration

**Files:**
- Create: `internal/pipeline/entity_cooccurrence.go`
- Create: `internal/pipeline/entity_cooccurrence_test.go`
- Modify: `cli/cmd/serve.go`

- [ ] **Step 1: Write the failing pipeline test**

Create `internal/pipeline/entity_cooccurrence_test.go`:

```go
package pipeline

import (
	"context"
	"testing"

	"github.com/amir-the-h/iran-watcher/internal/inference"
)

type mockCooccurrenceStore struct {
	upserted [][2]int64
}

func (m *mockCooccurrenceStore) UpsertEntityConnection(_ context.Context, fromID, toID int64) error {
	if fromID > toID {
		fromID, toID = toID, fromID
	}
	m.upserted = append(m.upserted, [2]int64{fromID, toID})
	return nil
}

func newCooccurrencePipeline(st *mockCooccurrenceStore) *EntityCooccurrencePipeline {
	return &EntityCooccurrencePipeline{name: "test-cooccurrence", store: st}
}

func TestEntityCooccurrence_NoPairsWhenFewerThanTwo(t *testing.T) {
	st := &mockCooccurrenceStore{}
	p := newCooccurrencePipeline(st)

	// Zero IDs
	out, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{"matched_ids": []int64{}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out["pairs_updated"].(int) != 0 {
		t.Errorf("want pairs_updated=0, got %v", out["pairs_updated"])
	}
	if len(st.upserted) != 0 {
		t.Errorf("want 0 upserts, got %d", len(st.upserted))
	}

	// One ID
	out, err = p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{"matched_ids": []int64{1}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out["pairs_updated"].(int) != 0 {
		t.Errorf("want pairs_updated=0 for single id, got %v", out["pairs_updated"])
	}
}

func TestEntityCooccurrence_PairsThreeEntities(t *testing.T) {
	st := &mockCooccurrenceStore{}
	p := newCooccurrencePipeline(st)

	out, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{"matched_ids": []int64{1, 2, 3}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// C(3,2) = 3 pairs
	if out["pairs_updated"].(int) != 3 {
		t.Errorf("want pairs_updated=3, got %v", out["pairs_updated"])
	}
	if len(st.upserted) != 3 {
		t.Errorf("want 3 upserts, got %d", len(st.upserted))
	}
}

func TestEntityCooccurrence_DeduplicatesPairs(t *testing.T) {
	st := &mockCooccurrenceStore{}
	p := newCooccurrencePipeline(st)

	// Two IDs produce exactly one pair.
	out, _ := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{"matched_ids": []int64{5, 3}},
	})
	if out["pairs_updated"].(int) != 1 {
		t.Errorf("want 1 pair, got %v", out["pairs_updated"])
	}
	// Verify normalized order: from < to
	if st.upserted[0] != [2]int64{3, 5} {
		t.Errorf("want normalized pair [3,5], got %v", st.upserted[0])
	}
}

func TestEntityCooccurrence_MissingContextReturnsZero(t *testing.T) {
	st := &mockCooccurrenceStore{}
	p := newCooccurrencePipeline(st)

	out, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out["pairs_updated"].(int) != 0 {
		t.Errorf("want 0, got %v", out["pairs_updated"])
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/pipeline/... -run "TestEntityCooccurrence" -v 2>&1 | head -20
```

Expected: FAIL — `EntityCooccurrencePipeline` undefined.

- [ ] **Step 3: Write the pipeline implementation**

Create `internal/pipeline/entity_cooccurrence.go`:

```go
package pipeline

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/amir-the-h/iran-watcher/internal/inference"
)

// cooccurrenceStore is the minimal store interface required by EntityCooccurrencePipeline.
type cooccurrenceStore interface {
	UpsertEntityConnection(ctx context.Context, fromID, toID int64) error
}

// EntityCooccurrencePipeline reads matched_ids from the pipeline context and
// upserts a connection row for every unique pair of co-occurring entities.
// Outputs: {"pairs_updated": N}
type EntityCooccurrencePipeline struct {
	name  string
	store cooccurrenceStore
}

// NewEntityCooccurrenceFactory returns a Factory for EntityCooccurrencePipeline.
func NewEntityCooccurrenceFactory(st cooccurrenceStore) Factory {
	return func(name string, _ map[string]any) (inference.Pipeline, error) {
		return &EntityCooccurrencePipeline{name: name, store: st}, nil
	}
}

func (p *EntityCooccurrencePipeline) Name() string                 { return p.name }
func (p *EntityCooccurrencePipeline) Type() inference.PipelineType { return inference.PipelineTypeInternal }
func (p *EntityCooccurrencePipeline) Close() error                 { return nil }

func (p *EntityCooccurrencePipeline) Run(ctx context.Context, input inference.PipelineInput) (map[string]any, error) {
	ids := extractMatchedIDs(input.Context)
	if len(ids) < 2 {
		return map[string]any{"pairs_updated": 0}, nil
	}

	count := 0
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			a, b := ids[i], ids[j]
			if err := p.store.UpsertEntityConnection(ctx, a, b); err != nil {
				return nil, fmt.Errorf("entity_cooccurrence: upsert (%d,%d): %w", a, b, err)
			}
			count++
		}
	}

	return map[string]any{"pairs_updated": count}, nil
}

// extractMatchedIDs pulls []int64 from context["matched_ids"].
// It handles both []int64 (direct) and []any (JSON-decoded from Redis context).
func extractMatchedIDs(ctx map[string]any) []int64 {
	raw, ok := ctx["matched_ids"]
	if !ok {
		return nil
	}
	// Direct type assertion for in-process chain calls.
	if ids, ok := raw.([]int64); ok {
		return ids
	}
	// JSON round-trip via Redis stores numbers as float64 inside []any.
	if anySlice, ok := raw.([]any); ok {
		ids := make([]int64, 0, len(anySlice))
		for _, v := range anySlice {
			switch n := v.(type) {
			case float64:
				ids = append(ids, int64(n))
			case json.Number:
				if i, err := n.Int64(); err == nil {
					ids = append(ids, i)
				}
			}
		}
		return ids
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/pipeline/... -run "TestEntityCooccurrence" -v 2>&1 | tail -15
```

Expected: all 4 tests PASS.

- [ ] **Step 5: Register the pipeline in serve.go**

In `cli/cmd/serve.go`, find the two existing entity pipeline registrations and add the new one after them:

```go
pipeReg.Register("entity_matcher", pipeline.NewEntityMatcherFactory(s))
pipeReg.Register("entity_discovery", pipeline.NewEntityDiscoveryFactory(s))
pipeReg.Register("entity_cooccurrence", pipeline.NewEntityCooccurrenceFactory(s))  // add this line
```

- [ ] **Step 6: Commit**

```bash
git add internal/pipeline/entity_cooccurrence.go internal/pipeline/entity_cooccurrence_test.go cli/cmd/serve.go
git commit -m "feat: add entity_cooccurrence pipeline"
```

---

## Task 6: API Handlers + Routes

**Files:**
- Create: `internal/api/handlers_entity_connections.go`
- Modify: `internal/api/api_test.go`
- Modify: `internal/api/server.go`

- [ ] **Step 1: Add mock store methods to api_test.go**

Find the line `func (m *mockStore) Close() error { return nil }` in `api_test.go` and add before it:

```go
func (m *mockStore) UpsertEntityConnection(_ context.Context, _, _ int64) error { return nil }
func (m *mockStore) ListEntityConnections(_ context.Context) ([]*model.EntityConnection, error) {
	return []*model.EntityConnection{}, nil
}
func (m *mockStore) DeleteAllEntityConnections(_ context.Context) error { return nil }
```

- [ ] **Step 2: Write the handler tests**

Create `internal/api/handlers_entity_connections_test.go`:

```go
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

type connectionsStore struct {
	*mockStore
	connections []*model.EntityConnection
}

func (s *connectionsStore) ListEntityConnections(_ context.Context) ([]*model.EntityConnection, error) {
	return s.connections, nil
}

func (s *connectionsStore) DeleteAllEntityConnections(_ context.Context) error {
	s.connections = nil
	return nil
}

func TestHandleListEntityConnections(t *testing.T) {
	srv, ms, _, _ := newTestServer(t)
	cs := &connectionsStore{
		mockStore: ms,
		connections: []*model.EntityConnection{
			{FromID: 1, ToID: 2, Weight: 5, FromName: "Alpha", ToName: "Beta"},
		},
	}
	srv.store = cs

	req := httptest.NewRequest(http.MethodGet, "/api/entity-connections", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken(t))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Connections []*model.EntityConnection `json:"connections"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Connections) != 1 {
		t.Fatalf("want 1 connection, got %d", len(resp.Connections))
	}
	if resp.Connections[0].Weight != 5 {
		t.Errorf("want weight=5, got %d", resp.Connections[0].Weight)
	}
}

func TestHandleDeleteEntityConnections(t *testing.T) {
	srv, ms, _, _ := newTestServer(t)
	cs := &connectionsStore{
		mockStore:   ms,
		connections: []*model.EntityConnection{{FromID: 1, ToID: 2, Weight: 3}},
	}
	srv.store = cs

	req := httptest.NewRequest(http.MethodDelete, "/api/entity-connections", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken(t))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d: %s", rr.Code, rr.Body.String())
	}
	if len(cs.connections) != 0 {
		t.Errorf("want empty after delete, got %d", len(cs.connections))
	}
}
```

`adminToken(t)` is already defined in `api_test.go` at line 444 — no changes needed.

- [ ] **Step 3: Run tests to verify they fail**

```bash
go test ./internal/api/... -run "TestHandleListEntityConnections|TestHandleDeleteEntityConnections" -v 2>&1 | head -30
```

Expected: FAIL — handlers not yet registered.

- [ ] **Step 4: Write the handlers**

Create `internal/api/handlers_entity_connections.go`:

```go
package api

import (
	"net/http"
)

// handleListEntityConnections GET /api/entity-connections
func (s *Server) handleListEntityConnections(w http.ResponseWriter, r *http.Request) {
	conns, err := s.store.ListEntityConnections(r.Context())
	if err != nil {
		s.logger.Error("api: list entity connections", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list entity connections")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connections": conns})
}

// handleDeleteEntityConnections DELETE /api/entity-connections
func (s *Server) handleDeleteEntityConnections(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteAllEntityConnections(r.Context()); err != nil {
		s.logger.Error("api: delete entity connections", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete entity connections")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 5: Register routes in server.go**

Find the entity candidates block in `server.go`:

```go
// Entity Candidates
r.Get("/entity-candidates", srv.handleListEntityCandidates)
```

Add after the entity candidates block (still inside the `r.Use(adminMw)` group):

```go
// Entity Connections
r.Get("/entity-connections", srv.handleListEntityConnections)
r.Delete("/entity-connections", srv.handleDeleteEntityConnections)
```

- [ ] **Step 6: Run tests to verify they pass**

```bash
go test ./internal/api/... -run "TestHandleListEntityConnections|TestHandleDeleteEntityConnections" -v 2>&1 | tail -15
```

Expected: both tests PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/api/handlers_entity_connections.go internal/api/handlers_entity_connections_test.go internal/api/api_test.go internal/api/server.go
git commit -m "feat: add entity connections API handlers and routes"
```

---

## Task 7: Frontend API Client + Pinia Store

**Files:**
- Modify: `web/package.json`
- Create: `web/src/api/connections.ts`
- Create: `web/src/stores/connections.ts`

- [ ] **Step 1: Add vis-network dependency**

In `web/package.json`, add to the `"dependencies"` object:

```json
"vis-network": "^9.1.9"
```

- [ ] **Step 2: Create the API client**

Create `web/src/api/connections.ts`:

```typescript
import { apiFetch } from './client'

export interface EntityConnection {
  from_id: number
  to_id: number
  weight: number
  updated_at: string
  from_name: string
  from_type: string
  to_name: string
  to_type: string
}

export interface EntityConnectionsResponse {
  connections: EntityConnection[]
}

export const listEntityConnections = () =>
  apiFetch<EntityConnectionsResponse>('/api/entity-connections')

export const deleteEntityConnections = () =>
  apiFetch<void>('/api/entity-connections', { method: 'DELETE' })
```

- [ ] **Step 3: Create the Pinia store**

Create `web/src/stores/connections.ts`:

```typescript
import { ref, computed } from 'vue'
import { defineStore } from 'pinia'
import { listEntityConnections, deleteEntityConnections, type EntityConnection } from '../api/connections'

export const useConnectionsStore = defineStore('connections', () => {
  const connections = ref<EntityConnection[]>([])
  const loading = ref(false)

  async function fetchConnections() {
    loading.value = true
    try {
      const resp = await listEntityConnections()
      connections.value = resp.connections
    } finally {
      loading.value = false
    }
  }

  async function resetConnections() {
    await deleteEntityConnections()
    connections.value = []
  }

  // Builds vis-network nodes/edges for a given minimum weight threshold.
  const graphData = computed(() => (minWeight: number) => {
    const filtered = connections.value.filter(c => c.weight >= minWeight)

    // Collect unique node IDs from filtered edges only.
    const nodeMap = new Map<number, { id: number; label: string; type: string; totalWeight: number }>()
    for (const c of filtered) {
      for (const [id, name, type] of [[c.from_id, c.from_name, c.from_type], [c.to_id, c.to_name, c.to_type]] as [number, string, string][]) {
        const existing = nodeMap.get(id)
        if (existing) {
          existing.totalWeight += c.weight
        } else {
          nodeMap.set(id, { id, label: name, type, totalWeight: c.weight })
        }
      }
    }

    // Scale node size between 10 and 40 based on totalWeight.
    const weights = [...nodeMap.values()].map(n => n.totalWeight)
    const maxW = Math.max(...weights, 1)

    const nodes = [...nodeMap.values()].map(n => ({
      id: n.id,
      label: n.label,
      title: `${n.label} (${n.type}) — ${n.totalWeight} connections`,
      size: 10 + Math.round((n.totalWeight / maxW) * 30),
    }))

    const edges = filtered.map(c => ({
      from: c.from_id,
      to: c.to_id,
      value: c.weight,
      title: `Co-occurrences: ${c.weight}`,
    }))

    return { nodes, edges }
  })

  return { connections, loading, fetchConnections, resetConnections, graphData }
})
```

- [ ] **Step 4: Commit**

```bash
git add web/package.json web/src/api/connections.ts web/src/stores/connections.ts
git commit -m "feat: add entity connections API client and Pinia store"
```

---

## Task 8: Connections Tab in TrackerView

**Files:**
- Modify: `web/src/views/TrackerView.vue`

- [ ] **Step 1: Add imports to TrackerView script section**

In the `<script setup lang="ts">` block, add these imports alongside the existing ones:

```typescript
import { onUnmounted } from 'vue'
import { Network } from 'vis-network'
import { useConnectionsStore } from '../stores/connections'
import { GitNetworkOutline } from '@vicons/ionicons5'
```

Also add `nextTick` to the existing `vue` import:
```typescript
import { ref, onMounted, computed, watch, h, nextTick } from 'vue'
```

- [ ] **Step 2: Add connections state and graph logic to script section**

Find `// ── Candidates tab ────────────────────────────────────────────────────────────` and add a new section after the candidates section ends:

```typescript
// ── Connections tab ───────────────────────────────────────────────────────────

const connectionsStore = useConnectionsStore()
const graphContainer  = ref<HTMLElement | null>(null)
const minWeight       = ref(1)
let network: InstanceType<typeof Network> | null = null

function destroyNetwork() {
  if (network) {
    network.destroy()
    network = null
  }
}

async function renderGraph() {
  if (!graphContainer.value) return
  destroyNetwork()

  const { nodes, edges } = connectionsStore.graphData(minWeight.value)
  if (nodes.length === 0) return

  const { DataSet } = await import('vis-network/standalone')
  network = new Network(
    graphContainer.value,
    { nodes: new DataSet(nodes), edges: new DataSet(edges) },
    {
      physics: { stabilization: { iterations: 100 } },
      edges: {
        scaling: { min: 1, max: 8 },
        smooth: { enabled: true, type: 'dynamic', roundness: 0.5 },
      },
      nodes: {
        shape: 'dot',
        font: { size: 12, color: '#e2e8f0' },
        color: { background: '#3b82f6', border: '#1d4ed8', highlight: { background: '#60a5fa', border: '#2563eb' } },
      },
      interaction: { hover: true, tooltipDelay: 200 },
    }
  )
}

watch(minWeight, renderGraph)

onUnmounted(destroyNetwork)
```

- [ ] **Step 3: Update onTabChange to handle the connections tab**

Find the `onTabChange` function (it currently handles `'candidates'`). Update it:

```typescript
async function onTabChange(tab: string) {
  if (tab === 'candidates') {
    await loadCandidates()
    await candidatesStore.fetchPendingCount()
  } else if (tab === 'connections') {
    await connectionsStore.fetchConnections()
    await nextTick()
    renderGraph()
  }
}
```

- [ ] **Step 4: Update the activeTab type**

Find:
```typescript
const activeTab = ref<'entities' | 'candidates'>('entities')
```
Change to:
```typescript
const activeTab = ref<'entities' | 'candidates' | 'connections'>('entities')
```

- [ ] **Step 5: Add the Connections tab pane to the template**

Find the closing `</n-tabs>` tag. Before it, add the Connections tab pane (after the `</n-tab-pane>` that closes the candidates pane):

```html
<n-tab-pane name="connections">
  <template #tab>
    <n-space :size="4" align="center">
      <n-icon :component="GitNetworkOutline" />
      Connections
    </n-space>
  </template>

  <div style="margin-bottom: 12px; display: flex; align-items: center; gap: 16px;">
    <span style="font-size: 13px; color: var(--n-text-color-3);">Min weight:</span>
    <n-slider
      v-model:value="minWeight"
      :min="1"
      :max="Math.max(1, Math.max(...connectionsStore.connections.map(c => c.weight), 1))"
      :step="1"
      style="width: 200px"
      @update:value="renderGraph"
    />
    <span style="font-size: 13px; min-width: 20px;">{{ minWeight }}</span>
    <n-button
      v-if="auth.isAdmin"
      size="small"
      type="error"
      secondary
      @click="connectionsStore.resetConnections().then(renderGraph)"
    >
      Reset
    </n-button>
  </div>

  <div v-if="connectionsStore.loading" style="text-align: center; padding: 40px;">
    <n-spin />
  </div>
  <div
    v-else-if="connectionsStore.connections.length === 0"
    style="text-align: center; padding: 60px; color: var(--n-text-color-3);"
  >
    No connections yet. Set up an <strong>entity_cooccurrence</strong> pipeline chained after entity_matcher to start building the graph.
  </div>
  <div
    v-else
    ref="graphContainer"
    style="width: 100%; height: 600px; background: var(--n-card-color); border-radius: 8px; border: 1px solid var(--n-border-color);"
  />
</n-tab-pane>
```

Also add `NSlider, NSpin` to the Naive UI import at the top of the script:
```typescript
import { useMessage, NButton, NSpace, NBadge, NSlider, NSpin } from 'naive-ui'
```

- [ ] **Step 6: Commit**

```bash
git add web/src/views/TrackerView.vue
git commit -m "feat: add Connections tab with force-directed entity graph"
```

---

## Task 9: Full Test Run + Docker Build

- [ ] **Step 1: Run all Go tests**

```bash
go test ./... 2>&1 | tail -30
```

Expected: all packages PASS (or show existing failures unrelated to this feature).

- [ ] **Step 2: Install frontend deps and type-check**

```bash
cd web && npm install && npm run build 2>&1 | tail -20
```

Expected: build succeeds with no TypeScript errors.

- [ ] **Step 3: Build the Docker image**

```bash
docker compose build --build-arg BUILDKIT_INLINE_CACHE=1 iran-watcher 2>&1 | tail -20
```

Expected: build completes successfully.

- [ ] **Step 4: Commit build verification note**

If any issues were found and fixed in the above steps, commit those fixes. Otherwise no action needed.

---

## Task 10: Documentation

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Update CLAUDE.md**

In the **Key Design Decisions** section, find the `**Entity discovery (Tracker)**` bullet and add a new bullet after it:

```markdown
- **Entity connections graph** — the `entity_cooccurrence` internal pipeline runs as a chain step after `entity_matcher`, reads `matched_ids` from context, and upserts a weight-incremented row in `entity_connections(from_id, to_id, weight)` for every unique pair of co-occurring tracked entities; `from_id < to_id` (CHECK constraint normalizes order); the Connections tab in the Tracker page renders all edges as a vis-network force-directed graph with a min-weight slider; API: `GET /api/entity-connections` (all edges with joined entity names), `DELETE /api/entity-connections` (reset all, admin only)
```

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: document entity connections graph feature"
```

---

## Setup Guide (for the user)

After the implementation is deployed, wire up the pipeline in the UI:

1. **Create pipeline:** Pipelines → New → Handler: `entity_cooccurrence`, Name: e.g. "Entity Co-occurrence", Type: Internal → Save → Activate
2. **Create action:** Actions → New → Trigger: `chain_only`, Pipeline: "Entity Co-occurrence" → Save → Activate
3. **Add chain:** On the `entity_matcher` action → Chains → Add → Target: the new action, Condition: none (always) → Save
4. Go to **Tracker → Connections** to see the graph populate as tweets are processed
