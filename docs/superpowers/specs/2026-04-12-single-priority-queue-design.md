# Single Priority Queue Design

**Date:** 2026-04-12  
**Status:** Approved

## Problem

The per-action sorted-set design (`iran-watcher:tasks:{action_id}`) is broken in practice.  
`BZPopMin` with multiple keys does **not** find the global minimum score across all keys — it pops the minimum from the **first non-empty key** (by argument order). Because `buildReadyKeys()` iterates a Go map (random order), all workers can pile up on whichever action key happens to appear first and ignore the rest.

## Solution

One global sorted set. All tasks go into `iran-watcher:tasks`, scored by `Depth`. Workers always pop from that single key — Redis returns the globally lowest-depth task with no ordering ambiguity.

---

## Queue Layer (`internal/queue/`)

### Redis key

| Before | After |
|--------|-------|
| `iran-watcher:tasks:{action_id}` (N sorted sets) | `iran-watcher:tasks` (one sorted set) |
| `iran-watcher:failed` (list) | unchanged |

Constant: `QueueTasksKey = "iran-watcher:tasks"` (replaces the per-action `QueueTasksKey(id)` function).

### `PushTask`

```
ZAdd("iran-watcher:tasks", score=task.Depth, member=taskJSON)
ZCard("iran-watcher:tasks") → pending count for broadcast
```

### `PopTask`

```
BZPopMin(ctx, timeout, "iran-watcher:tasks")
```

The variadic `keys ...string` parameter is removed from the signature — the implementation always pops from the global key. This is a breaking change to the `Consumer` interface; all callers are updated.

### `Len` / `TotalTaskLen`

Both become:
```
ZCard("iran-watcher:tasks")
```

The SCAN loop in `TotalTaskLen` is removed. Both methods are now aliases for the same single-key ZCard.

### `FlushQueue`

```
DEL "iran-watcher:tasks"
```

No SCAN needed.

### `queue_update` broadcast

Shape change:

```json
// Before
{ "action_id": 5, "pending": 42 }

// After
{ "pending": 42 }
```

`action_id` is removed — the global pending count is the only meaningful signal.

### `redisClient` interface

`LLen` was already removed. No further interface changes are needed.

---

## Worker Layer (`internal/worker/`)

### Remove `buildReadyKeys()`

Workers no longer pre-filter by availability. They always pop from the single global queue.

### Post-pop availability check

After popping a task, the worker runs this decision tree:

```
pop task from "iran-watcher:tasks"
  → look up action in ActionCache
      • not found (action deleted/deactivated): discard task, loop
  → check pipeline availability
      • ONNX / internal / notification: always runnable → execute
      • Ollama: try to acquire a backend
          - backend acquired → execute
          - no backend available → re-queue (ZAdd same score), loop
```

Re-queuing calls `producer.PushTask(ctx, task)` with the original `task.Depth`. Sorted sets deduplicate by member string, so re-queuing an identical task is idempotent — if it already exists in the set (raced with another push), the score is unchanged.

**Anti-spin**: If a worker re-queues a task because no Ollama backend is free, it sleeps 100 ms before looping back to `BZPopMin`. Without this, workers would spin at full speed against a queue containing only Ollama tasks while all backends are saturated.

### `WorkerInfo` — new fields

```go
type WorkerInfo struct {
    ID          int       `json:"id"`
    Status      string    `json:"status"`        // "active" | "idle"
    ActionID    int64     `json:"action_id"`
    Action      string    `json:"action"`
    Pipeline    string    `json:"pipeline"`
    TweetID     string    `json:"tweet_id"`
    Depth       int       `json:"depth"`
    StartedAt   time.Time `json:"started_at"`
    DurationMS  int64     `json:"duration_ms"`   // computed server-side
    BackendName string    `json:"backend_name"`  // display name, not URL; empty for non-Ollama
}
```

`Duration_ms` is computed as `time.Since(StartedAt).Milliseconds()` at the moment the stats snapshot is taken — the frontend does not need to track start times.

For idle workers: `Status="idle"`, all other fields are zero/empty.

### `PoolStats` — updated

```go
type PoolStats struct {
    Total   int          `json:"total"`
    Active  int          `json:"active"`
    Idle    int          `json:"idle"`
    Workers []WorkerInfo `json:"workers"` // ALL workers, not just active ones
}
```

`ActiveWorkers []WorkerInfo` and `ActionThroughput map[int64]float64` are removed.  
`Workers` always contains exactly `total` entries (active entries first, then idle).

### `worker_stats` WebSocket event

