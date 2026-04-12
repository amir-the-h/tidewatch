# Per-Action Queue Revert + Ollama Concurrency Cap

**Date:** 2026-04-12
**Status:** Approved

## Background

Two queue redesigns landed on 2026-04-12:
1. Per-action Redis lists → single global sorted set (`iran-watcher:tasks`) scored by `Depth`
2. Single global sorted set → two global sorted sets (adding `iran-watcher:tasks:ollama`)

Both changes introduced a peek-then-pop polling loop in the worker: on every iteration workers call `PeekMinDepth` (a `ZRangeByScore` Redis command) on one or both sorted sets before attempting `TryPopTask`/`TryPopOllamaTask`. This causes constant Redis traffic and log noise even when queues are idle, and degrades web UI responsiveness when pipelines are running.

This spec reverts to per-action Redis lists with `BRPOP` and adds an Ollama concurrency cap so workers never pop more Ollama tasks than there are backend slots.

Discord thread name template (`thread_name_template`) is unrelated to queue routing and is **not reverted**.

## Goals

- Eliminate polling: workers block cheaply in a single `BRPOP` call, zero Redis traffic when idle
- Cap Ollama in-flight tasks to actual backend capacity so the worker dashboard reflects reality
- Preserve the Ollama / non-Ollama separation concept at the key-selection level
- Keep `GET /api/queue` returning both `pending_tasks` and `pending_ollama` counts for the UI

## Non-Goals

- Cross-action depth-based priority ordering (dropped with sorted sets)
- Redis pub/sub for capacity notification (`sync.Cond` inside `BackendPool` is already event-driven)
- Any change to notifier, Discord, auth, search, or entity pipeline code

## Architecture

### Queue Layer (`internal/queue/`)

Replace the global sorted-set keys and sorted-set operations with per-action Redis lists, exactly as before the 2026-04-12 changes.

**`queue.go`**
- `QueueTasksKey(actionID int64) string` → `"iran-watcher:tasks:" + actionID` (function, not a constant)
- Remove constants `QueueTasksKey`, `QueueOllamaTasksKey`
- Remove fields `Depth int` and `IsOllama bool` from `Task`
- `Consumer` interface reverts to:
  ```go
  PopTask(ctx context.Context, timeout time.Duration, keys ...string) (*Task, error)
  PopFailed(ctx context.Context, timeout time.Duration) (*FailedTweet, error)
  ```
  Remove `TryPopTask`, `TryPopOllamaTask`, `PeekMinDepth`

**`redis.go`**
- `redisClient` interface: remove `ZAdd`, `BZPopMin`, `ZCard`, `ZRangeByScore`; restore `LLen`
- `PushTask`: `LPush(QueueTasksKey(task.ActionID), json)` — O(1), no score needed
- `PopTask(ctx, timeout, keys...)`: `BRPop(ctx, timeout, keys...)` — blocks until a task arrives on any key or timeout elapses; returns the first available task; zero CPU while waiting
- `Len`: `LLen(key)` — restore list-based implementation
- `TotalTaskLen`: SCAN over `iran-watcher:tasks:*` keys, sum `LLen` for each
- `FlushQueue`: SCAN over `iran-watcher:tasks:*` keys, `DEL` all matched keys

**Broadcast on push/pop**
`PushTask` broadcasts `{action_id, pending: LLen(key)}` after the push.
`PopTask` broadcasts `{action_id, pending: LLen(key)}` after the pop.
These are O(1) per operation; no global SCAN on every push/pop.

### Worker Layer (`internal/worker/`)

Restore `buildReadyKeys()` with an additional Ollama concurrency gate.

**New field on `Manager`:**
```go
ollamaActive atomic.Int32
```
Tracks how many Ollama tasks are currently executing across all workers.

**`buildReadyKeys() []string`**
```
for each entry in ActionCache:
  pipeline has no AvailabilityChecker (ONNX, internal, notification):
    → always append QueueTasksKey(entry.Action.ID)
  pipeline has AvailabilityChecker (Ollama):
    if entry.Pipeline.IsAvailable()
       AND ollamaActive.Load() < pool.TotalCapacity():
      → append QueueTasksKey(entry.Action.ID)
```

The `IsAvailable()` check uses the existing per-pipeline backend availability logic. The `ollamaActive < TotalCapacity()` gate ensures workers stop picking up new Ollama tasks once all backend slots are claimed, preventing tasks from piling up inside `AcquireForPipeline`.

**`runWorker(ctx, id)` loop:**
```
keys = buildReadyKeys()
if keys is empty:
  if pool != nil:
    pool.WaitForAvailability(ctx, 1s, nil)  // sync.Cond, zero Redis traffic
  else:
    select { <-ctx.Done | <-time.After(100ms) }
  continue

task, err = consumer.PopTask(ctx, 1s, keys...)
if task == nil: continue

entry = cache.Get(task.ActionID)
if entry == nil: discard, continue   // action deactivated between pop and process

isOllama := implements AvailabilityChecker
if isOllama: ollamaActive.Add(1)
processTask(taskCtx, id, task)
if isOllama: ollamaActive.Add(-1)
// No explicit cond.Broadcast() needed here — BackendPool.Release() already
// calls cond.Broadcast() when the pipeline returns its backend slot, waking
// any workers blocked in WaitForAvailability before ollamaActive is decremented.
// Within 1s the BRPop timeout closes the remaining window.
```

The 1-second `BRPop` timeout is the natural re-evaluation window: when an Ollama slot frees, workers blocked on non-Ollama keys pick up Ollama keys on their next loop iteration within 1 second.

