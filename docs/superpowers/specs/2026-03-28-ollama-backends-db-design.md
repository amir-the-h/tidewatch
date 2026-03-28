# Ollama Backends: DB-Stored with Per-Pipeline Priority Assignment

**Date:** 2026-03-28
**Status:** Approved

## Summary

Move Ollama backends from static YAML config to a DB-managed table. Backends are global (shared across all Ollama pipelines) and can be assigned to pipelines with an explicit priority order. Unassigned pipelines draw from the full pool. All CRUD and assignment operations are available via REST API and CLI. The UI gains backend management and pipeline assignment panels.

---

## DB Schema (migration 019)

```sql
CREATE TABLE ollama_backends (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT    NOT NULL,
    url        TEXT    NOT NULL UNIQUE,
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE pipeline_backends (
    pipeline_id INTEGER NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
    backend_id  INTEGER NOT NULL REFERENCES ollama_backends(id) ON DELETE CASCADE,
    priority    INTEGER NOT NULL DEFAULT 1,   -- 1 = highest priority, 2 = next, ...
    PRIMARY KEY (pipeline_id, backend_id)
);
```

`enabled = 0` means the backend is disabled and skipped during acquisition. "Full pool" throughout this spec means all **enabled** backends. Backends with no `pipeline_backends` rows are available to all pipelines via the full pool fallback.

---

## BackendPool (`internal/inference/backend_pool.go`)

A new `BackendPool` type owns all `*ollamaBackend` instances and their health-checking goroutines.

**Responsibilities:**
- Holds a `map[int64]*ollamaBackend` keyed by DB id
- Runs **one** health-checker goroutine per physical URL (not per pipeline)
- `Load([]BackendRecord)` rebuilds the map: adds new backends, removes deleted ones, skips unchanged entries; health goroutines only restart for new or changed entries
- `Get(id int64) *ollamaBackend` — lookup by id for pipeline acquisition
- `All() []*ollamaBackend` — all enabled backends; used for unassigned pipelines

**Health check:** `GET /api/tags` (lists installed models, no GPU load). Unchanged from current implementation.

**Benchmark:** `POST /api/generate` with a one-token prompt — runs once per `(url, model)` at pipeline load time, cached in a package-level `sync.Map`. This is model-specific so it remains per-pipeline, not per-pool.

---

## OllamaPipeline Changes

Replace the current `backends []*ollamaBackend` slice with:

```go
pool        *BackendPool
assignedIDs []int64       // backend IDs in priority order; empty = use full pool
```

**Acquire algorithm:**

```
if assignedIDs is empty:
    pick any healthy idle backend from pool (highest score wins — current behaviour)
else:
    loop forever:
        for each id in assignedIDs:
            if backend[id] is disabled or absent → skip
            wait up to fastWaitTimeout for backend[id] to become healthy+idle
            if acquired → use it
        (cycle back to top)
```

The per-backend wait uses the existing `cond.WaitTimeout` pattern from `acquireFastest`. A pipeline whose entire assigned list is disabled/absent falls back to the full pool rather than deadlocking.

`fastWaitTimeout` remains a single server-wide setting (`ollama.fast_wait_timeout`, default 5s). No per-assignment timeout.

---

## Config Cleanup

Remove from config struct and YAML parsing:
- `ollama.base_url`
- `ollama.base_urls`
- `ollama.backends`

Remove env vars:
- `IRAN_WATCHER_OLLAMA_BASE_URL`
- `IRAN_WATCHER_OLLAMA_BASE_URLS`

Keep:
- `ollama.fast_wait_timeout` / `IRAN_WATCHER_OLLAMA_FAST_WAIT_TIMEOUT`

---

## API Endpoints

All mutating endpoints are admin-only.

### Backend CRUD (`internal/api/handlers_ollama.go`)

