# Queue ETA Design

**Date:** 2026-03-30
**Status:** Approved

## Goal

Show an estimated time to drain (ETA) for each per-action queue and for the overall Redis pending count on the Queue page. ETAs are grounded in actual execution throughput, updated live via WebSocket.

## Approach

The backend maintains a per-action sliding window of the last N completion timestamps in memory. Throughput (items/sec) is derived from that window and broadcast on every `worker_stats` WebSocket event and returned in the initial `GET /api/queue` HTTP response. The frontend computes ETA = `pending / throughput` and displays it inline.

## Backend

### Sliding Window (`internal/worker/manager.go`)

`Manager` gains a `completionWindows sync.Map` mapping `action_id int64 → []time.Time`.

```go
const completionWindowSize = 20
```

`recordCompletion(actionID int64)` is called at the end of `processTask` (after saving the execution record, regardless of success or error):

1. Load existing slice for the action (or empty slice).
2. Append `time.Now()`.
3. Trim to the last `completionWindowSize` entries.
4. Store back.

`throughputPerAction() map[int64]float64` iterates `completionWindows`:

- `< 2` samples → rate = 0 (insufficient data)
- Otherwise → `rate = float64(len-1) / last.Sub(first).Seconds()`

### `PoolStats` struct

Add `ActionThroughput map[int64]float64 `json:"action_throughput"`` to `PoolStats`. `Stats()` calls `throughputPerAction()` to populate it.

### `GET /api/queue` response

Each queue entry gains `throughput float64` (items/sec), sourced from `pool.Stats().ActionThroughput[entry.ActionID]`. If the action has no window entry the field is `0`.

```json
{
  "action_id": 123,
  "action_name": "classify_sentiment",
  "pipeline_name": "hf-zero-shot",
  "pipeline_type": "onnx",
  "pending": 45,
  "workers": [...],
  "throughput": 0.42
}
```

### `worker_stats` WebSocket event

`broadcastWorkerStats()` calls `pool.Stats()` which now includes `action_throughput`. No change to the broadcast call site — the richer struct serialises automatically.

```json
{
  "type": "worker_stats",
  "payload": {
    "total": 10,
    "active": 3,
    "idle": 7,
    "active_workers": [...],
    "action_throughput": {
      "123": 0.42,
      "124": 1.1
    }
  }
}
```

## Frontend

### Queue store (`stores/queues.ts`)

`QueueEntry` gains:

```ts
throughput: number  // items/sec; 0 = not yet established
```

**`setQueues(entries)`** — maps `entry.throughput` (or `0` if absent) from the HTTP response.

**`setWorkerStats(stats)`** — reads `stats.action_throughput` (a `Record<number, number>`) and updates each matching queue entry's `throughput` in place. Entries not present in the map are left unchanged (keeps last known rate).

**`perActionEta(entry: QueueEntry): string | null`** helper:

```ts
function perActionEta(entry: QueueEntry): string | null {
  if (entry.throughput === 0 || entry.pending === 0) return null
  return fmtEta(Math.ceil(entry.pending / entry.throughput))
}
```

Uses the existing `fmtEta` utility already in the component.

### Overall ETA (`views/QueueView.vue`)

Remove the `pendingHistory` rolling-sample buffer and replace `drainEta` with:

```ts
const drainEta = computed<string | null>(() => {
  const totalThroughput = queuesStore.queues.reduce((s, q) => s + q.throughput, 0)
  if (totalThroughput === 0 || queuesStore.totalPending === 0) return null
  return fmtEta(Math.ceil(queuesStore.totalPending / totalThroughput))
})
```

### Queue table UI

**Active queues table:** Add an "ETA" column after "Pending". Each row shows:

- `perActionEta(row)` if available, formatted duration (e.g. `2m 14s`)
- A small muted rate badge: `(0.4/s)` — styled as secondary text, same line or below the ETA
- `—` if throughput is 0 (no data yet)

**Inactive queues table:** No ETA column — all pending counts are 0.

**Overall Redis Pending card:** ETA string is replaced by the new `drainEta` computed. No visual layout change.

## Constants

| Name | Value | Location |
|------|-------|----------|
| `completionWindowSize` | 20 | `internal/worker/manager.go` |

## Data Flow

```
task completes
  → recordCompletion(actionID)          // append timestamp, trim to 20
  → broadcastWorkerStats()              // calls pool.Stats() → throughputPerAction()
  → WS event: worker_stats { action_throughput: { id: rate } }
  → queuesStore.setWorkerStats()        // updates throughput per entry
  → QueueView re-renders ETA columns
```

On page load:
```
GET /api/queue
  → response includes throughput per entry
  → queuesStore.setQueues()
  → QueueView renders with ETA on first paint (if window has ≥ 2 samples)
```

## Edge Cases

- **New action / cold start:** `throughput = 0`, ETA shows `—`. Resolves after first 2 completions.
- **Action goes idle:** Window retains last N timestamps indefinitely — rate stays non-zero but reflects the historical rate, not current activity. This is acceptable; the ETA will update naturally once the action resumes processing.
- **Single worker, slow pipeline:** With only 1 completion in the window, rate = 0 until a second completion lands.
- **Negative drain (queue growing):** ETA is computed from throughput alone, not drain rate. If pending is growing faster than throughput, ETA reflects optimistic drain time assuming current throughput holds. No special handling needed.
