# Pipeline Run: Tweet Dispatch Order

**Date:** 2026-04-10
**Status:** Approved

## Problem

`POST /api/pipelines/{id}/run` always fetches tweets ordered `created_at DESC` (newest first). For notification pipelines, tweets should be dispatched oldest-first so push alerts arrive in chronological order.

## Goal

Add an optional `order` field to the run request that controls the dispatch order of tweets. Default is `"desc"` (newest first) to preserve existing behaviour.

## API Change

`POST /api/pipelines/{id}/run` request body gains one optional field:

```json
{ "new_only": bool, "start_node_id": int, "order": "asc" | "desc" }
```

- `"desc"` (default when omitted) — newest tweet dispatched first
- `"asc"` — oldest tweet dispatched first

This applies to both full-graph runs and `start_node_id` runs.

## Backend

### Store layer

`TweetFilter` in `internal/store/store.go` gains:

```go
OrderAsc bool
```

`ListTweets` in `internal/store/sqlite.go` switches its `ORDER BY` clause:

```sql
ORDER BY t.created_at ASC   -- when OrderAsc: true
ORDER BY t.created_at DESC  -- when OrderAsc: false (default)
```

### Handler

`internal/api/handlers_pipelines.go` request struct gains:

```go
Order string `json:"order"` // "asc" | "desc"; default "desc"
```

Before calling `ListTweets`, the handler sets:

```go
filter.OrderAsc = req.Order == "asc"
```

### MCP Tool

The `pipelines_run` tool description in `internal/mcpserver/tools.go` is updated to document the `order` field.

## Frontend

### API module (`web/src/api/pipelines.ts`)

`runPipeline` gains an optional fourth parameter:

```ts
export async function runPipeline(
  id: number,
  newOnly: boolean,
  startNodeId?: number,
  order?: 'asc' | 'desc',
): Promise<{ queued: number; async?: boolean }>
```

When `order` is provided and not `"desc"` it is included in the request body as `order`. (Omitting it when `"desc"` keeps the request minimal; either approach is acceptable.)

### `web/src/stores/pipelines.ts`

`run` method gains the same optional `order` param and passes it through.

### Run modal — `PipelinesView.vue`

A new `runOrder = ref<'asc' | 'desc'>('desc')` ref is added. It is reset to `'desc'` when the modal opens. A radio pair is added below the `new_only` group:

- **Newest first (default)**
- **Oldest first**

`onConfirmRunPipeline` passes `runOrder.value` as the fourth argument.

### Run-from-node modal — `PipelineBuilderView.vue`

A new `runFromNodeOrder = ref<'asc' | 'desc'>('desc')` ref is added. Reset to `'desc'` when `handleRunFromNode` opens the modal. The same radio pair is added to the run-from-node confirmation modal.

`onConfirmRunFromNode` passes `runFromNodeOrder.value` to `runPipeline`.

## Affected Files

| File | Change |
|------|--------|
| `internal/store/store.go` | Add `OrderAsc bool` to `TweetFilter` |
| `internal/store/sqlite.go` | Switch `ORDER BY` clause based on `OrderAsc` |
| `internal/api/handlers_pipelines.go` | Add `Order` field to request struct; set `filter.OrderAsc` |
| `internal/mcpserver/tools.go` | Update `pipelines_run` tool description |
| `web/src/api/pipelines.ts` | Add optional `order` param to `runPipeline` |
| `web/src/stores/pipelines.ts` | Add optional `order` param to `run` |
| `web/src/views/PipelinesView.vue` | Add `runOrder` ref and radio pair to run modal |
| `web/src/views/PipelineBuilderView.vue` | Add `runFromNodeOrder` ref and radio pair to run-from-node modal |

## Out of Scope

- Ordering by any field other than `created_at`
- Per-trigger-type ordering
- Pagination of the backfill (still limited to 5000 tweets per trigger link)
