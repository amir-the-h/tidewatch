# Pipeline Finished Trigger & Exists Condition Design

**Date:** 2026-04-10  
**Status:** Approved

## Problem

Pipeline 22 ("Iran Conflict Analytics") fans out from Start into 14 parallel sub-pipelines (Assassination Detection, Kinetic Detection, Coordinate Extractor, etc.). These run concurrently and each consumes Ollama backends independently — that part is already working. The missing piece is a way to trigger a downstream Notification pipeline only after **all** 14 sub-pipelines have finished for a given tweet, without embedding the notification inside the analytics pipeline itself.

Additionally, the condition node UI does not expose the `exists` / `not_exists` operators that are already implemented in the backend.

## Goals

1. Add a `pipeline_finished` trigger type that fires when every branch of a pipeline completes for a tweet, regardless of individual branch success or failure.
2. Allow a Notification pipeline (or any pipeline) to be linked to `pipeline_finished` scoped to a specific source pipeline.
3. Add `exists` and `not_exists` to the condition node operator dropdown in the UI.

## Architecture

### Reference Counting

When a pipeline is dispatched for a tweet, a counter tracks how many tasks are currently "in flight" for that `(pipeline_id, tweet_id)` pair. The counter is incremented each time a task is pushed to the queue and decremented each time the worker finishes processing a task. When the counter reaches zero all work is done and `pipeline_finished` fires.

This is the only approach that correctly handles:
- Parallel sub-pipeline branches of any depth
- Condition nodes that synchronously evaluate and may push 0 or N downstream tasks (conditions are evaluated synchronously inside `PushContinuationTasks` and never enter the queue, so they are transparent to the counter)
- Dead-end branches (condition fails → pushed=0 → counter just decrements, no new increment)

**Counter semantics:** `active_tasks += K - 1` per task lifecycle, where K is the number of downstream tasks the task spawned. A terminal task (leaf, no outgoing edges) has K=0, so it simply decrements by 1.

**Ordering:** The decrement happens in the worker manager **after** `PushContinuationTasks` returns (which includes any tasks pushed by `signalSubPipelineLeafDone` → parent continuation). This prevents the counter from prematurely hitting zero in the window between "task done" and "downstream tasks pushed."

**Example — Pipeline 22 with 14 sub-pipelines:**

| Event | Counter |
|---|---|
| Initial dispatch: 14 sub-pipeline entry tasks pushed | **14** |
| Sub-pipeline A: entry runs, pushes internal task | 14−1+1 = **14** |
| Sub-pipeline A: internal task runs, pushes leaf | 14−1+1 = **14** |
| Sub-pipeline A: leaf runs, no downstream → `signalSubPipelineLeafDone` → parent node 277 has no outgoing edges → pushed=0 | 14−1 = **13** |
| Sub-pipelines B–N each complete (same pattern) | **12 … 1 … 0** |
| Counter hits 0 → fire `pipeline_finished` for pipeline 22 | **fired** |

Internal sub-pipeline tasks cancel out (each push is +1, each completion is −1), so each sub-pipeline contributes exactly −1 to the counter when it fully completes.

### Task Struct: RootPipelineID

A new field `RootPipelineID int64` is added to `queue.Task`. It is set at initial dispatch (`PushInitialTasksForGraph`) to the top-level pipeline's ID and propagated unchanged through every continuation and sub-pipeline task. Tasks dispatched outside of any pipeline (e.g. standalone action tasks) leave it as 0 and are not tracked.

### Tracking Table

```sql
CREATE TABLE pipeline_run_tracking (
    pipeline_id  INTEGER NOT NULL,
    tweet_id     TEXT    NOT NULL,
    active_tasks INTEGER NOT NULL DEFAULT 0,
    event_payload BLOB,           -- original TweetEvent JSON for re-dispatch
    created_at   DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
    completed_at DATETIME,
    PRIMARY KEY (pipeline_id, tweet_id)
);
```

`event_payload` stores the original `TweetEvent` so that when `pipeline_finished` fires the system can re-dispatch with the same tweet context. All accumulated action metadata is loaded at runtime by `MergeAllTweetExecutionMetadata`, so no metadata needs to be stored here.

### Trigger Scoping: source_pipeline_id

`trigger_pipeline_links` gets a new nullable column:

```sql
ALTER TABLE trigger_pipeline_links ADD COLUMN source_pipeline_id INTEGER REFERENCES pipelines(id) ON DELETE CASCADE;
```

When `pipeline_finished` fires for pipeline X, `ListActiveTriggerPipelineLinks("pipeline_finished")` is filtered to only rows where `source_pipeline_id = X`. This allows multiple `pipeline_finished` links to coexist (e.g., a different notification for pipeline 22 vs. pipeline 30).

The UI trigger-link form shows a "Source Pipeline" picker when the trigger type `pipeline_finished` is selected.

## Data Flow

```
Tweet arrives
    └─ PushInitialTasksForGraph(pipeline_id=22)
           ├─ seed pipeline_run_tracking: active_tasks=14, event_payload=<tweet>
           └─ push 14 sub-pipeline entry tasks (each with RootPipelineID=22)
                   │
                   ▼  (worker loop, 14 in parallel)
           For each task in sub-pipeline:
               producer.PushTask(downstream) → active_tasks++
               worker completes task → (after PushContinuationTasks) active_tasks--
               if active_tasks == 0:
                   mark pipeline_run_tracking.completed_at
                   PushInitialPipelineTasks("pipeline_finished", tweet_event, source_pipeline_id=22)
                       └─ Notification pipeline fires
```

