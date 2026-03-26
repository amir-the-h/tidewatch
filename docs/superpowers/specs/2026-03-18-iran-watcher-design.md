# Iran Watcher — Design Spec

**Date:** 2026-03-18
**Status:** Approved (all sections)

## Overview

A Go application that connects to X (Twitter) Filtered Stream API to monitor specific accounts and keywords related to Iran news. Incoming tweets are queued through Redis, stored in SQLite, and processed by pluggable ONNX models for categorization. A REST API and CLI provide management and querying.

## Key Decisions

- **Single binary, worker pool architecture** (Approach C) — monolith with internal Redis queue decoupling stream ingestion from inference processing
- **SQLite** for persistent storage, **Redis** as real-time queue/buffer
- **ONNX Runtime** via `onnxruntime-go` for fully local, offline model execution — no Python
- **Model pipeline is pluggable** — architecture is model/task agnostic (classification, NER, sentiment, etc.)
- **Language agnostic** — no assumptions about input language
- **Dynamic source management** — accounts/keywords managed via REST API, CLI is a thin client over the API
- **Model downloads** via HuggingFace `hf` CLI tool (not reimplemented in Go)

## Project Structure

```
iran-watcher/
├── cmd/
│   └── iran-watcher/        # Single binary entrypoint
│       └── main.go
├── internal/
│   ├── config/               # App config (YAML/TOML), env vars
│   ├── stream/               # X filtered stream client
│   ├── store/                # SQLite repository layer
│   ├── queue/                # Redis queue producer/consumer
│   ├── inference/            # ONNX runtime wrapper, model registry
│   ├── api/                  # REST API (manage sources, query results)
│   ├── worker/               # Worker pool: Redis → ONNX → SQLite
│   └── model/                # Domain types (Tweet, Source, Category, etc.)
├── cli/                      # CLI commands (thin client over the API)
├── migrations/               # SQLite schema migrations
├── models/                   # ONNX model files (gitignored)
├── configs/                  # Default config files
└── docs/
```

## Section 1: X Filtered Stream Integration

Package: `internal/stream`

- **Rule management:** Each tracked account/keyword maps to an X stream rule. Adding/removing sources via the API syncs rules with X's rules endpoint.
- **Connection resilience:** Automatic reconnection with exponential backoff. Persistent goroutine maintains the SSE connection.
- **Tweet normalization:** Raw X API JSON → domain `Tweet` struct (ID, author, text, created_at, media URLs, referenced tweets). Downstream consumers are decoupled from X API format.
- **Rate limit awareness:** X allows 25 concurrent rules (basic tier), 1000 (Pro). Config specifies tier; rule management enforces the limit.
- **Authentication:** X API Bearer Token provided via environment variable `IRAN_WATCHER_X_BEARER_TOKEN`, with config file fallback.
- **Persistence:** On receiving a tweet, the stream listener writes the tweet row and `tweet_sources` junction rows to SQLite immediately. This ensures the tweet is persisted before any inference processing.
- **Queue output:** After persisting, the normalized tweet (including `matching_rules`) is serialized to JSON and `LPUSH`ed onto Redis list `iran-watcher:raw` for inference processing.

## Section 2: Data Storage (SQLite)

Package: `internal/store`

Repository pattern. Core tables:

- **sources** — `id`, `type` (account|keyword), `value`, `x_rule_id`, `active`, `created_at`, `updated_at`
- **tweets** — `id` (X tweet ID, PK), `author_id`, `author_handle`, `text`, `raw_json`, `created_at`, `fetched_at`
- **tweet_sources** — `tweet_id` (FK), `source_id` (FK), composite PK — junction table because a single tweet can match multiple rules (X Filtered Stream returns `matching_rules` as an array)
- **categorizations** — `id`, `tweet_id` (FK), `model_name`, `model_version`, `results` (JSON blob), `created_at`
- **models** — `id`, `name`, `version`, `path` (local ONNX path), `type` (classification, NER, sentiment, etc.), `active`, `created_at`

Indexes:
- `tweets`: index on `created_at`, `author_id`
- `tweet_sources`: indexes on `tweet_id`, `source_id`
- `categorizations`: indexes on `tweet_id`, `model_name`, `created_at`

Key decisions:
- `results` as JSON blob in categorizations — flexible for any model output shape, avoids schema changes when swapping models.
- Schema migrations via embedded SQL files (`embed` package), applied at startup.
- WAL mode enabled for concurrent reads (API) and writes (worker pool).

## Section 3: Redis Queue Layer

Package: `internal/queue`

