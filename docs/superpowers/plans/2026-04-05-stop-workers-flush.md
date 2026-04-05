# Stop Workers & Flush Queue Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a "Stop Workers & Flush Queue" button to the Queue page that cancels all in-flight worker tasks and drains all pending Redis task queues in one action.

**Architecture:** Add a `workerCancels sync.Map` to `Manager` so each running `processTask` call gets a per-task cancellable context. `Flush()` iterates the map and calls every cancel func. The existing `DELETE /api/workers/buffer` route already calls `Flush()` — no new routes needed. The frontend adds a button that calls buffer flush then queue flush in sequence.

**Tech Stack:** Go 1.25 (`sync.Map`, `context.WithCancel`), Vue 3 + Naive UI, `@vicons/ionicons5`

---

### Task 1: Write the failing test for `Flush()`

**Files:**
- Modify: `internal/worker/worker_test.go`

`Flush()` already exists as a no-op returning 0 — the test can compile now and will fail on the assertion `n != 1`.

- [ ] **Step 1: Add `ctxBlockPipeline`**

Add after the existing `blockPipeline` type (around line 608) in `worker_test.go`:

```go
// ctxBlockPipeline blocks Run until ctx is cancelled, then returns ctx.Err().
type ctxBlockPipeline struct {
	name    string
	started chan struct{} // receives a value when Run begins
}

func (c *ctxBlockPipeline) Name() string                 { return c.name }
func (c *ctxBlockPipeline) Type() inference.PipelineType { return inference.PipelineTypeONNX }
func (c *ctxBlockPipeline) Load(_ string) error          { return nil }
func (c *ctxBlockPipeline) Close() error                 { return nil }
func (c *ctxBlockPipeline) Run(ctx context.Context, _ inference.PipelineInput) (map[string]any, error) {
	select {
	case c.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return nil, ctx.Err()
}
```

- [ ] **Step 2: Add `TestManager_Flush_CancelsActiveWorkers`**

```go
func TestManager_Flush_CancelsActiveWorkers(t *testing.T) {
	started := make(chan struct{}, 1)
	pipe := &ctxBlockPipeline{name: "block", started: started}

	action := &model.Action{ID: 1, Name: "block", Active: true}
	cache := buildCacheWithEntries(map[int64]*CacheEntry{
		1: {Action: action, Pipeline: pipe},
	})

	consumer := &mockConsumer{tasks: []*queue.Task{makeTask(1, "tweet-flush")}}
	mgr := NewManager(2, consumer, &mockProducer{}, &mockStore{}, cache, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = mgr.Start(ctx) }()

	// Wait for the pipeline to start blocking.
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("pipeline did not start within 2s")
	}

	// Flush should cancel the in-flight task.
	n := mgr.Flush()
	if n != 1 {
		t.Errorf("Flush() returned %d, want 1", n)
	}

	// Give the worker a moment to finish cleanup.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		if int(mgr.active.Load()) == 0 {
			break
		}
	}
	if mgr.active.Load() != 0 {
		t.Errorf("expected 0 active workers after Flush, got %d", mgr.active.Load())
	}
}
```

- [ ] **Step 3: Run the test — confirm it fails**

```bash
cd /mnt/shared/projects/personal/iran-watcher && go test ./internal/worker/... -run TestManager_Flush_CancelsActiveWorkers -v -timeout 10s 2>&1
```

Expected: FAIL — `Flush()` returns 0 (no-op), assertion `n != 1` fires. The test may also time out on the active-worker check since cancellation never happens.

- [ ] **Step 4: Confirm the rest of the test suite still passes**

```bash
go test ./internal/worker/... -v -timeout 30s 2>&1 | tail -20
```

Expected: all tests except the new one PASS.

---

### Task 2: Implement `Flush()` in `Manager`

**Files:**
- Modify: `internal/worker/manager.go`

- [ ] **Step 1: Add `workerCancels` field to `Manager`**

In `internal/worker/manager.go`, add the field to the `Manager` struct (after `completionTrackers`):

```go
type Manager struct {
	totalWorkers       int
	consumer           queue.Consumer
	producer           queue.Producer
	store              store.Store
	cache              *ActionCache
	dispatcher         notifier.EventEmitter
	broadcaster        hub.Broadcaster
	logger             *slog.Logger
	active             atomic.Int32
	workerActions      sync.Map // key: int (worker ID), value: WorkerInfo
	workerCancels      sync.Map // key: int (worker ID), value: context.CancelFunc
	completionTrackers sync.Map // key: int64 (action_id), value: *rateTracker
}
```

- [ ] **Step 2: Pass a per-task context into `processTask`**

In `runWorker`, wrap the `processTask` call with a cancellable context. Replace the single `m.processTask(ctx, id, task)` call (currently at line ~174) with:

```go
taskCtx, taskCancel := context.WithCancel(ctx)
m.workerCancels.Store(id, taskCancel)
m.processTask(taskCtx, id, task)
taskCancel()
m.workerCancels.Delete(id)
```

The `processTask` signature already accepts a `context.Context` as its first argument — no signature change needed. The existing `ctx` passed in was the root context; now it receives `taskCtx` instead.

- [ ] **Step 3: Implement `Flush()`**

Replace the no-op `Flush()` at the bottom of `manager.go`:

```go
// Flush cancels the context of every worker that is currently executing a
// task. Workers blocked in BRPOP (idle) are unaffected. Returns the number
// of in-flight tasks that were cancelled.
func (m *Manager) Flush() int {
	n := 0
	m.workerCancels.Range(func(_, v any) bool {
		v.(context.CancelFunc)()
		n++
		return true
	})
	return n
}
```

