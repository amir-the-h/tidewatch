# Queue Page Worker Status Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expose per-worker action status from the Manager, and clean up the Queue page to remove dead sections and show a live worker list.

**Architecture:** Add a `sync.Map` to `Manager` that tracks workerID → action name during task execution; surface it via the existing `PoolStats` struct and `/api/health` endpoint; update the Vue frontend to remove the three dead UI sections and render the worker list.

**Tech Stack:** Go (`sync.Map`, `PoolStats`), Naive UI (`n-data-table`, `n-empty`), Vue 3 Composition API

---

### Task 1: Extend `PoolStats` with `WorkerInfo` and `ActiveWorkers`

**Files:**
- Modify: `internal/worker/pool.go`

- [ ] **Step 1: Read the current `PoolStats` struct**

Open `internal/worker/pool.go` and note the current struct (lines 16–24). It currently has `Total`, `Active`, `Idle`, `Queued`, `Pipelines`, `PipelineActive`, `PipelineRate`.

- [ ] **Step 2: Replace `PoolStats` and add `WorkerInfo`**

Replace the existing `PoolStats` struct with:

```go
// WorkerInfo describes a single worker currently processing a task.
type WorkerInfo struct {
	ID     int    `json:"id"`
	Action string `json:"action"`
}

// PoolStats holds a snapshot of worker pool activity.
type PoolStats struct {
	Total         int          `json:"total"`
	Active        int          `json:"active"`
	Idle          int          `json:"idle"`
	ActiveWorkers []WorkerInfo `json:"active_workers"`
}
```

Remove the dead fields (`Queued`, `Pipelines`, `PipelineActive`, `PipelineRate`) — they are never populated by any real code path.

- [ ] **Step 3: Verify the build compiles**

```bash
go build ./internal/worker/...
```

Expected: no errors. If anything in the codebase referenced the removed fields (`Queued`, `Pipelines`, `PipelineActive`, `PipelineRate`), the compiler will tell you — fix those references by removing them too.

- [ ] **Step 4: Commit**

```bash
git add internal/worker/pool.go
git commit -m "feat: add WorkerInfo and ActiveWorkers to PoolStats, drop dead fields"
```

---

### Task 2: Track per-worker action state in `Manager`

**Files:**
- Modify: `internal/worker/manager.go`

- [ ] **Step 1: Add `workerActions sync.Map` to `Manager`**

In `internal/worker/manager.go`, add the field to the `Manager` struct:

```go
type Manager struct {
	totalWorkers  int
	consumer      queue.Consumer
	producer      queue.Producer
	store         store.Store
	cache         *ActionCache
	logger        *slog.Logger
	active        atomic.Int32
	workerActions sync.Map // key: int (worker ID), value: string (action name)
}
```

`sync.Map` is already imported via `"sync"` — no new import needed.

- [ ] **Step 2: Update `processTask` to write and clear worker state**

In `processTask`, after `entry := m.cache.Get(task.ActionID)` succeeds (i.e., after the nil-check early return), add the store and deferred delete:

```go
func (m *Manager) processTask(ctx context.Context, workerID int, task *queue.Task) {
	m.active.Add(1)
	defer m.active.Add(-1)

	entry := m.cache.Get(task.ActionID)
	if entry == nil {
		if m.logger != nil {
			m.logger.Warn("worker: unknown action_id in task",
				"action_id", task.ActionID, "trigger", task.TriggerType)
		}
		return
	}

	// Track which action this worker is executing.
	m.workerActions.Store(workerID, entry.Action.Name)
	defer m.workerActions.Delete(workerID)

	// ... rest of the function unchanged
```

Only the two lines (`m.workerActions.Store` and the deferred `m.workerActions.Delete`) are new. Everything else in `processTask` stays identical.

- [ ] **Step 3: Update `Stats()` to snapshot `workerActions`**

Replace the current `Stats()` method:

```go
// Stats returns a snapshot of worker pool activity.
func (m *Manager) Stats() PoolStats {
	active := int(m.active.Load())
	idle := m.totalWorkers - active
	if idle < 0 {
		idle = 0
	}

	var activeWorkers []WorkerInfo
	m.workerActions.Range(func(k, v any) bool {
		activeWorkers = append(activeWorkers, WorkerInfo{
			ID:     k.(int),
			Action: v.(string),
		})
		return true
	})

	return PoolStats{
		Total:         m.totalWorkers,
		Active:        active,
		Idle:          idle,
		ActiveWorkers: activeWorkers,
	}
}
```

- [ ] **Step 4: Build to verify**

