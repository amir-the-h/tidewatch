# Entity Connections Graph Design

**Date:** 2026-03-30
**Status:** Approved

## Overview

Add a "Connections" tab to the Tracker page that visualizes co-occurrence relationships between tracked entities. When two or more tracked entities appear in the same tweet (as determined by the `entity_matcher` pipeline), a connection is recorded between them. Each subsequent co-occurrence increments the connection weight. The tab renders these connections as an interactive force-directed graph.

## Data Layer

### New table: `entity_connections`

```sql
CREATE TABLE entity_connections (
  from_id    INTEGER NOT NULL REFERENCES tracked_entities(id) ON DELETE CASCADE,
  to_id      INTEGER NOT NULL REFERENCES tracked_entities(id) ON DELETE CASCADE,
  weight     INTEGER NOT NULL DEFAULT 1,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (from_id, to_id),
  CHECK (from_id < to_id)
);
CREATE INDEX idx_entity_connections_from ON entity_connections(from_id);
CREATE INDEX idx_entity_connections_to ON entity_connections(to_id);
```

The `CHECK (from_id < to_id)` constraint normalizes pair order so each pair is stored exactly once. The pipeline always inserts with `min(a, b)` as `from_id` and `max(a, b)` as `to_id`.

Deleting a tracked entity cascades to remove all its connection rows automatically.

### Store interface additions

```go
UpsertEntityConnection(ctx context.Context, fromID, toID int64) error
ListEntityConnections(ctx context.Context) ([]EntityConnection, error)
DeleteAllEntityConnections(ctx context.Context) error
```

`UpsertEntityConnection` always normalizes order (`fromID < toID`) before the upsert, so callers don't need to think about it.

### New model: `EntityConnection`

```go
type EntityConnection struct {
    FromID       int64
    ToID         int64
    Weight       int
    UpdatedAt    time.Time
    // Joined fields for API responses
    FromName     string
    FromType     string
    ToName       string
    ToType       string
}
```

## Pipeline

### New internal pipeline: `entity_cooccurrence`

- **Handler name:** `entity_cooccurrence`
- **File:** `internal/pipeline/entity_cooccurrence.go`
- **Registered in:** `internal/pipeline/registry.go`

**Input:** `PipelineInput.Context["matched_ids"]` — a `[]int64` of tracked entity IDs set by the upstream `entity_matcher` execution.

**Logic:**
1. Extract `matched_ids` from context; if fewer than 2 IDs, return no-op (`{"pairs_updated": 0}`).
2. For every unique pair `(a, b)` where `a < b`, call `store.UpsertEntityConnection(ctx, a, b)`.
3. Return `{"pairs_updated": N}` where N is the number of pairs processed.

**Deduplication:** The pipeline relies on the `action_executions` table — the worker manager already skips re-running a pipeline for a tweet+action pair that has a `success` execution, so no extra deduplication logic is needed inside the pipeline itself.

**No config fields required.** The pipeline has no configurable parameters.

## Chain Setup (user-configured)

The user wires this up in the UI — no auto-wiring at startup:

1. Create a pipeline with handler `entity_cooccurrence`.
2. Create an action with trigger type `chain_only`.
3. On the `entity_matcher` action, add an ActionChain targeting the new action (no condition — fires unconditionally after every entity_matcher execution).

This follows the existing chain pattern. No worker changes required.

## API

### `GET /api/entity-connections`

Returns all connection edges with joined entity names and types. Used by the frontend graph.

**Response:**
```json
{
  "connections": [
    {
      "from_id": 1,
      "from_name": "Khamenei",
      "from_type": "PER",
      "to_id": 3,
      "to_name": "IRGC",
      "to_type": "ORG",
      "weight": 47,
      "updated_at": "2026-03-30T12:00:00Z"
    }
  ]
}
```

### `DELETE /api/entity-connections`

Truncates the `entity_connections` table. Useful for resetting and recalculating from scratch. Admin only.

**Response:** `204 No Content`

Both endpoints are added to `internal/api/handlers_entity_connections.go` and registered in the router alongside existing entity routes.

## Frontend

### New tab: Connections

Added as the third tab in `TrackerView.vue` (after Entities and Candidates). Tab is always visible but shows an empty state when no connections exist yet.

### Graph library: `vis-network`

`vis-network` handles force-directed layout, zoom, pan, and drag with minimal boilerplate. Added as a dependency to `web/package.json`.

### Graph behavior

- **Nodes:** One node per tracked entity that has at least one connection. Node size is proportional to total connection weight (sum of all edge weights for that entity). Node label shows entity name.
- **Edges:** Thickness proportional to weight. Hover tooltip shows co-occurrence count.
- **Min-weight filter:** A slider above the graph filters out edges below the selected threshold, hiding low-signal connections. Nodes with no remaining edges after filtering are also hidden.
- **Click a node:** Highlights the node and its direct edges; dims everything else. A small info panel (entity name, type, top 5 connections by weight) appears alongside the graph.
- **Click background:** Deselects, returns to full view.

### New Pinia store: `useConnectionsStore`

- `connections: EntityConnection[]`
- `fetchConnections()` — GET `/api/entity-connections`
- `resetConnections()` — DELETE `/api/entity-connections`, then clears local state
- Computed: `graphData` — transforms `connections` into `{nodes: [], edges: []}` format for vis-network, applying the current min-weight filter

### New API client: `web/src/api/connections.ts`

Thin wrapper over fetch:
- `getEntityConnections(): Promise<EntityConnection[]>`
- `deleteEntityConnections(): Promise<void>`

## File Checklist

**Backend:**
- `migrations/024_entity_connections.sql` — new table + indexes
- `internal/model/entity.go` — add `EntityConnection` struct
- `internal/store/store.go` — add 3 interface methods
- `internal/store/sqlite_entities.go` (or new file) — implement store methods
- `internal/pipeline/entity_cooccurrence.go` — new pipeline
- `internal/pipeline/registry.go` — register handler
- `internal/api/handlers_entity_connections.go` — new handler file
- `internal/api/router.go` — register 2 routes

**Frontend:**
- `web/package.json` — add `vis-network`
- `web/src/api/connections.ts` — API client
- `web/src/stores/connections.ts` — Pinia store
- `web/src/views/TrackerView.vue` — add Connections tab

## Out of Scope

- Connections involving entity candidates (candidates only accumulate connections after promotion, via the normal entity_matcher flow once promoted).
- Directed edges (connections are undirected, order normalized by `from_id < to_id`).
- Time-windowed graphs (showing connections over a date range) — add later if needed.
- Exporting the graph as an image or data file.
