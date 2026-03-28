# Worker Pipeline & Backend Display Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show the pipeline name and (for Ollama) the active backend URL in the Queue page worker list, with "waiting" while the pipeline is blocked acquiring a backend.

**Architecture:** A context-based callback (`WithBackendNotify`) is injected by the Manager before calling `Pipeline.Run()`. `OllamaPipeline.sendChat()` calls the callback with `"waiting"` before entering `acquire()` and again with `b.name` once a backend is acquired. The Manager stores full `WorkerInfo` structs (not bare strings) in `workerActions`, updating them via the callback. `Stats()` reads these structs directly.

**Tech Stack:** Go (`context.WithValue`, `sync.Map`), Vue 3, Naive UI

---

### Task 1: Add backend-notify context helpers to the inference package

**Files:**
- Create: `internal/inference/backend_notify.go`

- [ ] **Step 1: Create the file**

```go
package inference

import "context"

// backendNotifyKey is the context key for backend notification callbacks.
type backendNotifyKey struct{}

// WithBackendNotify returns a context carrying a backend notification callback.
// The callback is called by OllamaPipeline when a backend transitions state:
//   - "waiting" — before blocking in acquire()
//   - b.name    — once a backend is acquired
//
// Non-Ollama pipelines never call this callback; the Backend field stays "".
func WithBackendNotify(ctx context.Context, fn func(url string)) context.Context {
	return context.WithValue(ctx, backendNotifyKey{}, fn)
}

// notifyBackend calls the registered callback if one is present in ctx.
// Safe to call even when no callback is set.
func notifyBackend(ctx context.Context, url string) {
	if fn, ok := ctx.Value(backendNotifyKey{}).(func(string)); ok {
		fn(url)
	}
}
```

- [ ] **Step 2: Build to verify**

```bash
go build ./internal/inference/...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/inference/backend_notify.go
git commit -m "feat: add WithBackendNotify context helper to inference package"
```

---

### Task 2: Wire `notifyBackend` into `OllamaPipeline.sendChat()`

**Files:**
- Modify: `internal/inference/ollama.go`

The `sendChat` function (line 613) contains a `for` loop that calls `p.acquire(ctx)`. Add two `notifyBackend` calls:
1. Once, before the loop — to signal "waiting"
2. Once, immediately after `p.acquire` returns successfully — to signal the acquired backend name

- [ ] **Step 1: Read `sendChat` to confirm the loop structure**

Read `internal/inference/ollama.go` lines 613–700 to confirm the `for` loop starts at line 643 with `b, err := p.acquire(ctx)`.

- [ ] **Step 2: Add the notify calls**

In `sendChat`, the `for` loop currently starts at:
```go
	for {
		b, err := p.acquire(ctx)
		if err != nil {
			return "", fmt.Errorf("inference: ollama: acquire: %w", err)
		}
```

Change it to:
```go
	notifyBackend(ctx, "waiting")
	for {
		b, err := p.acquire(ctx)
		if err != nil {
			return "", fmt.Errorf("inference: ollama: acquire: %w", err)
		}
		notifyBackend(ctx, b.name)
```

Only these two lines change. `notifyBackend(ctx, "waiting")` is placed once before the loop (not inside it) because "waiting" is the state before any backend is held. `notifyBackend(ctx, b.name)` is placed immediately after a successful `acquire` — on retry (after `markUnhealthy`), the loop will call `acquire` again and re-notify with the new backend name.

- [ ] **Step 3: Build to verify**

```bash
go build ./internal/inference/...
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/inference/ollama.go
git commit -m "feat: notify backend context callback in OllamaPipeline.sendChat"
```

---

### Task 3: Extend `WorkerInfo` with `Pipeline` and `Backend` fields

**Files:**
- Modify: `internal/worker/pool.go`

- [ ] **Step 1: Update the `WorkerInfo` struct**

In `internal/worker/pool.go`, replace:
```go
// WorkerInfo describes a single worker currently processing a task.
type WorkerInfo struct {
	ID     int    `json:"id"`
	Action string `json:"action"`
}
```

With:
```go
// WorkerInfo describes a single worker currently processing a task.
type WorkerInfo struct {
	ID       int    `json:"id"`
	Action   string `json:"action"`
	Pipeline string `json:"pipeline"`
	Backend  string `json:"backend,omitempty"` // empty for non-Ollama; "waiting" or backend name for Ollama
}
```

