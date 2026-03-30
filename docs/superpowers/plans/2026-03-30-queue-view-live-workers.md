# Queue View: Live Workers & Pending Counts — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the Queue page so it shows all active workers per action (with a count badge + tooltip for multiples), and updates pending counts and worker assignments in real-time via WebSocket.

**Architecture:** Three Go-side fixes (broadcast on pop, workers slice in API response, active_workers in WS event) feed two frontend changes (store type + cross-update logic, Worker column rendering + active/inactive table split). Each layer is independently testable. No new event types — existing `queue_update` and `worker_stats` events are extended.

**Tech Stack:** Go 1.25.5, Redis (go-redis v9), Vue 3, Pinia, NaiveUI (`NTooltip`, `NCollapse`)

---

## Files

| File | Change |
|---|---|
| `internal/queue/redis.go` | Broadcast `queue_update` after `PopTask` (LLEN after BRPOP) |
| `internal/queue/queue_test.go` | Test `PopTask` broadcasts on success |
| `internal/api/handlers_queue.go` | `Worker *workerRef` → `Workers []workerRef`; extract `buildWorkerRefs`; fix sort |
| `internal/api/handlers_queue_test.go` | New file — test `buildWorkerRefs` multi-worker logic |
| `internal/worker/manager.go` | Add `active_workers` to `worker_stats` WS broadcast payload |
| `web/src/api/queue.ts` | `worker: WorkerRef \| null` → `workers: WorkerRef[]` |
| `web/src/stores/queues.ts` | `QueueEntry.workers` array; `setWorkerStats` cross-update |
| `web/src/views/QueueView.vue` | Worker column tooltip; active/inactive table split with collapse |

---

### Task 1: Broadcast `queue_update` after PopTask

**Files:**
- Modify: `internal/queue/redis.go`
- Modify: `internal/queue/queue_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/queue/queue_test.go`:

```go
// captureBroadcaster records all Broadcast calls for test assertions.
type captureBroadcaster struct {
	events []hub.Event
}

func (b *captureBroadcaster) Broadcast(e hub.Event) {
	b.events = append(b.events, e)
}

// brpopSuccessFakeClient returns a successful BRPOP result with a configurable LLEN.
type brpopSuccessFakeClient struct {
	fakeRedisClient
	brpopKey   string
	brpopVal   string
	llenResult int64
}

func (f *brpopSuccessFakeClient) BRPop(_ context.Context, _ time.Duration, _ ...string) *redis.StringSliceCmd {
	return redis.NewStringSliceResult([]string{f.brpopKey, f.brpopVal}, nil)
}

func (f *brpopSuccessFakeClient) LLen(_ context.Context, _ string) *redis.IntCmd {
	return redis.NewIntResult(f.llenResult, nil)
}

func TestPopTask_BroadcastsQueueUpdateOnSuccess(t *testing.T) {
	ev, _ := json.Marshal(TweetEvent{TweetID: "1", Text: "test", AuthorHandle: "user"})
	taskJSON, _ := json.Marshal(&Task{
		TriggerType: "new_tweet",
		ActionID:    7,
		Event:       json.RawMessage(ev),
		Context:     map[string]any{},
	})
	fake := &brpopSuccessFakeClient{
		brpopKey:   "iran-watcher:tasks:7",
		brpopVal:   string(taskJSON),
		llenResult: 5,
	}
	bc := &captureBroadcaster{}
	q := &RedisQueue{client: fake, broadcaster: bc}

	task, err := q.PopTask(context.Background(), 1*time.Second, "iran-watcher:tasks:7")
	if err != nil {
		t.Fatalf("PopTask: %v", err)
	}
	if task == nil {
		t.Fatal("expected non-nil task")
	}
	if len(bc.events) != 1 {
		t.Fatalf("expected 1 broadcast event, got %d", len(bc.events))
	}
	if bc.events[0].Type != "queue_update" {
		t.Errorf("event type: got %q, want %q", bc.events[0].Type, "queue_update")
	}
	payload, ok := bc.events[0].Payload.(map[string]int64)
	if !ok {
		t.Fatalf("expected map[string]int64 payload, got %T", bc.events[0].Payload)
	}
	if payload["action_id"] != 7 {
		t.Errorf("action_id: got %d, want 7", payload["action_id"])
	}
	if payload["pending"] != 5 {
		t.Errorf("pending: got %d, want 5", payload["pending"])
	}
}

func TestPopTask_NoBroadcastWhenNoBroadcaster(t *testing.T) {
	ev, _ := json.Marshal(TweetEvent{TweetID: "2", Text: "x", AuthorHandle: "u"})
	taskJSON, _ := json.Marshal(&Task{TriggerType: "new_tweet", ActionID: 3, Event: json.RawMessage(ev), Context: map[string]any{}})
	fake := &brpopSuccessFakeClient{brpopKey: "iran-watcher:tasks:3", brpopVal: string(taskJSON), llenResult: 0}
	q := &RedisQueue{client: fake} // no broadcaster

	task, err := q.PopTask(context.Background(), 1*time.Second, "iran-watcher:tasks:3")
	if err != nil {
		t.Fatalf("PopTask: %v", err)
	}
	if task == nil {
		t.Fatal("expected non-nil task")
	}
	// No panic = pass (no broadcaster means no broadcast attempt)
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test ./internal/queue/ -run TestPopTask_Broadcasts -v
```
Expected: `FAIL` — `captureBroadcaster` undefined, `brpopSuccessFakeClient` undefined.