`FlushPipeline` stays a no-op — no UI surface for per-pipeline interruption.

- [ ] **Step 4: Verify the build compiles**

```bash
docker compose build --build-arg BUILDKIT_INLINE_CACHE=1 iran-watcher 2>&1 | tail -20
```

Expected: build succeeds with no errors.

---

### Task 3: Verify test passes, commit

**Files:**
- Modify: `internal/worker/worker_test.go` (already done in Task 1)
- Modify: `internal/worker/manager.go` (already done in Task 2)

- [ ] **Step 1: Run the new test — confirm it passes**

```bash
cd /mnt/shared/projects/personal/iran-watcher && go test ./internal/worker/... -run TestManager_Flush_CancelsActiveWorkers -v -timeout 10s 2>&1
```

Expected: PASS.

- [ ] **Step 2: Run the full worker test suite**

```bash
go test ./internal/worker/... -v -timeout 30s 2>&1 | tail -20
```

Expected: all tests PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/worker/manager.go internal/worker/worker_test.go
git commit -m "feat: implement Manager.Flush() with per-task cancellable contexts"
```

---

### Task 4: Add `flushWorkerBuffer` to the frontend API client

**Files:**
- Modify: `web/src/api/queue.ts`

- [ ] **Step 1: Add the export**

Append to `web/src/api/queue.ts`:

```ts
export const flushWorkerBuffer = () =>
  apiFetch<{ flushed: number }>('/api/workers/buffer', { method: 'DELETE' })
```

The full file should now read:

```ts
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
  throughput?: number
}

export interface QueueResponse {
  queues: QueueEntry[]
}

export const getQueue = () => apiFetch<QueueResponse>('/api/queue')

export const flushQueue = () =>
  apiFetch<{ flushed: boolean }>('/api/queue', { method: 'DELETE' })

export const flushWorkerBuffer = () =>
  apiFetch<{ flushed: number }>('/api/workers/buffer', { method: 'DELETE' })
```

- [ ] **Step 2: Commit**

```bash
git add web/src/api/queue.ts
git commit -m "feat: add flushWorkerBuffer API call"
```

---

### Task 5: Add "Stop Workers & Flush Queue" button to Queue page

**Files:**
- Modify: `web/src/views/QueueView.vue`

- [ ] **Step 1: Update the import from `../api/queue`**

Find the line (currently line 9):

```ts
import { getQueue, flushQueue } from '../api/queue'
```

Replace with:

```ts
import { getQueue, flushQueue, flushWorkerBuffer } from '../api/queue'
```

- [ ] **Step 2: Update the icon import**

Find the line (currently line 13):

```ts
import { InformationCircleOutline, RefreshOutline, TrashOutline, BanOutline } from '@vicons/ionicons5'
```

Replace with:

```ts
import { InformationCircleOutline, RefreshOutline, TrashOutline, BanOutline, StopCircleOutline } from '@vicons/ionicons5'
```

- [ ] **Step 3: Add the `stopping` ref**

Find the line (currently line 62):

```ts
const flushing    = ref(false)
```

Add `stopping` after it:

```ts
const flushing    = ref(false)
const stopping    = ref(false)
```

- [ ] **Step 4: Add the `onStopWorkers` handler**

Add this function after `onFlush` (after line 88):

```ts
async function onStopWorkers() {
  stopping.value = true
  try {
    await flushWorkerBuffer()
    await flushQueue()
    msg.success('Workers stopped and queue flushed')
    await Promise.all([store.fetch(), fetchStats(), fetchQueues()])
  } catch (e: any) { msg.error(e.message) }
  finally { stopping.value = false }
}
```

- [ ] **Step 5: Add the button to the template**

In the template, find the existing `n-popconfirm` for the Flush Pending button (starts with `<n-popconfirm @positive-click="onFlush">`). Add the new button **before** it:

```html
<n-popconfirm @positive-click="onStopWorkers">
  <template #trigger>
    <n-tooltip trigger="hover">
      <template #trigger>
        <n-button type="error" :loading="stopping">
          <template #icon><n-icon><StopCircleOutline /></n-icon></template>
        </n-button>
      </template>
      Stop Workers &amp; Flush Queue
    </n-tooltip>
  </template>
  Cancel all active worker tasks and drop all pending queue items?
</n-popconfirm>
```

The header toolbar `n-space` should now contain four buttons in order: Refresh, Stop Workers, Flush Pending, Clear Failures.

- [ ] **Step 6: Build the Docker image to verify frontend + backend compile**

```bash
docker compose build --build-arg BUILDKIT_INLINE_CACHE=1 iran-watcher 2>&1 | tail -20
```

Expected: build succeeds.

- [ ] **Step 7: Commit**

```bash
git add web/src/views/QueueView.vue
git commit -m "feat: add Stop Workers & Flush Queue button to Queue page"
```

---

### Task 6: Update CLAUDE.md

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Update the `DELETE /api/workers/buffer` description in Key Design Decisions**

In the **Dead-letter queue** bullet point (or wherever `/api/workers/buffer` is referenced), update the description to reflect that `Flush()` is now a real implementation:

Find:
```
DELETE /api/queue` (flush all pending task queues)
```

This endpoint is mentioned in the **Dead-letter queue** decision. Add a note about the workers buffer endpoint nearby. In the **Event-driven actions system** section or the workers section, add or update the reference to include:

```
`DELETE /api/workers/buffer` — cancels the task-scoped context for every
actively-executing worker; workers finish cleanup and re-enter the BRPOP loop;
returns `{"flushed": N}` where N is the count of interrupted tasks
```

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: document DELETE /api/workers/buffer behaviour in CLAUDE.md"
```
