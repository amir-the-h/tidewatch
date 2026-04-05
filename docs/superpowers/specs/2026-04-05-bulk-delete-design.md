# Bulk Delete API Design

**Date:** 2026-04-05  
**Status:** Approved

## Overview

Add collection-level bulk delete endpoints to all resources that currently only support single-delete-by-ID. Each bulk delete accepts an optional JSON body with `ids` and resource-specific filter fields. Empty body `{}` deletes all records. Existing `DELETE /api/{resource}/{id}` routes remain unchanged. All new endpoints are also exposed as MCP tools.

## API Pattern

```
DELETE /api/{resource}
Content-Type: application/json

{ ...optional filters... }

→ 200 {"deleted": N}
```

Rules:
- All fields are optional. Omit all = delete everything.
- `ids` and filter fields combine with AND — a record must match all specified constraints.
- `ids` alone scopes to exactly those records.
- Response is always `{"deleted": N}` where N is the count of deleted rows.
- The existing `DELETE /api/{resource}/{id}` routes are untouched.

## Resources and Filter Sets

### `DELETE /api/rules`
| Field | Type | Description |
|-------|------|-------------|
| `ids` | `[]int64` | Delete only these rule IDs |
| `active` | `*bool` | Filter by active status |
| `local` | `*bool` | Filter by local vs remote rules |

### `DELETE /api/sources`
| Field | Type | Description |
|-------|------|-------------|
| `ids` | `[]int64` | Delete only these source IDs |
| `active` | `*bool` | Filter by active status |
| `type` | `string` | Filter by source type |

### `DELETE /api/pipelines`
| Field | Type | Description |
|-------|------|-------------|
| `ids` | `[]int64` | Delete only these pipeline IDs |
| `active` | `*bool` | Filter by active status |

### `DELETE /api/triggers/links`
| Field | Type | Description |
|-------|------|-------------|
| `ids` | `[]int64` | Delete only these link IDs |
| `pipeline_id` | `*int64` | Delete all links for a pipeline |
| `trigger_type` | `string` | Filter by trigger type |

### `DELETE /api/tracked-entities`
| Field | Type | Description |
|-------|------|-------------|
| `ids` | `[]int64` | Delete only these entity IDs |
| `active` | `*bool` | Filter by active status |
| `type` | `string` | Filter by entity type (e.g. `person`, `organization`) |

### `DELETE /api/entity-candidates`
| Field | Type | Description |
|-------|------|-------------|
| `ids` | `[]int64` | Delete only these candidate IDs |
| `status` | `string` | Filter by status: `pending`, `promoted`, `rejected` |
| `type` | `string` | Filter by NER type: `I-PER`, `I-LOC`, `I-ORG`, `I-MISC` |
| `auto_promoted` | `*bool` | Filter by auto-promotion flag |
| `min_confidence` | `*float64` | Minimum avg confidence (inclusive) |
| `max_confidence` | `*float64` | Maximum avg confidence (inclusive) |
| `min_occurrences` | `*int` | Minimum occurrence count (inclusive) |
| `max_occurrences` | `*int` | Maximum occurrence count (inclusive) |

### `DELETE /api/actions`
| Field | Type | Description |
|-------|------|-------------|
| `ids` | `[]int64` | Delete only these action IDs |
| `active` | `*bool` | Filter by active status |
| `type` | `string` | Filter by action type: `onnx`, `ollama`, `internal` |

### `DELETE /api/ollama/backends`
| Field | Type | Description |
|-------|------|-------------|
| `ids` | `[]int64` | Delete only these backend IDs |
| `enabled` | `*bool` | Filter by enabled status |

### `DELETE /api/notification-providers`
| Field | Type | Description |
|-------|------|-------------|
| `ids` | `[]int64` | Delete only these provider IDs |
| `active` | `*bool` | Filter by active status |
| `type` | `string` | Filter by provider type: `pushover`, `telegram` |

### `DELETE /api/notification-channels`
| Field | Type | Description |
|-------|------|-------------|
| `ids` | `[]int64` | Delete only these channel IDs |
| `active` | `*bool` | Filter by active status |
| `provider_id` | `*int64` | Delete all channels for a provider |

### `DELETE /api/search` (search jobs)
| Field | Type | Description |
|-------|------|-------------|
| `ids` | `[]string` | Delete only these job IDs (UUIDs) |
| `status` | `string` | Filter by status: `pending`, `done`, `error` |