- [ ] **Step 2: Build to verify**

```bash
go build ./internal/worker/...
```

Expected: no errors (existing code sets only `ID` and `Action` by name — new fields default to `""`).

- [ ] **Step 3: Commit**

```bash
git add internal/worker/pool.go
git commit -m "feat: add Pipeline and Backend fields to WorkerInfo"
```

---

### Task 4: Update Manager to store `WorkerInfo` structs and inject the backend callback

**Files:**
- Modify: `internal/worker/manager.go`

Two changes:

**A) `processTask`** — currently stores `entry.Action.Name` (a `string`); change to store a full `WorkerInfo`, inject the backend callback via `inference.WithBackendNotify`.

**B) `Stats()`** — currently does `v.(string)` type assertion; change to `v.(WorkerInfo)`.

- [ ] **Step 1: Update `processTask`**

The current block (around lines 123–143):
```go
	// Track which action this worker is executing.
	m.workerActions.Store(workerID, entry.Action.Name)
	defer m.workerActions.Delete(workerID)

	var ev queue.TweetEvent
	_ = json.Unmarshal(task.Event, &ev)

	mergedCtx := make(map[string]any, len(task.Context)+3)
	for k, v := range task.Context {
		mergedCtx[k] = v
	}
	mergedCtx["tweet_id"] = ev.TweetID
	mergedCtx["tweet_author"] = ev.AuthorHandle

	input := inference.PipelineInput{
		Text:    ev.Text,
		Context: mergedCtx,
	}

	startTime := time.Now()
	outputs, runErr := entry.Pipeline.Run(ctx, input)
```

Replace with:
```go
	// Track which action this worker is executing.
	info := WorkerInfo{
		ID:       workerID,
		Action:   entry.Action.Name,
		Pipeline: entry.Pipeline.Name(),
	}
	m.workerActions.Store(workerID, info)
	defer m.workerActions.Delete(workerID)

	var ev queue.TweetEvent
	_ = json.Unmarshal(task.Event, &ev)

	mergedCtx := make(map[string]any, len(task.Context)+3)
	for k, v := range task.Context {
		mergedCtx[k] = v
	}
	mergedCtx["tweet_id"] = ev.TweetID
	mergedCtx["tweet_author"] = ev.AuthorHandle

	input := inference.PipelineInput{
		Text:    ev.Text,
		Context: mergedCtx,
	}

	// Inject backend callback so Ollama pipelines can report which backend they acquire.
	inferenceCtx := inference.WithBackendNotify(ctx, func(backend string) {
		m.workerActions.Store(workerID, WorkerInfo{
			ID:       workerID,
			Action:   entry.Action.Name,
			Pipeline: entry.Pipeline.Name(),
			Backend:  backend,
		})
	})

	startTime := time.Now()
	outputs, runErr := entry.Pipeline.Run(inferenceCtx, input)
```

Note: `inferenceCtx` is only passed to `Run`. All other calls (`SaveActionExecution`, `evaluateChains`) continue to use the original `ctx`.

- [ ] **Step 2: Update `Stats()` to read `WorkerInfo` structs**

Replace the `Range` block in `Stats()`:
```go
	var activeWorkers []WorkerInfo
	m.workerActions.Range(func(k, v any) bool {
		activeWorkers = append(activeWorkers, WorkerInfo{
			ID:     k.(int),
			Action: v.(string),
		})
		return true
	})
```

With:
```go
	var activeWorkers []WorkerInfo
	m.workerActions.Range(func(_, v any) bool {
		activeWorkers = append(activeWorkers, v.(WorkerInfo))
		return true
	})
```

(The key `k` is no longer needed since `WorkerInfo.ID` is already in the stored struct.)

- [ ] **Step 3: Build to verify**

```bash
go build ./internal/worker/...
```

Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add internal/worker/manager.go
git commit -m "feat: store WorkerInfo structs in workerActions, inject backend callback"
```

---

### Task 5: Update worker tests for new `WorkerInfo` fields

**Files:**
- Modify: `internal/worker/worker_test.go`

The existing `TestManager_Stats_ActiveWorkers` test was written when `WorkerInfo` only had `ID` and `Action`. It needs to also assert `Pipeline` is populated. The `Stats()` change (now reads `v.(WorkerInfo)`) will panic if the stored value is still a `string` — but since Task 4 changed `processTask` to store `WorkerInfo`, this is consistent.

- [ ] **Step 1: Update `TestManager_Stats_ActiveWorkers` to assert Pipeline**

Find the block that checks `stats.ActiveWorkers[0]`:
```go
	if stats.ActiveWorkers[0].Action != "slow-action" {
		t.Errorf("expected action 'slow-action', got %q", stats.ActiveWorkers[0].Action)
	}
