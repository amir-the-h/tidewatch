# Iran Watcher

X (Twitter) Filtered Stream monitor for Iran-related news. Stores tweets in SQLite, queues through Redis, categorizes via local ONNX models. Pure Go — no Python.

## Architecture

Single binary, event-driven action system. Stream listener → `ActionCache` lookup → per-action `Task` push to Redis → N worker goroutines BRPOP tasks → run pipeline → save outputs → evaluate chain conditions → push downstream tasks. REST API + CLI for management.

See [design spec](docs/superpowers/specs/2026-03-18-iran-watcher-design.md) for full architecture details.

## Project Structure

```
cmd/iran-watcher/       → binary entrypoint
internal/config/        → YAML config + env var overrides
internal/stream/        → X Filtered Stream client (SSE)
internal/store/         → SQLite repository layer
internal/queue/         → Redis list-based producer/consumer
internal/inference/     → ONNX runtime wrapper, Pipeline interface
internal/pipeline/      → Internal pipeline registry + entity_matcher, twitter, notification pipelines
internal/twitter/       → Twitter OAuth 2.0 PKCE client + API calls (Retweet/Like/Quote)
internal/api/           → REST API (net/http or chi)
internal/worker/        → Simplified worker manager (Manager) + ActionCache
internal/model/         → Domain types (Tweet, Source, Notification, Action, etc.)
internal/notifier/      → Async push notifications (Pushover + Telegram)
internal/search/        → X search API backfill scheduler
internal/auth/          → JWT (HS256 via stdlib) + bcrypt password helpers + HTTP middleware
cli/                    → Cobra CLI (thin client over REST API)
migrations/             → Embedded SQL migration files
models/                 → ONNX model files (gitignored)
configs/                → Default config files
.github/workflows/      → GitHub Actions CI/CD (Docker build + push to Docker Hub)
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
- **Simplified event-driven worker** (`internal/worker.Manager`) — N worker goroutines each independently BRPOP from `iran-watcher:tasks`; no dispatcher goroutine, no per-action channels, no round-robin scheduler; workers are stateless and block on BRPOP when idle; `ActionCache` (`internal/worker.ActionCache`) is a hot-reloadable in-memory map of `action_id → (Action row + resolved Pipeline)` shared by the stream listener and all workers; on activate/deactivate the cache is fully rebuilt from DB
- **tweet_sources junction table** — tweets can match multiple stream rules
- **Dead-letter queue** (`iran-watcher:failed`) — pipeline execution errors dead-lettered here; task queue is `iran-watcher:tasks` (replaces `iran-watcher:raw`)
- **Pipeline hot-reload** — activate/deactivate pipeline or action via API triggers full `ActionCache` rebuild; workers pick up the new config on their next task
- **API is localhost-only** by default, no auth
- **Reprocess endpoint** queries SQLite for missing categorizations, not the dead-letter queue
- **Notifications** — runtime-configured push alerts via Pushover or Telegram; async buffered channel dispatch; `provider` field selects backend per notification
- **Digest jobs** — background async jobs that generate LLM digests over a filtered tweet window; stored in `digest_jobs` table with status tracking
- **Search jobs** — async background jobs for X search backfill; POST `/api/search/run` returns a job immediately (202), client polls GET `/api/search/{id}` until `done`/`error`; stored in `search_jobs` table; the periodic scheduler (`search.enabled: true`) tracks the newest tweet ID (from `meta.newest_id`) returned by each rule's last run and injects it as `since_id` on subsequent runs, so only genuinely new tweets are re-queued — the map is in-memory and resets on restart (first run after restart re-fetches from `StartTime` or the 7-day default)
- **Event-driven actions system** — every unit of work is a `(trigger_type, action_id, context)` `Task` in Redis (`iran-watcher:tasks`); an **Action** subscribes to a trigger type (`new_tweet`, `new_categorization`, `entity_matched`) and references a **Pipeline** (onnx, ollama, or internal); the stream listener pushes one Task per matching action per incoming tweet; workers run the pipeline, save outputs to `action_execution_metadata`, and evaluate **ActionChain** conditions to push downstream tasks; DB tables: `pipelines` (replaces `models`), `actions` (`trigger_type` + `pipeline_id`), `action_chains`, `action_executions`, `action_execution_metadata`, `twitter_credentials`; internal pipelines (`entity_matcher`, `twitter_retweet`, `twitter_like`, `twitter_quote`, `notification_push`, `notification_tg`) are registered at startup via `internal/pipeline.Registry`; REST API: `GET|POST /api/pipelines`, `PUT|DELETE /api/pipelines/{id}`, `POST /api/pipelines/{id}/{activate,deactivate}`, `GET /api/pipelines/handlers`, `GET|POST /api/actions`, `PUT|DELETE /api/actions/{id}`, `POST /api/actions/{id}/{activate,deactivate}`, `GET|POST /api/actions/{id}/chains`, `PUT|DELETE /api/actions/{id}/chains/{chain_id}`, `GET /api/triggers`, `GET /api/actions/{id}/executions`, `GET /api/executions`, `GET|DELETE /api/twitter/auth`, `GET /api/twitter/auth/start`, `GET /api/twitter/auth/callback`, `GET /api/twitter/auth/status`
- **Twitter OAuth 2.0 PKCE** — `internal/twitter/` handles the full OAuth 2.0 PKCE flow; state tokens are in-memory with 10-min TTL; connected account credentials stored in `twitter_credentials` table; token refresh is transparent; configured via `twitter.client_id` / `twitter.callback_url` (or env vars `IRAN_WATCHER_TWITTER_CLIENT_ID` / `IRAN_WATCHER_TWITTER_CALLBACK_URL`)
- **Authentication** — JWT-based (HS256 via stdlib); tokens issued at `POST /api/auth/login`, carried as `Authorization: Bearer <token>`; two roles: `admin` (full access) and `viewer` (read-only: map, tweets with filtering, tracked entities); users stored in `users` SQLite table; admin account seeded on first startup from config (`auth.admin_username` / `auth.admin_password`, env: `IRAN_WATCHER_AUTH_JWT_SECRET`/`ADMIN_USERNAME`/`ADMIN_PASSWORD`); user management at `GET|POST /api/users`, `PUT|DELETE /api/users/{id}` (admin only); `internal/auth` package handles token signing/verification and role-enforcement middleware; frontend route guard redirects unauthenticated users to `/login` and viewers to `/tweets` for admin-only routes; `/` redirects to `/map`
- **Map date-heat coloring** — geo-event pins on `/map` are colored on a blue→cyan→green→yellow→red spectrum based on `tweet.created_at` (the actual tweet publish time, not the categorization processing time); `GET /api/categorizations` returns a `tweet_created_at` field (joined from the `tweets` table via LEFT JOIN, falls back to `categorization.created_at` if tweet row is missing); oldest pins are blue (cold), newest are red (hot); a gradient legend in the header shows the date range
- **Stream controls in header** — stream connect/disconnect buttons and status indicator live in the top bar (admin only); stream status is polled alongside health/queue/worker stats every 3 seconds
- **NER alias suggestions** — when creating/editing a tracked entity the UI surfaces NER-derived alias candidates; edit mode calls `GET /api/tracked-entities/{id}/alias-suggestions` (contexts from the entity's own NER tracks); create mode debounces on name input and calls `GET /api/alias-suggestions?name={name}&type={type}` (searches across all same-type entity NER tracks); suggestions are filtered by entity type and exclude already-added aliases
- **Tweet chat** — interactive multi-turn LLM chat over a selected tweet set; entry points: bulk-select on `/tweets` → chat button, or per-entity chat button on `/tracked-entities` (fetches first 200 entity-track tweet IDs); before navigating `POST /api/chat/estimate` checks token count against the digest model's context window (Ollama `/api/tokenize` + `/api/show`, falls back to len/4); if within limit, tweet IDs are stored in `useChatStore` (Pinia) and the user is routed to `/chat`; `POST /api/chat/stream` streams SSE chunks (`data: "text"\n\n … data: [DONE]\n\n`) via `OllamaPipeline.ChatStream` which prepends tweet texts as numbered context in the system message; `ChatPipeline` interface (with `ChatStream` + `EstimateTokens`) lives in `internal/inference/pipeline.go`; both endpoints are admin-only
- **Ollama integration** — LLM summarization and digest via local Ollama; supports a pool of backends: `ollama.base_url` (single URL, backward-compat) or `ollama.base_urls` (list, takes precedence when set); multiple URLs spread load — each backend handles at most one in-flight request at a time so the worker concurrency limit scales with pool size; `IRAN_WATCHER_OLLAMA_BASE_URLS` env var accepts a comma-separated list
- **Stream auto-connect** — `stream.auto_connect: true` connects to X on startup without a CLI call
- **UI runtime config** — `API_BASE_URL` and `API_UPSTREAM` are injected at container startup by `web/docker-entrypoint.sh`: it writes `env-config.js` (SPA reads `window.__ENV__.API_BASE_URL`) and runs `envsubst` on `nginx.conf.template` to produce the live nginx config. `API_BASE_URL` defaults to `""` (same-origin) so the SPA calls `/api/...` as relative paths; nginx proxies those to `API_UPSTREAM` (default `http://iran-watcher:1452`) via Docker network — API traffic never leaves the server. Set `API_UPSTREAM` to override the backend target.

## Testing

- Unit tests: mock interfaces, in-memory SQLite (`:memory:`)
- Integration tests: `testcontainers-go` for Redis, gated behind `//go:build integration`
- ONNX tests: small test model in `testdata/`

## Documentation Requirements

After every fix or feature, update all affected docs before considering the task done:

- **CLAUDE.md** — update Project Structure, Key Design Decisions, Tech Stack, or Code Conventions if any changed
- **`docs/superpowers/specs/`** — update the relevant design spec if architecture, data flow, or API contracts changed
- **`configs/`** — update example/default config files if new config keys were added
- **CLI help strings** — keep `Use`, `Short`, and `Long` fields on Cobra commands in sync with actual behavior

If a new component, table, queue key, or API route is introduced, it must appear in CLAUDE.md's Project Structure or Key Design Decisions (whichever fits) before the task is committed.

## Build Verification

Before committing any change that touches Go code, frontend code, or Dockerfiles, build the local container image to verify the build succeeds:

```bash
docker compose build --build-arg BUILDKIT_INLINE_CACHE=1 iran-watcher
# or tag it explicitly
docker build -t iran-watcher:local .
```

Do not push changes that have not been verified to build successfully.