```bash
go build ./internal/worker/...
```

Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add internal/worker/manager.go
git commit -m "feat: track per-worker action state in Manager.Stats()"
```

---

### Task 3: Test `Manager.Stats()` reports active workers

**Files:**
- Modify: `internal/worker/worker_test.go`

- [ ] **Step 1: Write a failing test**

Add this test after `TestManager_ProcessesTweet` in `internal/worker/worker_test.go`:

```go
func TestManager_Stats_ActiveWorkers(t *testing.T) {
	// blockingPipeline blocks until released, letting us inspect Stats() mid-run.
	unblock := make(chan struct{})
	started := make(chan struct{})

	var blockingPipe struct {
		inference.Pipeline
	}
	// embed stubPipeline for Name/Type/Load/Close
	stub := &stubPipeline{name: "slow-action", pType: inference.PipelineTypeONNX}

	// We need a real Pipeline implementation that blocks.
	// Define it inline using a struct that satisfies the interface.
	pipe := &blockPipeline{stub: stub, started: started, unblock: unblock}

	action := &model.Action{ID: 1, Name: "slow-action", TriggerType: model.TriggerTypeNewTweet, Active: true}
	cache := buildCacheWithEntries(map[int64]*CacheEntry{
		1: {Action: action, Pipeline: pipe},
	})

	consumer := &mockConsumer{tasks: []*queue.Task{makeTask(1, "tweet-stats")}}
	producer := &mockProducer{}
	storeImpl := &mockStore{}

	mgr := NewManager(2, consumer, producer, storeImpl, cache, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = mgr.Start(ctx) }()

	// Wait until the pipeline has started executing.
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("pipeline did not start within 2s")
	}

	stats := mgr.Stats()
	if stats.Active != 1 {
		t.Errorf("expected 1 active worker, got %d", stats.Active)
	}
	if len(stats.ActiveWorkers) != 1 {
		t.Fatalf("expected 1 entry in ActiveWorkers, got %d", len(stats.ActiveWorkers))
	}
	if stats.ActiveWorkers[0].Action != "slow-action" {
		t.Errorf("expected action 'slow-action', got %q", stats.ActiveWorkers[0].Action)
	}

	// Unblock and verify workers clear.
	close(unblock)
	time.Sleep(50 * time.Millisecond)

	stats = mgr.Stats()
	if len(stats.ActiveWorkers) != 0 {
		t.Errorf("expected 0 active workers after completion, got %d", len(stats.ActiveWorkers))
	}
}
```

Also add the `blockPipeline` type at the bottom of the test file:

```go
// blockPipeline is a Pipeline that signals started and blocks until unblock is closed.
type blockPipeline struct {
	stub    *stubPipeline
	started chan struct{}
	unblock chan struct{}
}

