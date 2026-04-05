# Stop Workers & Flush Queue

**Date:** 2026-04-05
**Status:** Approved

## Summary

Add a "Stop Workers & Flush Queue" button to the Queue page that cancels all
in-flight worker tasks and drains all pending Redis task queues in one action.

## Background

Workers share a root context from `Manager.Start(ctx)`. There is no mechanism
to interrupt a pipeline execution that is already in progress. `Flush()` and
`FlushPipeline()` are currently no-ops. The `DELETE /api/workers/buffer` route
is already registered but calls the no-op implementation.

## Backend — `internal/worker/manager.go`

Add a `workerCancels sync.Map` field to `Manager` (key: `int` worker ID,
value: `context.CancelFunc`).

In `runWorker`, before each `processTask` call:

```
taskCtx, cancel := context.WithCancel(ctx)
m.workerCancels.Store(id, cancel)
```

After `processTask` returns (deferred):

```
cancel()
m.workerCancels.Delete(id)
```

Pass `taskCtx` to `processTask` instead of `ctx`.

`Flush()` implementation:

```
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

`FlushPipeline()` remains a no-op (no use case for per-pipeline interruption
from the UI).

### Cancellation behaviour by pipeline type

| Pipeline type | Effect of context cancel |
|---|---|
| Ollama | HTTP request to Ollama drops immediately |
| ONNX | Inference runs to completion; results are discarded, no downstream tasks pushed |
| Internal (entity_matcher, etc.) | Stops at next context check |
| Notification | HTTP/Telegram call drops if context is checked by the HTTP client |

Workers that are idle (blocked in BRPOP with a 1 s timeout) are not affected —
they will naturally unblock on the next poll cycle and re-enter the loop with a
fresh task context.

## API — `internal/api/handlers_health.go`

`handleFlushWorkerBuffer` already calls `flusher.Flush()` and returns
`{"flushed": N}`. No changes needed once the Manager implementation is real.

## Frontend — `web/src/api/queue.ts`

Add one export:

```ts
export const flushWorkerBuffer = () =>
  apiFetch<{ flushed: number }>('/api/workers/buffer', { method: 'DELETE' })
```

## Frontend — `web/src/views/QueueView.vue`

### Imports

Add `flushWorkerBuffer` to the import from `../api/queue`.
Add `StopCircleOutline` to the import from `@vicons/ionicons5`.

### State

```ts
const stopping = ref(false)
```

### Handler

```ts
async function onStopWorkers() {
  stopping.value = true
  try {
    await flushWorkerBuffer()         // cancel in-flight tasks
    await flushQueue()                 // drain pending queues
    msg.success('Workers stopped and queue flushed')
    await Promise.all([store.fetch(), fetchStats(), fetchQueues()])
  } catch (e: any) { msg.error(e.message) }
  finally { stopping.value = false }
}
```

### Button

Add to the header toolbar, to the left of the existing "Flush Pending" button:

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

## What is NOT changed

- `FlushPipeline()` remains a no-op.
- The existing "Flush Pending" (`BanOutline`) button is unchanged.
- No new API routes — `DELETE /api/workers/buffer` already exists.
- No changes to ONNX/Ollama/internal pipeline implementations.
- No changes to the worker goroutine lifecycle or `Start(ctx)`.
