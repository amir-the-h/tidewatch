# Worker Pipeline & Backend Display Design

**Date:** 2026-03-28
**Status:** Approved

## Goal

Extend the Queue page worker list to show, for each active worker: the pipeline it is running through, and (for Ollama pipelines) which backend it is currently using — or "waiting…" while blocked in `acquire()` waiting for a free backend.

## Background

`WorkerInfo` currently carries only `ID` and `Action`. The pipeline name is available at task-start time (`entry.Pipeline.Name()`). The Ollama backend URL is only known once `OllamaPipeline.sendChat()` wins the `acquire()` race — before that the worker is blocked waiting for a free backend from the pool.

## Data Model

`WorkerInfo` gains two new fields:

```go
type WorkerInfo struct {
    ID       int    `json:"id"`
    Action   string `json:"action"`
    Pipeline string `json:"pipeline"`
    Backend  string `json:"backend,omitempty"` // empty for non-Ollama; "waiting" or URL for Ollama
}
```

`Backend` lifecycle for an Ollama task:
1. Task starts → `Backend = ""`
2. `sendChat` enters acquire loop → `Backend = "waiting"`
3. Backend acquired → `Backend = b.name` (e.g. `http://192.168.1.100:11434`)
4. Task finishes → entry deleted from `workerActions`

For non-Ollama pipelines, `Backend` is never set and remains `""` throughout.

## Backend Changes

### `internal/inference/` — context callback helpers

Add two unexported helpers (in `pipeline.go` or a new `backend_notify.go`):

```go
type backendNotifyKey struct{}

// WithBackendNotify returns ctx carrying a backend notification callback.
func WithBackendNotify(ctx context.Context, fn func(url string)) context.Context {
    return context.WithValue(ctx, backendNotifyKey{}, fn)
}

// notifyBackend calls the registered callback if one is present in ctx.
func notifyBackend(ctx context.Context, url string) {
    if fn, ok := ctx.Value(backendNotifyKey{}).(func(string)); ok {
        fn(url)
    }
}
```

No changes to the `Pipeline` interface — this is purely internal to the inference package and the manager.

### `internal/inference/ollama.go` — `sendChat()`

Two call sites added:

1. Before the acquire loop begins: `notifyBackend(ctx, "waiting")`
2. Immediately after a backend `b` is acquired: `notifyBackend(ctx, b.name)`

If the acquire loop retries (backend error), these fire again on the next iteration — the frontend will show the latest value.

### `internal/worker/manager.go` — `processTask()`

Replace the current `workerActions.Store(workerID, entry.Action.Name)` pattern with storing a full `WorkerInfo` struct, and inject the backend callback:

```go
info := WorkerInfo{
    ID:       workerID,
    Action:   entry.Action.Name,
    Pipeline: entry.Pipeline.Name(),
}
m.workerActions.Store(workerID, info)
defer m.workerActions.Delete(workerID)

inferenceCtx := inference.WithBackendNotify(ctx, func(url string) {
    updated := WorkerInfo{
        ID:       workerID,
        Action:   entry.Action.Name,
        Pipeline: entry.Pipeline.Name(),
        Backend:  url,
    }
    m.workerActions.Store(workerID, updated)
})

outputs, runErr := entry.Pipeline.Run(inferenceCtx, input)
```

### `internal/worker/pool.go` — `Stats()`

Update the `Range` snapshot to read `WorkerInfo` structs instead of bare strings:

```go
m.workerActions.Range(func(k, v any) bool {
    activeWorkers = append(activeWorkers, v.(WorkerInfo))
    return true
})
```

## Frontend Changes

### `web/src/views/FailuresView.vue`

**Update `WorkerStats` interface** (the `active_workers` element type):
```ts
active_workers?: Array<{ id: number; action: string; pipeline: string; backend: string }>
```

**Add Pipeline and Backend columns** to the active workers `<n-data-table>`:

| Column | Width | Rendering |
|--------|-------|-----------|
| Worker | 90px | `#N` in muted color |
| Action | — | action name in green |
| Pipeline | — | pipeline name in default color |
| Backend | 180px | see below |

Backend cell rendering:
- `""` → `"—"` (muted, non-Ollama or pre-start)
- `"waiting"` → `"waiting…"` (muted/dimmed color `#585b70`)
- URL → strip `http://` or `https://` prefix, show `host:port` (e.g. `192.168.1.100:11434`) in default color

URL stripping: `url.replace(/^https?:\/\//, '')`

## What Is Not Changed

- The `Pipeline` interface — no new methods
- The health endpoint — `PoolStats` serialises `ActiveWorkers` automatically
- Polling interval (3s) unchanged
- Dead-letter table and drawer unchanged
