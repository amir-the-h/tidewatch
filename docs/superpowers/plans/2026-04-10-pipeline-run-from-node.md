# Pipeline: Run from Node — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `start_node_id` support to `POST /api/pipelines/{id}/run` so any action or pipeline node can be used as the entry point for a historical backfill, with matching UI in both the pipeline list run dialog and the graph editor.

**Architecture:** A new `PushTasksFromNode` function in `internal/worker/pipeline.go` replaces the start-node walk with a direct dispatch to a specified node. The handler gains a `start_node_id` field and a `collectNodeActionIDs` helper to scope the `new_only` exclusion set. The frontend exposes this via a node dropdown in the run modal (PipelinesView) and a per-node "▶ Run from here" hover button in the graph editor (PipelineBuilderView + node components).

**Tech Stack:** Go 1.25, net/http, Vue 3 (Composition API), Naive UI, VueFlow, Pinia.

---

## File Map

| File | Change |
|------|--------|
| `internal/worker/pipeline.go` | Add `PushTasksFromNode` |
| `internal/api/handlers_pipelines.go` | Add `start_node_id` to request; add `collectNodeActionIDs`; wire `PushTasksFromNode` |
| `internal/api/api_test.go` | Add 3 new test cases |
| `internal/mcpserver/tools.go` | Update `pipelines_run` description |
| `web/src/api/pipelines.ts` | Add `startNodeId` param to `runPipeline` |
| `web/src/stores/pipelines.ts` | Update `run` method signature |
| `web/src/views/PipelinesView.vue` | Add node-picker to run modal |
| `web/src/components/pipeline/ActionNode.vue` | Add hover "▶" button |
| `web/src/components/pipeline/PipelineNode.vue` | Add hover "▶" button |
| `web/src/views/PipelineBuilderView.vue` | Inject callback into node data; add run-from-node modal |

---

## Task 1: Add `PushTasksFromNode` to worker

**Files:**
- Modify: `internal/worker/pipeline.go` (after `PushInitialTasksForGraph`, ~line 139)
- Test: `internal/worker/pipeline_test.go`

- [ ] **Step 1: Write the failing test**

Open `internal/worker/pipeline_test.go` and add the following. Note that `mockProducer` is already defined in `worker_test.go` (same package). You need to add a `mockPipeline` type to `pipeline_test.go` since `dispatchInitialNode` requires the cache entry's Pipeline field:

```go
// mockPipeline satisfies inference.Pipeline for tests that only need dispatch
// (never actually run the pipeline).
type mockPipeline struct{ name string }

func (m *mockPipeline) Name() string                                                      { return m.name }
func (m *mockPipeline) Type() inference.PipelineType                                      { return inference.PipelineTypeInternal }
func (m *mockPipeline) Run(_ context.Context, _ inference.PipelineInput) (map[string]any, error) {
	return nil, nil
}
func (m *mockPipeline) Close() error { return nil }

func TestPushTasksFromNode_ActionNode(t *testing.T) {
	actionID := int64(7)
	graph := &model.PipelineGraph{
		Nodes: []*model.PipelineNode{
			{ID: 1, PipelineID: 10, Type: model.PipelineNodeTypeStart},
			{ID: 2, PipelineID: 10, Type: model.PipelineNodeTypeAction, ActionID: &actionID},
			{ID: 3, PipelineID: 10, Type: model.PipelineNodeTypeAction, ActionID: &actionID},
		},
		Edges: []*model.PipelineEdge{
			{ID: 1, PipelineID: 10, SourceNodeID: 1, TargetNodeID: 2},
			{ID: 2, PipelineID: 10, SourceNodeID: 2, TargetNodeID: 3},
		},
	}

	mp := &mockProducer{}
	cache := &ActionCache{entries: make(map[int64]*CacheEntry)}
	cache.entries[actionID] = &CacheEntry{
		Action:   &model.Action{ID: actionID, Name: "test", Active: true},
		Pipeline: &mockPipeline{name: "mock"},
	}

	startNode := graph.Nodes[2] // node ID 3 — second action, not the start
	pushed := PushTasksFromNode(
		context.Background(), nil, mp, cache,
		graph, 10, "new_tweet",
		[]byte(`{"tweet_id":"t1","text":"hello","author_handle":"u1"}`),
		"t1", startNode, nil,
	)
	if pushed != 1 {
		t.Fatalf("expected 1 pushed, got %d", pushed)
	}
	if mp.pushed[0].PipelineNodeID != 3 {
		t.Errorf("expected PipelineNodeID=3, got %d", mp.pushed[0].PipelineNodeID)
	}
	if mp.pushed[0].ActionID != 7 {
		t.Errorf("expected ActionID=7, got %d", mp.pushed[0].ActionID)
	}
}
```

Also add `"context"` and `"github.com/amir-the-h/iran-watcher/internal/inference"` to the imports in `pipeline_test.go` (add alongside the existing `"testing"` and `model` imports).