```json
{
  "type": "worker_stats",
  "payload": {
    "total": 25,
    "active": 3,
    "idle": 22,
    "workers": [
      {
        "id": 1,
        "status": "active",
        "action_id": 5,
        "action": "NER",
        "pipeline": "onnx",
        "tweet_id": "1234567890",
        "depth": 2,
        "started_at": "2026-04-12T10:00:00Z",
        "duration_ms": 3200,
        "backend_name": ""
      },
      {
        "id": 2,
        "status": "idle",
        "action_id": 0,
        "action": "",
        "pipeline": "",
        "tweet_id": "",
        "depth": 0,
        "started_at": "0001-01-01T00:00:00Z",
        "duration_ms": 0,
        "backend_name": ""
      }
    ]
  }
}
```

Broadcast on every task start and task end (same timing as before).

---

## API Layer (`internal/api/`)

### `GET /api/queue`

New response shape:

```json
{
  "pending": 150,
  "workers": [
    {
      "id": 1,
      "status": "active",
      "action_id": 5,
      "action": "NER",
      "pipeline": "onnx",
      "tweet_id": "1234567890",
      "depth": 2,
      "started_at": "2026-04-12T10:00:00Z",
      "duration_ms": 3200,
      "backend_name": ""
    },
    {
      "id": 2,
      "status": "idle",
      "action_id": 0,
      "action": "",
      "pipeline": "",
      "tweet_id": "",
      "depth": 0,
      "started_at": "0001-01-01T00:00:00Z",
      "duration_ms": 0,
      "backend_name": ""
    }
  ]
}
```

The handler calls `manager.Stats()` for the worker list and `queue.Len()` for the global pending count.

The old `queues: []QueueEntry` response is removed entirely.

### `GET /api/health`

The `workers.active_workers` field is updated to match the new `WorkerInfo` shape (adds `tweet_id`, `depth`, `started_at`, `duration_ms`, `backend_name`; removes per-field that changed).

No other health endpoint changes.

---

## UI Layer (`web/src/views/QueueView.vue`)

### Header cards (unchanged structure)

- **Pending** — global count from `queue_update` events / initial API response
- **Failed** — dead-letter count (unchanged)
- **Workers** — active / total count

### Main table — worker-centric

One row per worker, always showing all N workers. Active workers appear first, idle workers below.

| # | Status | Action | Type | Tweet | Depth | Duration | Backend |
|---|--------|--------|------|-------|-------|----------|---------|
| 1 | 🟢 active | NER | onnx | 1234567… | 2 | 3.2s | — |
| 2 | 🟢 active | summarizer | ollama | 5678901… | 1 | 12.4s | GPU-1 |
| 3 | ⚪ idle | — | — | — | — | — | — |

Column details:
- **Status** — colored dot + text: green `active`, grey `idle`
- **Action** — `action` field from `WorkerInfo`
- **Type** — `pipeline` field (onnx / ollama / internal / notification)
- **Tweet** — `tweet_id` truncated to 12 chars
- **Depth** — `depth` integer; empty for idle
- **Duration** — formatted from `duration_ms`: `0.0s` / `12.4s` / `2m 3s`; empty for idle
- **Backend** — `backend_name`; `—` for non-Ollama or when empty

The per-action queue breakdown table is **removed**.

### Failures section (unchanged)

Dead-letter table with pagination and Clear button — no changes.

### Action buttons (unchanged)

Refresh, Stop Workers & Flush Queue, Flush Pending, Clear Failures.

### Stores / composables

- `queue_update` handler: update global `pending` count only (remove `action_id` logic)
- `worker_stats` handler: replace `activeWorkers` ref with `workers` array (all workers)
- Remove `actionThroughput` store state
- Remove per-action queue state entirely

---

## Affected Files

| File | Change |
|------|--------|
| `internal/queue/queue.go` | `QueueTasksKey` becomes a constant; remove per-action key function |
| `internal/queue/redis.go` | `PushTask`/`PopTask`/`Len`/`TotalTaskLen`/`FlushQueue` → single global key; `queue_update` shape change; remove `keys` param from `PopTask` |
| `internal/queue/queue_test.go` | Update tests for new signatures and single-key behavior |
| `internal/worker/pool.go` | `WorkerInfo` gains new fields; `PoolStats.Workers` replaces `ActiveWorkers`; remove `ActionThroughput` |
| `internal/worker/manager.go` | Remove `buildReadyKeys()`; add post-pop availability check + re-queue; populate new `WorkerInfo` fields; update `worker_stats` broadcast |
| `internal/api/handlers_queue.go` | New `GET /api/queue` response shape |
| `internal/api/handlers_health.go` | Update `workers.active_workers` field shape |
| `web/src/views/QueueView.vue` | Replace action table with worker table; update event handlers |
| `web/src/stores/queue.ts` (if exists) | Update state shape for new events |
| `CLAUDE.md` | Update queue key description and worker stats event shape |

---

## Migration

On deploy: call `DELETE /api/queue` (flush) to drop any remaining per-action sorted-set keys from the old format. New tasks will be pushed to the single global key. No data migration needed — tweets are persisted in SQLite.
