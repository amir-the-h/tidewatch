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
internal/model/         → Domain types (Tweet, Source, NotificationProvider, NotificationChannel, Action, etc.)
internal/notifier/      → Async push notifications (Pushover + Telegram)
internal/search/        → X search API backfill scheduler
internal/auth/          → JWT (HS256 via stdlib) + bcrypt password helpers + HTTP middleware
internal/hub/           → WebSocket hub (fan-out broadcaster) + Client (read/write pumps), Broadcaster interface, NopBroadcaster
cli/                    → Cobra CLI (thin client over REST API)
migrations/             → Embedded SQL migration files
models/                 → ONNX model files (gitignored)
configs/                → Default config files
.github/workflows/      → GitHub Actions CI/CD (Docker build + push to Docker Hub)
```

## Tech Stack

- **Go 1.25.5** — no Python anywhere
- **SQLite** (via `modernc.org/sqlite` or `mattn/go-sqlite3`) — DELETE journal mode (no WAL/SHM files; data dir lives on NFS so WAL mode is unreliable), embedded migrations
- **Redis** — lists only (`LPUSH`/`BRPOP`), no Streams or Pub/Sub
- **onnxruntime-go** — local ONNX model execution
- **daulet/tokenizers** — HuggingFace tokenizer bindings (CGO + Rust)
- **HuggingFace `hf` CLI** — model downloads (external dependency, not reimplemented)
- **cobra** — CLI framework
- **gorilla/websocket** — WebSocket server for live-update push to the UI (`/api/ws`)

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

### Debugging API (no shell in app container)

The app runs in a scratch/distroless image with no shell utilities. To query the API for debugging, use a temporary container on the same Docker network:

```bash
# Find the network name
docker inspect $(docker compose ps -q iran-watcher) --format '{{json .NetworkSettings.Networks}}' | python3 -m json.tool | grep -o '"[^"]*iran[^"]*"'

# One-shot query (replace NETWORK and TOKEN)
docker run --rm --network iran-watcher_network alpine/curl \
  -s http://iran-watcher:8080/api/rules \
  -H "Authorization: Bearer $TOKEN"

