# Per-Action Queue Design

**Date:** 2026-03-29
**Status:** Approved

## Problem

Workers share a single Redis list `iran-watcher:tasks`. After popping a task, if the action uses an Ollama pipeline and all backends are busy, the worker blocks inside `pipeline.Run()` → `acquire()` waiting for a backend. The task is already removed from the queue, so it cannot be picked up by another worker, and the blocked worker cannot process ONNX or notification tasks queued behind it.

## Solution

Route tasks to per-action Redis keys and have workers pop only from queues whose pipeline is currently acquirable. ONNX, internal, and notification pipelines are always acquirable. Ollama pipelines are only included in the pop set when at least one assigned (or pool-wide) backend is healthy, enabled, and not in use.

---

## 1. Queue Layer

### 1.1 Key scheme

| Old | New |
|-----|-----|
| `iran-watcher:tasks` | `iran-watcher:tasks:{action_id}` (e.g. `iran-watcher:tasks:7`) |
| `iran-watcher:failed` | unchanged |
| `iran-watcher:raw` | removed (already deprecated) |

Constants in `internal/queue/queue.go`:

```go
const (
    QueueTasksPrefix = "iran-watcher:tasks:"
    QueueFailed      = "iran-watcher:failed"
)

func QueueTasksKey(actionID int64) string {
    return QueueTasksPrefix + strconv.FormatInt(actionID, 10)
}
```

`QueueTasks` and `QueueRaw` constants are removed. `FlushQueue` flushes the old `QueueRaw` key during the transition — not needed post-migration.

### 1.2 Producer

`PushTask` routes to `QueueTasksKey(task.ActionID)`. No interface change.

```go
func (q *RedisQueue) PushTask(ctx context.Context, task *Task) error {
    key := QueueTasksKey(task.ActionID)
    // LPUSH to key
}
```

### 1.3 Consumer interface

`PopTask` accepts variadic queue keys. The worker builds and passes the list; the method passes them straight to `BRPOP`.

```go
type Consumer interface {
    PopTask(ctx context.Context, timeout time.Duration, keys ...string) (*Task, error)
    PopFailed(ctx context.Context, timeout time.Duration) (*FailedTweet, error)
}
```

### 1.4 FlushQueue

`redisClient` gains a `Scan` method (already available on `redis.Client`). `FlushQueue` scans for `iran-watcher:tasks:*` in cursor pages and deletes each batch:

```go
func (q *RedisQueue) FlushQueue(ctx context.Context) error {
    var cursor uint64
    for {
        keys, next, err := q.client.Scan(ctx, cursor, QueueTasksPrefix+"*", 100).Result()
        if err != nil { return err }
        if len(keys) > 0 { q.client.Del(ctx, keys...) }
        if next == 0 { break }
        cursor = next
    }
    return nil
}
```

### 1.5 Queue depth stats

`RedisQueue` gains `TotalTaskLen(ctx context.Context) (int64, error)` — scans all `iran-watcher:tasks:*` keys and sums their `LLEN`. The health handler uses this instead of `Len(ctx, QueueTasks)`.

---

## 2. Availability Gating

### 2.1 Optional interface

`internal/inference/pipeline.go` gains:

```go
// AvailabilityChecker is implemented by pipelines whose execution requires
// a scarce resource (e.g. an Ollama backend). Pipelines that do not implement
// this interface are assumed always available.
type AvailabilityChecker interface {
    IsAvailable() bool
}
```

`OllamaPipeline` is the only type that implements `AvailabilityChecker`. ONNX, internal, and notification pipelines do not.

### 2.2 BackendPool additions

```go
// IsAnyAvailable reports whether any backend matching assignedIDs is
// healthy, enabled, and not in use. Empty/nil assignedIDs checks the full pool.
// Non-blocking; caller must not hold p.mu.
func (p *BackendPool) IsAnyAvailable(assignedIDs []int64) bool

// WaitForAvailability blocks until a matching backend is available,
// the timeout elapses, or ctx is cancelled.
func (p *BackendPool) WaitForAvailability(ctx context.Context, timeout time.Duration, assignedIDs []int64)
```

Both use the existing `p.cond` / `p.mu`. `isAnyAvailable` (private) holds the lock-required logic shared by both.

### 2.3 OllamaPipeline

```go
func (p *OllamaPipeline) IsAvailable() bool {
    return p.pool.IsAnyAvailable(p.assignedIDs)
}
```

Respects priority-ordered assigned backends; empty `assignedIDs` falls back to the full pool sorted by score (existing `bestEnabledIdle` logic).

### 2.4 ActionCache accessor

```go
func (c *ActionCache) Pool() *inference.BackendPool { return c.pool }
```

Allows `Manager` to call `WaitForAvailability` without receiving the pool as a separate constructor parameter.

---

## 3. Worker Loop

### 3.1 buildReadyKeys

```go
func (m *Manager) buildReadyKeys() []string {
    entries := m.cache.All()
    keys := make([]string, 0, len(entries))
    for _, e := range entries {
        if av, ok := e.Pipeline.(inference.AvailabilityChecker); ok {
            if av.IsAvailable() {
                keys = append(keys, queue.QueueTasksKey(e.Action.ID))
            }
        } else {
            // ONNX, internal, notification — always include
            keys = append(keys, queue.QueueTasksKey(e.Action.ID))
        }
    }
    return keys
}
```

### 3.2 runWorker loop

