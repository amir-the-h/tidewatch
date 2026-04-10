# Pipeline: Run from Node

**Date:** 2026-04-10
**Status:** Approved

## Problem

`POST /api/pipelines/{id}/run` always starts execution from the pipeline's Start node. There is no way to target a specific downstream node for backfill — for example, re-running only the notification step of a pipeline on all existing tweets without re-running every upstream detection action.

## Goal

Allow a caller to specify an optional `start_node_id` in the run request body. When provided, execution dispatches from that node instead of Start, carrying full context from all prior pipeline runs available at runtime.

## Context Preservation

The worker's `processTask` always calls `store.MergeAllTweetExecutionMetadata` before running any action. This loads every piece of stored `action_execution_metadata` for the tweet across all prior action executions — regardless of which node initiated the current run. Starting from a downstream node therefore carries the full context of all prior upstream runs automatically; no special context-threading is needed.

## Backend

### API Change

`POST /api/pipelines/{id}/run` request body gains one optional field:

```json
{ "new_only": bool, "start_node_id": int }
```

**Validation:**
- If `start_node_id` is provided, the handler looks up the node in the pipeline graph.
- Only `action` and `pipeline`-type nodes are valid start points. `start`, `condition` nodes are rejected with HTTP 400.

**Dispatch:**
- Without `start_node_id`: existing behaviour unchanged — `PushInitialTasksForGraph` walks from the Start node.
- With `start_node_id`: a new `PushTasksFromNode(graph, node, ...)` function is called instead. It calls `dispatchInitialNode` directly for the specified node, using the same dispatch logic that already handles action, pipeline, and condition node types.

**`new_only` scoping with `start_node_id`:**
When `new_only: true` and a `start_node_id` is provided, the exclusion set is built from only the action IDs reachable from (and including) the specified node — not the entire pipeline graph. A tweet is excluded if the action at the selected start node has already executed on it. This ensures tweets processed by upstream detection steps but not yet by the selected node are included.

### New function: `PushTasksFromNode`

Location: `internal/worker/pipeline.go`

```
func PushTasksFromNode(
    ctx context.Context,
    st store.Store,
    producer queue.Producer,
    cache *ActionCache,
    graph *model.PipelineGraph,
    pipelineID int64,
    triggerType string,
    eventPayload []byte,
    tweetID string,
    startNode *model.PipelineNode,
    logger *slog.Logger,
) int
```

Identical to `PushInitialTasksForGraph` except it receives a specific `*model.PipelineNode` rather than finding the Start node. Calls `dispatchInitialNode` for the provided node.

### MCP Tool

The `pipelines_run` tool description in `internal/mcpserver/tools.go` is updated to document the `start_node_id` parameter.

## Frontend

### Option A — Node picker in Run dialog (`PipelinesView.vue`)

The existing run modal gains:

- A lazy-loaded `<n-select>` dropdown labelled **"Start from node"**.
- Populated by fetching `GET /api/pipelines/{id}/graph` when the dialog opens (parallel to existing state resets).
- Only `action` and `pipeline`-type nodes are listed, sorted by `pos_y` then `pos_x`.
- Default option: `"Start of pipeline (default)"` — no `start_node_id` sent when selected.
- When a non-default node is selected a dimmed note renders below: *"Prior detection results for each tweet will be available as context."*
- The `new_only` radio description updates when a node is selected to: *"Only tweets not yet processed by the selected start node's action."*

New reactive refs in `PipelinesView.vue`:
- `runStartNodeId: ref<number | null>(null)` — cleared on dialog open
- `runGraphNodes: ref<GraphNode[]>([])` — populated on dialog open

`onConfirmRunPipeline` passes `start_node_id: runStartNodeId.value` when non-null.

### Option B — "Run from here" button in graph editor

**`ActionNode.vue` and `PipelineNode.vue`:**
- A **▶** icon button is added inside each node, visible on hover (CSS `opacity: 0` → `1` on parent `:hover`).
- Button is positioned top-right of the node container.
- On click, calls `props.data.onRunFromNode(props.id)` — a callback injected into each node's `data` object by the parent.
- No button is added to `StartNode.vue` or `ConditionNode.vue`.

**`PipelineBuilderView.vue`:**
- When constructing node data for action/pipeline nodes, inject `onRunFromNode: (nodeId: string) => void`.
- The handler sets `runFromNodeId` and `runFromNodeLabel` refs and opens a confirmation modal.
- The confirmation modal contains:
  - Title: *"Run from: [node label]"*
  - A `new_only` toggle (same semantics as the list-view dialog)
  - "Run" and "Cancel" buttons
- On confirm, calls `runPipeline(pipelineId, newOnly, Number(nodeId))`.

### API module (`web/src/api/pipelines.ts`)

`runPipeline` gains an optional third parameter:

```ts
export async function runPipeline(
  id: number,
  newOnly: boolean,
  startNodeId?: number,
): Promise<{ queued: number; async?: boolean }>
```

When `startNodeId` is provided it is included in the request body as `start_node_id`.

## Affected Files

| File | Change |
|------|--------|
| `internal/api/handlers_pipelines.go` | Add `start_node_id` to request struct; validate node type; call `PushTasksFromNode` when set; scope `new_only` exclusion set to reachable nodes |
| `internal/worker/pipeline.go` | Add `PushTasksFromNode` function |
| `internal/mcpserver/tools.go` | Update `pipelines_run` tool description |
| `web/src/api/pipelines.ts` | Add optional `startNodeId` param to `runPipeline` |
| `web/src/views/PipelinesView.vue` | Add node-picker dropdown and graph-fetch to run modal |
| `web/src/components/pipeline/ActionNode.vue` | Add hover "▶ Run from here" button |
| `web/src/components/pipeline/PipelineNode.vue` | Add hover "▶ Run from here" button |
| `web/src/views/PipelineBuilderView.vue` | Inject `onRunFromNode` callback into node data; add run-from-node confirmation modal |

## Out of Scope

- Condition nodes as start points (rejected at API level — conditions require upstream context that is not available without running prior nodes).
- Start node as an explicit `start_node_id` value (equivalent to omitting the field; rejected for simplicity).
- Persisting or visualising per-node run history in the graph editor.
