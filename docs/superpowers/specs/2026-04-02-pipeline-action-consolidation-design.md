# Pipeline-Action Consolidation Design

**Date:** 2026-04-02

## Overview

Consolidate the three-layer abstraction (Pipeline → Action → Chain) into two clean concepts:

- **Action** — the atomic execution unit. Absorbs what is currently a `Pipeline` row (type, config, handler). The current thin `Action` wrapper disappears.
- **Pipeline** — the DAG composer. Replaces what is currently called a `Chain`. A pipeline sequences and branches actions. Pipelines can also appear as nodes inside other pipelines (sub-pipeline composition).

All three current nav items (Pipelines, Actions, Chains) collapse into a single **Pipelines** page with three tabs: Actions | Pipelines | Executions.

---

## Motivation

The current model has unnecessary indirection: an `Action` is just a named pointer to a `Pipeline`, adding a layer without adding value. Users must create a `Pipeline` and then an `Action` wrapping it before they can use it in a `Chain`. Renaming and merging eliminates this friction and makes the mental model straightforward: build actions, compose them into pipelines.

---

## Data Model

### `actions` table (replaces current `pipelines`)

```sql
CREATE TABLE actions (
    id         INTEGER PRIMARY KEY,
    name       TEXT    NOT NULL UNIQUE,
    type       TEXT    NOT NULL,   -- onnx | ollama | internal | notification
    config     TEXT    NOT NULL DEFAULT '{}',
    active     INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME,
    updated_at DATETIME
);
```

The current thin `actions` table (columns: `id, name, pipeline_id, active`) is dropped. Its only purpose was to name a pipeline for use in chains — that purpose is now served directly by the action row itself.

### `pipelines` table (replaces current `chains`)

```sql
CREATE TABLE pipelines (
    id          INTEGER PRIMARY KEY,
    name        TEXT    NOT NULL,
    description TEXT,
    active      INTEGER NOT NULL DEFAULT 1,
    created_at  DATETIME,
    updated_at  DATETIME
);
```

### `pipeline_nodes` (renames `chain_nodes`)

```sql
CREATE TABLE pipeline_nodes (
    id              INTEGER PRIMARY KEY,
    pipeline_id     INTEGER NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
    type            TEXT    NOT NULL,  -- start | action | condition | pipeline
    action_id       INTEGER REFERENCES actions(id),
    sub_pipeline_id INTEGER REFERENCES pipelines(id),  -- set when type = 'pipeline'
    conditions      TEXT,              -- JSON array of PipelineCondition
    label           TEXT,
    pos_x           REAL,
    pos_y           REAL,
    created_at      TEXT
);
```

Node types:
- `start` — entry point (mandatory, one per pipeline)
- `action` — runs an action; `action_id` set
- `condition` — guards downstream edges; `conditions` JSON set
- `pipeline` — runs a sub-pipeline and waits for all its branches to finish; `sub_pipeline_id` set

### `pipeline_edges` (renames `chain_edges`)

```sql
CREATE TABLE pipeline_edges (
    id              INTEGER PRIMARY KEY,
    pipeline_id     INTEGER NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
    source_node_id  INTEGER NOT NULL REFERENCES pipeline_nodes(id),
    source_handle   TEXT    NOT NULL,  -- pass | fail | default
    target_node_id  INTEGER NOT NULL REFERENCES pipeline_nodes(id),
    created_at      TEXT
);
```

### `trigger_pipeline_links` (renames `trigger_chain_links`)

```sql
CREATE TABLE trigger_pipeline_links (
    id           INTEGER PRIMARY KEY,
    trigger_type TEXT    NOT NULL,
    pipeline_id  INTEGER NOT NULL REFERENCES pipelines(id),
    active       INTEGER NOT NULL DEFAULT 1,
    created_at   TEXT
);
```

### `action_backends` (renames `pipeline_backends`)

```sql
CREATE TABLE action_backends (
    action_id  INTEGER NOT NULL REFERENCES actions(id) ON DELETE CASCADE,
    backend_id INTEGER NOT NULL REFERENCES ollama_backends(id) ON DELETE CASCADE,
    priority   INTEGER NOT NULL,
    PRIMARY KEY (action_id, backend_id)
);
```

### `pipeline_node_executions` (new — fan-in sync for sub-pipeline nodes)

Tracks which terminal nodes of a sub-pipeline have completed so the parent pipeline knows when to continue. A terminal node is any node with no outgoing edges in the sub-pipeline's graph (can be an action node or a pipeline node).

