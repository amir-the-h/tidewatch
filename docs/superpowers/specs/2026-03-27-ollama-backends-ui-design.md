# Ollama Backends Visualization Page Design

**Date:** 2026-03-27
**Status:** Approved

## Problem

There is no way to see the health, performance scores, or activity of individual Ollama backends at runtime. With multiple machines (desktop, MacBook) in the pool, operators need visibility into which backends are up, how fast they are, and when they last handled a request.

## Goals

- Standalone admin page showing all Ollama backends grouped by pipeline
- Per-backend: healthy/unhealthy status, in-use indicator, tokens/sec score, request count, last active time
- Auto-refreshes every 5 seconds

---

## Backend Changes

### 1. State additions to `ollamaBackend`

```go
type ollamaBackend struct {
    baseURL      string
    client       *http.Client
    healthy      bool
    inUse        bool
    score        float64
    requestCount int64     // total completed requests
    lastActiveAt time.Time // zero value if never used
}
```

`recordEval()` (already called after every Extract/Digest/ChatStream response) also updates these under `p.mu`:

```go
b.requestCount++
b.lastActiveAt = time.Now()
```

### 2. New interface and stat type in `internal/inference/pipeline.go`

```go
// BackendStat holds a snapshot of a single Ollama backend's runtime state.
type BackendStat struct {
    URL          string     `json:"url"`
    Healthy      bool       `json:"healthy"`
    InUse        bool       `json:"in_use"`
    Score        float64    `json:"score"`
    RequestCount int64      `json:"request_count"`
    LastActiveAt *time.Time `json:"last_active_at,omitempty"` // nil if never used
}

// BackendStatter is implemented by pipelines that manage a backend pool.
type BackendStatter interface {
    BackendStats() []BackendStat
}
```

### 3. `OllamaPipeline.BackendStats()`

Snapshots all backends under `p.mu` and returns a `[]BackendStat`. Sets `LastActiveAt` to nil when the backend has never been used (zero `lastActiveAt`).

---

## API Endpoint

**`GET /api/ollama/backends`** — admin only

Handler iterates `registry.All()`, type-asserts each pipeline to `BackendStatter`, skips pipelines that don't implement it (ONNX, seq2seq, etc.), and returns:

```json
[
  {
    "pipeline": "extraction",
    "backends": [
      {
        "url": "http://desktop:11434",
        "healthy": true,
        "in_use": false,
        "score": 45.2,
        "request_count": 312,
        "last_active_at": "2026-03-27T10:30:00Z"
      },
      {
        "url": "http://macbook:11434",
        "healthy": false,
        "in_use": false,
        "score": 28.1,
        "request_count": 87,
        "last_active_at": "2026-03-27T09:15:00Z"
      }
    ]
  },
  {
    "pipeline": "digest",
    "backends": [...]
  }
]
```

Response type: `[]PipelineBackends` where:

```go
type PipelineBackends struct {
    Pipeline string       `json:"pipeline"`
    Backends []BackendStat `json:"backends"`
}
```

---

## Frontend

### New files

- `web/src/api/ollama.ts` — typed API wrapper
- `web/src/views/OllamaView.vue` — page component

### `web/src/api/ollama.ts`

```typescript
export interface BackendStat {
  url: string
  healthy: boolean
  in_use: boolean
  score: number
  request_count: number
  last_active_at?: string  // ISO 8601, absent if never used
}

export interface PipelineBackends {
  pipeline: string
  backends: BackendStat[]
}

export function listOllamaBackends(): Promise<PipelineBackends[]> {
  return apiFetch<PipelineBackends[]>('/api/ollama/backends')
}
```

### `web/src/views/OllamaView.vue`

**State:** `ref<PipelineBackends[]>` local to the component (no Pinia store needed). Polls every 5 seconds via `setInterval` on `onMounted`, cleared on `onUnmounted`.

**Layout:**

- **Page header** — "Ollama Backends" title + last-refreshed relative timestamp ("Updated 2s ago")
- **One section per pipeline** — section title with pipeline name + badge showing `{healthy}/{total} healthy`
- **Backend cards** in a responsive grid (2–3 columns per pipeline section)

**Per-backend card:**

| Element | Detail |
|---------|--------|
| Status dot | Green `●` if `healthy`, red `●` if not; pulsing amber `●` overlay when `in_use` |
| URL | Machine address; full URL as tooltip if truncated |
| Score bar | Thin progress bar scaled to max score across all backends; label `{score.toFixed(1)} tok/s`; grey if score is 0 (not yet measured) |
| Requests | `{request_count} req` |
| Last active | Relative time ("3 min ago"); "Never" if `last_active_at` is absent |

The score bar uses relative scaling (fastest backend fills 100%) so users can immediately see which machine is faster without knowing absolute token rates.

### Router & navigation

- New route `/ollama` added to `web/src/router/index.ts` (admin-only, no auth guard change needed — non-admin routes already redirect viewers)
- New menu item "Ollama Backends" added to the admin sidebar in `App.vue` between Models and another admin route
- Route uses `ServerOutline` icon (consistent with infrastructure pages)

---

## Files Changed

**Backend:**
- `internal/inference/pipeline.go` — add `BackendStat`, `BackendStatter`
- `internal/inference/ollama.go` — add `requestCount`/`lastActiveAt` fields + `recordEval` updates + `BackendStats()` method
- `internal/api/handlers_ollama.go` — new file: `handleOllamaBackends` handler
- `internal/api/server.go` — register `GET /api/ollama/backends` route (admin middleware)

**Frontend:**
- `web/src/api/ollama.ts` — new file
- `web/src/views/OllamaView.vue` — new file
- `web/src/router/index.ts` — add `/ollama` route
- `web/src/App.vue` — add sidebar menu item