- [ ] **Step 2: Run test to confirm it fails**

```bash
go test ./internal/worker/... -run TestPushTasksFromNode_ActionNode -v
```

Expected: `FAIL — undefined: PushTasksFromNode`

- [ ] **Step 3: Implement `PushTasksFromNode`**

In `internal/worker/pipeline.go`, add directly after the closing brace of `PushInitialTasksForGraph` (~line 139):

```go
// PushTasksFromNode dispatches tasks starting from a specific node instead of
// the pipeline's Start node. Context from prior runs is always available at
// runtime via MergeAllTweetExecutionMetadata — the worker loads all stored
// action_execution_metadata for the tweet regardless of which node initiates the run.
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
) int {
	var initCtx map[string]any
	{
		var ev queue.TweetEvent
		if err := json.Unmarshal(eventPayload, &ev); err == nil {
			initCtx = map[string]any{
				"rule_ids":     ev.RuleIDs,
				"tweet_id":     ev.TweetID,
				"tweet":        ev.Text,
				"tweet_author": ev.AuthorHandle,
			}
		} else {
			initCtx = map[string]any{}
		}
	}
	return dispatchInitialNode(ctx, st, producer, cache, graph, pipelineID,
		triggerType, eventPayload, tweetID, startNode, initCtx, logger)
}
```

- [ ] **Step 4: Run test to confirm it passes**

```bash
go test ./internal/worker/... -run TestPushTasksFromNode_ActionNode -v
```

Expected: `PASS`

- [ ] **Step 5: Run full worker tests**

```bash
go test ./internal/worker/... -v
```

Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/worker/pipeline.go internal/worker/pipeline_test.go
git commit -m "feat(worker): add PushTasksFromNode for arbitrary start-node dispatch"
```

---

## Task 2: Update `handleRunPipeline` to support `start_node_id`

**Files:**
- Modify: `internal/api/handlers_pipelines.go`
- Test: `internal/api/api_test.go`

- [ ] **Step 1: Write failing tests**

Add these three tests to `internal/api/api_test.go`:

```go
func TestRunPipeline_InvalidStartNodeType(t *testing.T) {
	srv, ms, _, _ := newTestServer(t)
	ms.pipelines[1] = &model.Pipeline{ID: 1, Name: "p1", Active: true}
	ms.triggerLinks = []*model.TriggerPipelineLink{
		{ID: 1, PipelineID: 1, TriggerType: "new_tweet", Active: true},
	}
	ms.pipelineGraphs[1] = &model.PipelineGraph{
		Nodes: []*model.PipelineNode{
			{ID: 9, PipelineID: 1, Type: model.PipelineNodeTypeStart},
		},
		Edges: []*model.PipelineEdge{},
	}

	body := `{"start_node_id": 9}` // start-type node — should be rejected
	req := httptest.NewRequest(http.MethodPost, "/api/pipelines/1/run", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken(t))
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRunPipeline_StartNodeIdDispatchesFromNode(t *testing.T) {
	srv, ms, mp, _ := newTestServer(t)
	ms.pipelines[1] = &model.Pipeline{ID: 1, Name: "p1", Active: true}
	ms.triggerLinks = []*model.TriggerPipelineLink{
		{ID: 1, PipelineID: 1, TriggerType: "new_tweet", Active: true},
	}
	actionID1 := int64(5)
	actionID2 := int64(6)
	ms.pipelineGraphs[1] = &model.PipelineGraph{
		Nodes: []*model.PipelineNode{
			{ID: 9, PipelineID: 1, Type: model.PipelineNodeTypeStart},
			{ID: 10, PipelineID: 1, Type: model.PipelineNodeTypeAction, ActionID: &actionID1},
			{ID: 11, PipelineID: 1, Type: model.PipelineNodeTypeAction, ActionID: &actionID2},
		},
		Edges: []*model.PipelineEdge{
			{ID: 1, PipelineID: 1, SourceNodeID: 9, TargetNodeID: 10},
			{ID: 2, PipelineID: 1, SourceNodeID: 10, TargetNodeID: 11},
		},
	}
	ms.tweets["t1"] = &model.Tweet{ID: "t1", Text: "tweet 1", AuthorHandle: "h1"}

	body := `{"start_node_id": 11}` // start from node 11 (action 6), skipping node 10
	req := httptest.NewRequest(http.MethodPost, "/api/pipelines/1/run", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken(t))
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}

	// Handler dispatches in a background goroutine; wait for it.
	time.Sleep(100 * time.Millisecond)

	if len(mp.pushed) != 1 {
		t.Fatalf("expected 1 pushed task (only node 11), got %d", len(mp.pushed))
	}
	if mp.pushed[0].PipelineNodeID != 11 {
		t.Errorf("expected PipelineNodeID=11, got %d", mp.pushed[0].PipelineNodeID)
	}
	if mp.pushed[0].ActionID != 6 {
		t.Errorf("expected ActionID=6, got %d", mp.pushed[0].ActionID)
	}
}

