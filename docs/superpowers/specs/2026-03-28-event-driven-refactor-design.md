# Event-Driven Pipeline Refactor — Design Spec

**Date:** 2026-03-28
**Status:** Approved

## Overview

Refactor the app from a pipeline-centric dispatch model to a fully event-driven action system. The core change: every unit of work is a `(trigger, action, context)` task in Redis — no in-memory fan-out, no per-action channels. Pipelines and actions are cleanly separated. Actions chain conditionally based on pipeline outputs. Notifications and Twitter interactions become first-class internal pipelines.

## Section 1: Core Concepts

Four first-class concepts underpin the entire system:

**Trigger** — a named event type that initiates work. `new_tweet` is the first. Future triggers include internal events (`new_categorization`, `entity_matched`) and Twitter events (`mention`, `new_reply`). Triggers carry a typed payload and are defined in Go code — no DB table needed. When a trigger fires, it resolves which actions subscribe to it and pushes one task per action to Redis.

**Pipeline** — execution logic. Types: `onnx`, `ollama`, `internal`. Internal pipelines register themselves at startup via a registry. All pipelines implement the same `Pipeline` interface and return structured outputs as `map[string]any`. Pipelines are stored in a `pipelines` table (replacing the current `models` table).

**Action** — subscribes to a trigger type, references a pipeline, optionally scoped to specific rule IDs. An action says: "when trigger X fires (optionally matching rules Y), run pipeline Z."

**Chain** — a conditional link from one action to another. Stored in `action_chains`. Conditions are evaluated against the accumulated flat context after an action completes. All conditions must pass (implicit AND). Passing chains cause new tasks to be pushed to Redis for downstream actions.

## Section 2: Database Schema

### `pipelines` table

Replaces the `models` table. All pipeline types — ONNX, Ollama, and internal — are rows here.

```sql
CREATE TABLE pipelines (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT NOT NULL UNIQUE,
    type        TEXT NOT NULL,        -- onnx | ollama | internal
    config      TEXT NOT NULL DEFAULT '{}',
    active      INTEGER NOT NULL DEFAULT 1,
    created_at  DATETIME NOT NULL,
    updated_at  DATETIME NOT NULL
);
```

Config JSON by type:
- `onnx`: `{"path": "/models/...", "model_type": "classification", "labels": ["conflict", "protest"]}`
- `ollama`: `{"model": "llama3.2", "system_prompt": "..."}` — backend pool managed via app config, not per-pipeline
- `internal`: `{"handler": "entity_matcher"}` — `handler` is the key used to look up the registered Go factory at startup

### `actions` table

```sql
CREATE TABLE actions (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    name         TEXT NOT NULL,
    trigger_type TEXT NOT NULL,        -- new_tweet | new_categorization | entity_matched | ...
    pipeline_id  INTEGER NOT NULL REFERENCES pipelines(id),
    rule_ids     TEXT NOT NULL DEFAULT '[]',  -- JSON array, empty = all rules
    active       INTEGER NOT NULL DEFAULT 1,
    created_at   DATETIME NOT NULL,
    updated_at   DATETIME NOT NULL
);
```

### `action_chains` table

```sql
CREATE TABLE action_chains (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    source_action_id INTEGER NOT NULL REFERENCES actions(id) ON DELETE CASCADE,
    target_action_id INTEGER NOT NULL REFERENCES actions(id) ON DELETE CASCADE,
    conditions       TEXT NOT NULL DEFAULT '[]',  -- JSON array of {key, op, value}
    created_at       DATETIME NOT NULL
);
```

Condition object: `{"key": "label", "op": "==", "value": "conflict"}`

Supported operators: `==`, `!=`, `>`, `>=`, `<`, `<=`, `is_empty`, `not_empty`, `contains`, `not_contains`, `is_array`, `is_object`, `array_empty`, `array_not_empty`

- `is_array` / `is_object` — check the JSON type of the value
- `array_empty` / `array_not_empty` — check whether a JSON array output has zero or more elements

### Kept as-is

- `action_executions` — one row per execution; `status`, `error`, `executed_at`
- `action_execution_metadata` — key-value outputs per execution; doubles as the canonical output store feeding chain context
- `twitter_credentials` — unchanged
- `tweets`, `tweet_sources`, `sources` — unchanged
- `categorizations` — unchanged; the worker continues to write a `categorizations` row after running an `onnx` or `ollama` pipeline action, preserving the existing `/api/categorizations` endpoint and map view. Writing categorizations is a worker-level responsibility based on pipeline type, not the pipeline's own `Run()` method.