- [ ] **Step 3: Add missing import to queue_test.go**

The test needs the `hub` package. Add to the import block in `internal/queue/queue_test.go`:

```go
import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/hub"
	"github.com/redis/go-redis/v9"
)
```

- [ ] **Step 4: Implement broadcast in PopTask**

In `internal/queue/redis.go`, replace the `PopTask` function body:

```go
// PopTask blocks until a Task is available on one of the given keys or the
// timeout elapses. Returns (nil, nil) on timeout or when no keys are provided.
func (q *RedisQueue) PopTask(ctx context.Context, timeout time.Duration, keys ...string) (*Task, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	res, err := q.client.BRPop(ctx, timeout, keys...).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, fmt.Errorf("queue: pop task: %w", err)
	}
	if len(res) < 2 {
		return nil, nil
	}
	var t Task
	if err := json.Unmarshal([]byte(res[1]), &t); err != nil {
		return nil, fmt.Errorf("queue: pop task: unmarshal: %w", err)
	}
	if q.broadcaster != nil {
		pending, _ := q.client.LLen(ctx, res[0]).Result()
		q.broadcaster.Broadcast(hub.Event{
			Type: "queue_update",
			Payload: map[string]int64{
				"action_id": t.ActionID,
				"pending":   pending,
			},
		})
	}
	return &t, nil
}
```

- [ ] **Step 5: Run tests to confirm they pass**

```bash
go test ./internal/queue/ -v
```
Expected: all PASS including `TestPopTask_BroadcastsQueueUpdateOnSuccess` and `TestPopTask_NoBroadcastWhenNoBroadcaster`.

- [ ] **Step 6: Commit**

```bash
git add internal/queue/redis.go internal/queue/queue_test.go
git commit -m "feat: broadcast queue_update with pending count after PopTask"
```

---

### Task 2: Workers slice in API response

**Files:**
- Modify: `internal/api/handlers_queue.go`
- Create: `internal/api/handlers_queue_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/api/handlers_queue_test.go`:

```go
package api

import (
	"testing"

	"github.com/amir-the-h/iran-watcher/internal/worker"
)

func TestBuildWorkerRefs_MultipleWorkersOnSameAction(t *testing.T) {
	workers := []worker.WorkerInfo{
		{ID: 1, Action: "Detect Assassination", Pipeline: "ollama", Backend: "http://localhost:11434"},
		{ID: 4, Action: "Detect Assassination", Pipeline: "ollama", Backend: "http://localhost:11435"},
		{ID: 2, Action: "Summarize", Pipeline: "ollama", Backend: "http://localhost:11434"},
	}
	got := buildWorkerRefs(workers)

	if len(got["Detect Assassination"]) != 2 {
		t.Errorf("Detect Assassination: want 2 workers, got %d", len(got["Detect Assassination"]))
	}
	if got["Detect Assassination"][0].ID != 1 {
		t.Errorf("first worker ID: want 1, got %d", got["Detect Assassination"][0].ID)
	}
	if got["Detect Assassination"][1].ID != 4 {
		t.Errorf("second worker ID: want 4, got %d", got["Detect Assassination"][1].ID)
	}
	if len(got["Summarize"]) != 1 {
		t.Errorf("Summarize: want 1 worker, got %d", len(got["Summarize"]))
	}
	if got["Summarize"][0].Backend != "http://localhost:11434" {
		t.Errorf("Summarize backend: want %q, got %q", "http://localhost:11434", got["Summarize"][0].Backend)
	}
}

func TestBuildWorkerRefs_EmptyInput(t *testing.T) {
	got := buildWorkerRefs(nil)
	if len(got) != 0 {
		t.Errorf("want empty map, got %d entries", len(got))
	}
}
```

