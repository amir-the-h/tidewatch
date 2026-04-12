# Queue ETA Design

**Date:** 2026-04-12
**Status:** Approved

## Overview

Show separate estimated time-to-empty for the non-Ollama task queue and the Ollama task queue, displayed on both the Queue page stat cards and the app header.

## Data Model

Add a `pendingHistory` rolling buffer to `useQueuesStore`:

```ts
interface PendingSnapshot { ts: number; tasks: number; ollama: number }
const pendingHistory = ref<PendingSnapshot[]>([])
```

Every `fetchCounts()` call appends a snapshot and trims entries older than 60 seconds. This gives a live 60-second window of (timestamp, pending counts) observations.

## ETA Computation

Two computed properties: `etaTasks` and `etaOllama`.

For each:
1. Return `null` if current pending is 0.
2. Return `null` if the window spans less than 5 seconds (not enough data).
3. Compute rate from the oldest and newest entries in the window:
   ```
   rate = (oldest.pending - newest.pending) / (newest.ts - oldest.ts)  [tasks/ms]
   ```
4. Return `null` if rate ≤ 0 (queue is growing or flat — no meaningful ETA).
5. ETA (ms) = `current.pending / rate`.

## Formatting

A shared `formatEta(ms: number | null): string` utility:

| Condition | Display |
|-----------|---------|
| `null` | `—` |
| < 10 000 ms | `~<10s` |
| < 60 000 ms | `~Xs` |
| < 3 600 000 ms | `~Xm Ys` |
| ≥ 3 600 000 ms | `>1h` |

## Display Locations

### Queue Page (`QueueView.vue`)

Under each of the two pending stat cards, add a small secondary line showing the ETA. Example:

```
Pending (tasks)
42
ETA ~3m 12s
```

Hidden (or shows `—`) when ETA is null.

### Header (`App.vue`)

Next to the existing pending count badge, add a small dim ETA label. Only rendered when ETA is non-null (i.e., queue is actively draining). Keeps the header uncluttered when idle.

## Files Changed

- `web/src/stores/queues.ts` — add `pendingHistory`, `etaTasks`, `etaOllama`
- `web/src/utils/format.ts` — add `formatEta`
- `web/src/views/QueueView.vue` — add ETA secondary lines to stat cards
- `web/src/App.vue` — add ETA label next to pending badge in header

No backend changes required.