- **Queues:** `iran-watcher:raw` (incoming tweets), `iran-watcher:failed` (dead-letter for failed inference).
- **Producer:** Stream listener `LPUSH`es serialized tweets onto `:raw`.
- **Consumer:** Workers use `BRPOP` on `:raw` with timeout — blocking, FIFO, no polling.
- **At-most-once delivery:** If a worker crashes mid-inference, the tweet is lost from Redis. This is an acceptable trade-off — tweets are already persisted to SQLite before categorization, and uncategorized tweets can be reprocessed via a CLI command (`iran-watcher tweet reprocess`). Avoids `RPOPLPUSH` complexity.
- **Dead-letter:** If **all** models fail on a tweet, the tweet is pushed to `:failed` with error metadata (model name, error message, timestamp). If only some models fail, successful results are written to SQLite and the tweet is still pushed to `:failed` with annotation of which models failed, so it can be retried for those models only.
- **Redis features used:** Lists only. No Streams, no Pub/Sub.

## Section 4: ONNX Inference & Model Registry

Package: `internal/inference`

- **Runtime:** `onnxruntime-go` bindings. ONNX Runtime shared library is a system dependency (path in config).
- **Model registry:** `models` SQLite table tracks available models. Files stored in `models/` directory (gitignored).
- **Pipeline interface:**
  ```go
  type Pipeline interface {
      Name() string
      Load(modelPath string) error
      // Run takes the raw tweet text (UTF-8 string as bytes).
      // Each pipeline handles its own tokenization internally.
      Run(text []byte) (json.RawMessage, error)
      // RunBatch is an extension point for models that benefit from
      // batched inference. Default implementation loops over Run().
      // Not used by the worker pool initially (workers process one
      // tweet at a time), but available for future optimization.
      RunBatch(texts [][]byte) ([]json.RawMessage, error)
      Close() error
  }
  ```
  Each model type implements this, handling its own tokenization and pre/post-processing in Go.
- **Tokenization:** Uses `daulet/tokenizers` which wraps the Rust HuggingFace `tokenizers` library via CGO. This is chosen over a pure-Go reimplementation for correctness and compatibility with HuggingFace model tokenizer configs. Build requirement: CGO enabled, Rust toolchain available.
- **Model download:** CLI/API command shells out to `hf download <repo-id> --include "*.onnx" --local-dir ./models/<repo-id>`, then registers in SQLite. Prerequisite check verifies `hf` is in `$PATH`; returns a clear error message with install instructions if missing. For gated models, `hf` handles authentication via its own token config.
- **Active models:** Worker pool loads all `active` models at startup. Multiple models can process the same tweet — separate `categorizations` rows per model.

## Section 5: REST API & CLI

### REST API

Package: `internal/api` — configurable port, JSON throughout.

| Endpoint | Description |
|---|---|
| `GET /api/sources` | List all sources (supports `?type=account\|keyword&active=true\|false`) |
| `POST /api/sources` | Create source. Body: `{"type": "account\|keyword", "value": "@handle or keyword"}`. Triggers X rule sync. |
| `DELETE /api/sources/:id` | Remove source by ID. Deletes corresponding X rule. |
| `GET /api/tweets` | Query tweets (filters: `?source_id=`, `?from=&to=` date range, `?page=&per_page=` pagination) |
| `GET /api/tweets/:id` | Single tweet with its categorizations |
| `POST /api/tweets/reprocess` | Reprocess tweets. Queries SQLite for tweets missing categorization rows (or missing rows for specific models). Does NOT read from the dead-letter queue. For partial failures, only runs models that haven't already produced a result for that tweet (checks `categorizations` table). Body: `{"tweet_ids": [...], "model_names": [...]}` (both optional; defaults to all uncategorized tweets, all active models). Re-enqueues matching tweets onto `iran-watcher:raw`. |
| `GET /api/models` | List all models |
| `POST /api/models` | Register a model. Body: `{"name": "", "version": "", "path": "", "type": ""}` |
| `DELETE /api/models/:id` | Remove model by ID |
| `POST /api/models/:id/activate` | Activate a model |
| `POST /api/models/:id/deactivate` | Deactivate a model |
| `POST /api/models/download` | Download model via `hf` CLI. Body: `{"repo_id": "org/model-name"}` |
| `GET /api/categorizations` | Query results (filters: `?model_name=`, `?tweet_id=`, `?from=&to=`) |
| `GET /api/health` | Status of stream, Redis, SQLite, loaded models |
| `GET /api/search` | List recent search jobs (`?limit=N`) |
| `POST /api/search/run` | Start async background search job; returns `202 Accepted` with job object immediately |
| `GET /api/search/{id}` | Poll search job status and result (`status`: `pending`\|`running`\|`done`\|`error`) |
| `DELETE /api/search/{id}` | Delete a search job record |
| `DELETE /api/workers/buffer` | Flush all in-memory pipeline task buffers (discard queued tasks) |
| `DELETE /api/workers/buffer/{pipeline}` | Flush in-memory buffer for a single named pipeline |

**Security:** API binds to `127.0.0.1` by default (localhost only). No authentication — intended for local/trusted network use. Bind address configurable if the user wants to expose it behind a reverse proxy with auth.

### CLI

Package: `cli` — thin client using `cobra`, calls REST API over HTTP.

- `iran-watcher serve` — starts full application (stream + workers + API)
- `iran-watcher source add/list/remove`
- `iran-watcher model add/list/remove/activate/deactivate`
- `iran-watcher tweet list/show`
- `iran-watcher health`

