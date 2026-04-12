# Depth-Priority Queue Design

**Date:** 2026-04-12  
**Status:** Approved

## Overview

Tasks deeper in a pipeline graph should be processed after tasks closer to the Start node. This ensures early-stage actions (which produce outputs consumed by downstream stages) are not starved behind deep sub-pipeline work from other in-flight tweets.

Priority is expressed as a `Depth` integer on each task. Lower depth = higher priority. Depth is the number of action-hops from the pipeline's Start node.

## Data Model

### `queue.Task` — new field

```go
Depth int `json:"depth"`
```

- Initial tasks dispatched by the stream listener or search backfill → `Depth = 0`
- Each continuation hop → `Depth = parent.Depth + 1`
- Sub-pipeline entry tasks → `Depth = parent.Depth + 1` (crossing into a sub-pipeline counts as one hop)
- Condition nodes are evaluated synchronously during dispatch — no task is created for them, so they do not increment depth

**Backwards compatibility:** tasks serialized before this change deserialize with `Depth = 0` (the zero value), which correctly assigns them the highest priority rather than penalising them.

### Example

```
Start → Action-A (depth 0) → Action-B (depth 1) → [SubPipeline node]
                                                       → Action-C (depth 2)
                                                       → Action-D (depth 3)
```

Global processing order: A → B → C → D, across all in-flight tweets.

## Redis Layer

Per-action task queues change from **Redis lists → Redis sorted sets**. The score is `task.Depth`.

### Interface changes (`redisClient`)

Three sorted-set methods are added alongside the existing list methods (which remain for the dead-letter queue):

```go
ZAdd(ctx context.Context, key string, members ...redis.Z) *redis.IntCmd
BZPopMin(ctx context.Context, timeout float64, keys ...string) *redis.ZSliceWithKeyCmd
ZCard(ctx context.Context, key string) *redis.IntCmd
```

### `PushTask`

`LPush(key, taskJSON)` → `ZAdd(key, redis.Z{Score: float64(task.Depth), Member: taskJSON})`

`LPush` returned the total list length, which was used for the WebSocket `queue_update` broadcast. `ZAdd` only returns the number of *new* elements added (0 or 1), not the total. After `ZAdd`, an explicit `ZCard` call is required to obtain the actual queue depth for broadcasting. The `{action_id, pending}` event shape is unchanged.

### `PopTask`

`BRPop(timeout, keys...)` → `BZPopMin(timeout, keys...)`.

`BZPopMin` blocks until the element with the lowest score is available across any of the provided keys, then returns `(key, []Z)`. The `PopTask` signature is unchanged to callers. The post-pop `LLen` call for the broadcast is replaced with `ZCard`.

**Note:** `BZPopMin` accepts `timeout float64` (seconds), while `BRPop` accepts `time.Duration`. The implementation must convert: `timeout.Seconds()`.

### `Len` / `TotalTaskLen`

`LLen` → `ZCard` for keys matching `iran-watcher:tasks:*`. The dead-letter queue (`iran-watcher:failed`) continues to use `LLen`.

### Unchanged

- `FlushQueue` — `Del` works on any Redis type; no change needed
- `ListFailed` / `ClearFailed` — operate on the plain-list failed queue; unchanged
- `FlushQueue` scan pattern `iran-watcher:tasks:*` — unchanged

### Deduplication note

Sorted sets deduplicate by member string. Because task JSON encodes `tweet_id`, `action_id`, `pipeline_node_id`, and `context`, two tasks with identical payloads represent identical work. If such a duplicate is enqueued, the existing entry (with its score) is updated to the new score — the lower score wins, which is the correct behaviour.

## Depth Propagation (`internal/worker/pipeline.go`)

### `dispatchInitialNode` — new `depth int` parameter

Called from `PushInitialTasksForGraph` with `depth = 0`.

- Action node: `task.Depth = depth`
- Condition node recursion: passes the **same depth** (conditions are routing only)
- Sub-pipeline node: entry tasks get `depth + 1`

### `pushContinuationTask`

```go
newTask.Depth = task.Depth + 1
```

### Sub-pipeline tasks in `PushContinuationTasks`

When a `PipelineNodeTypePipeline` node is reached during continuation, sub-pipeline entry tasks get `task.Depth + 1`. This continues the hop count seamlessly through the nesting boundary.

### `PushTasksFromNode` (manual backfill)

Passes `depth = 0` — a user-initiated run starts fresh from the highest priority.

## Worker Layer

No changes to `Manager`, `ActionCache`, or `buildReadyKeys`. Workers already do a variadic pop across all ready-action keys; Redis returns the globally lowest-score task automatically.

## UI Layer

No changes required. The Queue page (`QueueView.vue`) displays a `pending` count per action, sourced from:

1. The initial HTTP `GET /api/queue` response — `handleGetQueue` calls `Len()` which will use `ZCard` internally; the API response shape is unchanged.
2. The live WebSocket `queue_update` event — shape `{action_id, pending}` is unchanged; the backend now derives the count via `ZCard` instead of `LPush`'s return value.

No new columns, stores, or API fields are needed.

## Migration

No data migration is required. On deploy, any tasks remaining in the old list-format queues will not be picked up by `BZPopMin`. Because tweets are persisted to SQLite before queueing, flushing the queue on deploy is safe and appropriate — tasks can be re-dispatched via manual pipeline run if needed. The `DELETE /api/queue` endpoint (flush all pending queues) should be called once after deploying.

## Affected Files

| File | Change |
|------|--------|
| `internal/queue/queue.go` | Add `Depth int` to `Task` |
| `internal/queue/redis.go` | Extend `redisClient` interface; `PushTask` → `ZAdd`; `PopTask` → `BZPopMin`; `Len`/`TotalTaskLen` → `ZCard` |
| `internal/worker/pipeline.go` | Add `depth` param to `dispatchInitialNode`; propagate `Depth` in all task construction sites |
| `internal/queue/queue_test.go` | Update mock/test expectations for sorted-set operations |
| `internal/queue/queue_integration_test.go` | Update integration test push/pop to sorted-set API |
| `web/` | No changes required |
