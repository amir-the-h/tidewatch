# Connections Graph — Entity-Focused Filtering Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the all-connections graph with an entity-focused subgraph view backed by a new `GET /api/entity-connections/subgraph` endpoint; nothing renders until the user picks an entity.

**Architecture:** A new store method `GetEntityConnectionSubgraph` performs BFS in Go over all connection edges to compute the neighborhood up to N hops. A new API handler exposes this as a query endpoint. The frontend replaces the current "load all" flow with an entity picker + depth slider that re-fetches on change (debounced 300 ms).

**Tech Stack:** Go (net/http, chi), SQLite (modernc.org/sqlite), Vue 3 + Pinia + Naive UI, vis-network

---

## File Map

| File | Change |
|---|---|
| `internal/store/store.go` | Add `GetEntityConnectionSubgraph` to `Store` interface |
| `internal/store/sqlite.go` | Implement `GetEntityConnectionSubgraph` |
| `internal/store/sqlite_test.go` | Add 5 tests for `GetEntityConnectionSubgraph` |
| `internal/api/handlers_entity_connections.go` | Add `handleGetEntityConnectionSubgraph` handler |
| `internal/api/server.go` | Register `GET /api/entity-connections/subgraph` route |
| `internal/api/handlers_entity_connections_test.go` | Add `GetEntityConnectionSubgraph` to `connectionsStore`; add handler tests |
| `internal/api/api_test.go` | Add no-op `GetEntityConnectionSubgraph` to `mockStore` |
| `internal/stream/stream_test.go` | Add no-op `GetEntityConnectionSubgraph` to `mockStore` |
| `internal/worker/worker_test.go` | Add no-op `GetEntityConnectionSubgraph` to `mockStore` |
| `web/src/api/connections.ts` | Add `getEntitySubgraph` function |
| `web/src/stores/connections.ts` | Rename `connections`→`subgraph`; replace `fetchConnections` with `fetchSubgraph`; simplify `graphData` |
| `CLAUDE.md` | Update "Entity connections graph" section |

---

## Task 1: Store interface + BFS implementation

**Files:**
- Modify: `internal/store/store.go` (EntityConnections section, ~line 205)
- Modify: `internal/store/sqlite.go` (after `DeleteAllEntityConnections`, ~line 1910)

- [ ] **Step 1: Add method to Store interface**

In `internal/store/store.go`, add `GetEntityConnectionSubgraph` after `DeleteAllEntityConnections`:

```go
// EntityConnections
UpsertEntityConnection(ctx context.Context, fromID, toID int64) error
ListEntityConnections(ctx context.Context) ([]*model.EntityConnection, error)
GetEntityConnectionSubgraph(ctx context.Context, entityID int64, depth, minWeight int) ([]*model.EntityConnection, error)
DeleteAllEntityConnections(ctx context.Context) error
```

- [ ] **Step 2: Implement in sqlite.go**

Add after `DeleteAllEntityConnections` in `internal/store/sqlite.go`:

```go
// GetEntityConnectionSubgraph returns the subgraph of entity connections reachable
// from entityID within depth hops, including only edges with weight >= minWeight.
func (s *sqliteStore) GetEntityConnectionSubgraph(ctx context.Context, entityID int64, depth, minWeight int) ([]*model.EntityConnection, error) {
	all, err := s.ListEntityConnections(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: get entity connection subgraph: %w", err)
	}

	// Build adjacency list using only edges that meet the weight threshold.
	adj := make(map[int64][]int64)
	for _, c := range all {
		if c.Weight >= minWeight {
			adj[c.FromID] = append(adj[c.FromID], c.ToID)
			adj[c.ToID] = append(adj[c.ToID], c.FromID)
		}
	}

	// BFS from entityID up to depth hops.
	visited := map[int64]bool{entityID: true}
	frontier := []int64{entityID}
	for d := 0; d < depth && len(frontier) > 0; d++ {
		var next []int64
		for _, id := range frontier {
			for _, neighbor := range adj[id] {
				if !visited[neighbor] {
					visited[neighbor] = true
					next = append(next, neighbor)
				}
			}
		}
		frontier = next
	}

	// Return only edges where both endpoints are reachable and weight qualifies.
	out := make([]*model.EntityConnection, 0)
	for _, c := range all {
		if c.Weight >= minWeight && visited[c.FromID] && visited[c.ToID] {
			out = append(out, c)
		}
	}
	return out, nil
}
```

