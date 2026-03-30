# Connections Graph — Entity-Focused Filtering

**Date:** 2026-03-30
**Status:** Approved

## Summary

Replace the all-connections graph view with an entity-focused subgraph view. The user picks a focal entity; only that entity's neighborhood (up to a configurable depth and minimum co-occurrence weight) is rendered. Nothing is shown until an entity is selected.

## Problem

The current Connections tab renders every entity and edge at once. With even a moderate number of tracked entities the graph becomes unreadable.

## Design

### Controls

A filter bar above the graph contains three controls:

| Control | Type | Default | Range |
|---|---|---|---|
| Entity | Searchable select (name + type badge) | — (empty) | All tracked entities |
| Depth | Slider | 2 | 1–5 |
| Min weight | Slider | 1 | 1–max observed weight |

- **Entity is required.** When none is selected, the graph area shows an empty-state prompt: *"Select an entity to explore its connections."*
- Depth and min-weight changes debounce 300 ms before issuing a new fetch.
- The admin-only **Reset** button (truncates `entity_connections` table) remains in the filter bar. After reset, `selectedEntityId` is set to `null` so the empty-state prompt is shown immediately rather than leaving a stale graph over an empty store.

### API

New endpoint:

```
GET /api/entity-connections/subgraph?entity_id=X&depth=Y&min_weight=Z
```

| Param | Type | Required | Default |
|---|---|---|---|
| `entity_id` | int64 | yes | — |
| `depth` | int | no | 2 |
| `min_weight` | int | no | 1 |

Response body is identical to `GET /api/entity-connections`:

```json
{ "connections": [ EntityConnection… ] }
```

Returns 400 if `entity_id` is missing or non-positive. Returns an empty `connections` array (not 404) if the entity has no edges meeting the criteria.

### Backend — Store Layer

New store method:

```go
GetEntityConnectionSubgraph(ctx context.Context, entityID int64, depth, minWeight int) ([]*model.EntityConnection, error)
```

Implementation (Go BFS inside the store method):

1. Call `ListEntityConnections(ctx)` to load all edges.
2. Build an adjacency list `map[int64][]int64` using only edges with `weight >= minWeight`.
3. BFS from `entityID` up to `depth` hops; collect the reachable entity ID set.
4. Return the subset of edges where both `from_id` and `to_id` are in the reachable set **and** `weight >= minWeight`.
5. Include the focal entity itself even if it has zero qualifying edges (returns empty slice).

Add `GetEntityConnectionSubgraph` to the `Store` interface in `internal/store/store.go`.

### Backend — Handler

New handler `handleGetEntityConnectionSubgraph` in `internal/api/handlers_entity_connections.go`:

- Parse and validate `entity_id` (required), `depth` (clamp 1–5), `min_weight` (clamp ≥ 1).
- Call `s.store.GetEntityConnectionSubgraph(...)`.
- Write `{"connections": [...]}` — same shape as `handleListEntityConnections`.

Register the route in `server.go` as `GET /api/entity-connections/subgraph` (viewer role, before the existing `/api/entity-connections` routes so it isn't shadowed).

### Frontend — API Layer (`web/src/api/connections.ts`)

Add:

```ts
export const getEntitySubgraph = (entityID: number, depth: number, minWeight: number) =>
  apiFetch<EntityConnectionsResponse>(
    `/api/entity-connections/subgraph?entity_id=${entityID}&depth=${depth}&min_weight=${minWeight}`
  )
```

### Frontend — Store (`web/src/stores/connections.ts`)

- Rename `connections` → `subgraph` (holds the currently displayed subgraph edges).
- Replace `fetchConnections()` with `fetchSubgraph(entityID, depth, minWeight)` which calls `getEntitySubgraph(...)`.
- `graphData` computed operates on `subgraph` (unchanged algorithm).
- Keep `resetConnections()` (calls `DELETE /api/entity-connections`; clears `subgraph` to `[]`).
- Drop `fetchConnections` from the public API entirely — the old "load all on tab open" behaviour is gone.

### Frontend — TrackerView (`web/src/views/TrackerView.vue`)

**New reactive state:**

```ts
const selectedEntityId = ref<number | null>(null)
const depth = ref(2)
// minWeight already exists
```

**Filter bar layout:**

```
[Entity: Isfahan (LOC) ▼]  [Depth: ──●── 2]  [Min weight: ──●── 1]  [Reset]
```

Entity picker is an `<n-select>` with `filterable`, populated from `store.items` (already loaded). Option label format: `"Name (TYPE)"`.

**Reactivity:**

```ts
let debounceTimer = 0
watch([selectedEntityId, depth, minWeight], ([eid]) => {
  if (!eid) { destroyNetwork(); return }
  clearTimeout(debounceTimer)
  debounceTimer = window.setTimeout(async () => {
    await connectionsStore.fetchSubgraph(eid, depth.value, minWeight.value)
    await nextTick()
    await renderGraph()
  }, 300)
})
```

**onTabChange:** Remove the `fetchConnections()` call; only ensure `store.fetchEntities()` has run (already called in `onMounted`).

**Empty state:** Shown when `selectedEntityId` is null (replaces the existing "no connections yet" message for this case).

**Min-weight slider max:** Computed from `connectionsStore.subgraph` (max weight among the currently fetched subgraph edges). This means the slider ceiling updates when the entity or depth changes, which is intentional — the range is always meaningful for the visible data.

**Graph rendering:** No changes to vis-network config. The focal entity node will naturally appear largest because it has the highest `totalWeight` (connected to every other node in the subgraph by definition).

## Testing

- Unit test `GetEntityConnectionSubgraph` in `sqlite_test.go`: depth-1 returns only direct neighbors; depth-2 returns neighbors-of-neighbors; min_weight filters edges; entity with no connections returns empty slice; unknown entity returns empty slice.
- Update the mock `Store` (used in worker/stream tests) to add the new method with a no-op implementation.

## Documentation Updates

- `CLAUDE.md` — update "Entity connections graph" in Key Design Decisions to mention the subgraph endpoint and entity-focused filter.
- This spec file.
