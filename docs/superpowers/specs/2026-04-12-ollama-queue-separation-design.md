# Ollama Queue Separation Design

**Date:** 2026-04-12
**Status:** Approved

## Problem

All tasks share a single global sorted set `iran-watcher:tasks`. When a DAG pipeline dispatches a batch of Ollama tasks at the same depth, every idle worker unblocks simultaneously. Because `IsAvailable()` is a non-atomic read, multiple workers pass the capacity check before any of them has acquired a backend slot. All of them enter `processTask`, increment the active counter, and then block or fail inside `AcquireForPipeline`. The net result:

- More workers appear "active" than backends can serve
- Workers that fail to acquire re-queue the task and spin at 100 ms intervals
- Workers that could process non-Ollama tasks at any depth are blocked competing for Ollama backend slots
- If all tasks at a given depth are Ollama and backends are saturated, no work at deeper depths can proceed

## Solution

Two Redis sorted sets, both scored by `Depth`. Workers use a peek-then-pop loop that compares the minimum depth across both queues and only pops from the Ollama queue when backend capacity exists. Workers are never blocked waiting for Ollama capacity — they fall back to the non-Ollama queue or block idle.

---

## Queue Layer (`internal/queue/`)

### Redis keys

| Key | Contents | Score |
|-----|----------|-------|
| `iran-watcher:tasks` | ONNX, internal, notification tasks | `task.Depth` |
| `iran-watcher:tasks:ollama` | Ollama tasks only | `task.Depth` |
| `iran-watcher:failed` | Dead-letter list | — (unchanged) |

New constant: `QueueOllamaTasksKey = "iran-watcher:tasks:ollama"`

### `Task` struct — new field

```go
IsOllama bool `json:"is_ollama"`
```

Set at every push call site by inspecting the action's pipeline type via `ActionCache`. Survives re-queue round-trips because the field is part of the serialised JSON.

### `PushTask`

Routes to the correct key based on `task.IsOllama`:

```
if task.IsOllama:
    ZAdd("iran-watcher:tasks:ollama", score=task.Depth, member=taskJSON)
    ZCard("iran-watcher:tasks:ollama") → pending_ollama for broadcast
else:
    ZAdd("iran-watcher:tasks", score=task.Depth, member=taskJSON)
    ZCard("iran-watcher:tasks") → pending_tasks for broadcast
```

Broadcast after every push:

```json
{ "pending_tasks": N, "pending_ollama": M }
```

### `Consumer` interface — new methods

```go
// TryPopTask is a non-blocking ZPopMin on iran-watcher:tasks.
// Returns (nil, nil) if the queue is empty.
TryPopTask(ctx context.Context) (*Task, error)

// TryPopOllamaTask is a non-blocking ZPopMin on iran-watcher:tasks:ollama.
// Returns (nil, nil) if the queue is empty.
TryPopOllamaTask(ctx context.Context) (*Task, error)

// PeekMinDepth returns the score (Depth) of the lowest-scored member in the
// given key without consuming it. Returns (0, false) if the key is empty.
PeekMinDepth(ctx context.Context, key string) (depth float64, ok bool)
```

`PopTask` (blocking `BZPopMin` on `iran-watcher:tasks`) is retained for the idle-wait step.

### `TotalTaskLen`

Returns the sum of both queues:

```
ZCard("iran-watcher:tasks") + ZCard("iran-watcher:tasks:ollama")
```

### `FlushQueue`

Deletes both keys:

```
DEL "iran-watcher:tasks" "iran-watcher:tasks:ollama"
```

### `queue_update` broadcast shape

```json
// Before
{ "pending": 42 }

// After
{ "pending_tasks": 42, "pending_ollama": 18 }
```

---

## Worker Layer (`internal/worker/`)

### New worker loop

```
each iteration:
  1. PeekMinDepth("iran-watcher:tasks")        → D1 (∞ if empty)
  2. if Ollama capacity available (IsAvailable()):
       PeekMinDepth("iran-watcher:tasks:ollama") → D2 (∞ if empty)
     else:
       D2 = ∞
  3. if D1 == ∞ and D2 == ∞:
       PopTask(ctx, 250ms)  ← BZPopMin block; run if got task; continue
  4. if D1 <= D2:
       TryPopTask(ctx) → run immediately (no capacity check needed)
  5. else (D2 < D1):
       TryPopOllamaTask(ctx) → run (AcquireForPipeline inside pipeline)
```