### Migration

1. Every `models` row → `pipelines` row with `type=onnx`; config carries path and model_type
2. Every current `actions` row referencing a `model_id` → updated to `pipeline_id` pointing at migrated pipeline row, `trigger_type=new_tweet`
3. `TwitterAction` rows (`retweet`, `like`, `quote`) → `pipeline_id` references to auto-created `internal` pipeline rows (`handler: twitter_retweet`, `handler: twitter_like`, `handler: twitter_quote`)
4. `models` table dropped after migration

## Section 3: Pipeline Interface

### Updated `Pipeline` interface

```go
type Pipeline interface {
    Name() string
    Type() PipelineType  // onnx | ollama | internal
    Run(ctx context.Context, input PipelineInput) (map[string]any, error)
    Close() error
}

type PipelineInput struct {
    Text    string         // primary text (tweet body)
    Context map[string]any // accumulated flat chain context from upstream actions
    Config  map[string]any // pipeline-specific config from DB row
}
```

`Run()` returns structured `map[string]any` outputs — not raw JSON. Outputs are stored in `action_execution_metadata` and merged into the chain context for downstream conditions.

`RunBatch` is dropped (was never used by the worker pool). Specialised sub-interfaces (`SummarizationPipeline`, `DigestPipeline`, `ChatPipeline`, `ExtractionPipeline`) remain, updated to embed the new `Pipeline` signature.

### Output keys

Output keys are open-ended — each pipeline defines whatever keys make sense. Known examples:

| Pipeline | Example output keys |
|---|---|
| ONNX classifier | `label`, `confidence` |
| ONNX NER | `entities` (JSON array), `entity_count` |
| Ollama summarizer | `summary` |
| Ollama (general) | any structured keys the model returns |
| Entity matcher | `matched_ids` (JSON array), `matched` |
| Notification | `sent`, `provider` |
| Twitter retweet | `retweet_id` |
| Twitter quote | `quote_tweet_id`, `text` |

Keys are not validated at the schema level. The UI surfaces known keys from recent executions as autocomplete when building chain conditions.

### Internal pipeline registry

```go
// internal/pipeline/registry.go
type Registry struct { ... }

func (r *Registry) Register(handler string, factory PipelineFactory)
func (r *Registry) Get(handler string) (Pipeline, bool)
```

Registrations happen in `main.go` at startup:

```go
pipelineRegistry.Register("entity_matcher",    pipeline.NewEntityMatcherPipeline)
pipelineRegistry.Register("notification_push", pipeline.NewPushoverPipeline)
pipelineRegistry.Register("notification_tg",   pipeline.NewTelegramPipeline)
pipelineRegistry.Register("twitter_retweet",   pipeline.NewRetweetPipeline)
pipelineRegistry.Register("twitter_like",      pipeline.NewLikePipeline)
pipelineRegistry.Register("twitter_quote",     pipeline.NewQuotePipeline)
```

Each factory receives the DB config (`map[string]any`) and returns a `Pipeline`.

## Section 4: Queue & Worker

### Task queue

Single Redis queue: `iran-watcher:tasks`. Each item is a JSON-serialised struct:

```go
type Task struct {
    TriggerType string          `json:"trigger_type"`
    Event       json.RawMessage `json:"event"`      // serialised trigger payload (Tweet, etc.)
    ActionID    int64           `json:"action_id"`
    Context     map[string]any  `json:"context"`    // accumulated flat outputs from upstream
}
```

`iran-watcher:raw` is retired. `iran-watcher:failed` stays for dead-lettered tasks (pipeline execution errors).

The reprocess endpoint (`POST /api/tweets/reprocess`) is updated to push to `iran-watcher:tasks` instead of `iran-watcher:raw`. It creates `Task` items with `trigger_type=new_tweet` and empty context, targeted at specific action IDs (defaulting to all active `onnx`/`ollama` pipeline actions).

### Who pushes tasks

Trigger handlers push tasks — not the worker:

- **Stream listener** — on tweet received, queries the `ActionCache` for all active actions with `trigger_type=new_tweet`, filters by `rule_ids` overlap with the tweet's matching rule IDs, pushes one `Task` per matching action with empty context
- **Worker** (post-execution) — evaluates `action_chains` where `source_action_id = completed_action.id`, checks conditions against merged context, pushes new `Task` per passing chain
- **Internal pipelines** — pipelines that emit their own events (e.g. entity matcher emits `entity_matched`) push tasks directly via the queue producer

### Worker pool