## Component Changes

### `internal/model` / `queue`

- Add `RootPipelineID int64` to `queue.Task`.
- Add `"pipeline_finished"` trigger type constant alongside existing ones.

### `internal/worker/pipeline.go`

- `PushInitialTasksForGraph`: after counting pushed tasks N, call `st.InitPipelineRunTracking(ctx, pipelineID, tweetID, N, eventPayload)`. Set `RootPipelineID` on all pushed tasks.
- `pushContinuationTask` and all other `producer.PushTask` call sites: propagate `RootPipelineID` from parent task and call `st.IncrementPipelineRunTracking(ctx, rootPipelineID, tweetID, +1)` after a successful push.
- `PushContinuationTasks`: no structural change; the decrement is handled by the worker manager after this function returns.

### `internal/worker/manager.go`

After `PushContinuationTasks` returns in `processTask`:

```go
if task.RootPipelineID != 0 {
    done, err := m.store.DecrementPipelineRunTracking(ctx, task.RootPipelineID, tweetID)
    if err == nil && done {
        m.firePipelineFinished(ctx, task.RootPipelineID, tweetID)
    }
}
```

`firePipelineFinished` loads the stored `event_payload` from `pipeline_run_tracking` and calls `PushInitialPipelineTasks` with trigger type `"pipeline_finished"` and `sourcePipelineID = task.RootPipelineID`.

### `internal/store`

New methods on `Store` interface:

```go
InitPipelineRunTracking(ctx, pipelineID int64, tweetID string, initialCount int, eventPayload []byte) error
IncrementPipelineRunTracking(ctx, pipelineID int64, tweetID string, delta int) error
// Returns true when active_tasks reaches 0 (pipeline done).
DecrementPipelineRunTracking(ctx, pipelineID int64, tweetID string) (done bool, err error)
GetPipelineRunEventPayload(ctx, pipelineID int64, tweetID string) ([]byte, error)
```

`DecrementPipelineRunTracking` performs a single atomic `UPDATE ... SET active_tasks = active_tasks - 1 ... RETURNING active_tasks` and returns `done = (active_tasks == 0)`. It also sets `completed_at` when done.

### `internal/store` — `ListActiveTriggerPipelineLinks`

Adds an optional `sourcePipelineID int64` parameter. For `pipeline_finished` trigger type the query adds `AND (source_pipeline_id = ? OR source_pipeline_id IS NULL)`.

### `internal/api`

- `GET /api/triggers` response includes `pipeline_finished` in the list.
- `POST /api/triggers/links` accepts the new optional `source_pipeline_id` field in the request body.
- `GET /api/triggers/links` response includes `source_pipeline_id` per link.

### `web/src` (UI)

**Condition node operator dropdown** (`ConditionNodeForm` or equivalent):
- Add `exists` and `not_exists` to the operator list. When either is selected, the "value" input is hidden (not needed).

**Trigger link form** (`TriggersView` or the link creation modal):
- When trigger type `pipeline_finished` is selected, show a "Source Pipeline" searchable dropdown populated from `GET /api/pipelines`.
- `source_pipeline_id` is required when trigger type is `pipeline_finished`.

## Database Migration

One new migration file:

```sql
-- up
CREATE TABLE pipeline_run_tracking (
    pipeline_id   INTEGER NOT NULL,
    tweet_id      TEXT    NOT NULL,
    active_tasks  INTEGER NOT NULL DEFAULT 0,
    event_payload BLOB,
    created_at    DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
    completed_at  DATETIME,
    PRIMARY KEY (pipeline_id, tweet_id)
);

ALTER TABLE trigger_pipeline_links
    ADD COLUMN source_pipeline_id INTEGER REFERENCES pipelines(id) ON DELETE CASCADE;

-- down
DROP TABLE pipeline_run_tracking;
-- SQLite does not support DROP COLUMN for older versions; migration is one-directional.
```

## Edge Cases

- **Pipeline with 0 initial tasks** (empty pipeline, no action nodes reachable from Start): `InitPipelineRunTracking` is called with `initialCount=0`. `DecrementPipelineRunTracking` is never called. `pipeline_finished` is not fired. Acceptable — an empty pipeline produces no useful output.
- **Same tweet re-triggers the same pipeline** (e.g., manual re-run): `InitPipelineRunTracking` upserts, resetting `active_tasks` and `completed_at = NULL`. If the prior run is still in flight this causes a counter conflict. Mitigation: the backfill handler already has `new_only` logic to skip tweets already processed; `pipeline_finished` is not relevant for backfill runs (they use `start_node_id`).
- **Worker crash mid-run**: counter never reaches 0, `pipeline_finished` never fires for that tweet. Acceptable — the at-most-once delivery guarantee already applies to the queue; no special recovery is needed.
- **Sub-pipeline within sub-pipeline**: `RootPipelineID` propagates unchanged through all levels. All nested tasks count against the same root counter.

## Out of Scope

- Notification deduplication (the Notification pipeline's own idempotency is handled at the action level via `telegram_message_id`).
- `pipeline_finished` for sub-pipelines (only top-level pipeline IDs are tracked; `RootPipelineID` is always the outermost pipeline).
- Exposing `pipeline_run_tracking` rows via API.