- [ ] **Step 3: Verify it compiles**

```bash
docker compose build iran-watcher 2>&1 | tail -5
```

Expected: build succeeds (or compile errors only — fix before continuing).

---

## Task 2: Store tests for GetEntityConnectionSubgraph

**Files:**
- Modify: `internal/store/sqlite_test.go` (append after `TestDeleteAllEntityConnections`)

- [ ] **Step 1: Write the failing tests**

Append to `internal/store/sqlite_test.go`:

```go
func TestGetEntityConnectionSubgraph_Depth1(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	a := seedTrackedEntity(t, s, "Alpha")
	b := seedTrackedEntity(t, s, "Beta")
	c := seedTrackedEntity(t, s, "Gamma")

	_ = s.UpsertEntityConnection(ctx, a.ID, b.ID) // A-B weight=1
	_ = s.UpsertEntityConnection(ctx, b.ID, c.ID) // B-C weight=1

	conns, err := s.GetEntityConnectionSubgraph(ctx, a.ID, 1, 1)
	if err != nil {
		t.Fatalf("GetEntityConnectionSubgraph: %v", err)
	}
	// Depth 1 from A: reachable = {A, B}. Edge A-B qualifies; B-C does not (C unreachable).
	if len(conns) != 1 {
		t.Fatalf("want 1 edge, got %d", len(conns))
	}
}

func TestGetEntityConnectionSubgraph_Depth2(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	a := seedTrackedEntity(t, s, "Alpha")
	b := seedTrackedEntity(t, s, "Beta")
	c := seedTrackedEntity(t, s, "Gamma")

	_ = s.UpsertEntityConnection(ctx, a.ID, b.ID)
	_ = s.UpsertEntityConnection(ctx, b.ID, c.ID)

	conns, err := s.GetEntityConnectionSubgraph(ctx, a.ID, 2, 1)
	if err != nil {
		t.Fatalf("GetEntityConnectionSubgraph: %v", err)
	}
	// Depth 2: reachable = {A, B, C}. Both edges qualify.
	if len(conns) != 2 {
		t.Fatalf("want 2 edges, got %d: %+v", len(conns), conns)
	}
}

func TestGetEntityConnectionSubgraph_MinWeightFilters(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	a := seedTrackedEntity(t, s, "Alpha")
	b := seedTrackedEntity(t, s, "Beta")
	c := seedTrackedEntity(t, s, "Gamma")

	_ = s.UpsertEntityConnection(ctx, a.ID, b.ID) // weight=1 after one upsert
	// Upsert b-c three times to get weight=3
	for i := 0; i < 3; i++ {
		_ = s.UpsertEntityConnection(ctx, b.ID, c.ID)
	}

	conns, err := s.GetEntityConnectionSubgraph(ctx, a.ID, 2, 2)
	if err != nil {
		t.Fatalf("GetEntityConnectionSubgraph: %v", err)
	}
	// min_weight=2: A-B (weight=1) excluded from adjacency, so only {A} reachable.
	// Result: empty (no edges meet threshold AND have both endpoints reachable).
	if len(conns) != 0 {
		t.Fatalf("want 0 edges, got %d", len(conns))
	}
}

func TestGetEntityConnectionSubgraph_UnknownEntityReturnsEmpty(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	a := seedTrackedEntity(t, s, "Alpha")
	b := seedTrackedEntity(t, s, "Beta")
	_ = s.UpsertEntityConnection(ctx, a.ID, b.ID)

	conns, err := s.GetEntityConnectionSubgraph(ctx, 9999, 2, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(conns) != 0 {
		t.Fatalf("want empty for unknown entity, got %d", len(conns))
	}
}

func TestGetEntityConnectionSubgraph_EntityWithNoConnectionsReturnsEmpty(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	a := seedTrackedEntity(t, s, "Alpha")

	conns, err := s.GetEntityConnectionSubgraph(ctx, a.ID, 2, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(conns) != 0 {
		t.Fatalf("want empty slice, got %d", len(conns))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
docker compose run --rm iran-watcher go test ./internal/store/... -run TestGetEntityConnectionSubgraph -v 2>&1 | tail -20
```

Expected: compile error — `GetEntityConnectionSubgraph` undefined (method not yet added to interface or sqlite).

- [ ] **Step 3: Run tests after Task 1 implementation**

```bash
docker compose run --rm iran-watcher go test ./internal/store/... -run TestGetEntityConnectionSubgraph -v 2>&1 | tail -20
```