```

Add a Pipeline assertion immediately after:
```go
	if stats.ActiveWorkers[0].Action != "slow-action" {
		t.Errorf("expected action 'slow-action', got %q", stats.ActiveWorkers[0].Action)
	}
	if stats.ActiveWorkers[0].Pipeline != "slow-action" {
		t.Errorf("expected pipeline 'slow-action', got %q", stats.ActiveWorkers[0].Pipeline)
	}
```

(In the test, the action name and pipeline name are both `"slow-action"` — `action.Name = "slow-action"` and `stub.name = "slow-action"`.)

- [ ] **Step 2: Build the package to catch any type assertion panics**

```bash
go build ./internal/worker/...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/worker/worker_test.go
git commit -m "test: assert Pipeline field in TestManager_Stats_ActiveWorkers"
```

---

### Task 6: Update the frontend Queue page worker table

**Files:**
- Modify: `web/src/views/FailuresView.vue`

Two changes: update the TypeScript type, and add Pipeline + Backend columns to the active workers table.

- [ ] **Step 1: Update `WorkerStats` interface**

Find:
```ts
interface WorkerStats {
  total: number
  active: number
  idle: number
  active_workers?: Array<{ id: number; action: string }>
}
```

Replace with:
```ts
interface WorkerStats {
  total: number
  active: number
  idle: number
  active_workers?: Array<{ id: number; action: string; pipeline: string; backend: string }>
}
```

- [ ] **Step 2: Replace the active workers `<n-data-table>` columns**

Find the current columns definition (in the `<!-- Active workers -->` card):
```html
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
```

Replace with:
```html
      <n-data-table
        v-else
        :columns="[
          { title: 'Worker', key: 'id', width: 90,
            render: (row: { id: number; action: string; pipeline: string; backend: string }) =>
              h('span', { style: { color: '#585b70', fontVariantNumeric: 'tabular-nums' } }, `#${row.id}`)
          },
          { title: 'Action', key: 'action',
            render: (row: { id: number; action: string; pipeline: string; backend: string }) =>
              h('span', { style: { color: '#a6e3a1' } }, row.action)
          },
          { title: 'Pipeline', key: 'pipeline',
            render: (row: { id: number; action: string; pipeline: string; backend: string }) =>
              h('span', { style: { color: '#cdd6f4' } }, row.pipeline || '—')
          },
          { title: 'Backend', key: 'backend', width: 180,
            render: (row: { id: number; action: string; pipeline: string; backend: string }) => {
              if (!row.backend) return h('span', { style: { color: '#585b70' } }, '—')
              if (row.backend === 'waiting') return h('span', { style: { color: '#585b70' } }, 'waiting…')
              const display = row.backend.replace(/^https?:\/\//, '')
              return h('span', { style: { color: '#cdd6f4', fontVariantNumeric: 'tabular-nums' } }, display)
            }
          },
        ]"
        :data="workerStats.active_workers"
        :pagination="false"
        size="small"
      />
```

- [ ] **Step 3: TypeScript check**

```bash
cd /mnt/shared/projects/personal/iran-watcher/web && npm run build
```

Expected: build succeeds with no type errors.

- [ ] **Step 4: Commit**

```bash
git add web/src/views/FailuresView.vue
git commit -m "feat: add Pipeline and Backend columns to active workers table"
```

---

### Task 7: Build verification

- [ ] **Step 1: Run all Go tests**

```bash
go test ./...
```

Expected: all pass (or CGO link errors on non-Docker environments — those are expected per CLAUDE.md).

- [ ] **Step 2: Docker build**

```bash
docker compose build --build-arg BUILDKIT_INLINE_CACHE=1 iran-watcher
```

Expected: build succeeds with no errors.

- [ ] **Step 3: Commit any fixups**

If the build exposed issues not caught earlier:

```bash
git add <changed files>
git commit -m "fix: resolve build issues from pipeline backend display changes"
```
