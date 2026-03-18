# Iran Watcher — Design Spec

**Date:** 2026-03-18
**Status:** Approved (all sections), not yet committed

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
- **Output:** Normalized tweet serialized to JSON, `LPUSH`ed onto Redis list `iran-watcher:raw`.

## Section 2: Data Storage (SQLite)

Package: `internal/store`

Repository pattern. Core tables:

- **sources** — `id`, `type` (account|keyword), `value`, `x_rule_id`, `active`, `created_at`, `updated_at`
- **tweets** — `id` (X tweet ID, PK), `author_id`, `author_handle`, `text`, `raw_json`, `source_id` (FK), `created_at`, `fetched_at`
- **categorizations** — `id`, `tweet_id` (FK), `model_name`, `model_version`, `results` (JSON blob), `created_at`
- **models** — `id`, `name`, `version`, `path` (local ONNX path), `type` (classification, NER, sentiment, etc.), `active`, `created_at`

Key decisions:
- `results` as JSON blob in categorizations — flexible for any model output shape, avoids schema changes when swapping models.
- Schema migrations via embedded SQL files (`embed` package), applied at startup.
- WAL mode enabled for concurrent reads (API) and writes (worker pool).

## Section 3: Redis Queue Layer

Package: `internal/queue`

- **Queues:** `iran-watcher:raw` (incoming tweets), `iran-watcher:failed` (dead-letter for failed inference).
- **Producer:** Stream listener `LPUSH`es serialized tweets onto `:raw`.
- **Consumer:** Workers use `BRPOP` on `:raw` with timeout — blocking, FIFO, no polling.
- **At-least-once semantics:** Acceptable trade-off — if worker crashes mid-inference, tweet is lost from Redis but can be re-fetched. Avoids `RPOPLPUSH` complexity.
- **Dead-letter:** Failed inference → tweet pushed to `:failed` with error annotation.
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
      Run(input []byte) (json.RawMessage, error)
      Close() error
  }
  ```
  Each model type implements this, handling its own tokenization and pre/post-processing in Go.
- **Tokenization:** Go port of HuggingFace tokenizers (e.g., `daulet/tokenizers` wrapping Rust `tokenizers` via CGO), or BPE/WordPiece implemented in Go.
- **Model download:** CLI/API command shells out to `hf download <repo-id> --include "*.onnx" --local-dir ./models/<repo-id>`, then registers in SQLite. Prerequisite check verifies `hf` is in `$PATH`.
- **Active models:** Worker pool loads all `active` models at startup. Multiple models can process the same tweet — separate `categorizations` rows per model.

## Section 5: REST API & CLI

### REST API

Package: `internal/api` — configurable port, JSON throughout.

| Endpoint | Description |
|---|---|
| `GET/POST/DELETE /api/sources` | Manage tracked accounts/keywords. Create triggers X rule sync. |
| `GET /api/tweets` | Query tweets (filters: source, date range, pagination) |
| `GET /api/tweets/:id` | Single tweet with categorizations |
| `GET/POST/DELETE /api/models` | List, register, remove models |
| `POST /api/models/:id/activate` | Activate a model |
| `POST /api/models/:id/deactivate` | Deactivate a model |
| `POST /api/models/download` | Trigger `hf` CLI download |
| `GET /api/categorizations` | Query results (filters: model, tweet, date range) |
| `GET /api/health` | Status of stream, Redis, SQLite, loaded models |

### CLI

Package: `cli` — thin client using `cobra`, calls REST API over HTTP.

- `iran-watcher serve` — starts full application (stream + workers + API)
- `iran-watcher source add/list/remove`
- `iran-watcher model add/list/remove/activate/deactivate`
- `iran-watcher tweet list/show`
- `iran-watcher health`

Base URL configurable, defaults to `localhost`.

## Section 6: Worker Pool & Lifecycle

### Worker Pool

Package: `internal/worker`

- Configurable goroutine count (default: number of active models, tunable via config).
- Each worker loops on `BRPOP` from `iran-watcher:raw`.
- On receiving a tweet: runs through **all active models** sequentially, writes a `categorizations` row per model.
- Model failure: log error, push tweet to `:failed`, continue with next model — one bad model doesn't block others.
- Workers respect `context.Context` for graceful shutdown.

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