**Anti-spin / re-queue safety net:** If a worker reaches step 5, pops an Ollama task, and then `AcquireForPipeline` fails (race between peek and acquire), the task is re-queued to `iran-watcher:tasks:ollama` using `task.IsOllama`. No sleep is added — the loop immediately repeats from step 1, where it will likely see D2 = ∞ (or another worker acquired the last slot) and fall through to the 250 ms block. This makes the re-queue path a rare and self-correcting edge case, not a spin loop.

**`IsAvailable()` in step 2:** This remains a non-atomic read, but it now only gates the peek, not processTask entry. The worst case is a worker peeks a stale `ok=true`, pops an Ollama task, and fails inside the pipeline — which triggers the re-queue safety net above. This is a significant improvement over the current design where all workers enter processTask.

### `IsOllama` on Task — set at push time

Every function that constructs and pushes a `Task` must set `IsOllama`:

| Call site | How to determine |
|-----------|-----------------|
| `PushInitialTasksForGraph` | `cache.Get(actionID).Pipeline.Type() == inference.PipelineTypeOllama` |
| `PushContinuationTasks` | same lookup |
| `PushInitialPipelineTasks` | same lookup |
| Stream listener | same lookup |
| Worker re-queue (safety net) | `task.IsOllama` already set — preserved from original push |

### Re-queue path (safety net only)

```go
if acquireErr != nil {
    _ = m.producer.PushTask(ctx, task)  // task.IsOllama routes it back to ollama queue
    continue  // no sleep — loop re-checks immediately, will block if no capacity
}
```

The `time.Sleep(100 * time.Millisecond)` anti-spin is **removed** — it is no longer needed because the loop naturally falls through to the 250 ms BZPopMin block when no work is available.

---

## API Layer (`internal/api/`)

### `GET /api/queue`

Updated response:

```json
{
  "pending_tasks": 42,
  "pending_ollama": 18,
  "workers": [ ... ]
}
```

Handler calls `queue.Len(ctx, QueueTasksKey)` and `queue.Len(ctx, QueueOllamaTasksKey)` separately.

### `DELETE /api/queue`

Calls `FlushQueue` which now deletes both keys. No change to the endpoint signature.

---

## UI Layer (`web/src/views/QueueView.vue`)

### Header cards

Replace the single "Pending" card with two cards:

| Card | Value | Source |
|------|-------|--------|
| Pending | `pending_tasks` | non-Ollama queue depth |
| Pending Ollama | `pending_ollama` | Ollama queue depth |

### Stores / composables

- `queue_update` handler: read `pending_tasks` and `pending_ollama` (replace `pending`)
- `stores/queues.ts`: replace `pending: ref(0)` with `pendingTasks: ref(0)` and `pendingOllama: ref(0)`
- `GET /api/queue` initial load: populate both refs

All other queue page elements (worker table, failures section, action buttons) are unchanged.

---

## Affected Files

| File | Change |
|------|--------|
| `internal/queue/queue.go` | Add `QueueOllamaTasksKey` constant; add `TryPopTask`, `TryPopOllamaTask`, `PeekMinDepth` to `Consumer` interface; add `IsOllama bool` to `Task` |
| `internal/queue/redis.go` | `PushTask` routes on `IsOllama`; implement `TryPopTask`, `TryPopOllamaTask`, `PeekMinDepth`; update `TotalTaskLen`, `FlushQueue`; update `queue_update` broadcast shape |
| `internal/queue/queue_test.go` | Tests for routing, new Consumer methods, updated broadcast shape |
| `internal/worker/manager.go` | Replace worker loop with peek-then-pop; remove 100 ms sleep; update re-queue safety net |
| `internal/worker/pipeline.go` | Set `task.IsOllama` in `PushInitialTasksForGraph`, `PushContinuationTasks`, `PushInitialPipelineTasks` |
| `internal/stream/` | Set `task.IsOllama` when constructing tasks from stream events |
| `internal/api/handlers_queue.go` | Return `pending_tasks` and `pending_ollama` separately |
| `web/src/stores/queues.ts` | Replace `pending` with `pendingTasks` + `pendingOllama` |
| `web/src/composables/useWebSocket.ts` | Update `queue_update` handler |
| `web/src/views/QueueView.vue` | Two pending cards |
| `CLAUDE.md` | Update queue key descriptions, `queue_update` event shape |

---

## Migration

On deploy: call `DELETE /api/queue` to flush any tasks remaining in `iran-watcher:tasks` from the old format. The old key may contain Ollama tasks that would have been routed to `iran-watcher:tasks:ollama` under the new scheme — flushing ensures a clean start. New tasks will be routed correctly from the first push. No data migration needed — tweets are persisted in SQLite.