# Interactive shell
docker run --rm -it --network iran-watcher_network busybox sh
# then inside: wget -qO- http://iran-watcher:8080/api/health
```

Internal API port is `8080` (env `IRAN_WATCHER_API_BIND=0.0.0.0:8080`). The SQLite DB can be read directly from the host via Python's `sqlite3` module at the bind-mounted path.

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
- **Simplified event-driven worker** (`internal/worker.Manager`) — N worker goroutines each independently BRPOP from per-action queues (`iran-watcher:tasks:{action_id}`); no dispatcher goroutine, no per-action channels, no round-robin scheduler; before each BRPOP a worker calls `buildReadyKeys()` which filters the action cache to only those whose pipeline is currently acquirable (`AvailabilityChecker.IsAvailable()`): ONNX, internal, and notification pipelines are always included; Ollama pipelines are included only when a healthy idle backend exists; `Consumer.PopTask(ctx, timeout, keys ...string)` issues a single variadic BRPOP across all ready keys; `ActionCache` (`internal/worker.ActionCache`) is a hot-reloadable in-memory map of `action_id → (Action row + resolved Pipeline)` shared by the stream listener and all workers; on activate/deactivate the cache is fully rebuilt from DB
- **tweet_sources junction table** — tweets can match multiple stream rules
- **Dead-letter queue** (`iran-watcher:failed`) — pipeline execution errors dead-lettered here; tasks are queued per-action at `iran-watcher:tasks:{action_id}` (replaces the single `iran-watcher:tasks` key); `FlushQueue` scans `iran-watcher:tasks:*` to drain all per-action keys atomically
- **Pipeline hot-reload** — activate/deactivate pipeline or action via API triggers full `ActionCache` rebuild; workers pick up the new config on their next task
- **API is localhost-only** by default, no auth
- **Chain run endpoint** — `POST /api/chains/{id}/run` is the replacement for the removed action-level run; it queries SQLite for historical tweets eligible for the chain's start node and dispatches them as tasks; body `{ "new_only": bool }` — `new_only: true` excludes tweet IDs already recorded in `action_executions` for any action node in the chain; returns 202 `{ "queued": N }`; action-level run (`POST /api/actions/{id}/run`) no longer exists; CLI: `iran-watcher chain run <id> [--new-only]`
- **Notifications** — runtime-configured push alerts via Pushover or Telegram; delivered through the `NotificationPipeline` action system; `NotificationProvider` stores credentials (pushover: `app_token`+`user_key`, telegram: `bot_token`); `NotificationChannel` stores a `message_template` (supports `${key}` substitution) and provider-specific delivery config (e.g. `chat_id` for Telegram); the `Dispatcher` in `internal/notifier` is an async event emitter used for system events but no longer dispatches to notification configs; REST API: `GET|POST /api/notification-providers`, `GET|PUT|DELETE /api/notification-providers/{id}`, `POST /api/notification-providers/{id}/{activate,deactivate}`, `GET|POST /api/notification-channels`, `GET|PUT|DELETE /api/notification-channels/{id}`, `POST /api/notification-channels/{id}/{activate,deactivate}`
- **Digest jobs** — background async jobs that generate LLM digests over a filtered tweet window; stored in `digest_jobs` table with status tracking; `POST /api/digest` creates the job, pushes a `digest_requested` Task to Redis for each active action with that trigger type, and returns 202; workers execute the job via `manager.processDigestTask` which casts the action's pipeline to `inference.DigestPipeline` and updates the job in DB; the Ollama pipeline's backend-priority assignments are fully respected through the action system
- **Search jobs** — async background jobs for X search backfill; POST `/api/search/run` returns a job immediately (202), client polls GET `/api/search/{id}` until `done`/`error`; stored in `search_jobs` table; the periodic scheduler (`search.enabled: true`) tracks the newest tweet ID (from `meta.newest_id`) returned by each rule's last run and injects it as `since_id` on subsequent runs, so only genuinely new tweets are re-queued — the map is in-memory and resets on restart (first run after restart re-fetches from `StartTime` or the 7-day default)
- **Event-driven actions system** — every unit of work is a `(trigger_type, action_id, context)` `Task` in Redis (`iran-watcher:tasks:{action_id}`); an **Action** subscribes to a trigger type (`new_tweet`, `new_categorization`, `entity_matched`, `chain_only`, `digest_requested`) and references a **Pipeline** (onnx, ollama, or internal); the stream listener pushes one Task per matching action per incoming tweet; workers run the pipeline, save outputs to `action_execution_metadata`, and evaluate **ActionChain** conditions to push downstream tasks; DB tables: `pipelines` (replaces `models`), `actions` (`trigger_type` + `pipeline_id`), `action_chains`, `action_executions`, `action_execution_metadata`, `twitter_credentials`, `ollama_backends`, `pipeline_backends`; internal pipelines (`entity_matcher`, `twitter_retweet`, `twitter_like`, `twitter_quote`, `notification_push`, `notification_tg`) are registered at startup via `internal/pipeline.Registry`; REST API: `GET|POST /api/pipelines`, `PUT|DELETE /api/pipelines/{id}`, `POST /api/pipelines/{id}/{activate,deactivate}`, `GET /api/pipelines/handlers`, `GET|POST /api/actions`, `PUT|DELETE /api/actions/{id}`, `POST /api/actions/{id}/{activate,deactivate}`, `GET|POST /api/actions/{id}/chains`, `PUT|DELETE /api/actions/{id}/chains/{chain_id}`, `POST /api/chains/{id}/run` (manual chain backfill: dispatches eligible historical tweets through the chain's start node; body { "new_only": bool }; new_only: true excludes tweet IDs already in action_executions for any chain action node; returns 202 { "queued": N }), `GET /api/triggers`, `GET /api/actions/{id}/executions`, `GET /api/executions`, `GET /api/executions/{id}/metadata`, `GET|DELETE /api/twitter/auth`, `GET /api/twitter/auth/start`, `GET /api/twitter/auth/callback`, `GET /api/twitter/auth/status`, `GET /api/queue` (per-action queue breakdown), `DELETE /api/queue` (flush all pending task queues)
- **Twitter OAuth 2.0 PKCE** — `internal/twitter/` handles the full OAuth 2.0 PKCE flow; state tokens are in-memory with 10-min TTL; connected account credentials stored in `twitter_credentials` table; token refresh is transparent; configured via `twitter.client_id` / `twitter.callback_url` (or env vars `IRAN_WATCHER_TWITTER_CLIENT_ID` / `IRAN_WATCHER_TWITTER_CALLBACK_URL`)
- **Authentication** — JWT-based (HS256 via stdlib); tokens issued at `POST /api/auth/login`, carried as `Authorization: Bearer <token>`; two roles: `admin` (full access) and `viewer` (read-only: map, tweets with filtering, tracked entities); users stored in `users` SQLite table; admin account seeded on first startup from config (`auth.admin_username` / `auth.admin_password`, env: `IRAN_WATCHER_AUTH_JWT_SECRET`/`ADMIN_USERNAME`/`ADMIN_PASSWORD`); user management at `GET|POST /api/users`, `PUT|DELETE /api/users/{id}` (admin only); `internal/auth` package handles token signing/verification and role-enforcement middleware; frontend route guard redirects unauthenticated users to `/login` and viewers to `/tweets` for admin-only routes; `/` redirects to `/map`; **CLI** stores the token at `~/.iran-watcher/token` (written by `iran-watcher auth login`, read transparently by all commands)
- **Map date-heat coloring** — geo-event pins on `/map` are colored on a blue→cyan→green→yellow→red spectrum based on `tweet.created_at` (the actual tweet publish time, not the pipeline processing time); `GET /api/map` returns geo-event pins sourced from `action_execution_metadata` (key `locations`), with `tweet_created_at` joined from the `tweets` table for date-heat coloring; oldest pins are blue (cold), newest are red (hot); a gradient legend in the header shows the date range
- **Map coordinate configuration** — each pipeline can declare which `action_execution_metadata` key holds its coordinate output and the format of that value via a `map_config: { coordinate_key, coordinate_type }` section in the pipeline's `config` JSON; the five supported types are `array_of_pairs` ([[lat,lon],...]), `single_pair` ([lat,lon]), `object` ({"lat":x,"lon":x}), `array_of_objects` ([{"lat":x,"lon":x},...]), and `geojson_point` (GeoJSON Point, lon/lat swapped on parse); pipelines without a `map_config` key produce no pins; configured in the Pipelines UI form under "Map Output"
- **Stream controls in header** — stream connect/disconnect buttons and status indicator live in the top bar (admin only); stream status is pushed via WebSocket (`stream_status` event) and stored in `stores/stream.ts`
- **NER alias suggestions** — when creating/editing a tracked entity the UI surfaces NER-derived alias candidates; edit mode calls `GET /api/tracked-entities/{id}/alias-suggestions` (contexts from the entity's own NER tracks); create mode debounces on name input and calls `GET /api/alias-suggestions?name={name}&type={type}` (searches across all same-type entity NER tracks); suggestions are filtered by entity type and exclude already-added aliases
- **Entity discovery (Tracker)** — the Tracker feature (formerly "Tracked Entities") auto-discovers entities from tweet NER output via the `entity_discovery` configurable internal pipeline; discovered entities are accumulated as candidates in the `entity_candidates` table with rolling-avg confidence and occurrence counts; candidates are auto-promoted when they meet configurable thresholds (`min_confidence`, `min_frequency`, `auto_promote_types`); others queue for manual review (promote, reject, or merge as alias) via the Candidates tab at `/tracker`; API: `GET|POST /api/entity-candidates`, `GET /api/entity-candidates/{id}`, `POST /api/entity-candidates/{id}/{promote,reject,merge}`, `DELETE /api/entity-candidates/{id}`; frontend: `/tracker` has two tabs — Entities (existing) and Candidates (new)
- **Tweet chat** — interactive multi-turn LLM chat over a selected tweet set; entry points: bulk-select on `/tweets` → chat button, or per-entity chat button on `/tracked-entities` (fetches first 200 entity-track tweet IDs); before navigating `POST /api/chat/estimate` checks token count against the digest model's context window (Ollama `/api/tokenize` + `/api/show`, falls back to len/4); if within limit, tweet IDs are stored in `useChatStore` (Pinia) and the user is routed to `/chat`; `POST /api/chat/stream` streams SSE chunks (`data: "text"\n\n … data: [DONE]\n\n`) via `OllamaPipeline.ChatStream` which prepends tweet texts as numbered context in the system message; `ChatPipeline` interface (with `ChatStream` + `EstimateTokens`) lives in `internal/inference/pipeline.go`; both endpoints are admin-only
- **Ollama integration** — LLM summarization and digest via local Ollama; backends are stored in SQLite (`ollama_backends` table) and managed via REST API (`GET|POST /api/ollama/backends`, `PUT|DELETE /api/ollama/backends/{id}`, `POST /api/ollama/backends/{id}/{enable,disable}`); at startup, backends are loaded from DB into `inference.BackendPool` which tracks health, score, and in-use state; pipeline-to-backend priority assignments are stored in `pipeline_backends` table (`GET /api/pipelines/{id}/backends`, `PUT /api/pipelines/{id}/backends`, `DELETE /api/pipelines/{id}/backends/{backend_id}`); `BackendPool.AcquireForPipeline(assignedIDs)` tries assigned backends in priority order then falls back to any healthy backend; `ollama.base_url` / `ollama.base_urls` config and `IRAN_WATCHER_OLLAMA_BASE_URLS` env var seed the DB on first startup for backward compatibility; CLI: `iran-watcher ollama backends {list,add,update,remove,enable,disable}` and `iran-watcher pipeline backends {list,set,unassign}`
- **WebSocket live updates** — `GET /api/ws` is an authenticated WebSocket endpoint; JWT is passed as `?token=` query param (header auth is not possible from browser WS); the server-side `Hub` (`internal/hub`) runs a fan-out goroutine that reads from an unbuffered channel and broadcasts JSON event messages to all connected `Client`s; each `Client` owns a write pump (drains a 256-item buffered channel) and a read pump (consumes pings/pongs, enforces read deadline); the `Broadcaster` interface (`Broadcast(msg []byte)`) is implemented by `Hub` and a `NopBroadcaster` (used in tests); the Hub is wired into: queue `Producer` (task enqueue/dequeue counts), worker `Manager` (execution results), `BackendPool` (backend health changes), stream `Client` (connect/disconnect), and API server (CRUD mutations on pipelines, actions, chains, etc.); event types carried as JSON `{"type":"...","payload":{...}}` include `queue_update`, `worker_stats` (payload includes `active_workers: WorkerInfo[]`, `action_throughput: map[action_id]float64` items/sec), `execution_result`, `backend_health`, `stream_status`, and `crud`; frontend composable `useWebSocket.ts` manages the WS connection with exponential-backoff reconnect and ping/pong keepalive; connection state is in `stores/ws.ts`; queue+worker stats, executions, and Ollama backends each have dedicated Pinia stores updated via WS events — polling for these has been removed from `QueueView.vue`, `OllamaView.vue`, and `App.vue`; nginx proxies `/api/ws` with `Upgrade` and `Connection` headers set
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