### `DELETE /api/digest` (digest jobs)
| Field | Type | Description |
|-------|------|-------------|
| `ids` | `[]string` | Delete only these job IDs (UUIDs) |
| `status` | `string` | Filter by status: `pending`, `done`, `error` |

## Store Layer

Each resource gets a `BulkDelete*Filter` struct and a `BulkDelete*` store function in `internal/store/`. Filter fields use pointer types for optional booleans and numerics so nil means "not filtered". String fields use empty string as "not filtered".

```go
// Example for entity candidates
type BulkDeleteEntityCandidatesFilter struct {
    IDs            []int64
    Status         string
    Type           string
    AutoPromoted   *bool
    MinConfidence  *float64
    MaxConfidence  *float64
    MinOccurrences *int
    MaxOccurrences *int
}

func (s *sqliteStore) BulkDeleteEntityCandidates(ctx context.Context, f BulkDeleteEntityCandidatesFilter) (int64, error)
```

SQL strategy: build a `WHERE` clause dynamically, appending conditions only for non-zero fields, then execute `DELETE FROM {table} WHERE {clause}`. No `WHERE` clause when all fields are zero = delete all. Return `RowsAffected`.

Cascading deletes rely on existing SQLite `ON DELETE CASCADE` constraints — no extra cleanup queries needed.

Filter structs for each resource live alongside the existing store files:
- `sqlite.go` — rules, sources, tracked-entities, entity candidates, search jobs, digest jobs
- `sqlite_actions.go` — actions
- `sqlite_pipelines.go` — pipelines, triggers/links
- `sqlite_notification_providers.go` — notification providers, notification channels
- `sqlite_ollama.go` — ollama backends

## API Handlers

Each resource gets a new handler function (e.g. `handleBulkDeleteEntityCandidates`) registered on the collection route. Handler pattern:

```go
func (s *Server) handleBulkDeleteEntityCandidates(w http.ResponseWriter, r *http.Request) {
    var f store.BulkDeleteEntityCandidatesFilter
    if err := json.NewDecoder(r.Body).Decode(&f); err != nil {
        writeError(w, http.StatusBadRequest, "invalid request body")
        return
    }
    n, err := s.store.BulkDeleteEntityCandidates(r.Context(), f)
    if err != nil {
        s.logger.Error("api: bulk delete entity candidates", "error", err)
        writeError(w, http.StatusInternalServerError, "failed to bulk delete entity candidates")
        return
    }
    writeJSON(w, http.StatusOK, map[string]int64{"deleted": n})
}
```

Router registration (admin-only group):
```go
r.Delete("/entity-candidates", srv.handleBulkDeleteEntityCandidates)
r.Delete("/entity-candidates/{id}", srv.handleDeleteEntityCandidate) // unchanged
```

## MCP Tools

Each bulk endpoint gets a new tool entry in `internal/mcpserver/tools.go`, named `{resource}_bulk_delete`:

```go
{
    Name:        "entity_candidates_bulk_delete",
    Description: "Bulk-delete entity candidates. All filters optional — omit all to delete every candidate. Filters combine with AND.",
    Method:      "DELETE",
    Path:        "/api/entity-candidates",
    Body: &BodyDef{
        Description: "ids ([]int), status (string: pending/promoted/rejected), type (string: I-PER/I-LOC/I-ORG/I-MISC), auto_promoted (bool), min_confidence (float), max_confidence (float), min_occurrences (int), max_occurrences (int). All optional.",
    },
},
```

Existing single-delete tools are unchanged. The dispatcher already handles `DELETE` requests with a body correctly.

## Implementation Order

1. **Store layer** — add `BulkDelete*Filter` structs and `BulkDelete*` functions for all 12 resources
2. **Store interface** — add new method signatures to the `Store` interface in `internal/store/store.go`
3. **API handlers** — add `handleBulkDelete*` handler functions
4. **Router** — register new collection `DELETE` routes in `server.go`
5. **MCP tools** — add `*_bulk_delete` tool entries in `tools.go`
6. **Tests** — unit tests for each store function covering: empty filter (delete all), ids-only, filter-only, combined ids+filter, no matching rows

## What Does NOT Change

- All existing `DELETE /api/{resource}/{id}` single-delete routes and their handlers
- All existing single-delete MCP tools
- `DELETE /api/tweets` and `DELETE /api/executions` (already bulk with filters)
- `DELETE /api/entity-connections`, `DELETE /api/failures`, `DELETE /api/queue` (already all-or-nothing)
- `DELETE /api/users/{id}` and `DELETE /api/twitter/auth` (excluded by design)