Expected: all 5 tests PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/store/store.go internal/store/sqlite.go internal/store/sqlite_test.go
git commit -m "feat: add GetEntityConnectionSubgraph store method with BFS"
```

---

## Task 3: Update mock stores

The three test-internal `mockStore` types each implement `store.Store`. Adding the method to the interface means they won't compile until updated.

**Files:**
- Modify: `internal/api/api_test.go` (after `DeleteAllEntityConnections` stub, ~line 419)
- Modify: `internal/stream/stream_test.go` (after `DeleteAllEntityConnections` stub, ~line 197)
- Modify: `internal/worker/worker_test.go` (after `DeleteAllEntityConnections` stub, ~line 336)

- [ ] **Step 1: Add no-op to api_test.go**

In `internal/api/api_test.go`, after:
```go
func (m *mockStore) DeleteAllEntityConnections(_ context.Context) error { return nil }
```
Add:
```go
func (m *mockStore) GetEntityConnectionSubgraph(_ context.Context, _ int64, _, _ int) ([]*model.EntityConnection, error) {
	return []*model.EntityConnection{}, nil
}
```

- [ ] **Step 2: Add no-op to stream_test.go**

In `internal/stream/stream_test.go`, after:
```go
func (m *mockStore) DeleteAllEntityConnections(_ context.Context) error { return nil }
```
Add:
```go
func (m *mockStore) GetEntityConnectionSubgraph(_ context.Context, _ int64, _, _ int) ([]*model.EntityConnection, error) {
	return []*model.EntityConnection{}, nil
}
```

- [ ] **Step 3: Add no-op to worker_test.go**

In `internal/worker/worker_test.go`, after:
```go
func (m *mockStore) DeleteAllEntityConnections(_ context.Context) error { return nil }
```
Add:
```go
func (m *mockStore) GetEntityConnectionSubgraph(_ context.Context, _ int64, _, _ int) ([]*model.EntityConnection, error) {
	return []*model.EntityConnection{}, nil
}
```

- [ ] **Step 4: Run all tests to confirm compile + pass**

```bash
docker compose run --rm iran-watcher go test ./... 2>&1 | tail -20
```

Expected: all packages compile and tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/api/api_test.go internal/stream/stream_test.go internal/worker/worker_test.go
git commit -m "test: add GetEntityConnectionSubgraph no-op to mock stores"
```

---

## Task 4: API handler and route

**Files:**
- Modify: `internal/api/handlers_entity_connections.go`
- Modify: `internal/api/server.go` (~line 251)

- [ ] **Step 1: Add handler**

In `internal/api/handlers_entity_connections.go`, add the import for `strconv` and append the handler. Full file after edit:

```go
package api

import (
	"net/http"
	"strconv"
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

// handleGetEntityConnectionSubgraph GET /api/entity-connections/subgraph
func (s *Server) handleGetEntityConnectionSubgraph(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	entityIDStr := q.Get("entity_id")
	if entityIDStr == "" {
		writeError(w, http.StatusBadRequest, "entity_id is required")
		return
	}
	entityID, err := strconv.ParseInt(entityIDStr, 10, 64)
	if err != nil || entityID <= 0 {
		writeError(w, http.StatusBadRequest, "entity_id must be a positive integer")
		return
	}

	depth := 2
	if ds := q.Get("depth"); ds != "" {
		if d, err := strconv.Atoi(ds); err == nil {
			if d < 1 {
				d = 1
			}
			if d > 5 {
				d = 5
			}
			depth = d
		}
	}

	minWeight := 1
	if mws := q.Get("min_weight"); mws != "" {
		if mw, err := strconv.Atoi(mws); err == nil && mw > 1 {
			minWeight = mw
		}
	}

	conns, err := s.store.GetEntityConnectionSubgraph(r.Context(), entityID, depth, minWeight)
	if err != nil {
		s.logger.Error("api: get entity connection subgraph", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get entity connection subgraph")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connections": conns})
}
```

- [ ] **Step 2: Register route in server.go**

In `internal/api/server.go`, find this block:

```go
			// Entity Connections
			r.Get("/entity-connections", srv.handleListEntityConnections)
			r.Delete("/entity-connections", srv.handleDeleteEntityConnections)
```

Replace with:

```go
			// Entity Connections
			r.Get("/entity-connections/subgraph", srv.handleGetEntityConnectionSubgraph)
			r.Get("/entity-connections", srv.handleListEntityConnections)
			r.Delete("/entity-connections", srv.handleDeleteEntityConnections)
```