**Remove from Manager:**
- Peek-then-pop logic (`PeekMinDepth`, `TryPopTask`, `TryPopOllamaTask` call sites) — replaced by `buildReadyKeys()` + `BRPop`
- `IsOllama` routing at push call sites (`digest`, `summaries`, `reprocess` etc.) — remove the `IsOllama` and `Depth` field assignments
- `completionTrackers`, `rateTracker`, `throughputPerAction` — removed by the sorted-set PR; do **not** restore, current state without them is cleaner

**`Stats() PoolStats`** — reverts `Workers []WorkerInfo` shape (already done in sorted-set PR); no change needed here.

**`Flush() / FlushPipeline()`** — unchanged.

### BackendPool (`internal/inference/`)

Add one method:

```go
// TotalCapacity returns the sum of max_concurrent for all healthy, enabled backends.
// Returns 0 if the pool is empty or all backends are disabled/unhealthy.
func (p *BackendPool) TotalCapacity() int32
```

Holds `p.mu.Lock()` briefly, iterates backends, sums `maxConcurrent` where `enabled && healthy`. Called by `buildReadyKeys()` on every worker loop iteration — cheap.

No other changes to BackendPool. `WaitForAvailability` and `cond.Broadcast()` on `Release` remain the mechanism for waking workers that are in `WaitForAvailability`.

### API Layer (`internal/api/`)

**`GET /api/queue`** response:
```json
{
  "pending_tasks": 12,
  "pending_ollama": 3,
  "workers": [...]
}
```

- `pending_tasks`: `queue.TotalTaskLen(ctx)` — SCAN over all `iran-watcher:tasks:*` keys, sum `LLen`
- `pending_ollama`: iterate ActionCache for Ollama actions, sum `queue.Len(ctx, QueueTasksKey(id))` for each
- `workers`: unchanged from current implementation

This is a read-only endpoint called when the Queue page loads; the SCAN cost is acceptable.

**`DELETE /api/queue`** — calls `FlushQueue` which reverts to SCAN + DEL; no change to API contract.

**`GET /api/workers/buffer/flush`** — unchanged.

### UI (`web/src/`)

`stores/queue.ts` shape stays as-is (`pending_tasks`, `pending_ollama`, `workers`). The API response shape is preserved so no frontend store changes are needed.

`QueueView.vue` — no changes needed; worker table stays.

If the WS `queue_update` event previously carried `pending_tasks`/`pending_ollama` globally, revert it to per-action `{action_id, pending}` payloads. The queue store can ignore these events and rely on the polling `GET /api/queue` for global counts, or aggregate them — either is acceptable.

## Data Flow

```
Stream listener
  → PushTask(task) → LPush iran-watcher:tasks:{action_id}
  → Broadcast {action_id, pending: LLen}

Worker N (idle)
  → buildReadyKeys() → [key_A, key_B, key_C]  (Ollama keys gated by ollamaActive < TotalCapacity)
  → BRPop(1s, key_A, key_B, key_C)           ← blocks here, zero CPU, zero Redis traffic
  → task arrives on key_B
  → cache.Get(task.ActionID) → entry
  → if Ollama: ollamaActive++
  → processTask(...)
  → if Ollama: ollamaActive--; cond.Broadcast()
  → loop

Worker M (all Ollama, capacity full)
  → buildReadyKeys() → []  (only Ollama actions, all slots taken)
  → WaitForAvailability(ctx, 1s)              ← sync.Cond.Wait(), zero Redis traffic
  → backend releases → cond.Broadcast() wakes M
  → buildReadyKeys() → [ollama_key_X]         (slot now free)
  → BRPop(1s, ollama_key_X)
```

## What Is Not Changed

- `internal/notifier/` — Discord thread name template, DiscordBotClient, all providers
- `internal/pipeline/` — all handlers (discord_tag_thread, entity_matcher, etc.)
- `internal/store/` — no schema changes
- `migrations/` — no new migrations
- `internal/auth/`, `internal/hub/`, `internal/stream/` — unchanged
- Dead-letter queue (`iran-watcher:failed`) — unchanged, still a Redis list with BRPop

## Files Changed

| File | Change |
|------|--------|
| `internal/queue/queue.go` | Revert to per-action key function; remove Depth, IsOllama, TryPop*, PeekMinDepth |
| `internal/queue/redis.go` | Revert to LPush/BRPop/LLen; remove sorted-set ops |
| `internal/queue/queue_test.go` | Revert tests |
| `internal/worker/manager.go` | Restore buildReadyKeys(); add ollamaActive; remove peek-then-pop |
| `internal/worker/manager_test.go` | Update tests |
| `internal/inference/backend_pool.go` | Add TotalCapacity() |
| `internal/inference/backend_pool_test.go` | Add TotalCapacity test |
| `internal/api/queue.go` | Revert pending_ollama computation |
| `internal/worker/producer.go` (or push sites) | Remove IsOllama/Depth field assignments |
| `CLAUDE.md` | Update Key Design Decisions |

## Testing

- Unit: `TestRedisQueue_PushPop` verifies LPush/BRPop round-trip; `TestTotalTaskLen` verifies SCAN sum
- Unit: `TestBuildReadyKeys_OllamaCap` verifies Ollama keys excluded when `ollamaActive >= TotalCapacity`
- Unit: `TestBackendPool_TotalCapacity` verifies sum reflects enabled healthy backends only
- Build: `docker compose build iran-watcher` must succeed before commit