```sql
CREATE TABLE pipeline_node_executions (
    id             INTEGER PRIMARY KEY,
    parent_node_id INTEGER NOT NULL REFERENCES pipeline_nodes(id),
    tweet_id       TEXT    NOT NULL,
    leaf_node_id   INTEGER NOT NULL REFERENCES pipeline_nodes(id),
    completed      INTEGER NOT NULL DEFAULT 0,
    created_at     DATETIME
);
```

---

## Migration

Single SQL migration (Approach A — clean break):

1. Create new `actions` table with all columns from current `pipelines`
2. `INSERT INTO actions SELECT id, name, type, config, active, created_at, updated_at FROM pipelines`
3. Fix node FK: current `chain_nodes.action_id` referenced old thin `actions.id`. Remap via `old_action.pipeline_id` so nodes point to the new action rows (which carry the same IDs as old pipeline rows).
4. Drop old `actions` and `pipelines` tables
5. Rename: `chains`→`pipelines`, `chain_nodes`→`pipeline_nodes`, `chain_edges`→`pipeline_edges`, `trigger_chain_links`→`trigger_pipeline_links`, `pipeline_backends`→`action_backends`
6. Add `sub_pipeline_id` column to `pipeline_nodes`
7. Create `pipeline_node_executions` table

---

## Go Code Changes

### Models (`internal/model/`)

| Old | New |
|-----|-----|
| `pipeline.go` — `Pipeline`, `PipelineType` | → `action.go` — `Action`, `ActionType` |
| `action.go` — thin `Action` struct removed | `ActionExecution`, `ActionExecutionMetadata` stay |
| `chain.go` — `Chain`, `ChainNode`, `ChainEdge`, `ChainGraph`, `TriggerChainLink` | → `pipeline.go` — `Pipeline`, `PipelineNode`, `PipelineEdge`, `PipelineGraph`, `TriggerPipelineLink` |

`PipelineNode` gains a `SubPipelineID *int64` field and a `pipeline` node type constant.

### Task struct (`internal/queue/`)

```go
type Task struct {
    TriggerType          string
    ActionID             int64
    TweetID              string
    PipelineNodeID       int64            // renamed from ChainNodeID
    ParentPipelineNodeID *int64           // new: set when task is inside a sub-pipeline
    Context              map[string]string
}
```

### Store (`internal/store/`)

| Old file | New file |
|----------|----------|
| `pipelines.go` | → `actions.go` (same methods, renamed types) |
| `actions.go` | merged into `actions.go` |
| `chains.go` | → `pipelines.go` |

New store method: `MarkPipelineNodeLeafComplete(ctx, parentNodeID, tweetID, leafNodeID) (allDone bool, err error)` — atomically marks a leaf completed and returns whether all leaves for `(parentNodeID, tweetID)` are now done.

### Worker (`internal/worker/`)

| Old | New |
|-----|-----|
| `chain.go` | → `pipeline.go` |
| `PushInitialChainTasks` | → `PushInitialPipelineTasks` |
| `PushContinuationTasks` | updated — handles new `pipeline` node type |

**Pipeline node execution** (new in `PushContinuationTasks`):

When an outgoing edge leads to a `pipeline` node:
1. Load sub-pipeline graph; identify terminal nodes (nodes with no outgoing edges, regardless of type)
2. Insert one `pipeline_node_executions` row per terminal node for `(parent_node_id, tweet_id)`
3. Push initial tasks for sub-pipeline's first action nodes, each with `ParentPipelineNodeID` set to the pipeline node's ID

When `PushContinuationTasks` reaches a node with no outgoing edges and the task has `ParentPipelineNodeID` set:
1. Call `MarkPipelineNodeLeafComplete`
2. If all terminal nodes done: merge sub-pipeline metadata into context, call `PushContinuationTasks` from the parent pipeline node

### ActionCache (`internal/worker/action_cache.go`)

Reads from new `actions` table instead of `pipelines`. Type/field renames only — no logic changes. Backend assignments read from `action_backends` instead of `pipeline_backends`.

### API (`internal/api/`)