- [ ] **Step 2: Run test to confirm it fails**

```bash
go test ./internal/api/ -run TestBuildWorkerRefs -v
```
Expected: `FAIL` — `buildWorkerRefs` undefined.

- [ ] **Step 3: Update handlers_queue.go**

Replace the entire `internal/api/handlers_queue.go` with:

```go
package api

import (
	"net/http"
	"sort"

	"github.com/amir-the-h/iran-watcher/internal/queue"
	"github.com/amir-the-h/iran-watcher/internal/worker"
)

type queueEntry struct {
	ActionID     int64       `json:"action_id"`
	ActionName   string      `json:"action_name"`
	PipelineName string      `json:"pipeline_name"`
	PipelineType string      `json:"pipeline_type"`
	Pending      int64       `json:"pending"`
	Workers      []workerRef `json:"workers,omitempty"`
}

type workerRef struct {
	ID      int    `json:"id"`
	Backend string `json:"backend,omitempty"`
}

// buildWorkerRefs groups WorkerInfo entries by action name into a map of
// workerRef slices. Multiple workers on the same action are all included.
func buildWorkerRefs(workerInfos []worker.WorkerInfo) map[string][]workerRef {
	m := make(map[string][]workerRef)
	for _, wi := range workerInfos {
		m[wi.Action] = append(m[wi.Action], workerRef{ID: wi.ID, Backend: wi.Backend})
	}
	return m
}

func (s *Server) handleGetQueue(w http.ResponseWriter, r *http.Request) {
	if s.actionCache == nil {
		writeJSON(w, http.StatusOK, map[string]any{"queues": []queueEntry{}})
		return
	}

	entries := s.actionCache.All()

	workerByAction := make(map[string][]workerRef)
	if s.pool != nil {
		workerByAction = buildWorkerRefs(s.pool.Stats().ActiveWorkers)
	}

	result := make([]queueEntry, 0, len(entries))
	for _, e := range entries {
		var pending int64
		if s.sizer != nil {
			pending, _ = s.sizer.Len(r.Context(), queue.QueueTasksKey(e.Action.ID))
		}
		qe := queueEntry{
			ActionID:     e.Action.ID,
			ActionName:   e.Action.Name,
			PipelineName: e.Pipeline.Name(),
			PipelineType: string(e.Pipeline.Type()),
			Pending:      pending,
		}
		if refs, ok := workerByAction[e.Action.Name]; ok {
			qe.Workers = refs
		}
		result = append(result, qe)
	}

	// Sort: active rows (worker or pending) first, then by pending desc, then name.
	sort.Slice(result, func(i, j int) bool {
		ri, rj := result[i], result[j]
		iActive := len(ri.Workers) > 0 || ri.Pending > 0
		jActive := len(rj.Workers) > 0 || rj.Pending > 0
		if iActive != jActive {
			return iActive
		}
		if ri.Pending != rj.Pending {
			return ri.Pending > rj.Pending
		}
		return ri.ActionName < rj.ActionName
	})

	writeJSON(w, http.StatusOK, map[string]any{"queues": result})
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
go test ./internal/api/ -run TestBuildWorkerRefs -v
```
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/api/handlers_queue.go internal/api/handlers_queue_test.go
git commit -m "feat: return workers slice per action in GET /api/queue"
```

---

### Task 3: Include active_workers in worker_stats WS broadcast

**Files:**
- Modify: `internal/worker/manager.go`

No dedicated unit test — the payload extension is covered by build verification and end-to-end observation via the frontend.

- [ ] **Step 1: Update broadcastWorkerStats in manager.go**

Find the `broadcastWorkerStats` method (currently around line 48) and replace it:

```go
// broadcastWorkerStats pushes the current pool stats to all connected WS clients.
func (m *Manager) broadcastWorkerStats() {
	if m.broadcaster == nil {
		return
	}
	s := m.Stats()
	m.broadcaster.Broadcast(hub.Event{
		Type: "worker_stats",
		Payload: map[string]any{
			"total":          s.Total,
			"active":         s.Active,
			"idle":           s.Idle,
			"active_workers": s.ActiveWorkers,
		},
	})
}
```

- [ ] **Step 2: Confirm existing tests still pass**

```bash
go test ./internal/worker/ -v
```
Expected: all PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/worker/manager.go
git commit -m "feat: include active_workers in worker_stats WebSocket broadcast"
```