func (b *blockPipeline) Name() string                 { return b.stub.Name() }
func (b *blockPipeline) Type() inference.PipelineType { return b.stub.Type() }
func (b *blockPipeline) Load(s string) error          { return b.stub.Load(s) }
func (b *blockPipeline) Close() error                 { return b.stub.Close() }
func (b *blockPipeline) Run(_ context.Context, _ inference.PipelineInput) (map[string]any, error) {
	select {
	case b.started <- struct{}{}:
	default:
	}
	<-b.unblock
	return map[string]any{"label": "test"}, nil
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
go test ./internal/worker/... -run TestManager_Stats_ActiveWorkers -v
```

Expected: FAIL — `ActiveWorkers` will be nil (0 entries) because the tracking isn't implemented yet. (If Task 2 was done first, this should already pass — run it to confirm.)

- [ ] **Step 3: Run all worker tests**

```bash
go test ./internal/worker/... -v
```

Expected: all tests pass.

- [ ] **Step 4: Commit**

```bash
git add internal/worker/worker_test.go
git commit -m "test: verify Manager.Stats() reports active workers mid-execution"
```

---

### Task 4: Update the frontend Queue page

**Files:**
- Modify: `web/src/views/FailuresView.vue`

- [ ] **Step 1: Remove dead imports and dead script-section code**

In the `<script setup>` block, remove:
- `TrashBinOutline` from the `@vicons/ionicons5` import
- The `flushingPipeline` ref and `onFlushPipeline` function (lines 14–27)
- The `PipelineRow` interface (line 97)
- The `formatEta` function (lines 99–110)
- The `pipelineRows` computed (lines 112–121)

Also update the `WorkerStats` interface to match the real API shape:

```ts
interface WorkerStats {
  total: number
  active: number
  idle: number
  active_workers?: Array<{ id: number; action: string }>
}
```

Remove the `queued`, `pipelines`, `pipeline_active`, `pipeline_rate` fields from the interface.

- [ ] **Step 2: Update the stats cards grid from 4 columns to 3**

Replace the entire `<!-- Stats cards -->` `<n-grid>` block with:

```html
<!-- Stats cards -->
<n-grid :cols="3" :x-gap="12" :y-gap="12" responsive="screen" :item-responsive="true">
  <n-gi span="3 s:1">
    <n-card size="small" embedded>
      <n-statistic label="Redis Pending">
        <template #default>
          <span :style="{ color: (queueStats?.pending ?? 0) > 0 ? '#f9e2af' : '#cdd6f4' }">
            {{ queueStats?.pending ?? '—' }}
          </span>
        </template>
      </n-statistic>
    </n-card>
  </n-gi>
  <n-gi span="3 s:1">
    <n-card size="small" embedded>
      <n-statistic label="Dead-Letter">
        <template #default>
          <span :style="{ color: (queueStats?.failed ?? 0) > 0 ? '#f38ba8' : '#cdd6f4' }">
            {{ queueStats?.failed ?? '—' }}
          </span>
        </template>
      </n-statistic>
    </n-card>
  </n-gi>
  <n-gi span="3 s:1">
    <n-card size="small" embedded>
      <n-statistic label="Workers">
        <template #default>
          <span :style="{ color: (workerStats?.active ?? 0) > 0 ? '#a6e3a1' : '#cdd6f4' }">
            {{ workerStats?.active ?? '—' }}
          </span>
          <span style="color: #585b70; font-size: 14px;"> / {{ workerStats?.total ?? '—' }}</span>
        </template>
        <template #suffix>
          <span v-if="workerStats" style="color: #585b70; font-size: 12px; margin-left: 8px;">
            {{ workerStats.idle }} idle
          </span>
        </template>
      </n-statistic>
      <div v-if="workerStats" style="margin-top: 8px; height: 4px; border-radius: 2px; background: rgba(255,255,255,0.08); overflow: hidden;">
        <div :style="{
          height: '100%',
          width: workerStats.total > 0 ? (workerStats.active / workerStats.total * 100) + '%' : '0%',
          background: workerStats.active > 0 ? '#a6e3a1' : 'rgba(255,255,255,0.15)',
          transition: 'width 0.5s ease',
        }" />
      </div>
    </n-card>
  </n-gi>
</n-grid>
```

- [ ] **Step 3: Replace the per-pipeline breakdown block with the active workers section**

Remove the entire:
```html
<!-- Per-pipeline breakdown -->
<n-card v-if="pipelineRows.length > 0" ...>
  ...
</n-card>
```

Replace it with:

```html
<!-- Active workers -->
<n-card v-if="workerStats" size="small" title="Workers">
  <n-empty
    v-if="!workerStats.active_workers || workerStats.active_workers.length === 0"
    :description="`All ${workerStats.total} workers idle`"
    style="padding: 16px 0;"
  />
  <n-data-table
    v-else
    :columns="[
      { title: 'Worker', key: 'id', width: 90,
        render: (row: { id: number; action: string }) =>
          h('span', { style: { color: '#585b70', fontVariantNumeric: 'tabular-nums' } }, `#${row.id}`)
      },
      { title: 'Action', key: 'action',
        render: (row: { id: number; action: string }) =>
          h('span', { style: { color: '#a6e3a1' } }, row.action)
      },
    ]"
    :data="workerStats.active_workers"
    :pagination="false"
    size="small"
  />
</n-card>
```

- [ ] **Step 4: Verify no TypeScript errors**

```bash
cd web && npx vue-tsc --noEmit
```

Expected: no errors. Fix any type errors that arise from the removed fields.

- [ ] **Step 5: Commit**

```bash
git add web/src/views/FailuresView.vue
git commit -m "feat: redesign Queue page — remove dead sections, add active worker list"
```

---

### Task 5: Build verification

- [ ] **Step 1: Run all Go tests**

```bash
go test ./...
```

Expected: all pass.

- [ ] **Step 2: Build the Docker image**

```bash
docker compose build --build-arg BUILDKIT_INLINE_CACHE=1 iran-watcher
```

Expected: build succeeds with no errors.

- [ ] **Step 3: Commit if any fixups were needed**

If the build exposed any compilation issues not caught in earlier tasks, fix and commit them now:

```bash
git add <changed files>
git commit -m "fix: resolve build issues from queue page worker status changes"
```