| Method   | Path                                  | Description                         |
|----------|---------------------------------------|-------------------------------------|
| `GET`    | `/api/ollama/backends`                | List all backends with live stats   |
| `POST`   | `/api/ollama/backends`                | Create `{name, url}`                |
| `PUT`    | `/api/ollama/backends/{id}`           | Update `{name?, url?}`              |
| `DELETE` | `/api/ollama/backends/{id}`           | Delete                              |
| `POST`   | `/api/ollama/backends/{id}/enable`    | Enable                              |
| `POST`   | `/api/ollama/backends/{id}/disable`   | Disable                             |

`GET /api/ollama/backends` response shape is backward-compatible; backends gain an `id` field.

### Pipeline-Backend Assignments

| Method   | Path                                              | Description                                      |
|----------|---------------------------------------------------|--------------------------------------------------|
| `GET`    | `/api/pipelines/{id}/backends`                    | List assigned backends ordered by priority       |
| `PUT`    | `/api/pipelines/{id}/backends`                    | Replace full list `[{backend_id, priority}]`     |
| `DELETE` | `/api/pipelines/{id}/backends/{backend_id}`       | Remove one assignment                            |

### Reload Behaviour

Every mutating backend or assignment call does in sequence:
1. `pool.Load(backends)` — rebuild BackendPool from DB
2. `actionCache.Load()` — rebuild all pipelines with updated `assignedIDs`

Both complete before the response is returned. Clients see consistent state immediately.

---

## CLI Commands

### Backend management (extends `cli/cmd/ollama.go`)

```
iran-watcher ollama backends list
iran-watcher ollama backends add --name <name> --url <url>
iran-watcher ollama backends update <id> [--name <name>] [--url <url>]
iran-watcher ollama backends remove <id>
iran-watcher ollama backends enable <id>
iran-watcher ollama backends disable <id>
```

### Pipeline-backend assignments (new subcommand in `cli/cmd/pipeline.go`)

```
iran-watcher pipeline backends list <pipeline-id>
iran-watcher pipeline backends set <pipeline-id> <backend-id>:<priority> [...]
iran-watcher pipeline backends unassign <pipeline-id> <backend-id>
```

`pipeline backends set` replaces the full assignment list atomically. Example:
`iran-watcher pipeline backends set 3 1:1 2:2` — assigns backend 1 at priority 1, backend 2 at priority 2 for pipeline 3.

---

## UI Changes (`web/src/views/OllamaView.vue`)

Three panels:

**1. Backend management panel**
- Table: name, URL, enabled toggle, health indicator, score, request count
- Add button → inline form (name + URL)
- Edit (name/URL) and delete per row
- Enable/disable toggle per row

**2. Pipeline assignment panel**
- Per Ollama pipeline: drag-to-reorder list of assigned backends showing priority order
- "Add backend" dropdown to assign an existing backend with next available priority
- Remove button per assignment
- Unassigned pipelines show a grey "any available" indicator

**3. Live stats panel (read-only)**
- Existing backend stats view (health, in_use, score, last active) — unchanged, gains `id` field

**New API calls in `web/src/api/ollama.ts`:**
`createBackend`, `updateBackend`, `deleteBackend`, `enableBackend`, `disableBackend`, `listPipelineBackends`, `setPipelineBackends`, `removePipelineBackend`

---

## Wiring Changes (`cli/cmd/serve.go`)

- Remove `ollamaBackendConfigs(cfg.OllamaBackends())` call
- On startup: create `BackendPool`, call `pool.Load(backends from DB)`
- Pass `*BackendPool` into `worker.NewActionCache(...)` and `api.NewServer(...)`
- `ActionCache.Load()` reads `pipeline_backends JOIN ollama_backends` to populate `assignedIDs` per pipeline
- `api.Server` holds `*BackendPool` so mutating handlers can trigger `pool.Load()` + `actionCache.Load()`

---

## CLAUDE.md Updates

- Remove `ollama.base_url` / `ollama.base_urls` / `IRAN_WATCHER_OLLAMA_BASE_URLS` references
- Add `ollama_backends` and `pipeline_backends` to the store table list
- Update Ollama integration note to describe DB-stored backends and per-pipeline priority assignment
- Add new API routes and CLI commands to Key Design Decisions