---

### Task 4: Update frontend API type and Pinia store

**Files:**
- Modify: `web/src/api/queue.ts`
- Modify: `web/src/stores/queues.ts`

- [ ] **Step 1: Update web/src/api/queue.ts**

Replace the file content:

```typescript
import { apiFetch } from './client'

export interface WorkerRef {
  id: number
  backend?: string
}

export interface QueueEntry {
  action_id: number
  action_name: string
  pipeline_name: string
  pipeline_type: 'onnx' | 'ollama' | 'internal' | 'notification'
  pending: number
  workers: WorkerRef[]
}

export interface QueueResponse {
  queues: QueueEntry[]
}

export const getQueue = () => apiFetch<QueueResponse>('/api/queue')

export const flushQueue = () =>
  apiFetch<{ flushed: boolean }>('/api/queue', { method: 'DELETE' })
```

- [ ] **Step 2: Update web/src/stores/queues.ts**

Replace the file content:

```typescript
// web/src/stores/queues.ts
import { defineStore } from 'pinia'
import { ref, computed } from 'vue'

export interface QueueEntry {
  action_id: number
  action_name: string
  pipeline_name: string
  pipeline_type: string
  pending: number
  workers: Array<{ id: number; backend: string }>
}

export interface WorkerStats {
  total: number
  active: number
  idle: number
  active_workers: Array<{ id: number; action: string; pipeline: string; backend: string }>
}

export const useQueuesStore = defineStore('queues', () => {
  const queues = ref<QueueEntry[]>([])
  const workerStats = ref<WorkerStats | null>(null)

  // Set the full list from the initial HTTP fetch (onMounted in QueueView).
  function setQueues(entries: QueueEntry[]) {
    queues.value = entries.map(e => ({
      ...e,
      workers: e.workers ?? [],
    }))
  }

  // Update a single action's pending count from a WS queue_update event.
  function setQueueEntry(payload: { action_id: number; pending: number }) {
    const entry = queues.value.find(q => q.action_id === payload.action_id)
    if (entry) {
      entry.pending = payload.pending
    }
  }

  // Update pool-level stats and cross-update the workers field on each queue
  // entry so the Worker column stays live without a full page refresh.
  function setWorkerStats(stats: WorkerStats) {
    workerStats.value = stats
    const activeWorkers = stats.active_workers ?? []
    for (const entry of queues.value) {
      entry.workers = activeWorkers
        .filter(w => w.action === entry.action_name)
        .map(w => ({ id: w.id, backend: w.backend ?? '' }))
    }
  }

  const totalPending = computed(() =>
    queues.value.reduce((sum, q) => sum + q.pending, 0)
  )

  return { queues, workerStats, totalPending, setQueues, setQueueEntry, setWorkerStats }
})
```

- [ ] **Step 3: Commit**

```bash
git add web/src/api/queue.ts web/src/stores/queues.ts
git commit -m "feat: update queue store — workers array, live cross-update from worker_stats"
```

---

### Task 5: Update QueueView — Worker column and active/inactive split

**Files:**
- Modify: `web/src/views/QueueView.vue`

- [ ] **Step 1: Update imports and add computed splits**

In the `<script setup>` block:

1. Add `NTooltip` to the naive-ui import:
```typescript
import { useMessage, NButton, NSpace, NTag, NIcon, NTooltip } from 'naive-ui'
```

2. Add computed queues after the `queueColumns` definition (around line 153):
```typescript
// Split active (pending or workers) from idle rows.
const activeQueues   = computed(() => queuesStore.queues.filter(q => q.pending > 0 || q.workers.length > 0))
const inactiveQueues = computed(() => queuesStore.queues.filter(q => q.pending === 0 && q.workers.length === 0))
```

- [ ] **Step 2: Update fetchQueues to use the new workers field**

Replace the mapping in `fetchQueues` (around line 37-40):

```typescript
async function fetchQueues() {
  try {
    const data = await getQueue()
    queuesStore.setQueues(data.queues)
    pendingHistory.value.push({ t: Date.now(), v: queuesStore.totalPending })
    if (pendingHistory.value.length > 10) pendingHistory.value.shift()
  } catch { /* ignore */ }
}
```