```go
func (m *Manager) runWorker(ctx context.Context, id int) {
    for {
        if ctx.Err() != nil { return }

        keys := m.buildReadyKeys()
        if len(keys) == 0 {
            // No active actions, or all Ollama backends busy and no ONNX actions.
            // Wait for a backend to free up then re-evaluate.
            m.cache.Pool().WaitForAvailability(ctx, 1*time.Second, nil)
            continue
        }

        task, err := m.consumer.PopTask(ctx, 1*time.Second, keys...)
        if err != nil { /* log, continue */ }
        if task == nil { continue } // timeout — re-evaluate keys

        m.processTask(ctx, id, task)
    }
}
```

Key points:
- **Short BRPOP timeout (1 s)** — ensures keys are re-evaluated after each timeout cycle, picking up newly activated actions and reflecting backend availability changes.
- **No re-queue logic** — tasks are never pushed back. A worker pops only when the pipeline is available.
- **Residual race** — between `IsAvailable()` returning true and `acquire()` inside `Run()`, another worker may take the last backend. This window is microseconds wide vs. seconds-long Ollama requests; the race is negligible. In the worst case the worker blocks briefly on `acquire()` (not indefinitely — it resolves when the current request completes).

---

## 4. API — Queue Endpoint

### 4.1 New endpoint

`GET /api/queue` — returns per-action queue snapshot. Called only by the Queue page (polled every 3 s).

**Response:**
```json
{
  "queues": [
    {
      "action_id": 7,
      "action_name": "Categorize",
      "pipeline_name": "llm-categorize",
      "pipeline_type": "ollama",
      "pending": 3,
      "worker": { "id": 2, "backend": "ollama:11434" }
    },
    {
      "action_id": 12,
      "action_name": "NER Extract",
      "pipeline_name": "ner",
      "pipeline_type": "onnx",
      "pending": 0,
      "worker": null
    }
  ]
}
```

**Handler logic:**
1. `cache.All()` → active actions
2. For each action: `queue.Len(ctx, QueueTasksKey(action.ID))` → pending count
3. `manager.Stats().ActiveWorkers` → match on `action_name` to populate `worker` field (worker ID + backend)
4. Sort by pending desc, then action name asc

### 4.2 Health endpoint update

`GET /api/health` `queue.pending` field switches from `Len(ctx, QueueTasks)` to `queue.TotalTaskLen(ctx)`.

---

## 5. UI — Queue Page

### 5.1 Route

| Old | New |
|-----|-----|
| `/failures` | `/queue` |
| `FailuresView.vue` | `QueueView.vue` |

Add `{ path: '/failures', redirect: '/queue' }` in the router. Nav item key updated from `/failures` to `/queue`.

### 5.2 API client

New file `web/src/api/queue.ts`:
- `getQueue()` → `GET /api/queue`
- `flushQueue()` moved here from `failures.ts` (re-exported from `failures.ts` for backward compat)

### 5.3 Page layout

1. **Header** — "Queue" title + Refresh / Flush Pending / Clear Dead-Letter buttons
2. **Stats row** (3 cards) — Total Pending (with drain ETA), Dead-Letter count, Workers active/total bar — same as today
3. **Queues table** (new, replaces "Active Workers" card):

   | Column | Detail |
   |--------|--------|
   | Action | action name |
   | Pipeline | name + small type badge (onnx / ollama / internal / notification) |
   | Pending | count; amber when > 0; `—` when 0 |
   | Worker | `#2 · ollama:11434` when active; `#2 · waiting…` when backend is being acquired; `—` when idle |

   Rows with pending > 0 or an active worker sort to the top.

4. **Dead-letter table** — unchanged

The existing "Active Workers" card is removed; worker context is now inline in the Queues table.

---

## 6. Migration

At deploy time, delete the old `iran-watcher:tasks` key **before** starting the new binary:

```bash
redis-cli DEL iran-watcher:tasks iran-watcher:raw
```

The new `FlushQueue` (via `DELETE /api/queue`) only scans `iran-watcher:tasks:*` per-action keys; it will not touch the old bare key. No data migration is required; at-most-once delivery means in-flight task loss is acceptable.

---

## 7. Files Changed

| File | Change |
|------|--------|
| `internal/queue/queue.go` | Remove `QueueTasks`/`QueueRaw`; add `QueueTasksPrefix`, `QueueTasksKey`; update `Consumer` interface |
| `internal/queue/redis.go` | `PushTask` routes by action ID; `PopTask` takes variadic keys; `FlushQueue` uses SCAN; add `TotalTaskLen`; add `Scan` to `redisClient` interface |
| `internal/inference/pipeline.go` | Add `AvailabilityChecker` interface |
| `internal/inference/backend_pool.go` | Add `IsAnyAvailable`, `WaitForAvailability`, private `isAnyAvailable` |
| `internal/inference/ollama.go` | Add `IsAvailable()` on `OllamaPipeline` |
| `internal/worker/action_cache.go` | Add `Pool()` accessor |
| `internal/worker/manager.go` | Replace `runWorker` loop; add `buildReadyKeys` |
| `internal/worker/worker.go` | Update `PopTask` call signature (legacy pool, no-op path) |
| `internal/api/` | Add `GET /api/queue` handler; update health handler to use `TotalTaskLen` |
| `web/src/router/index.ts` | Rename route; add `/failures` redirect |
| `web/src/views/FailuresView.vue` | Rename to `QueueView.vue`; add Queues table; remove Active Workers card |
| `web/src/api/failures.ts` | Move `flushQueue` to `queue.ts`; keep re-export |
| `web/src/api/queue.ts` | New file: `getQueue`, `flushQueue` |
