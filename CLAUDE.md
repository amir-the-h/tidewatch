# Iran Watcher

X (Twitter) Filtered Stream monitor for Iran-related news. Stores tweets in SQLite, queues through Redis, categorizes via local ONNX models. Pure Go — no Python.

## Architecture

Single binary, worker pool pattern. Stream listener → Redis queue → worker pool (ONNX inference) → SQLite. REST API + CLI for management.

See [design spec](docs/superpowers/specs/2026-03-18-iran-watcher-design.md) for full architecture details.

## Project Structure

```
cmd/iran-watcher/       → binary entrypoint
internal/config/        → YAML config + env var overrides
internal/stream/        → X Filtered Stream client (SSE)
internal/store/         → SQLite repository layer
internal/queue/         → Redis list-based producer/consumer
internal/inference/     → ONNX runtime wrapper, Pipeline interface
internal/api/           → REST API (net/http or chi)
internal/worker/        → Worker pool goroutines
internal/model/         → Domain types (Tweet, Source, Notification, DigestJob, etc.)
internal/notifier/      → Async push notifications (Pushover + Telegram)
internal/search/        → X search API backfill scheduler
cli/                    → Cobra CLI (thin client over REST API)
migrations/             → Embedded SQL migration files
models/                 → ONNX model files (gitignored)
configs/                → Default config files
```

## Tech Stack

- **Go 1.25.5** — no Python anywhere
- **SQLite** (via `modernc.org/sqlite` or `mattn/go-sqlite3`) — WAL mode, embedded migrations
- **Redis** — lists only (`LPUSH`/`BRPOP`), no Streams or Pub/Sub
- **onnxruntime-go** — local ONNX model execution
- **daulet/tokenizers** — HuggingFace tokenizer bindings (CGO + Rust)
- **HuggingFace `hf` CLI** — model downloads (external dependency, not reimplemented)
- **cobra** — CLI framework

## Build Requirements

- Go 1.25.5+
- CGO enabled (required by `daulet/tokenizers`)
- Rust toolchain (required by `daulet/tokenizers`)
- ONNX Runtime shared library installed on system

## Commands

### Docker (preferred)

```bash
# Build and start all services (API + UI)
docker compose build
docker compose up -d

# Rebuild a single service after code changes
docker compose build iran-watcher
docker compose up -d --no-deps iran-watcher

# View logs
docker compose logs -f iran-watcher

# Stop
docker compose down
```

Production compose lives at `/mnt/storage/docker/compose/iran-watcher/` with bind-mounted volumes and absolute build paths. The repo's `docker-compose.yml` uses named volumes and relative paths — keep them in sync when adding new env vars or services.

### Native (tests only)

```bash
go test ./...                        # unit tests
go test -tags integration ./...      # unit + integration tests
```

> Native `go build` / `go run` is not recommended — the binary requires CGO, Rust toolchain, and ONNX Runtime shared libraries. Use Docker instead.

## Code Conventions

- All packages under `internal/` — nothing exported outside the module
- Repository pattern for SQLite access (`internal/store`)
- `Pipeline` interface for all ONNX model integrations (`internal/inference`)
- Structured logging via `slog` — include component name and relevant IDs
- `context.Context` threaded through all components for graceful shutdown
- Config: YAML file with `IRAN_WATCHER_` env var overrides
- Errors: wrap with `fmt.Errorf("component: %w", err)` for context

## Key Design Decisions

- **At-most-once delivery** for the Redis queue — acceptable because tweets are persisted to SQLite before queueing
- **tweet_sources junction table** — tweets can match multiple stream rules
- **Dead-letter queue** (`iran-watcher:failed`) — annotates which models failed; partial successes still written
- **Model hot-reload** — activate/deactivate via API without restart
- **API is localhost-only** by default, no auth
- **Reprocess endpoint** queries SQLite for missing categorizations, not the dead-letter queue
- **Notifications** — runtime-configured push alerts via Pushover or Telegram; async buffered channel dispatch; `provider` field selects backend per notification
- **Digest jobs** — background async jobs that generate LLM digests over a filtered tweet window; stored in `digest_jobs` table with status tracking
- **Ollama integration** — LLM summarization and digest via local Ollama; `ollama.base_url` config
- **Stream auto-connect** — `stream.auto_connect: true` connects to X on startup without a CLI call

## Testing

- Unit tests: mock interfaces, in-memory SQLite (`:memory:`)
- Integration tests: `testcontainers-go` for Redis, gated behind `//go:build integration`
- ONNX tests: small test model in `testdata/`