- [ ] **Step 3: Replace the Worker column render function**

Replace the Worker column definition (the object starting at `title: 'Worker'`):

```typescript
{
  title: 'Worker',
  key: 'workers',
  width: 220,
  render: (row: QueueEntry) => {
    const workers = row.workers ?? []
    if (workers.length === 0) return h('span', { style: { color: '#585b70' } }, '—')

    function workerLabel(w: { id: number; backend: string }) {
      if (w.backend === 'waiting') return `#${w.id} · waiting…`
      if (w.backend) return `#${w.id} · ${w.backend.replace(/^https?:\/\//, '')}`
      return `#${w.id}`
    }

    if (workers.length === 1) {
      return h('span', {
        style: { color: '#a6e3a1', fontVariantNumeric: 'tabular-nums' },
      }, workerLabel(workers[0]))
    }

    // Multiple workers: count badge with tooltip listing each one.
    return h(NTooltip, { trigger: 'hover' }, {
      trigger: () => h('span', {
        style: { color: '#a6e3a1', fontVariantNumeric: 'tabular-nums', cursor: 'default' },
      }, `${workers.length} workers`),
      default: () => h('div', {}, workers.map(w =>
        h('div', { style: { whiteSpace: 'nowrap', lineHeight: '1.6' } }, workerLabel(w))
      )),
    })
  },
},
```

- [ ] **Step 4: Replace the Action Queues card template section**

Find the `<!-- Per-action queues table -->` section and replace it:

```html
<!-- Per-action queues table -->
<n-card size="small" title="Action Queues">
  <n-empty
    v-if="queuesStore.queues.length === 0"
    description="No active actions"
    style="padding: 16px 0;"
  />
  <template v-else>
    <!-- Active rows (pending > 0 or workers assigned) -->
    <n-empty
      v-if="activeQueues.length === 0"
      description="All queues idle"
      style="padding: 12px 0 4px;"
    />
    <n-data-table
      v-else
      :columns="queueColumns"
      :data="activeQueues"
      :pagination="false"
      size="small"
    />

    <!-- Inactive rows — collapsed by default -->
    <n-collapse v-if="inactiveQueues.length > 0" style="margin-top: 8px;">
      <n-collapse-item :title="`${inactiveQueues.length} inactive`" name="inactive">
        <n-data-table
          :columns="queueColumns"
          :data="inactiveQueues"
          :pagination="false"
          size="small"
          :row-props="() => ({ style: 'opacity: 0.5;' })"
        />
      </n-collapse-item>
    </n-collapse>
  </template>
</n-card>
```

- [ ] **Step 5: Commit**

```bash
git add web/src/views/QueueView.vue
git commit -m "feat: queue view — multi-worker tooltip, active/inactive table split"
```

---

### Task 6: Build verification

**Files:** none (verification only)

- [ ] **Step 1: Run all Go unit tests**

```bash
go test ./...
```
Expected: all PASS, no compilation errors.

- [ ] **Step 2: Build the Docker image**

```bash
docker compose build iran-watcher
```
Expected: build succeeds with no errors.

- [ ] **Step 3: Verify frontend build inside Docker**

The Docker build compiles the Vue app via `npm run build`. A successful `docker compose build` confirms the TypeScript types compile cleanly.

- [ ] **Step 4: Smoke test (optional, if services are running)**

```bash
docker compose up -d --no-deps iran-watcher
docker compose logs -f iran-watcher
```

Open the Queue page in the browser. Confirm:
- Pending counts decrease as workers consume tasks (live)
- Actions with no pending and no workers appear in the collapsed "N inactive" section
- Actions with a single worker show `#id · backend`
- Actions with multiple workers show `N workers` badge; hovering shows each worker on its own line

---

## Self-Review Notes

- All spec requirements covered: workers slice (Task 2), broadcast on pop (Task 1), active_workers in WS (Task 3), store cross-update (Task 4), Worker column tooltip + active/inactive split (Task 5)
- `buildWorkerRefs` extracted and tested independently — no ActionCache dependency needed in tests
- `setQueueEntry` semantics unchanged — still updates `pending` only; `setWorkerStats` is the sole updater of `workers` on queue entries
- `web/src/api/queue.ts` updated alongside the store — no stale `worker` field left anywhere in the frontend
- Sort predicate updated from `ri.Worker != nil` → `len(ri.Workers) > 0` in Task 2
