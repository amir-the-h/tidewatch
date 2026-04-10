# Pipeline Run Order Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an optional `order` field (`"asc"` | `"desc"`) to `POST /api/pipelines/{id}/run` so callers can control whether tweets are dispatched oldest-first or newest-first.

**Architecture:** `TweetFilter` gains `OrderAsc bool`; `ListTweets` switches its `ORDER BY` clause accordingly. The handler reads the new `order` JSON field and sets the flag. Both frontend run dialogs get a radio pair. Default is `"desc"` (newest first) — existing behaviour unchanged.

**Tech Stack:** Go 1.25 (net/http, SQLite), Vue 3 Composition API, Naive UI, Pinia.

---

## File Map

| File | Change |
|------|--------|
| `internal/store/store.go` | Add `OrderAsc bool` to `TweetFilter` |
| `internal/store/sqlite.go` | Conditional `ORDER BY` in `ListTweets` |
| `internal/store/sqlite_test.go` | Add `TestListTweets_OrderAsc` |
| `internal/api/api_test.go` | Update `mockStore.ListTweets` to sort; add `TestRunPipeline_AscOrder` |
| `internal/api/handlers_pipelines.go` | Add `Order string` to request struct; set `filter.OrderAsc` |
| `internal/mcpserver/tools.go` | Document `order` in `pipelines_run` description |
| `web/src/api/pipelines.ts` | Add optional `order` param to `runPipeline` |
| `web/src/stores/pipelines.ts` | Add optional `order` param to `run` |
| `web/src/views/PipelinesView.vue` | Add `runOrder` ref and radio pair to run modal |
| `web/src/views/PipelineBuilderView.vue` | Add `runFromNodeOrder` ref and radio pair to run-from-node modal |

---

## Task 1: Store — `OrderAsc` field and conditional `ORDER BY`

**Files:**
- Modify: `internal/store/store.go:11-20`
- Modify: `internal/store/sqlite.go:168`
- Test: `internal/store/sqlite_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/store/sqlite_test.go` (after `TestListTweets_HasEntityTrack`):

```go
func TestListTweets_OrderAsc(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	old := &model.Tweet{
		ID: "t_old", AuthorID: "a1", AuthorHandle: "h1", Text: "old",
		RawJSON: []byte(`{}`), CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	new_ := &model.Tweet{
		ID: "t_new", AuthorID: "a2", AuthorHandle: "h2", Text: "new",
		RawJSON: []byte(`{}`), CreatedAt: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := s.SaveTweet(ctx, old); err != nil {
		t.Fatalf("SaveTweet old: %v", err)
	}
	if err := s.SaveTweet(ctx, new_); err != nil {
		t.Fatalf("SaveTweet new: %v", err)
	}

	// Default (no OrderAsc): newest first.
	desc, err := s.ListTweets(ctx, TweetFilter{Page: 1, PerPage: 100})
	if err != nil {
		t.Fatalf("list desc: %v", err)
	}
	if len(desc) != 2 || desc[0].ID != "t_new" || desc[1].ID != "t_old" {
		t.Errorf("expected newest-first [t_new,t_old], got [%s,%s]", desc[0].ID, desc[1].ID)
	}

	// OrderAsc: oldest first.
	asc, err := s.ListTweets(ctx, TweetFilter{Page: 1, PerPage: 100, OrderAsc: true})
	if err != nil {
		t.Fatalf("list asc: %v", err)
	}
	if len(asc) != 2 || asc[0].ID != "t_old" || asc[1].ID != "t_new" {
		t.Errorf("expected oldest-first [t_old,t_new], got [%s,%s]", asc[0].ID, asc[1].ID)
	}
}
```

- [ ] **Step 2: Run test to confirm it fails**

```bash
go test ./internal/store/... -run TestListTweets_OrderAsc -v
```

Expected: FAIL — `OrderAsc` field does not exist on `TweetFilter`.

- [ ] **Step 3: Add `OrderAsc` to `TweetFilter`**

In `internal/store/store.go`, find:

```go
type TweetFilter struct {
	RuleID         *int64
	SourceID       *int64
	EntityID       *int64
	HasEntityTrack bool
	From           *time.Time
	To             *time.Time
	Page           int
	PerPage        int
}
```

Replace with:

```go
type TweetFilter struct {
	RuleID         *int64
	SourceID       *int64
	EntityID       *int64
	HasEntityTrack bool
	From           *time.Time
	To             *time.Time
	Page           int
	PerPage        int
	OrderAsc       bool
}
```

- [ ] **Step 4: Update `ListTweets` ORDER BY clause**

In `internal/store/sqlite.go`, find:

```go
	query += " ORDER BY t.created_at DESC LIMIT ? OFFSET ?"
```

Replace with:

```go
	if filter.OrderAsc {
		query += " ORDER BY t.created_at ASC LIMIT ? OFFSET ?"
	} else {
		query += " ORDER BY t.created_at DESC LIMIT ? OFFSET ?"
	}
```

- [ ] **Step 5: Run test to confirm it passes**

```bash
go test ./internal/store/... -run TestListTweets_OrderAsc -v
```

Expected: PASS.

- [ ] **Step 6: Run full store tests**

```bash
go test ./internal/store/... -v
```

Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add internal/store/store.go internal/store/sqlite.go internal/store/sqlite_test.go
git commit -m "feat(store): add OrderAsc to TweetFilter for ascending tweet listing"
```

---

## Task 2: Handler — `order` request field and mock update

**Files:**
- Modify: `internal/api/handlers_pipelines.go:225-228`
- Modify: `internal/api/api_test.go` (mock + new test)

- [ ] **Step 1: Update `mockStore.ListTweets` to sort**

The mock currently iterates over a Go map (non-deterministic order). Add sorting so tests can verify dispatch order.

Add `"sort"` to the imports block at the top of `internal/api/api_test.go`:

Find:
```go
import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
```

Replace with:
```go
import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"
```

Then find the `mockStore.ListTweets` implementation:

```go
func (m *mockStore) ListTweets(_ context.Context, f store.TweetFilter) ([]*model.Tweet, error) {
	out := make([]*model.Tweet, 0, len(m.tweets))
	for _, t := range m.tweets {
		if f.HasEntityTrack && !m.entityTrackedTweets[t.ID] {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}
```

Replace with:

```go
func (m *mockStore) ListTweets(_ context.Context, f store.TweetFilter) ([]*model.Tweet, error) {
	out := make([]*model.Tweet, 0, len(m.tweets))
	for _, t := range m.tweets {
		if f.HasEntityTrack && !m.entityTrackedTweets[t.ID] {
			continue
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		if f.OrderAsc {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}
```

- [ ] **Step 2: Write the failing test**

Add to `internal/api/api_test.go` (after `TestRunPipeline_StartNodeIdNewOnlyScoped`):

```go
func TestRunPipeline_AscOrder(t *testing.T) {
	srv, ms, mp, _ := newTestServer(t)
	ms.pipelines[1] = &model.Pipeline{ID: 1, Name: "p1", Active: true}
	ms.triggerLinks = []*model.TriggerPipelineLink{
		{ID: 1, PipelineID: 1, TriggerType: "new_tweet", Active: true},
	}
	actionID := int64(5)
	ms.pipelineGraphs[1] = &model.PipelineGraph{
		Nodes: []*model.PipelineNode{
			{ID: 1, PipelineID: 1, Type: model.PipelineNodeTypeStart},
			{ID: 2, PipelineID: 1, Type: model.PipelineNodeTypeAction, ActionID: &actionID},
		},
		Edges: []*model.PipelineEdge{
			{ID: 1, PipelineID: 1, SourceNodeID: 1, TargetNodeID: 2},
		},
	}
	ms.tweets["t_old"] = &model.Tweet{
		ID: "t_old", Text: "old tweet", AuthorHandle: "h1",
		CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	ms.tweets["t_new"] = &model.Tweet{
		ID: "t_new", Text: "new tweet", AuthorHandle: "h2",
		CreatedAt: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
	}

	body := `{"order": "asc"}`
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

	if len(mp.pushed) != 2 {
		t.Fatalf("expected 2 pushed tasks, got %d", len(mp.pushed))
	}
	var ev0 struct{ TweetID string `json:"tweet_id"` }
	_ = json.Unmarshal(mp.pushed[0].Event, &ev0)
	if ev0.TweetID != "t_old" {
		t.Errorf("expected first task for t_old (oldest), got %s", ev0.TweetID)
	}
}
```

- [ ] **Step 3: Run test to confirm it fails**

```bash
go test ./internal/api/... -run TestRunPipeline_AscOrder -v
```

Expected: FAIL — `Order` field not on request struct / `OrderAsc` not passed to filter.

- [ ] **Step 4: Add `Order` field to request struct and pass `OrderAsc` to filter**

In `internal/api/handlers_pipelines.go`, find:

```go
	var req struct {
		NewOnly     bool   `json:"new_only"`
		StartNodeID *int64 `json:"start_node_id"`
	}
```

Replace with:

```go
	var req struct {
		NewOnly     bool   `json:"new_only"`
		StartNodeID *int64 `json:"start_node_id"`
		Order       string `json:"order"` // "asc" | "desc"; default "desc"
	}
```

Then find the tweet filter construction (in the background goroutine):

```go
			filter := store.TweetFilter{Page: 1, PerPage: 5000}
```

Replace with:

```go
			filter := store.TweetFilter{Page: 1, PerPage: 5000, OrderAsc: newOrder == "asc"}
```

But first, capture `req.Order` before the goroutine (like `newOnly` is captured). Find:

```go
	capturedStartNode := startNode
	go func() {
		ctx := s.ctx
		newOnly := req.NewOnly
```

Replace with:

```go
	capturedStartNode := startNode
	go func() {
		ctx := s.ctx
		newOnly := req.NewOnly
		newOrder := req.Order
```

Then the filter line:

```go
			filter := store.TweetFilter{Page: 1, PerPage: 5000}
```

Replace with:

```go
			filter := store.TweetFilter{Page: 1, PerPage: 5000, OrderAsc: newOrder == "asc"}
```

- [ ] **Step 5: Run test to confirm it passes**

```bash
go test ./internal/api/... -run TestRunPipeline_AscOrder -v
```

Expected: PASS.

- [ ] **Step 6: Run full API tests**

```bash
go test ./internal/api/... -v
```

Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add internal/api/handlers_pipelines.go internal/api/api_test.go
git commit -m "feat(api): add order field to pipeline run endpoint"
```

---

## Task 3: MCP — update `pipelines_run` tool description

**Files:**
- Modify: `internal/mcpserver/tools.go`

- [ ] **Step 1: Update the description**

Find:

```go
		Body: &BodyDef{
			Description: "Object with optional fields: new_only (bool, skip tweets already processed by the start node's action), start_node_id (int, dispatch from this action or pipeline node instead of the pipeline's Start node — full context from prior runs is available automatically).",
		},
```

Replace with:

```go
		Body: &BodyDef{
			Description: "Object with optional fields: new_only (bool, skip tweets already processed by the start node's action), start_node_id (int, dispatch from this action or pipeline node instead of the pipeline's Start node — full context from prior runs is available automatically), order (string, \"asc\" for oldest-first dispatch or \"desc\" for newest-first; default \"desc\").",
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
git commit -m "feat(mcp): document order field in pipelines_run tool"
```

---

## Task 4: Frontend — API function and store

**Files:**
- Modify: `web/src/api/pipelines.ts:87-94`
- Modify: `web/src/stores/pipelines.ts`

- [ ] **Step 1: Update `runPipeline` in `web/src/api/pipelines.ts`**

Find:

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

Replace with:

```ts
export const runPipeline = (id: number, newOnly: boolean, startNodeId?: number, order?: 'asc' | 'desc') =>
  apiFetch<{ queued: number; async?: boolean }>(`/api/pipelines/${id}/run`, {
    method: 'POST',
    body: JSON.stringify({
      new_only: newOnly,
      ...(startNodeId !== undefined ? { start_node_id: startNodeId } : {}),
      ...(order === 'asc' ? { order } : {}),
    }),
  })
```

- [ ] **Step 2: Update `run` in `web/src/stores/pipelines.ts`**

Find:

```ts
  async function run(id: number, newOnly: boolean, startNodeId?: number): Promise<{ queued: number; async?: boolean }> {
    return runPipeline(id, newOnly, startNodeId)
  }
```

Replace with:

```ts
  async function run(id: number, newOnly: boolean, startNodeId?: number, order?: 'asc' | 'desc'): Promise<{ queued: number; async?: boolean }> {
    return runPipeline(id, newOnly, startNodeId, order)
  }
```

- [ ] **Step 3: Build to confirm no type errors**

```bash
cd web && npm run build
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add web/src/api/pipelines.ts web/src/stores/pipelines.ts
git commit -m "feat(ui): add order param to runPipeline API and store"
```

---

## Task 5: PipelinesView — order radio pair in run modal

**Files:**
- Modify: `web/src/views/PipelinesView.vue`

- [ ] **Step 1: Add `runOrder` ref**

Find the existing run modal refs block:

```ts
const showRunModal      = ref(false)
const runPipelineRow    = ref<Pipeline | null>(null)
const runNewOnly        = ref(false)
const runningPipeline   = ref(false)
const runStartNodeId    = ref<number | null>(null)
const runGraphNodes     = ref<PipelineNode[]>([])
const runGraphLoading   = ref(false)
```

Replace with:

```ts
const showRunModal      = ref(false)
const runPipelineRow    = ref<Pipeline | null>(null)
const runNewOnly        = ref(false)
const runOrder          = ref<'asc' | 'desc'>('desc')
const runningPipeline   = ref(false)
const runStartNodeId    = ref<number | null>(null)
const runGraphNodes     = ref<PipelineNode[]>([])
const runGraphLoading   = ref(false)
```

- [ ] **Step 2: Reset `runOrder` when the modal opens**

Find in `openRunPipeline`:

```ts
  runPipelineRow.value = row
  runNewOnly.value = false
  runStartNodeId.value = null
```

Replace with:

```ts
  runPipelineRow.value = row
  runNewOnly.value = false
  runOrder.value = 'desc'
  runStartNodeId.value = null
```

- [ ] **Step 3: Pass `runOrder` to `runPipeline` call**

Find in `onConfirmRunPipeline`:

```ts
    const res = await runPipeline(
      runPipelineRow.value.id,
      runNewOnly.value,
      runStartNodeId.value ?? undefined,
    )
```

Replace with:

```ts
    const res = await runPipeline(
      runPipelineRow.value.id,
      runNewOnly.value,
      runStartNodeId.value ?? undefined,
      runOrder.value === 'asc' ? 'asc' : undefined,
    )
```

- [ ] **Step 4: Add order radio pair to the run modal template**

Find the end of the run modal content (just before `</n-space>` that closes the modal body):

```html
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

Replace with:

```html
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
      <n-radio-group v-model:value="runOrder">
        <n-space vertical :size="8">
          <n-radio value="desc">Newest first (default)</n-radio>
          <n-radio value="asc">Oldest first</n-radio>
        </n-space>
      </n-radio-group>
    </n-space>
  </n-modal>
```

- [ ] **Step 5: Build to confirm no type errors**

```bash
cd web && npm run build
```

Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add web/src/views/PipelinesView.vue
git commit -m "feat(ui): add order radio to pipeline run modal"
```

---

## Task 6: PipelineBuilderView — order radio pair in run-from-node modal

**Files:**
- Modify: `web/src/views/PipelineBuilderView.vue`

- [ ] **Step 1: Add `runFromNodeOrder` ref**

Find the run-from-node state block:

```ts
// Run-from-node state (Option B — invoked from graph editor node buttons)
const showRunFromNodeModal = ref(false)
const runFromNodeId        = ref<string | null>(null)
const runFromNodeLabel     = ref('')
const runFromNodeNewOnly   = ref(false)
const runningFromNode      = ref(false)
```

Replace with:

```ts
// Run-from-node state (Option B — invoked from graph editor node buttons)
const showRunFromNodeModal = ref(false)
const runFromNodeId        = ref<string | null>(null)
const runFromNodeLabel     = ref('')
const runFromNodeNewOnly   = ref(false)
const runFromNodeOrder     = ref<'asc' | 'desc'>('desc')
const runningFromNode      = ref(false)
```

- [ ] **Step 2: Reset `runFromNodeOrder` when the modal opens**

Find in `handleRunFromNode`:

```ts
  runFromNodeId.value    = nodeId
  runFromNodeLabel.value = node?.data?.label ?? `Node #${nodeId}`
  runFromNodeNewOnly.value = false
  showRunFromNodeModal.value = true
```

Replace with:

```ts
  runFromNodeId.value      = nodeId
  runFromNodeLabel.value   = node?.data?.label ?? `Node #${nodeId}`
  runFromNodeNewOnly.value = false
  runFromNodeOrder.value   = 'desc'
  showRunFromNodeModal.value = true
```

- [ ] **Step 3: Pass `runFromNodeOrder` to `runPipeline` call**

Find in `onConfirmRunFromNode`:

```ts
    const res = await runPipeline(pipelineId, runFromNodeNewOnly.value, numId)
```

Replace with:

```ts
    const res = await runPipeline(pipelineId, runFromNodeNewOnly.value, numId, runFromNodeOrder.value === 'asc' ? 'asc' : undefined)
```

- [ ] **Step 4: Add order radio pair to the run-from-node modal template**

Find:

```html
      <n-radio-group v-model:value="runFromNodeNewOnly">
        <n-space vertical :size="8">
          <n-radio :value="false">All eligible tweets</n-radio>
          <n-radio :value="true">Only tweets not yet processed by this node's action</n-radio>
        </n-space>
      </n-radio-group>
    </n-space>
  </n-modal>
```

Replace with:

```html
      <n-radio-group v-model:value="runFromNodeNewOnly">
        <n-space vertical :size="8">
          <n-radio :value="false">All eligible tweets</n-radio>
          <n-radio :value="true">Only tweets not yet processed by this node's action</n-radio>
        </n-space>
      </n-radio-group>
      <n-radio-group v-model:value="runFromNodeOrder">
        <n-space vertical :size="8">
          <n-radio value="desc">Newest first (default)</n-radio>
          <n-radio value="asc">Oldest first</n-radio>
        </n-space>
      </n-radio-group>
    </n-space>
  </n-modal>
```

- [ ] **Step 5: Build to confirm no type errors**

```bash
cd web && npm run build
```

Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add web/src/views/PipelineBuilderView.vue
git commit -m "feat(ui): add order radio to run-from-node modal"
```

---

## Task 7: Build verification and CLAUDE.md update

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Docker build**

```bash
docker compose build --build-arg BUILDKIT_INLINE_CACHE=1 iran-watcher
```

Expected: build succeeds.

- [ ] **Step 2: Update CLAUDE.md**

In `CLAUDE.md`, find the `POST /api/pipelines/{id}/run` description in Key Design Decisions. Update the body description to include `"order"`:

Find:
```
body `{ "new_only": bool, "start_node_id": int }`; `start_node_id` is optional — when provided, dispatches from that action or pipeline node instead of the Start node; `new_only` exclusion is scoped to the named node's action when `start_node_id` is set; full context from prior runs is available automatically via `MergeAllTweetExecutionMetadata`; returns 202 `{ "queued": N, "async": true }`
```

Replace with:
```
body `{ "new_only": bool, "start_node_id": int, "order": "asc"|"desc" }`; `start_node_id` is optional — when provided, dispatches from that action or pipeline node instead of the Start node; `new_only` exclusion is scoped to the named node's action when `start_node_id` is set; `order` defaults to `"desc"` (newest first) — set `"asc"` to dispatch oldest tweet first (useful for notification pipelines so alerts arrive in chronological order); full context from prior runs is available automatically via `MergeAllTweetExecutionMetadata`; returns 202 `{ "queued": N, "async": true }`
```

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: document order field in pipeline run endpoint (CLAUDE.md)"
```