| Old route | New route |
|-----------|-----------|
| `GET|POST /api/pipelines` | → `/api/actions` |
| `PUT|DELETE /api/pipelines/{id}` | → `/api/actions/{id}` |
| `POST /api/pipelines/{id}/{activate,deactivate}` | → `/api/actions/{id}/{activate,deactivate}` |
| `GET /api/pipelines/handlers` | → `/api/actions/handlers` |
| `GET|PUT|DELETE /api/pipelines/{id}/backends` | → `/api/actions/{id}/backends` |
| `GET|POST /api/chains` | → `/api/pipelines` |
| `GET|PUT|DELETE /api/chains/{id}` | → `/api/pipelines/{id}` |
| `POST /api/chains/{id}/{activate,deactivate}` | → `/api/pipelines/{id}/{activate,deactivate}` |
| `GET|PUT /api/chains/{id}/graph` | → `/api/pipelines/{id}/graph` |
| `POST /api/chains/{id}/run` | → `/api/pipelines/{id}/run` |
| `GET|POST /api/trigger-chain-links` | → `/api/trigger-pipeline-links` |
| `DELETE|POST /api/trigger-chain-links/{id}` | → `/api/trigger-pipeline-links/{id}` |

**Cycle detection endpoint behavior:** `PUT /api/pipelines/{id}/graph` rejects with 400 if any `pipeline` node in the graph would create a transitive cycle (DFS through sub-pipeline references).

**Graph request** gains pipeline node support:
```go
type PipelineNodeRequest struct {
    ClientID        string
    Type            string    // start | action | condition | pipeline
    ActionID        *int64
    SubPipelineID   *int64    // new
    Conditions      []PipelineCondition
    Label           string
    PosX            float64
    PosY            float64
}
```

### Internal Pipeline Registry (`internal/pipeline/`)

Type renames only (`model.Pipeline` → `model.Action`). No logic changes.

---

## Frontend

### Page structure

Single page at `/pipelines` with three tabs. The current nav items **Pipelines**, **Actions**, and **Chains** collapse into a single **Pipelines** nav item.

#### Actions tab

Replaces current `PipelinesView.vue`. Content is functionally identical to the current pipelines view:
- Table of actions with type badge, active toggle, CRUD
- Form drawer: config editor, handler selector, model path, labels, system prompt, notification channel config, map output config, Ollama backend assignments modal

#### Pipelines tab

Replaces current `ChainsView.vue`:
- Table of pipelines with active status, node count, timestamps, run button
- Clicking a pipeline opens the DAG builder at `/pipelines/:id/build`

**DAG builder** (`PipelineBuilderView.vue`, replaces `ChainBuilderView.vue`):
- Adds a fourth node type: **Pipeline node** — distinct visual style, shows sub-pipeline name
- Pipeline node form: dropdown of existing pipelines, excluding the current pipeline and any that would form a cycle (validated at save time by the API)
- All other builder behavior unchanged

#### Executions tab

Replaces the executions tab in `ActionsView.vue`:
- Global execution history
- Filters: action (multi-select), pipeline (multi-select), status (success/error), date range
- Pagination
- Expanding a row shows `action_execution_metadata` key/value pairs inline

### API client (`web/src/api/`)

| Old file | New file |
|----------|----------|
| `pipelines.ts` | → `actions.ts` |
| `actions.ts` | merged into `actions.ts` |
| `chains.ts` | → `pipelines.ts` |

---

## Execution Flow: Sub-pipeline Node

```
PushContinuationTasks encounters a pipeline node
  ↓
Load sub-pipeline graph
Find terminal nodes (nodes with no outgoing edges)
INSERT pipeline_node_executions rows (one per terminal node, completed=0)
Push initial tasks for sub-pipeline start node's direct actions
  (each task carries ParentPipelineNodeID = pipeline_node.id)
  ↓
Workers execute sub-pipeline actions
  (context propagates through sub-pipeline as normal)
  ↓
Sub-pipeline terminal node reached (no outgoing edges)
  ↓
MarkPipelineNodeLeafComplete(parentNodeID, tweetID, terminalNodeID)
  → If not all leaves done: stop
  → If all leaves done:
      Merge all sub-pipeline metadata into context
      PushContinuationTasks from parent pipeline node
  ↓
Parent pipeline continues on its outgoing edges
```

---

## Invariants

- A pipeline cannot (transitively) reference itself — enforced at graph save time via DFS
- A `pipeline` node type requires `sub_pipeline_id` set and `action_id` null
- An `action` node type requires `action_id` set and `sub_pipeline_id` null
- `pipeline_node_executions` rows are scoped to `(parent_node_id, tweet_id)` — parallel pipelines on different tweets do not interfere

---

## Out of Scope

- Timeout/cancellation for stalled sub-pipeline executions (leaf never completes)
- Passing explicit outputs from a sub-pipeline node to condition nodes in the parent (sub-pipeline metadata is merged into shared context, same as any action)
- Nested depth limit (cycles are prevented; depth is unrestricted)