- [ ] **Step 3: Build to verify**

```bash
docker compose build iran-watcher 2>&1 | tail -5
```

Expected: successful build.

---

## Task 5: Handler tests

**Files:**
- Modify: `internal/api/handlers_entity_connections_test.go`

- [ ] **Step 1: Add GetEntityConnectionSubgraph to connectionsStore and write tests**

Replace the entire `internal/api/handlers_entity_connections_test.go` with:

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

func (s *connectionsStore) GetEntityConnectionSubgraph(_ context.Context, entityID int64, _, _ int) ([]*model.EntityConnection, error) {
	var out []*model.EntityConnection
	for _, c := range s.connections {
		if c.FromID == entityID || c.ToID == entityID {
			out = append(out, c)
		}
	}
	if out == nil {
		out = []*model.EntityConnection{}
	}
	return out, nil
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

func TestHandleGetEntityConnectionSubgraph_MissingEntityID(t *testing.T) {
	srv, ms, _, _ := newTestServer(t)
	srv.store = &connectionsStore{mockStore: ms}

	req := httptest.NewRequest(http.MethodGet, "/api/entity-connections/subgraph", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken(t))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleGetEntityConnectionSubgraph_ReturnsSubgraph(t *testing.T) {
	srv, ms, _, _ := newTestServer(t)
	cs := &connectionsStore{
		mockStore: ms,
		connections: []*model.EntityConnection{
			{FromID: 1, ToID: 2, Weight: 3, FromName: "Alpha", ToName: "Beta"},
			{FromID: 2, ToID: 3, Weight: 1, FromName: "Beta", ToName: "Gamma"},
		},
	}
	srv.store = cs

	req := httptest.NewRequest(http.MethodGet, "/api/entity-connections/subgraph?entity_id=1&depth=2&min_weight=1", nil)
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
	if len(resp.Connections) == 0 {
		t.Fatal("want at least one connection")
	}
}
```

- [ ] **Step 2: Run handler tests**

```bash
docker compose run --rm iran-watcher go test ./internal/api/... -run "TestHandleEntityConnection\|TestHandleGetEntityConnection" -v 2>&1 | tail -30
```

Expected: all tests PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/api/handlers_entity_connections.go internal/api/handlers_entity_connections_test.go internal/api/server.go
git commit -m "feat: add GET /api/entity-connections/subgraph endpoint"
```

---

## Task 6: Frontend API + store

**Files:**
- Modify: `web/src/api/connections.ts`
- Modify: `web/src/stores/connections.ts`

- [ ] **Step 1: Add getEntitySubgraph to connections.ts**

Replace the entire `web/src/api/connections.ts` with:

```ts
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

export const getEntitySubgraph = (entityID: number, depth: number, minWeight: number) =>
  apiFetch<EntityConnectionsResponse>(
    `/api/entity-connections/subgraph?entity_id=${entityID}&depth=${depth}&min_weight=${minWeight}`
  )
```

- [ ] **Step 2: Rewrite connections store**

Replace the entire `web/src/stores/connections.ts` with:

```ts
import { ref, computed } from 'vue'
import { defineStore } from 'pinia'
import { getEntitySubgraph, deleteEntityConnections, type EntityConnection } from '../api/connections'

export const useConnectionsStore = defineStore('connections', () => {
  const subgraph = ref<EntityConnection[]>([])
  const loading = ref(false)

  async function fetchSubgraph(entityID: number, depth: number, minWeight: number) {
    loading.value = true
    try {
      const resp = await getEntitySubgraph(entityID, depth, minWeight)
      subgraph.value = resp.connections
    } finally {
      loading.value = false
    }
  }

  async function resetConnections() {
    await deleteEntityConnections()
    subgraph.value = []
  }

  // Builds vis-network nodes/edges from the current subgraph.
  const graphData = computed(() => () => {
    const nodeMap = new Map<number, { id: number; label: string; type: string; totalWeight: number }>()
    for (const c of subgraph.value) {
      for (const [id, name, type] of [[c.from_id, c.from_name, c.from_type], [c.to_id, c.to_name, c.to_type]] as [number, string, string][]) {
        const existing = nodeMap.get(id)
        if (existing) {
          existing.totalWeight += c.weight
        } else {
          nodeMap.set(id, { id, label: name, type, totalWeight: c.weight })
        }
      }
    }

    const weights = [...nodeMap.values()].map(n => n.totalWeight)
    const maxW = Math.max(...weights, 1)

    const nodes = [...nodeMap.values()].map(n => ({
      id: n.id,
      label: n.label,
      title: `${n.label} (${n.type}) — ${n.totalWeight} connections`,
      size: 10 + Math.round((n.totalWeight / maxW) * 30),
    }))

    const edges = subgraph.value.map(c => ({
      from: c.from_id,
      to: c.to_id,
      value: c.weight,
      title: `Co-occurrences: ${c.weight}`,
    }))

    return { nodes, edges }
  })

  return { subgraph, loading, fetchSubgraph, resetConnections, graphData }
})
```

Note: `graphData` is now a no-argument computed function — the backend already applied the weight filter. The call site in `renderGraph` in `TrackerView.vue` must change from `connectionsStore.graphData(minWeight.value)` to `connectionsStore.graphData()` — this is done in Task 7 Step 2 (the watch replacement), but `renderGraph` itself also needs patching (see Task 7 Step 2 note).

- [ ] **Step 3: Build to check for TypeScript errors**

```bash
docker compose build iran-watcher 2>&1 | tail -10
```

Expected: builds successfully (frontend compile is part of the Docker build).

---

## Task 7: Frontend view (TrackerView.vue connections tab)

**Files:**
- Modify: `web/src/views/TrackerView.vue`

The connections tab currently:
1. Fetches all connections on tab activate (`fetchConnections()`)
2. Shows a min-weight slider
3. `watch(minWeight, renderGraph)` re-renders on weight change

After this task:
1. Does nothing on tab activate (no fetch)
2. Shows entity picker + depth slider + min-weight slider
3. Combined watch on `[selectedEntityId, depth, minWeight]` debounces → `fetchSubgraph` → `renderGraph`

- [ ] **Step 1: Add new reactive state in the `<script setup>` block**

In `TrackerView.vue`, find this block in the Connections section:

```ts
const connectionsStore = useConnectionsStore()
const graphContainer  = ref<HTMLElement | null>(null)
const minWeight       = ref(1)
let network: any = null
let renderInFlight = false
```

Replace with:

```ts
const connectionsStore = useConnectionsStore()
const graphContainer   = ref<HTMLElement | null>(null)
const selectedEntityId = ref<number | null>(null)
const depth            = ref(2)
const minWeight        = ref(1)
let network: any = null
let renderInFlight = false
let debounceTimer = 0
```

- [ ] **Step 2: Replace the watch + onTabChange connections logic**

In `renderGraph`, find and change the `graphData` call:

```ts
    const { nodes, edges } = connectionsStore.graphData(minWeight.value)
```

Replace with:

```ts
    const { nodes, edges } = connectionsStore.graphData()
```

Find and remove this watch:

```ts
watch(minWeight, renderGraph)
```

Find the `onTabChange` function. Remove the connections branch:

```ts
  } else if (tab === 'connections') {
    try {
      await connectionsStore.fetchConnections()
      await nextTick()
      await renderGraph()
    } catch (e: any) {
      message.error('Failed to load connections: ' + (e?.message ?? 'unknown error'))
    }
  }
```

Replace it with:

```ts
  } else if (tab === 'connections') {
    // Nothing to fetch until an entity is selected.
  }
```

Add a new watch after the removed one:

```ts
watch([selectedEntityId, depth, minWeight], ([eid]) => {
  clearTimeout(debounceTimer)
  if (!eid) {
    destroyNetwork()
    return
  }
  debounceTimer = window.setTimeout(async () => {
    try {
      await connectionsStore.fetchSubgraph(eid as number, depth.value, minWeight.value)
      await nextTick()
      await renderGraph()
    } catch (e: any) {
      message.error('Failed to load connections: ' + (e?.message ?? 'unknown error'))
    }
  }, 300)
})
```

- [ ] **Step 3: Compute entity picker options**

Add this computed after the new state declarations:

```ts
const entityPickerOptions = computed(() =>
  store.items.map(e => ({
    label: e.type ? `${e.name} (${e.type})` : e.name,
    value: e.id,
  }))
)
```

- [ ] **Step 4: Update the connections tab template**

Find the connections tab `<n-tab-pane name="connections">` template section. Replace the filter bar div:

```html
<div style="margin-bottom: 12px; display: flex; align-items: center; gap: 16px;">
  <span style="font-size: 13px; color: var(--n-text-color-3);">Min weight:</span>
  <n-slider
    v-model:value="minWeight"
    :min="1"
    :max="connectionsStore.connections.reduce((m, c) => c.weight > m ? c.weight : m, 1)"
    :step="1"
    style="width: 200px"
  />
  <span style="font-size: 13px; min-width: 20px;">{{ minWeight }}</span>
  <n-button
    v-if="auth.isAdmin"
    size="small"
    type="error"
    secondary
    @click="destroyNetwork(); connectionsStore.resetConnections().catch((e: any) => message.error('Reset failed: ' + e?.message))"
  >
    Reset
  </n-button>
</div>
```

With:

```html
<div style="margin-bottom: 12px; display: flex; align-items: center; gap: 16px; flex-wrap: wrap;">
  <n-select
    v-model:value="selectedEntityId"
    :options="entityPickerOptions"
    filterable
    clearable
    placeholder="Select an entity…"
    style="width: 240px"
  />
  <span style="font-size: 13px; color: var(--n-text-color-3);">Depth:</span>
  <n-slider
    v-model:value="depth"
    :min="1"
    :max="5"
    :step="1"
    :disabled="!selectedEntityId"
    style="width: 120px"
  />
  <span style="font-size: 13px; min-width: 12px;">{{ depth }}</span>
  <span style="font-size: 13px; color: var(--n-text-color-3);">Min weight:</span>
  <n-slider
    v-model:value="minWeight"
    :min="1"
    :max="connectionsStore.subgraph.reduce((m, c) => c.weight > m ? c.weight : m, 1)"
    :step="1"
    :disabled="!selectedEntityId"
    style="width: 120px"
  />
  <span style="font-size: 13px; min-width: 20px;">{{ minWeight }}</span>
  <n-button
    v-if="auth.isAdmin"
    size="small"
    type="error"
    secondary
    @click="selectedEntityId = null; destroyNetwork(); connectionsStore.resetConnections().catch((e: any) => message.error('Reset failed: ' + e?.message))"
  >
    Reset
  </n-button>
</div>
```

- [ ] **Step 5: Update the empty state and graph container**

Find and replace the empty state `<div v-else-if="connectionsStore.connections.length === 0" ...>`:

```html
<div
  v-else-if="connectionsStore.connections.length === 0"
  style="text-align: center; padding: 60px; color: var(--n-text-color-3);"
>
  No connections yet. Set up an <strong>entity_cooccurrence</strong> pipeline chained after entity_matcher to start building the graph.
</div>
```

Replace with:

```html
<div
  v-else-if="!selectedEntityId"
  style="text-align: center; padding: 60px; color: var(--n-text-color-3);"
>
  Select an entity to explore its connections.
</div>
<div
  v-else-if="connectionsStore.subgraph.length === 0 && !connectionsStore.loading"
  style="text-align: center; padding: 60px; color: var(--n-text-color-3);"
>
  No connections found for this entity with the current filters.
</div>
```

The existing graph container div (`ref="graphContainer"`) stays unchanged.

- [ ] **Step 6: Build the full project**

```bash
docker compose build iran-watcher 2>&1 | tail -10
```

Expected: successful build.

- [ ] **Step 7: Commit**

```bash
git add web/src/api/connections.ts web/src/stores/connections.ts web/src/views/TrackerView.vue
git commit -m "feat: entity-focused connections graph with depth and weight filters"
```

---

## Task 8: Documentation

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Update CLAUDE.md**

Find the **Entity connections graph** bullet in Key Design Decisions. It currently ends with:
```
...frontend Connections tab renders a vis-network force-directed graph with a min-weight slider filter; store: `useConnectionsStore` in `web/src/stores/connections.ts`
```

Replace just the tail of that bullet to read:
```
...frontend Connections tab renders a vis-network force-directed graph focused on a selected entity; controls: entity picker (required), depth slider (1–5, default 2), min-weight slider; `GET /api/entity-connections/subgraph?entity_id=X&depth=Y&min_weight=Z` returns the BFS neighborhood; store: `useConnectionsStore` in `web/src/stores/connections.ts`
```

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: update entity connections graph section in CLAUDE.md"
```

---

## Final Verification

- [ ] Run full test suite:

```bash
docker compose run --rm iran-watcher go test ./... 2>&1 | tail -20
```

Expected: all tests pass.

- [ ] Build final image:

```bash
docker compose build iran-watcher 2>&1 | tail -5
```

Expected: successful build.