```go
type Worker struct {
    id        int
    queue     queue.Consumer    // BRPOP iran-watcher:tasks
    store     store.Store
    pipelines *pipeline.Registry
    actions   *ActionCache      // hot-reloadable in-memory action+pipeline lookup
    logger    *slog.Logger
}
```

`ActionCache` is an in-memory map of `action_id → (action row + resolved Pipeline)` loaded at startup from the DB and shared (read-mostly) across all workers. When an action is activated or deactivated via the API, the cache is fully rebuilt from the DB. The stream listener also holds a reference to the same cache to resolve `new_tweet` subscriptions without a DB round-trip per tweet.

The Manager launches N worker goroutines and holds a WaitGroup for graceful shutdown. That is all it does — no dispatcher goroutine, no scheduler goroutine, no per-action channels.

Each worker runs an independent infinite loop:

1. `BRPOP iran-watcher:tasks` → `Task`
2. Load action (from cache, invalidated on hot-reload signal)
3. Resolve pipeline by `action.pipeline_id`
4. `pipeline.Run(ctx, PipelineInput{Text, Context, Config})`
5. Merge outputs into context (flat map, later keys overwrite earlier on conflict)
6. Save `action_execution` row + output entries to `action_execution_metadata`
7. Load `action_chains` where `source_action_id = action.id`
8. For each chain: evaluate conditions against merged context; push new `Task` to `iran-watcher:tasks` for any that pass
9. Loop back to step 1

Workers block on `BRPOP` when idle — no busy-polling. Redis handles the wait.

### Ollama concurrency

Workers acquire an Ollama backend slot (existing scoring + `in_use` flag) before calling `Run()`. If no backend is free the worker blocks until one is released. Tasks queue in Redis rather than memory, so blocking a worker here is acceptable. The scoring system (health checks, request count, last active) is unchanged. Chat and digest tasks that require a high-scoring backend pass a minimum score threshold via `PipelineInput.Config` and the backend selector respects it.

### Hot-reload

When an action is activated or deactivated via the API, the `ActionCache` is invalidated. Workers pick up the new config on their next task. No channel reconciliation needed.

## Section 5: REST API & UI

### `/api/pipelines` — replaces `/api/models`

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/pipelines` | List pipelines (`?type=`, `?active=`) |
| `POST` | `/api/pipelines` | Create pipeline |
| `PUT` | `/api/pipelines/{id}` | Update name, config |
| `DELETE` | `/api/pipelines/{id}` | Delete |
| `POST` | `/api/pipelines/{id}/activate` | Activate |
| `POST` | `/api/pipelines/{id}/deactivate` | Deactivate |
| `GET` | `/api/pipelines/handlers` | List registered internal handler names |
| `POST` | `/api/pipelines/download` | Download ONNX model via `hf` CLI, creates pipeline row on completion |

`/api/models` is removed.

### `/api/actions` — updated

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/actions` | List (`?trigger_type=`, `?pipeline_id=`, `?active=`) |
| `POST` | `/api/actions` | Create (`trigger_type`, `pipeline_id`, `rule_ids`, `name`) |
| `PUT` | `/api/actions/{id}` | Update |
| `DELETE` | `/api/actions/{id}` | Delete |
| `POST` | `/api/actions/{id}/activate` | Activate |
| `POST` | `/api/actions/{id}/deactivate` | Deactivate |

### `/api/actions/{id}/chains` — new

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/actions/{id}/chains` | List outbound chains |
| `POST` | `/api/actions/{id}/chains` | Create chain to target action with conditions |
| `PUT` | `/api/actions/{id}/chains/{chain_id}` | Update conditions |
| `DELETE` | `/api/actions/{id}/chains/{chain_id}` | Delete chain |

### `/api/triggers` — new

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/triggers` | List available trigger types (registered in code) with payload schema — used by UI to populate trigger dropdown |

### Execution log — unchanged

`GET /api/actions/{id}/executions` and `GET /api/executions` stay as-is. Outputs are visible in the execution log via the existing metadata rows.

### UI

- **`/pipelines`** replaces `/models` — create/edit ONNX, Ollama, and internal pipelines; internal handler selected from dropdown populated by `/api/pipelines/handlers`
- **`/actions`** updated — trigger type selector added; pipeline selector replaces old type-specific config form; rule ID filter unchanged
- **Chain builder** — on action detail/edit page, a "Chains" section shows outbound chains; each chain shows target action and condition list; conditions built with a `key / operator / value` row UI; known output keys from recent executions surfaced as autocomplete on the key field
- **`/models`** page removed
