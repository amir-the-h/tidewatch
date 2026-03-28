# Queue Page — Worker Status Redesign

**Date:** 2026-03-28
**Status:** Approved

## Problem

The Queue page (`FailuresView.vue`) shows several stats that have no real backing data:

- **"In-Memory Buffered"** — reads `workerStats.queued`, a field that doesn't exist in the `/api/health` response. Always renders `—`.
- **Per-pipeline breakdown table** — reads `workerStats.pipelines`, `pipeline_active`, `pipeline_rate`, none of which are returned by the API. The table never populates.
- **Flush pipeline buffer buttons** — tied to the per-pipeline table, also dead.

The page also lacks per-worker visibility: you can see that 3 of 5 workers are active, but not what each active worker is doing.

## Goals

1. Remove all UI sections that show no real data.
2. Add a per-worker status list showing, for each busy worker, the action name it is currently executing.
3. Show idle worker count as a summary stat alongside the active worker list.

## Backend Changes

### `internal/worker/manager.go`

Add a `workerActions sync.Map` field to `Manager` (key: `int` worker ID, value: `string` action name).

In `processTask`:
- After resolving `entry` from `ActionCache`, store `m.workerActions.Store(workerID, entry.Action.Name)`.
- In the deferred cleanup, call `m.workerActions.Delete(workerID)`.

`WorkerInfo` struct (new, in the same file or `pool_stats.go`):
```go
type WorkerInfo struct {
    ID     int    `json:"id"`
    Action string `json:"action"`
}
```

Update `PoolStats`:
```go
type PoolStats struct {
    Total         int          `json:"total"`
    Active        int          `json:"active"`
    Idle          int          `json:"idle"`
    ActiveWorkers []WorkerInfo `json:"active_workers"`
}
```

Update `Stats()` to snapshot `workerActions` into `ActiveWorkers` before returning.

### `internal/api/handlers_health.go`

No changes needed — `Stats()` already populates the `workers` field in the health response. The new `active_workers` field will be included automatically once `PoolStats` is updated.

## Frontend Changes

### `web/src/views/FailuresView.vue`

**Remove:**
- "In-Memory Buffered" stats card
- Per-pipeline breakdown `<n-card>` block (entire `v-if="pipelineRows.length > 0"` section)
- `pipelineRows` computed, `formatEta`, `PipelineRow` interface, `flushingPipeline` ref, `onFlushPipeline` function
- `TrashBinOutline` icon import (used only by flush pipeline button)

**Update `WorkerStats` interface:**
```ts
interface WorkerStats {
  total: number
  active: number
  idle: number
  active_workers?: Array<{ id: number; action: string }>
}
```

**Stats cards** — reduce from 4 to 3:
1. Redis Pending (unchanged)
2. Dead-Letter (unchanged)
3. Workers — show `active / total`, keep progress bar, add idle count label (e.g. "3 idle")

**Add Active Workers section** (between stats cards and dead-letter table):
- `v-if="workerStats"` wrapper
- Title: "Workers"
- When `workerStats.active_workers` is non-empty: a compact table with columns `Worker` (ID) and `Action` (name), no pagination
- When all workers are idle: `<n-empty description="All N workers idle" />` (N from `workerStats.total`)

## Data Flow

```
processTask() start
  → workerActions.Store(id, action.Name)
  → pipeline runs
  → workerActions.Delete(id)   (deferred)

GET /api/health
  → Stats() snapshots workerActions
  → response.workers.active_workers = [{id, action}, ...]

Frontend polls /api/health every 3s
  → renders active workers table or idle state
```

## What Is Not Changed

- Dead-letter queue table and drawer remain untouched.
- Flush Pending and Clear Failures header buttons remain.
- Polling interval (3s) unchanged.
- No new API endpoints.
- `Flush()` / `FlushPipeline()` no-ops remain (unused by UI after this change).