func TestRunPipeline_StartNodeIdNewOnlyScoped(t *testing.T) {
	srv, ms, mp, _ := newTestServer(t)
	ms.pipelines[1] = &model.Pipeline{ID: 1, Name: "p1", Active: true}
	ms.triggerLinks = []*model.TriggerPipelineLink{
		{ID: 1, PipelineID: 1, TriggerType: "new_tweet", Active: true},
	}
	actionID1 := int64(5)
	actionID2 := int64(6)
	ms.pipelineGraphs[1] = &model.PipelineGraph{
		Nodes: []*model.PipelineNode{
			{ID: 9, PipelineID: 1, Type: model.PipelineNodeTypeStart},
			{ID: 10, PipelineID: 1, Type: model.PipelineNodeTypeAction, ActionID: &actionID1},
			{ID: 11, PipelineID: 1, Type: model.PipelineNodeTypeAction, ActionID: &actionID2},
		},
		Edges: []*model.PipelineEdge{
			{ID: 1, PipelineID: 1, SourceNodeID: 9, TargetNodeID: 10},
			{ID: 2, PipelineID: 1, SourceNodeID: 10, TargetNodeID: 11},
		},
	}
	ms.tweets["t1"] = &model.Tweet{ID: "t1", Text: "tweet 1", AuthorHandle: "h1"}
	ms.tweets["t2"] = &model.Tweet{ID: "t2", Text: "tweet 2", AuthorHandle: "h2"}

	// t1 already ran action 6 (node 11's action) — should be excluded.
	// t1 also ran action 5 (node 10) — irrelevant when start_node_id=11.
	ms.executions = []*model.ActionExecution{
		{ID: 1, ActionID: 6, TweetID: "t1", Status: "success"},
		{ID: 2, ActionID: 5, TweetID: "t2", Status: "success"}, // only ran upstream action, NOT action 6
	}

	body := `{"new_only": true, "start_node_id": 11}`
	req := httptest.NewRequest(http.MethodPost, "/api/pipelines/1/run", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken(t))
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}

	// Handler dispatches in a background goroutine; wait for it.
	time.Sleep(100 * time.Millisecond)

	// Only t2 should be queued: t1 already ran action 6, t2 only ran action 5.
	if len(mp.pushed) != 1 {
		t.Fatalf("expected 1 pushed task, got %d", len(mp.pushed))
	}
	tweetIDs := make(map[string]bool)
	for _, task := range mp.pushed {
		var ev struct{ TweetID string `json:"tweet_id"` }
		_ = json.Unmarshal(task.Event, &ev)
		tweetIDs[ev.TweetID] = true
	}
	if !tweetIDs["t2"] {
		t.Errorf("expected t2 to be queued, got tasks for: %v", tweetIDs)
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test ./internal/api/... -run "TestRunPipeline_InvalidStartNodeType|TestRunPipeline_StartNodeIdDispatchesFromNode|TestRunPipeline_StartNodeIdNewOnlyScoped" -v
```

Expected: FAIL (fields not recognised / wrong behaviour).

- [ ] **Step 3: Add `collectNodeActionIDs` helper**

In `internal/api/handlers_pipelines.go`, add this function after `collectGraphActionIDs`:

```go
// collectNodeActionIDs returns the action IDs directly reachable from the given
// node. For action nodes: the node's own action_id. For pipeline nodes: all
// action IDs found inside the referenced sub-pipeline graph.
// Used to scope the new_only exclusion set when start_node_id is provided.
func (s *Server) collectNodeActionIDs(ctx context.Context, node *model.PipelineNode) []int64 {
	seen := make(map[int64]struct{})
	var ids []int64
	add := func(id int64) {
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	switch node.Type {
	case model.PipelineNodeTypeAction:
		if node.ActionID != nil {
			add(*node.ActionID)
		}
	case model.PipelineNodeTypePipeline:
		if node.SubPipelineID != nil {
			subGraph, err := s.store.GetPipelineGraph(ctx, *node.SubPipelineID)
			if err == nil {
				for _, subNode := range subGraph.Nodes {
					if subNode.Type == model.PipelineNodeTypeAction && subNode.ActionID != nil {
						add(*subNode.ActionID)
					}
				}
			}
		}
	}
	return ids
}
```

- [ ] **Step 4: Update the run request struct and handler**

In `internal/api/handlers_pipelines.go`, find the request struct (around line 223):

```go
var req struct {
    NewOnly bool `json:"new_only"`
}
```

Replace with:

```go
var req struct {
    NewOnly     bool   `json:"new_only"`
    StartNodeID *int64 `json:"start_node_id"`
}
```

After the `if !pipeline.Active` block and before the `allLinks` query, there is already a graph load at the bottom of the handler. Keep that. Now add node validation after the graph is loaded (around line 296, after `graph, err := s.store.GetPipelineGraph`):

```go
// Validate start_node_id if provided.
var startNode *model.PipelineNode
if req.StartNodeID != nil {
    startNode = worker.FindNodeInGraph(graph, *req.StartNodeID)
    if startNode == nil {
        writeError(w, http.StatusBadRequest, "start_node_id not found in pipeline graph")
        return
    }
    if startNode.Type == model.PipelineNodeTypeStart || startNode.Type == model.PipelineNodeTypeCondition {
        writeError(w, http.StatusBadRequest, "start_node_id must be an action or pipeline node")
        return
    }
}
```

Then inside the background goroutine, replace the `new_only` exclusion set build:

**Find this block** (around line 309):
```go
var executedIDs map[string]struct{}
if newOnly {
    executedIDs = make(map[string]struct{})
    actionIDs := s.collectGraphActionIDs(ctx, graph)
    for _, actionID := range actionIDs {
```

**Replace with:**
```go
var executedIDs map[string]struct{}
if newOnly {
    executedIDs = make(map[string]struct{})
    var actionIDs []int64
    if startNode != nil {
        actionIDs = s.collectNodeActionIDs(ctx, startNode)
    } else {
        actionIDs = s.collectGraphActionIDs(ctx, graph)
    }
    for _, actionID := range actionIDs {
```

Then find the dispatch call inside the tweet loop (around line 365):

```go
count += worker.PushInitialTasksForGraph(
    ctx, s.store, s.producer, s.actionCache,
    graph, id, link.TriggerType, evPayload, tweet.ID, s.logger,
)
```

Replace with:

```go
if startNode != nil {
    count += worker.PushTasksFromNode(
        ctx, s.store, s.producer, s.actionCache,
        graph, id, link.TriggerType, evPayload, tweet.ID, startNode, s.logger,
    )
} else {
    count += worker.PushInitialTasksForGraph(
        ctx, s.store, s.producer, s.actionCache,
        graph, id, link.TriggerType, evPayload, tweet.ID, s.logger,
    )
}
```

- [ ] **Step 5: Export `FindNodeInGraph` from worker package**

The handler needs to look up a node by ID without importing the private `findNode` helper. Add this exported wrapper to `internal/worker/pipeline.go`:

```go
// FindNodeInGraph returns the node with the given ID, or nil if not found.
func FindNodeInGraph(g *model.PipelineGraph, id int64) *model.PipelineNode {
	return findNode(g, id)
}
```

- [ ] **Step 6: Run the three new tests**

```bash
go test ./internal/api/... -run "TestRunPipeline_InvalidStartNodeType|TestRunPipeline_StartNodeIdDispatchesFromNode|TestRunPipeline_StartNodeIdNewOnlyScoped" -v
```

Expected: all three PASS.

- [ ] **Step 7: Run full API tests**

```bash
go test ./internal/api/... -v
```

Expected: all pass.

- [ ] **Step 8: Commit**

```bash
git add internal/api/handlers_pipelines.go internal/api/api_test.go internal/worker/pipeline.go
git commit -m "feat(api): add start_node_id support to pipeline run endpoint"
```

---

## Task 3: Update MCP tool description

**Files:**
- Modify: `internal/mcpserver/tools.go`

- [ ] **Step 1: Update the `pipelines_run` tool description**

In `internal/mcpserver/tools.go`, find:

```go
{
    Name:        "pipelines_run",
    Description: "Dispatch historical tweets through the pipeline's start node. Returns queued count.",
    Method:      "POST",
    Path:        "/api/pipelines/{id}/run",
    PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Pipeline ID", Required: true}},
    Body: &BodyDef{
        Description: "Object with field: new_only (bool, skip tweets already processed by this pipeline).",
    },
},
```

Replace with:

```go
{
    Name:        "pipelines_run",
    Description: "Dispatch historical tweets through a pipeline. Returns queued count (async: true means running in background).",
    Method:      "POST",
    Path:        "/api/pipelines/{id}/run",
    PathParams:  []ParamDef{{Name: "id", Type: "integer", Description: "Pipeline ID", Required: true}},
    Body: &BodyDef{
        Description: "Object with optional fields: new_only (bool, skip tweets already processed by the start node's action), start_node_id (int, dispatch from this action or pipeline node instead of the pipeline's Start node — full context from prior runs is available automatically).",
    },
},
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/mcpserver/...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/mcpserver/tools.go
git commit -m "feat(mcp): document start_node_id in pipelines_run tool description"
```

---

## Task 4: Update frontend API and store

**Files:**
- Modify: `web/src/api/pipelines.ts`
- Modify: `web/src/stores/pipelines.ts`

- [ ] **Step 1: Update `runPipeline` in `web/src/api/pipelines.ts`**

Find:

```ts
export const runPipeline = (id: number, newOnly: boolean) =>
  apiFetch<{ queued: number; async?: boolean }>(`/api/pipelines/${id}/run`, {
    method: 'POST',
    body: JSON.stringify({ new_only: newOnly }),
  })
```

Replace with:

```ts
export const runPipeline = (id: number, newOnly: boolean, startNodeId?: number) =>
  apiFetch<{ queued: number; async?: boolean }>(`/api/pipelines/${id}/run`, {
    method: 'POST',
    body: JSON.stringify({
      new_only: newOnly,
      ...(startNodeId !== undefined ? { start_node_id: startNodeId } : {}),
    }),
  })
```

- [ ] **Step 2: Update `run` in `web/src/stores/pipelines.ts`**

Find:

```ts
async function run(id: number, newOnly: boolean): Promise<{ queued: number; async?: boolean }> {
  return runPipeline(id, newOnly)
}
```

Replace with:

```ts
async function run(id: number, newOnly: boolean, startNodeId?: number): Promise<{ queued: number; async?: boolean }> {
  return runPipeline(id, newOnly, startNodeId)
}
```

- [ ] **Step 3: Verify TypeScript compiles**

```bash
cd web && npm run type-check
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add web/src/api/pipelines.ts web/src/stores/pipelines.ts
git commit -m "feat(frontend): add startNodeId param to runPipeline API and store"
```

---

## Task 5: Option A — Node picker in run modal (PipelinesView)

**Files:**
- Modify: `web/src/views/PipelinesView.vue`

The run modal in `PipelinesView.vue` needs:
1. New refs: `runStartNodeId`, `runGraphNodes`, `runGraphLoading`
2. `openRunPipeline` fetches the graph
3. Run modal gets a `<n-select>` for start node
4. `onConfirmRunPipeline` passes `startNodeId`

- [ ] **Step 1: Add new imports and refs**

Find the import line for `runPipeline`:
```ts
  runPipeline, listModels,
```

Add `getPipelineGraph` to the import from `'../api/pipelines'`:
```ts
  runPipeline, getPipelineGraph, listModels,
```

Also ensure `type PipelineNode` is imported. Find the line:
```ts
import {
  getPipeline, getPipelineGraph, savePipelineGraph, listPipelines,
  type Pipeline, type PipelineCondition,
} from '../api/pipelines'
```

Wait — that import is in `PipelineBuilderView.vue`. In `PipelinesView.vue`, find the import from `'../api/pipelines'` (search for `runPipeline`). It will look like:
```ts
  runPipeline, listModels,
```
as part of a larger import block. Add `getPipelineGraph` and `type PipelineNode` to that import.

After the existing run modal refs:
```ts
const showRunModal      = ref(false)
const runPipelineRow    = ref<Pipeline | null>(null)
const runNewOnly        = ref(false)
const runningPipeline   = ref(false)
```

Add:
```ts
const runStartNodeId    = ref<number | null>(null)
const runGraphNodes     = ref<PipelineNode[]>([])
const runGraphLoading   = ref(false)
```

- [ ] **Step 2: Update `openRunPipeline` to fetch the graph**

Find:
```ts
function openRunPipeline(row: Pipeline) {
  runPipelineRow.value = row
  runNewOnly.value = false
  showRunModal.value = true
}
```

Replace with:
```ts
async function openRunPipeline(row: Pipeline) {
  runPipelineRow.value = row
  runNewOnly.value = false
  runStartNodeId.value = null
  runGraphNodes.value = []
  showRunModal.value = true
  runGraphLoading.value = true
  try {
    const graph = await getPipelineGraph(row.id)
    runGraphNodes.value = graph.nodes.filter(n => n.type === 'action' || n.type === 'pipeline')
      .sort((a, b) => a.pos_y !== b.pos_y ? a.pos_y - b.pos_y : a.pos_x - b.pos_x)
  } catch { /* non-fatal — picker just stays empty */ }
  finally { runGraphLoading.value = false }
}
```

- [ ] **Step 3: Update `onConfirmRunPipeline` to pass `startNodeId`**

Find:
```ts
    const res = await runPipeline(runPipelineRow.value.id, runNewOnly.value)
```

Replace with:
```ts
    const res = await runPipeline(
      runPipelineRow.value.id,
      runNewOnly.value,
      runStartNodeId.value ?? undefined,
    )
```

- [ ] **Step 4: Update the run modal template**

Find the run modal:
```html
  <!-- Run Pipeline Modal -->
  <n-modal
    v-model:show="showRunModal"
    preset="dialog"
    :title="`Run pipeline — ${runPipelineRow?.name}`"
    positive-text="Run"
    negative-text="Cancel"
    :loading="runningPipeline"
    @positive-click="onConfirmRunPipeline"
  >
    <n-radio-group v-model:value="runNewOnly" style="margin-top: 8px;">
      <n-space vertical :size="8">
        <n-radio :value="false">All eligible tweets</n-radio>
        <n-radio :value="true">Only tweets without existing executions for this pipeline's actions</n-radio>
      </n-space>
    </n-radio-group>
  </n-modal>
```

Replace with:
```html
  <!-- Run Pipeline Modal -->
  <n-modal
    v-model:show="showRunModal"
    preset="dialog"
    :title="`Run pipeline — ${runPipelineRow?.name}`"
    positive-text="Run"
    negative-text="Cancel"
    :loading="runningPipeline"
    @positive-click="onConfirmRunPipeline"
  >
    <n-space vertical :size="12" style="margin-top: 8px;">
      <n-form-item label="Start from node" :show-feedback="false">
        <n-select
          v-model:value="runStartNodeId"
          :loading="runGraphLoading"
          :options="[
            { label: 'Start of pipeline (default)', value: null },
            ...runGraphNodes.map(n => ({ label: n.label || `Node #${n.id}`, value: n.id }))
          ]"
          placeholder="Start of pipeline (default)"
          clearable
        />
      </n-form-item>
      <n-text v-if="runStartNodeId" depth="3" style="font-size: 12px;">
        Prior detection results for each tweet will be available as context.
      </n-text>
      <n-radio-group v-model:value="runNewOnly">
        <n-space vertical :size="8">
          <n-radio :value="false">All eligible tweets</n-radio>
          <n-radio :value="true">
            {{ runStartNodeId
              ? "Only tweets not yet processed by the selected node's action"
              : "Only tweets without existing executions for this pipeline's actions" }}
          </n-radio>
        </n-space>
      </n-radio-group>
    </n-space>
  </n-modal>
```

Also add `NFormItem` to the Naive UI imports if not already present. Find the naive-ui import block and add `NFormItem` if missing.

- [ ] **Step 5: Verify the page renders without console errors**

```bash
cd web && npm run build
```

Expected: build succeeds with no type errors.

- [ ] **Step 6: Commit**

```bash
git add web/src/views/PipelinesView.vue
git commit -m "feat(ui): add node-picker to pipeline run modal"
```

---

## Task 6: Option B — "Run from here" hover button in node components

**Files:**
- Modify: `web/src/components/pipeline/ActionNode.vue`
- Modify: `web/src/components/pipeline/PipelineNode.vue`

- [ ] **Step 1: Update `ActionNode.vue`**

Replace the entire file content with:

```vue
<script setup lang="ts">
import { Handle, Position, useVueFlow } from '@vue-flow/core'

const props = defineProps<{
  id: string
  data: {
    label: string
    pipelineType?: string
    selected?: boolean
    onRunFromNode?: (id: string) => void
  }
}>()

const { edges, removeEdges } = useVueFlow()

function disconnectSource() {
  const ids = edges.value.filter(e => e.source === props.id).map(e => e.id)
  if (ids.length) removeEdges(ids)
}

function disconnectTarget() {
  const ids = edges.value.filter(e => e.target === props.id).map(e => e.id)
  if (ids.length) removeEdges(ids)
}
</script>

<template>
  <div class="action-node" :style="{ borderColor: data.selected ? '#cba6f7' : '#585b70' }">
    <Handle type="target" :position="Position.Left" id="input" style="background: #585b70"
      @contextmenu.prevent="disconnectTarget" />
    <div style="font-weight: 600; margin-bottom: 2px">{{ data.label }}</div>
    <div v-if="data.pipelineType" style="font-size: 10px; color: #6c7086">{{ data.pipelineType }}</div>
    <button
      v-if="data.onRunFromNode"
      class="run-from-btn"
      title="Run from here"
      @click.stop="data.onRunFromNode(id)"
    >▶</button>
    <Handle type="source" :position="Position.Right" id="default" style="background: #a6e3a1"
      @contextmenu.prevent="disconnectSource" />
  </div>
</template>

<style scoped>
.action-node {
  position: relative;
  padding: 8px 12px;
  border-radius: 6px;
  background: #1e1e2e;
  border: 1px solid #585b70;
  min-width: 120px;
  font-size: 12px;
  color: #cdd6f4;
}
.run-from-btn {
  position: absolute;
  top: 4px;
  right: 4px;
  background: transparent;
  border: none;
  color: #6c7086;
  cursor: pointer;
  font-size: 10px;
  padding: 2px 4px;
  border-radius: 3px;
  opacity: 0;
  transition: opacity 0.15s, color 0.15s;
}
.action-node:hover .run-from-btn {
  opacity: 1;
}
.run-from-btn:hover {
  color: #a6e3a1;
  background: #313244;
}
</style>
```

- [ ] **Step 2: Update `PipelineNode.vue`**

Replace the entire file content with:

```vue
<script setup lang="ts">
import { Handle, Position } from '@vue-flow/core'
import { NTag, NText } from 'naive-ui'

const props = defineProps<{
  id: string
  data: {
    label: string
    subPipelineId?: number
    subPipelineName?: string
    onRunFromNode?: (id: string) => void
  }
  selected?: boolean
}>()
</script>

<template>
  <div :class="['pipeline-node', { selected }]">
    <Handle type="target" :position="Position.Top" />
    <div class="pipeline-node__header">
      <NTag type="info" size="small" :bordered="false">pipeline</NTag>
    </div>
    <NText class="pipeline-node__name">{{ data.subPipelineName || `Pipeline #${data.subPipelineId}` }}</NText>
    <div v-if="data.label" class="pipeline-node__label">{{ data.label }}</div>
    <button
      v-if="data.onRunFromNode"
      class="run-from-btn"
      title="Run from here"
      @click.stop="data.onRunFromNode(id)"
    >▶</button>
    <Handle type="source" :position="Position.Bottom" />
  </div>
</template>

<style scoped>
.pipeline-node {
  position: relative;
  background: #2d1b69;
  border: 2px solid #7c3aed;
  border-radius: 8px;
  padding: 8px 12px;
  min-width: 140px;
  cursor: pointer;
}
.pipeline-node.selected { border-color: #a78bfa; }
.pipeline-node__header { margin-bottom: 4px; }
.pipeline-node__name { font-size: 13px; font-weight: 600; display: block; }
.pipeline-node__label { font-size: 11px; color: #a0a0a0; margin-top: 2px; }
.run-from-btn {
  position: absolute;
  top: 4px;
  right: 4px;
  background: transparent;
  border: none;
  color: #a78bfa80;
  cursor: pointer;
  font-size: 10px;
  padding: 2px 4px;
  border-radius: 3px;
  opacity: 0;
  transition: opacity 0.15s, color 0.15s;
}
.pipeline-node:hover .run-from-btn {
  opacity: 1;
}
.run-from-btn:hover {
  color: #a78bfa;
  background: #3d2a7a;
}
</style>
```

- [ ] **Step 3: Build to confirm no type errors**

```bash
cd web && npm run build
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add web/src/components/pipeline/ActionNode.vue web/src/components/pipeline/PipelineNode.vue
git commit -m "feat(ui): add hover run-from-here button to action and pipeline nodes"
```

---

## Task 7: Option B — Run-from-node modal in PipelineBuilderView

**Files:**
- Modify: `web/src/views/PipelineBuilderView.vue`

- [ ] **Step 1: Add `runPipeline` to the imports**

Find the import from `'../api/pipelines'`:
```ts
import {
  getPipeline, getPipelineGraph, savePipelineGraph, listPipelines,
  type Pipeline, type PipelineCondition,
} from '../api/pipelines'
```

Add `runPipeline`:
```ts
import {
  getPipeline, getPipelineGraph, savePipelineGraph, listPipelines, runPipeline,
  type Pipeline, type PipelineCondition,
} from '../api/pipelines'
```

Also add `NModal, NSwitch` to the naive-ui imports if not already present. Find:
```ts
import {
  NButton, NText, NInput, NSelect, NDivider, NInputNumber,
  NSpin, NScrollbar, NIcon, useMessage,
} from 'naive-ui'
```

Add `NModal, NCheckbox, NSpace` if not present (check the existing import first and only add what's missing).

- [ ] **Step 2: Add run-from-node state and handler**

After the existing state declarations (e.g., after `const isDirty = ref(false)`), add:

```ts
// Run-from-node state (Option B)
const showRunFromNodeModal  = ref(false)
const runFromNodeId         = ref<string | null>(null)
const runFromNodeLabel      = ref('')
const runFromNodeNewOnly    = ref(false)
const runningFromNode       = ref(false)

function handleRunFromNode(nodeId: string) {
  const node = findNode(nodeId)
  runFromNodeId.value    = nodeId
  runFromNodeLabel.value = node?.data?.label ?? `Node #${nodeId}`
  runFromNodeNewOnly.value = false
  showRunFromNodeModal.value = true
}

async function onConfirmRunFromNode(): Promise<false | void> {
  if (!runFromNodeId.value) return false
  const numId = parseInt(runFromNodeId.value, 10)
  if (isNaN(numId)) { msg.error('Invalid node id'); return false }
  runningFromNode.value = true
  try {
    const res = await runPipeline(pipelineId, runFromNodeNewOnly.value, numId)
    msg.success(res.async
      ? `Running from "${runFromNodeLabel.value}" in the background`
      : `Queued ${res.queued} tweets from "${runFromNodeLabel.value}"`)
    showRunFromNodeModal.value = false
  } catch (e: any) { msg.error(e.message) }
  finally { runningFromNode.value = false }
  return false
}
```

- [ ] **Step 3: Inject `onRunFromNode` into node data**

Create a helper function right after the `handleRunFromNode` / `onConfirmRunFromNode` block:

```ts
function withRunCallback(data: Record<string, any>): Record<string, any> {
  return { ...data, onRunFromNode: handleRunFromNode }
}
```

Now find every place nodes of type `action` or `pipeline` are constructed in `PipelineBuilderView.vue`. There are four locations: `loadGraph`, `addActionNode`, `addPipelineNode`, and after `saveGraph`. In each one, wrap the `data` object with `withRunCallback(...)` **for action and pipeline type nodes only**.

**In `loadGraph`** — find:
```ts
    const vfNodes: Node[] = graph.nodes.map(n => ({
      id:       String(n.id),
      type:     n.type,
      position: { x: n.pos_x, y: n.pos_y },
      data: {
        label:           n.label || (n.type === 'start' ? 'Start' : ''),
        actionId:        n.action_id,
        subPipelineId:   n.sub_pipeline_id,
        subPipelineName: n.sub_pipeline_id
          ? pipes.find(pp => pp.id === n.sub_pipeline_id)?.name
```

Replace with:
```ts
    const vfNodes: Node[] = graph.nodes.map(n => {
      const baseData = {
        label:           n.label || (n.type === 'start' ? 'Start' : ''),
        actionId:        n.action_id,
        subPipelineId:   n.sub_pipeline_id,
        subPipelineName: n.sub_pipeline_id
          ? pipes.find(pp => pp.id === n.sub_pipeline_id)?.name
          : undefined,
        conditions:      n.conditions ?? [],
      }
      return {
        id:       String(n.id),
        type:     n.type,
        position: { x: n.pos_x, y: n.pos_y },
        data: (n.type === 'action' || n.type === 'pipeline') ? withRunCallback(baseData) : baseData,
      }
    })
```

(Note: verify the original `data` block's closing structure by reading the file; the above merges the conditions field that may already be included. Match the original field list exactly and only add `withRunCallback` wrapping.)

**In `addActionNode`** — find:
```ts
    data: { label: action.name, actionId: action.id, pipelineType: `#${action.id}` },
```
Replace with:
```ts
    data: withRunCallback({ label: action.name, actionId: action.id, pipelineType: `#${action.id}` }),
```

**In `addPipelineNode`** — find:
```ts
    data: { label: pipe.name, subPipelineId: pipe.id, subPipelineName: pipe.name },
```
Replace with:
```ts
    data: withRunCallback({ label: pipe.name, subPipelineId: pipe.id, subPipelineName: pipe.name }),
```

**After `saveGraph`** — find the `setNodes(saved.nodes.map(n => ({` block and apply the same `withRunCallback` pattern as in `loadGraph` for action/pipeline types.

**After `reloadGraph`** — same treatment for the `setNodes(graph.nodes.map(n => ({` block.

- [ ] **Step 4: Add the run-from-node modal to the template**

At the end of the `<template>` section (before the closing `</template>`), add:

```html
  <!-- Run from Node Modal (Option B) -->
  <n-modal
    v-model:show="showRunFromNodeModal"
    preset="dialog"
    :title="`Run from: ${runFromNodeLabel}`"
    positive-text="Run"
    negative-text="Cancel"
    :loading="runningFromNode"
    @positive-click="onConfirmRunFromNode"
  >
    <n-space vertical :size="12" style="margin-top: 8px;">
      <n-text depth="3" style="font-size: 12px;">
        Prior detection results for each tweet will be available as context.
      </n-text>
      <n-radio-group v-model:value="runFromNodeNewOnly">
        <n-space vertical :size="8">
          <n-radio :value="false">All eligible tweets</n-radio>
          <n-radio :value="true">Only tweets not yet processed by this node's action</n-radio>
        </n-space>
      </n-radio-group>
    </n-space>
  </n-modal>
```

Also ensure `NModal, NSpace, NText, NRadioGroup, NRadio` are imported from naive-ui in `PipelineBuilderView.vue`. Add any missing ones to the existing naive-ui import.

- [ ] **Step 5: Build to confirm no errors**

```bash
cd web && npm run build
```

Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add web/src/views/PipelineBuilderView.vue
git commit -m "feat(ui): inject run-from-node callback into graph nodes; add confirmation modal"
```

---

## Task 8: Build verification (Docker)

**Per CLAUDE.md, all Go + frontend changes must be verified with a local Docker build before the feature is considered complete.**

- [ ] **Step 1: Build the container**

```bash
docker compose build --build-arg BUILDKIT_INLINE_CACHE=1 iran-watcher
```

Expected: exits 0. If any compilation error appears, fix it before proceeding.

- [ ] **Step 2: Update CLAUDE.md**

In the `Key Design Decisions` section of `CLAUDE.md`, find the bullet point for:
> `POST /api/pipelines/{id}/run` (manual backfill: dispatches eligible historical tweets through the pipeline's start node; body `{ "new_only": bool }`;

Update it to:
> `POST /api/pipelines/{id}/run` (manual backfill: dispatches eligible historical tweets through the pipeline; body `{ "new_only": bool, "start_node_id": int }`; when `start_node_id` is provided, execution starts from that specific action or pipeline node instead of Start — full context from prior runs is available via `MergeAllTweetExecutionMetadata`; `new_only` exclusion set is scoped to the start node's own action(s); returns 202 `{ "queued": N, "async": true }`)

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: update CLAUDE.md for start_node_id pipeline run support"
```