Base URL configurable, defaults to `localhost`.

## Section 6: Worker Manager & Lifecycle

### Fair-Share Worker Manager

Package: `internal/worker` — `Manager` type

- Configurable goroutine count (default: number of active models, tunable via config).
- **Dispatcher goroutine** pops `StreamTweet`s from Redis (`BRPOP`) and fans each tweet out to per-pipeline in-memory task channels (buffer size 512).
- **N worker goroutines** independently round-robin over pipeline channels (staggered by worker ID), so a fast pipeline cannot starve slower ones regardless of per-pipeline processing speed.
- After each task, a worker advances its round-robin offset so it must cycle through all other pipeline slots before returning to the same pipeline — enforcing true fairness.
- **Per-pipeline concurrency caps:** Pipelines implementing `ConcurrencyLimiter.MaxConcurrency()` (e.g. Ollama at 1) are skipped when at capacity; the worker tries other pipelines instead.
- **`completionTracker`** per tweet: coordinates entity matching and dead-letter push after all assigned pipelines finish. The last pipeline to complete calls `finalizeTweet` inline.
- **Model hot-reload:** Registry notifies the dispatcher via a channel; `syncPipelineChans` reconciles pipeline channels and schedOrder without a restart.
- Workers respect `context.Context` for graceful shutdown.
- `Flush()` / `FlushPipeline(name)` drain in-memory buffers on demand (exposed via API).

### Lifecycle Coordinator

In `cmd/iran-watcher/main.go`:

**Startup order:**
1. Load config
2. Connect SQLite, run migrations
3. Connect Redis
4. Load active ONNX models into memory
5. Start stream listener
6. Start worker pool
7. Start API server

**Shutdown (SIGINT/SIGTERM):**
1. Cancel context
2. Stream listener disconnects
3. Workers drain current work
4. API server graceful shutdown with timeout
5. Close SQLite and Redis connections

All components report errors to a central error channel. Fatal errors (Redis down, SQLite corrupt) trigger graceful shutdown.

## Section 7: Configuration Reference

Config loaded from `configs/iran-watcher.yaml` with environment variable overrides (prefix `IRAN_WATCHER_`).

| Key | Env Override | Type | Default | Required |
|---|---|---|---|---|
| `x.bearer_token` | `IRAN_WATCHER_X_BEARER_TOKEN` | string | — | yes |
| `x.tier` | `IRAN_WATCHER_X_TIER` | string (`basic`\|`pro`) | `basic` | no |
| `redis.addr` | `IRAN_WATCHER_REDIS_ADDR` | string | `localhost:6379` | no |
| `redis.password` | `IRAN_WATCHER_REDIS_PASSWORD` | string | — | no |
| `redis.db` | `IRAN_WATCHER_REDIS_DB` | int | `0` | no |
| `sqlite.path` | `IRAN_WATCHER_SQLITE_PATH` | string | `./data/iran-watcher.db` | no |
| `api.bind` | `IRAN_WATCHER_API_BIND` | string | `127.0.0.1:8080` | no |
| `worker.count` | `IRAN_WATCHER_WORKER_COUNT` | int | number of active models | no |
| `inference.onnx_lib_path` | `IRAN_WATCHER_ONNX_LIB_PATH` | string | system default | no |
| `inference.models_dir` | `IRAN_WATCHER_MODELS_DIR` | string | `./models` | no |
| `log.level` | `IRAN_WATCHER_LOG_LEVEL` | string (`debug`\|`info`\|`warn`\|`error`) | `info` | no |
| `log.format` | `IRAN_WATCHER_LOG_FORMAT` | string (`text`\|`json`) | `text` | no |

## Section 8: Error Handling & Logging

- **Structured logging** using `slog` (Go stdlib). All log entries include component name, timestamp, and relevant IDs (tweet ID, model name, source ID).
- **Transient errors** (Redis timeout, X API rate limit, network blip): logged at `warn`, retried with exponential backoff.
- **Fatal errors** (SQLite corruption, ONNX runtime crash, invalid config): logged at `error`, trigger graceful shutdown.
- **Model inference errors**: logged at `error` with tweet ID and model name, tweet routed to dead-letter queue (see Section 3).
- **X stream disconnects**: logged at `warn`, automatic reconnection per X API guidelines.

## Section 9: Testing Strategy

- **Unit tests**: Each `internal/` package tested in isolation. Interfaces (e.g., `Pipeline`, store repository) enable mock-based testing.
- **Integration tests**: Use `testcontainers-go` for Redis. SQLite uses in-memory databases (`:memory:`). X stream tested with a mock HTTP server returning SSE events.
- **ONNX inference tests**: Use a small test ONNX model (e.g., a trivial classifier) committed to the repo under `testdata/`.
- **CLI tests**: Test CLI commands against a test API server.
- **Build tags**: Integration tests gated behind `//go:build integration` to keep `go test ./...` fast by default.
