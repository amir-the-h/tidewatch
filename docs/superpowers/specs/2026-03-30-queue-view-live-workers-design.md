# Queue View: Live Workers & Pending Counts

**Date:** 2026-03-30
**Status:** Approved

## Problem

The Queue page has three gaps:

1. **Workers column shows only one worker per action.** `handleGetQueue` builds a `map[string]workerRef` — when multiple workers are processing the same action, the last write wins and earlier workers are silently dropped.
2. **Pending counts never decrease.** `RedisQueue.PushTask` broadcasts a `queue_update` event after `LPUSH`, but `PopTask` (BRPOP) broadcasts nothing. Consumed tasks are invisible to the UI until the page is reloaded.
3. **Workers column is static after page load.** `worker_stats` WS events carry only `{total, active, idle}` — `active_workers` is omitted, so the per-action worker assignments can't update live.

## Design

### 1. Go Backend

#### 1a. `internal/api/handlers_queue.go` — workers slice

Replace `Worker *workerRef` with `Workers []workerRef` on `queueEntry`.

Build `workerByAction map[string][]workerRef` (append, not overwrite) from `pool.Stats().ActiveWorkers`. Populate `qe.Workers` from the slice; omit the field (empty slice) when no workers are active on that action.

Update the sort predicate: `ri.Worker != nil` → `len(ri.Workers) > 0`.

#### 1b. `internal/queue/redis.go` — broadcast on pop

After a successful BRPOP in `PopTask`, run `LLEN` on the popped key and broadcast:

```json
{ "type": "queue_update", "payload": { "action_id": <id>, "pending": <llen> } }
```

`action_id` is parsed from the key name (`iran-watcher:tasks:{action_id}`).

`PopTask` receives the key that was popped from the BRPOP reply — extract the action ID from it directly.

#### 1c. `internal/worker/manager.go` — active_workers in WS broadcast

Extend `broadcastWorkerStats` to include `active_workers` in the payload:

```json
{
  "total": 8,
  "active": 3,
  "idle": 5,
  "active_workers": [
    { "id": 1, "action": "Detect Assassination", "pipeline": "ollama", "backend": "localhost:11434" },
    { "id": 4, "action": "Detect Assassination", "pipeline": "ollama", "backend": "localhost:11435" }
  ]
}
```

### 2. Pinia Store (`web/src/stores/queues.ts`)

- `QueueEntry.worker: { id, backend } | null` → `workers: Array<{ id: number; backend: string }>` (empty array = no active workers)
- `setQueues` maps the `workers` field from the API response (already a slice)
- `setQueueEntry` updates `pending` only — unchanged semantics
- `setWorkerStats` cross-updates each queue entry's `workers` by filtering `active_workers` on `wi.action === entry.action_name`; this is the sole updater of the `workers` field after page load

### 3. UI (`web/src/views/QueueView.vue`)

#### Worker column cell rendering

| Condition | Rendered output |
|---|---|
| `workers.length === 0` | `—` (grey) |
| `workers.length === 1` | `#2 · backend` (existing format) |
| `workers.length > 1` | `<n-tooltip>` trigger: `2 workers` badge; tooltip body: one line per worker (`#2 · backend1`, `#5 · backend2`) |

#### Table split: active vs. inactive

**Active table** (always visible): rows where `pending > 0 || workers.length > 0`.

**Inactive section** (collapsed by default): rows where `pending === 0 && workers.length === 0`, rendered inside an `<n-collapse>` with the toggle label `Show N inactive`. Same columns, slightly dimmed styling to signal idle state.

## Data Flow

```
BRPOP fires (worker consumes task)
  → LLEN on popped key
  → broadcast queue_update {action_id, pending}
  → frontend setQueueEntry: updates pending on matching row

worker starts/finishes task
  → broadcastWorkerStats {total, active, idle, active_workers}
  → frontend setWorkerStats: updates workers[] on each matching queue entry
  → Worker cell re-renders (badge or single or —)
  → active/inactive split recomputes
```

## Out of Scope

- Persisting `active_workers` across page reloads (current behavior: accurate after first WS event)
- Showing worker history or execution timeline
- Per-action worker concurrency limits
