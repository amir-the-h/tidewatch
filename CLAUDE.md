# Tidewatch

Self-hosted real-time X (Twitter) Filtered Stream monitor with configurable DAG processing pipelines. Stores tweets in SQLite, queues through Redis, categorizes via local ONNX models. Pure Go — no Python.

## Architecture

Single binary, event-driven action system. Stream listener → `ActionCache` lookup → Task push to per-action Redis lists: `tidewatch:tasks:{action_id}` (LPUSH) → N worker goroutines call `buildReadyKeys()` then `BRPOP` across ready keys → run pipeline → save outputs → evaluate chain conditions → push downstream tasks. REST API + CLI for management.

See [README.md](README.md) for the user-facing overview, configuration, CLI and REST API reference.

## Project Structure

```
cmd/tidewatch/          → binary entrypoint
cmd/tidewatch-mcp/      → MCP server binary entrypoint (pure Go, no CGO)
internal/config/        → YAML config + env var overrides
internal/stream/        → X Filtered Stream client (SSE)
internal/store/         → SQLite repository layer
internal/queue/         → Redis per-action list producer/consumer (LPUSH/BRPOP); dead-letter list
internal/inference/     → ONNX runtime wrapper, Pipeline interface
internal/pipeline/      → Internal pipeline registry + entity_matcher, entity_cooccurrence, reverse_geocoder, twitter, notification pipelines
internal/twitter/       → Twitter OAuth 2.0 PKCE client + API calls (Retweet/Like/Quote)
internal/api/           → REST API (net/http or chi)
internal/worker/        → Simplified worker manager (Manager) + ActionCache
internal/model/         → Domain types (Tweet, Source, NotificationProvider, NotificationChannel, Action, Pipeline, PipelineNode, PipelineEdge, PipelineGraph, etc.; Chain/ChainNode/ChainEdge/ChainGraph/ActionChain removed)
internal/notifier/      → Async push notifications (Pushover + Telegram + Discord Bot)
internal/search/        → X search API backfill scheduler
internal/auth/          → JWT (HS256 via stdlib) + bcrypt password helpers + HTTP middleware
internal/hub/           → WebSocket hub (fan-out broadcaster) + Client (read/write pumps), Broadcaster interface, NopBroadcaster
internal/mcpserver/     → MCP server: table-driven ToolDef registry, HTTP Dispatcher, stdio/SSE transports
cli/                    → Cobra CLI (thin client over REST API)
migrations/             → Embedded SQL migration files
models/                 → ONNX model files (gitignored)
configs/                → Default config files
.github/workflows/      → GitHub Actions: test.yml (gofmt, vet, go test -race; web: check:ui + build), docker-publish.yml (Docker build + push to Docker Hub)
.github/dependabot.yml  → Weekly grouped dependency updates (gomod, npm /web), monthly Actions + Docker base images; ignores (bumped by hand): nvidia/cuda, onnxruntime_go, daulet/tokenizers, TypeScript majors
docs/examples/          → Worked pipeline examples (geopolitical-monitoring detector/extractor flows)
docs/vision.md          → Roadmap beyond X: generic Signals, source connectors (RSS, Telegram, GDELT, ADS-B, AIS, conflict datasets), track layer, zones, detectors, MapLibre map (planning only)
docs/phase-1-signals.md → Phase 1 design: tweets → signals, migration reset, connector framework (x_stream, x_search, rss), trigger filters (not started)
docs/ui-standard.md     → Web UI standard ("Tidewatch Console"): tokens, layout, components, rules
web/                    → Vue 3 SPA (Naive UI + Tailwind v4); tokens in web/src/styles/main.css, UI kit in web/src/components/ui, shell in web/src/components/layout, page-specific pieces in web/src/components/<page> (e.g. actions/ — the action form split by type; tracker/ — the Tracker page's Entities, Candidates and Connections tabs; notifications/ — the Providers and Channels tabs and their Sheets; queue/ — the Queue page stat tiles, worker and dead-letter tables and failure-detail Sheet; tweets/ — the Tweets page filters, server-paged table with row selection, bulk-selection bar, tweet-detail Sheet and the admin-only Search tab; digest/ — the Digest page request form and digest job cards; chat/ — the Chat page message turn (markdown answer + tool-call steps) and composer; map/ — the Map page Leaflet composable (pin paging + date-heat colouring from the heat-* tokens), popup HTML builder, legend and token-styled Leaflet stylesheet; pipeline/ — the Pipeline builder's Vue Flow node components, dagre auto-layout, "+" node picker, selection inspector, run-from-node Dialog, graph state module and token-styled canvas stylesheet)
scripts/                → prepare-nominatim.sh (Geofabrik PBF download + merge; regions via GEOFABRIK_REGIONS)
docker-compose.gpu.yml  → Optional NVIDIA GPU override (ONNX falls back to CPU without it)
```

## Tech Stack

- **Go 1.26** — no Python anywhere
- **SQLite** (via `modernc.org/sqlite` or `mattn/go-sqlite3`) — DELETE journal mode (no WAL/SHM files; data dir lives on NFS so WAL mode is unreliable), embedded migrations
- **Redis** — per-action lists for task queues (`tidewatch:tasks:{action_id}`, `LPush`/`BRPop`); plain list for dead-letter queue (`tidewatch:failed`, `LPush`/`BRPop`); `TotalTaskLen` and `FlushQueue` use `SCAN` + `LLen`/`DEL`; no sorted sets, no Streams, no Pub/Sub
- **onnxruntime-go** — local ONNX model execution; the Go module version is paired with the ONNX Runtime shared library (`ORT_VERSION` in `Dockerfile`, currently v1.27.0 ↔ ORT 1.24.1 GPU/CUDA 12) — ORT ≥ 1.29 GPU builds are CUDA 13 only, so upgrading means bumping the `nvidia/cuda` base image and host driver too
- **daulet/tokenizers** — HuggingFace tokenizer bindings (CGO + Rust); Go module version must equal `TOKENIZERS_VERSION` (prebuilt `libtokenizers.a`) in `Dockerfile` and `.github/workflows/test.yml`
- **HuggingFace `hf` CLI** — model downloads (external dependency, not reimplemented)
- **cobra** — CLI framework
- **gorilla/websocket** — WebSocket server for live-update push to the UI (`/api/ws`)
- **Web UI** — Vue 3 + Pinia + Naive UI, Tailwind v4 for layout, `@vue-flow/core` + `@dagrejs/dagre` for the auto-laid-out Pipeline builder canvas, `@lucide/vue` icons, `marked` + `dompurify` for sanitised LLM markdown (`components/MarkdownContent.vue`), IBM Plex Sans/Mono via `@fontsource` (self-hosted); npm is the package manager (`package-lock.json`, `npm ci` in `web/Dockerfile`)

## Build Requirements

- Go 1.26+
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
docker compose build tidewatch
docker compose up -d --no-deps tidewatch

# View logs
docker compose logs -f tidewatch

# Stop
docker compose down
```

The repo's `docker-compose.yml` is the portable reference deployment (CPU by default; add `-f docker-compose.gpu.yml` for CUDA; UI published on `${UI_PORT:-3000}`). Production deployments typically keep their own compose file — keep it in sync when adding new env vars or services.

### Debugging API (no shell in app container)

The app runs in a scratch/distroless image with no shell utilities. To query the API for debugging, use a temporary container on the same Docker network:

```bash
# Find the network name
docker inspect $(docker compose ps -q tidewatch) --format '{{json .NetworkSettings.Networks}}' | python3 -m json.tool | grep -o '"[^"]*tidewatch[^"]*"'

# One-shot query (replace NETWORK and TOKEN)
docker run --rm --network tidewatch_network alpine/curl \
  -s http://tidewatch:8080/api/rules \
  -H "Authorization: Bearer $TOKEN"

# Interactive shell
docker run --rm -it --network tidewatch_network busybox sh
# then inside: wget -qO- http://tidewatch:8080/api/health
```

Internal API port is `8080` (env `TIDEWATCH_API_BIND=0.0.0.0:8080`). The SQLite DB can be read directly from the host via Python's `sqlite3` module at the bind-mounted path.

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
- Config: YAML file with `TIDEWATCH_` env var overrides
- Errors: wrap with `fmt.Errorf("component: %w", err)` for context

## Key Design Decisions

- **At-most-once delivery** — acceptable because tweets are persisted to SQLite before queueing; per-action Redis lists (`tidewatch:tasks:{action_id}`) use `LPUSH` / `BRPOP`; dead-letter list (`tidewatch:failed`) also uses plain Redis list; `FlushQueue` SCANs and DELetes all `tidewatch:tasks:*` keys; `DELETE /api/queue` flushes the pending task queue
- **Simplified event-driven worker** (`internal/worker.Manager`) — N worker goroutines each independently build a ready-keys list via `buildReadyKeys()`: ONNX/internal/notification action keys are always included; Ollama action keys are included only when `IsAvailable()` AND `ollamaActive < pool.TotalCapacity()`; workers then block on `BRPOP` across those keys with a 1-second timeout (zero Redis traffic when idle); when no keys are ready (all Ollama capacity taken), workers block in `WaitForAvailability` (sync.Cond, zero Redis traffic); `ollamaActive atomic.Int32` on Manager is a soft pre-filter — incremented before an Ollama task executes, decremented after; `BackendPool.TotalCapacity()` sums `max_concurrent` for all healthy+enabled backends; `AcquireForPipeline` is the authoritative serialization point (transient over-subscription can occur between `buildReadyKeys` and `ollamaActive.Add`); after popping a task, the worker does a post-pop check: if the action is not in `ActionCache` → discard the task; `ActionCache` (`internal/worker.ActionCache`) is a hot-reloadable in-memory map of `action_id → (Action row + resolved Pipeline)` shared by the stream listener and all workers; on activate/deactivate the cache is fully rebuilt from DB
- **tweet_sources junction table** — tweets can match multiple stream rules
- **Dead-letter queue** (`tidewatch:failed`) — pipeline execution errors dead-lettered here (plain Redis list); tasks are routed to per-action lists `tidewatch:tasks:{action_id}`; `FlushQueue` SCANs and DELetes all `tidewatch:tasks:*` keys; `DELETE /api/queue` flushes the pending task queue; `DELETE /api/workers/buffer` cancels the task-scoped context for every actively-executing worker (idle workers in `BRPOP` are unaffected), workers finish cleanup and re-enter the `BRPOP` loop, returns `{"flushed": N}` where N is the count of interrupted tasks; the Queue page "Stop Workers & Flush Queue" button calls this then `DELETE /api/queue`
- **Pipeline hot-reload** — activate/deactivate pipeline or action via API triggers full `ActionCache` rebuild; workers pick up the new config on their next task
- **API binds `127.0.0.1:8080`** by default (Docker compose binds `0.0.0.0:8080` on the internal network only)
- **Notifications** — runtime-configured push alerts via Pushover, Telegram, or Discord Bot; delivered through the `NotificationPipeline` action system; `NotificationProvider` stores credentials (pushover: `app_token`+`user_key`, telegram: `bot_token`, discord: `bot_token` only); Discord uses the Bot API (`Authorization: Bot <token>`) instead of webhooks — `DiscordBotClient` in `internal/notifier` creates Forum threads via `POST /channels/{forum_channel_id}/threads`, posts replies via `POST /channels/{thread_id}/messages`, fetches available tags via `GET /channels/{forum_channel_id}`, and patches thread tags via `PATCH /channels/{thread_id}`; `NotificationChannel` stores a `message_template` (supports `${key}` substitution) and provider-specific delivery config (e.g. `chat_id` for Telegram, `forum_channel_id` for Discord); Discord notification actions support an optional `thread_name_template` (string) — rendered with the same `${key}` substitution as `message_template`, truncated to 100 runes, and used as the Forum thread name when creating a new thread; if absent, the notifier falls back to "notification" as the thread name; reply-only actions (those that always have `discord_thread_id` in context) do not need this field; Pushover notification actions support optional `url`/`url_title` config keys (the "URL (optional)" supplementary button) — both are rendered with the same `${key}` substitution as `message_template` (e.g. `url: "https://tidewatch.example.com/tweets?tweet=${tweet_id}"` links the push notification to the tweet's detail page), unlike other delivery-config keys (`chat_id`, `forum_channel_id`, etc.) which are passed to the provider as-is; the `Dispatcher` in `internal/notifier` is an async event emitter used for system events but no longer dispatches to notification configs; REST API: `GET|POST /api/notification-providers`, `GET|PUT|DELETE /api/notification-providers/{id}`, `POST /api/notification-providers/{id}/{activate,deactivate}`, `GET|POST /api/notification-channels`, `GET|PUT|DELETE /api/notification-channels/{id}`, `POST /api/notification-channels/{id}/{activate,deactivate}`
- **Discord thread tagging** — `discord_tag_thread` internal pipeline handler resolves LLM-generated tag names to Discord Forum tag IDs and patches an existing thread; reads `discord_thread_id` from pipeline context (set by the upstream Discord notification action) and tag names from a configurable context key (default `discord_tag_names`); calls `GET /channels/{forum_channel_id}` to resolve names case-insensitively to snowflake IDs, then `PATCH /channels/{thread_id}`; unknown tag names are silently skipped; user-facing action config keys: `channel_id` (required — must point to a Discord notification channel; `bot_token` and `forum_channel_id` are resolved at build time from the linked provider and channel respectively), `tag_names_key` (optional, default `"discord_tag_names"`); typical graph: Discord Notification → Ollama Tag Generator → discord_tag_thread
- **Digest jobs** — background async jobs that generate LLM digests over a filtered tweet window; stored in `digest_jobs` table with status tracking; `POST /api/digest` creates the job, pushes a `digest_requested` Task to Redis for each active action with that trigger type, and returns 202; workers execute the job via `manager.processDigestTask` which casts the action's pipeline to `inference.DigestPipeline` and updates the job in DB; the Ollama pipeline's backend-priority assignments are fully respected through the action system
- **Search jobs** — async background jobs for X search backfill; POST `/api/search/run` returns a job immediately (202), client polls GET `/api/search/{id}` until `done`/`error`; stored in `search_jobs` table; the periodic scheduler (`search.enabled: true`) tracks the newest tweet ID (from `meta.newest_id`) returned by each rule's last run and injects it as `since_id` on subsequent runs, so only genuinely new tweets are re-queued — the map is in-memory and resets on restart (first run after restart re-fetches from `StartTime` or the 7-day default)
- **Event-driven actions system** — every unit of work is a `(trigger_type, action_id, context)` `Task` in the global Redis sorted set `tidewatch:tasks`; an **Action** is now the atomic execution unit: it has a type + config (onnx, ollama, or internal handler) and subscribes to a trigger type (`new_tweet`, `new_categorization`, `entity_matched`, `chain_only`, `digest_requested`, `pipeline_finished`); a **Pipeline** is now the DAG workflow composer (replaces Chain): it is a directed graph of Action nodes connected by edges with conditions, replacing `action_chains`; Pipeline nodes can reference other pipelines via `sub_pipeline_id`, enabling modular fan-in workflows; the stream listener pushes one Task per matching action per incoming tweet; workers run the action, save outputs to `action_execution_metadata`, and evaluate Pipeline edge conditions to push downstream tasks; `pipeline_node_executions` tracks sub-pipeline completions; DB tables: `actions` (type + config, replaces `pipelines` + old `actions`), `pipelines` (DAG workflows, replaces `action_chains`), `pipeline_nodes`, `pipeline_edges`, `action_executions`, `action_execution_metadata`, `pipeline_node_executions`, `pipeline_run_tracking`, `twitter_credentials`, `ollama_backends`, `action_backends`; `action_chains` and all `chain_*` tables are gone; internal handlers (`entity_matcher`, `entity_cooccurrence`, `reverse_geocoder`, `twitter_retweet`, `twitter_like`, `twitter_quote`, `notification_push`, `notification_tg`, `discord_tag_thread`) are registered at startup via `internal/pipeline.Registry`; REST API: `GET|POST /api/actions` (was `/api/pipelines`), `PUT|DELETE /api/actions/{id}`, `POST /api/actions/{id}/{activate,deactivate}`, `GET /api/actions/handlers`, `GET|POST /api/pipelines` (was `/api/chains`), `PUT|DELETE /api/pipelines/{id}`, `POST /api/pipelines/{id}/{activate,deactivate}`, `POST /api/pipelines/{id}/run` (manual backfill: dispatches eligible historical tweets through the pipeline; body `{ "new_only": bool, "start_node_id": int, "order": "asc"|"desc" }`; `start_node_id` is optional — when provided, dispatches from that action or pipeline node instead of the Start node; `new_only` exclusion is scoped to the named node's action when `start_node_id` is set; `order` defaults to `"desc"` (newest first) — set `"asc"` to dispatch oldest tweet first (useful for notification pipelines so alerts arrive in chronological order); full context from prior runs is available automatically via `MergeAllTweetExecutionMetadata`; returns 202 `{ "queued": N, "async": true }`), `GET /api/triggers`, `GET /api/triggers/links` (`TriggerPipelineLink` records mapping trigger types to pipeline entry nodes), `GET /api/actions/{id}/executions`, `GET /api/executions`, `GET /api/executions/{id}/metadata`, `GET|DELETE /api/twitter/auth`, `GET /api/twitter/auth/start`, `GET /api/twitter/auth/callback`, `GET /api/twitter/auth/status`, `GET /api/queue` (global queue status: `{pending_tasks: int64, pending_ollama: int64, workers: []WorkerInfo}`), `DELETE /api/queue` (flush the global pending task queue); CLI: `tidewatch action` manages execution units (was `pipeline`), `tidewatch pipeline` manages DAG workflows (was `chain`)
- **Twitter OAuth 2.0 PKCE** — `internal/twitter/` handles the full OAuth 2.0 PKCE flow; state tokens are in-memory with 10-min TTL; connected account credentials stored in `twitter_credentials` table; token refresh is transparent; configured via `twitter.client_id` / `twitter.callback_url` (or env vars `TIDEWATCH_TWITTER_CLIENT_ID` / `TIDEWATCH_TWITTER_CALLBACK_URL`)
- **Authentication** — JWT-based (HS256 via stdlib); `auth.jwt_secret` / `TIDEWATCH_AUTH_JWT_SECRET` is required — `serve` refuses to start without it (no insecure fallback); a warning is logged if the admin is seeded with the default `changeme` password; tokens issued at `POST /api/auth/login`, carried as `Authorization: Bearer <token>`; two roles: `admin` (full access) and `viewer` (read-only: map, tweets with filtering, tracked entities); users stored in `users` SQLite table; admin account seeded on first startup from config (`auth.admin_username` / `auth.admin_password`, env: `TIDEWATCH_AUTH_JWT_SECRET`/`ADMIN_USERNAME`/`ADMIN_PASSWORD`); user management at `GET|POST /api/users`, `PUT|DELETE /api/users/{id}` (admin only); `internal/auth` package handles token signing/verification and role-enforcement middleware; frontend route guard redirects unauthenticated users to `/login` and viewers to `/tweets` for admin-only routes; `/` redirects to `/map`; **CLI** stores the token at `~/.tidewatch/token` (written by `tidewatch auth login`, read transparently by all commands)
- **Map date-heat coloring** — geo-event pins on `/map` are colored on a blue→cyan→green→yellow→red spectrum based on `tweet.created_at` (the actual tweet publish time, not the pipeline processing time); `GET /api/map` returns geo-event pins sourced from `action_execution_metadata` (key `locations`), with `tweet_created_at` joined from the `tweets` table for date-heat coloring; oldest pins are blue (cold), newest are red (hot); a gradient legend in the header shows the date range
- **Map coordinate configuration** — each pipeline can declare which `action_execution_metadata` key holds its coordinate output and the format of that value via a `map_config: { coordinate_key, coordinate_type }` section in the pipeline's `config` JSON; the five supported types are `array_of_pairs` ([[lat,lon],...]), `single_pair` ([lat,lon]), `object` ({"lat":x,"lon":x}), `array_of_objects` ([{"lat":x,"lon":x},...]), and `geojson_point` (GeoJSON Point, lon/lat swapped on parse); pipelines without a `map_config` key produce no pins; configured in the Pipelines UI form under "Map Output"
- **Reverse geocoding** — `reverse_geocoder` internal pipeline handler calls a self-hosted `mediagis/nominatim` Docker service (Geofabrik OSM extracts merged by `scripts/prepare-nominatim.sh` — regions via `GEOFABRIK_REGIONS`, output via `NOMINATIM_PBF_NAME`, default `regions.osm.pbf`); reads any supported coordinate format from upstream metadata (via `model.ParseCoordinates`), rounds to 4 d.p. for cache keying, stores results in `geocode_cache` SQLite table; returns a `location_names: []string` parallel to the coordinate array (empty string = no name found); gracefully returns empty strings when Nominatim is unavailable; configured per-action via `coordinate_key`, `coordinate_type`, `output_key`, `nominatim_url`; app-level default URL is `nominatim_url` config key (env: `TIDEWATCH_NOMINATIM_URL`, default: `http://nominatim:8080`)
- **Stream controls in status bar** — stream connect/disconnect button and status indicator live in the `StatusBar` at the top of the content area (admin only), next to queue depth/ETA, failed count and worker utilisation; stream status is pushed via WebSocket (`stream_status` event) and stored in `stores/stream.ts`
- **NER alias suggestions** — when creating/editing a tracked entity the UI surfaces NER-derived alias candidates; edit mode calls `GET /api/tracked-entities/{id}/alias-suggestions` (contexts from the entity's own NER tracks); create mode debounces on name input and calls `GET /api/alias-suggestions?name={name}&type={type}` (searches across all same-type entity NER tracks); suggestions are filtered by entity type and exclude already-added aliases
- **Entity discovery (Tracker)** — the Tracker feature (formerly "Tracked Entities") auto-discovers entities from tweet NER output via the `entity_discovery` configurable internal pipeline; discovered entities are accumulated as candidates in the `entity_candidates` table with rolling-avg confidence and occurrence counts; candidates are auto-promoted when they meet configurable thresholds (`min_confidence`, `min_frequency`, `auto_promote_types`); others queue for manual review (promote, reject, or merge as alias) via the Candidates tab at `/tracker`; API: `GET|POST /api/entity-candidates`, `GET /api/entity-candidates/{id}`, `POST /api/entity-candidates/{id}/{promote,reject,merge}`, `DELETE /api/entity-candidates/{id}`; frontend: `/tracker` has three tabs — Entities, Candidates (admin only; the candidates API is admin-only) and Connections, synced to `?tab=`
- **Entity connections graph** — co-occurrence relationships between tracked entities are stored in the `entity_connections` table (`from_id`, `to_id`, `weight`, `updated_at`; `CHECK (from_id < to_id)` normalizes pair order; cascades on entity delete); the `entity_cooccurrence` internal pipeline reads `matched_ids` from the `entity_matcher` context and calls `UpsertEntityConnection` for every unique pair, incrementing weight on each co-occurrence; wired as a `chain_only` action chained after `entity_matcher` (no auto-wiring — user configures it in the UI); API: `GET /api/entity-connections` (returns all edges with joined entity names/types), `DELETE /api/entity-connections` (truncates table, admin only); frontend Connections tab renders a vis-network force-directed graph focused on a user-selected entity; controls: entity picker (searchable, required), depth slider (1–5, default 2), min-weight slider; nothing renders until an entity is selected; API: `GET /api/entity-connections/subgraph?entity_id=X&depth=Y&min_weight=Z` returns the BFS neighborhood (Go BFS in store layer); store: `useConnectionsStore` in `web/src/stores/connections.ts` (`subgraph` ref, `fetchSubgraph`, `graphData`)
- **Pipeline finished trigger** — `pipeline_finished` fires when every task in a top-level pipeline completes for a tweet, regardless of branch outcome; detected via reference counting in `pipeline_run_tracking (pipeline_id, tweet_id, active_tasks, event_payload)` — counter seeded at N (initial tasks pushed by `PushInitialTasksForGraph`), incremented by `pushTrackedTask` on each `producer.PushTask` call, decremented (via defer) in `processTask` after `PushContinuationTasks`; when `active_tasks` reaches 0, `firePipelineFinished` dispatches to pipelines linked via `trigger_type=pipeline_finished` scoped to the source pipeline ID; `RootPipelineID` on `queue.Task` propagates through all sub-pipeline tasks so every level of nesting counts against the same root counter; `trigger_pipeline_links.source_pipeline_id` scopes a `pipeline_finished` link to a specific pipeline — required for this trigger type; condition node operators `exists`/`not_exists` check for key presence in the merged metadata context (backend already supported; now exposed in UI)
- **Specialized Chat Actions** — interactive multi-turn LLM assistant at `/chat`; multiple chat actions can be configured, each with its own tool whitelist; `POST /api/chat/stream` body accepts optional `action_id` field to target a specific chat action (routes to that action's Ollama backend and allowed tools); `GET /api/chat/tools?action_id={id}` returns tool name+description pairs for a given action (used by frontend action-form picker); `BuildOllamaTools(allowed []string)` in `internal/mcpserver` filters the ToolDefs: `allowed` empty = all tools, non-empty = only matching tool names; `OllamaPipeline.AgentStream` drives the tool-calling loop: send messages + filtered `[]mcpserver.OllamaTool` definitions to Ollama (`stream:false`), execute any `tool_calls` via `ToolExecutor` (satisfied by `mcpserver.Dispatcher`), write SSE events (`tool_call`, `tool_result`, `text_chunk`, `[DONE]`), repeat until no tool_calls remain; `Dispatcher.Execute()` looks up the ToolDef by name and calls the API server's own loopback URL with admin credentials; `OllamaTool`/`OllamaToolFunc` types live in `internal/mcpserver` (not `inference`) to keep the CGO-free MCP binary buildable; `ToolExecutor` interface itself has no `mcpserver` dependency; `ChatPipeline` accepts `[]mcpserver.OllamaTool` so `internal/inference` imports `internal/mcpserver`, but no cycle exists since `mcpserver` does not import `inference`; the `Server` holds a `toolExec inference.ToolExecutor` field constructed in `cli/cmd/serve.go` via `mcpserver.NewDispatcherWithCredentials("http://127.0.0.1:{port}", adminUser, adminPass)`; Chat actions store `allowed_tools: []string` in config JSON (per-action tool selection, empty array = all tools); frontend Chat Pinia store uses `conversations: Record<string, DisplayMessage[]>` keyed by action ID; `selectedActionId` is persisted in localStorage; chat page renders an action selector dropdown; the frontend renders tool steps as collapsible `<details>` elements (args + result) above the streamed text answer; admin-only
- **Ollama integration** — LLM summarization and digest via local Ollama; backends are stored in SQLite (`ollama_backends` table) and managed via REST API (`GET|POST /api/ollama/backends`, `PUT|DELETE /api/ollama/backends/{id}`, `POST /api/ollama/backends/{id}/{enable,disable}`); at startup, backends are loaded from DB into `inference.BackendPool` which tracks health, score, and active-request count; each backend has a `max_concurrent` column (default 1) that acts as a semaphore matching `OLLAMA_NUM_PARALLEL` on the Ollama server — the pool allows up to `max_concurrent` simultaneous requests per backend instead of the old boolean `inUse` flag; `BackendStat` exposes `active_count` and `max_concurrent` (replaces `in_use`); pipeline-to-backend priority assignments are stored in `action_backends` table; `BackendPool.AcquireForPipeline(assignedIDs)` tries assigned backends in priority order then falls back to any backend with capacity; CLI: `tidewatch ollama backends {list,add,update,remove,enable,disable}` and `tidewatch action backends {list,set,unassign}`
- **WebSocket live updates** — `GET /api/ws` is an authenticated WebSocket endpoint; JWT is passed as `?token=` query param (header auth is not possible from browser WS); the server-side `Hub` (`internal/hub`) runs a fan-out goroutine that reads from an unbuffered channel and broadcasts JSON event messages to all connected `Client`s; each `Client` owns a write pump (drains a 256-item buffered channel) and a read pump (consumes pings/pongs, enforces read deadline); the `Broadcaster` interface (`Broadcast(msg []byte)`) is implemented by `Hub` and a `NopBroadcaster` (used in tests); the Hub is wired into: queue `Producer` (task enqueue/dequeue counts), worker `Manager` (execution results), `BackendPool` (backend health changes), stream `Client` (connect/disconnect), and API server (CRUD mutations on pipelines, actions, chains, etc.); event types carried as JSON `{"type":"...","payload":{...}}` include `queue_update` (payload: `{action_id, pending}` — per-action queue depth after a push or pop; global `pending_tasks`/`pending_ollama` counts come from the `GET /api/queue` poll), `worker_stats` (payload: `{total, active, idle, workers: WorkerInfo[]}` — all N worker slots; active workers first; `WorkerInfo` includes `id`, `status` ("active"|"idle"), `action_id`, `action`, `pipeline`, `tweet_id`, `started_at`, `duration_ms` (computed server-side), `backend_name` (display name for Ollama, empty otherwise)), `execution_result`, `backend_health`, `stream_status`, and `crud`; frontend composable `useWebSocket.ts` manages the WS connection with exponential-backoff reconnect and ping/pong keepalive; connection state is in `stores/ws.ts`; queue+worker stats, executions, and Ollama backends each have dedicated Pinia stores updated via WS events — polling for these has been removed from `QueueView.vue`, `OllamaView.vue`, and `App.vue`; nginx proxies `/api/ws` with `Upgrade` and `Connection` headers set
- **Web UI standard** — the SPA follows `docs/ui-standard.md` ("Tidewatch Console"): one dark theme (no theme switching); colour tokens are CSS variables named after shadcn-vue (`--background`, `--primary`, `--muted-foreground`, `--sidebar-*`, `--chart-*`) in `web/src/styles/main.css`, exposed to Tailwind via `@theme inline` and to Naive UI via `buildNaiveOverrides()` in `web/src/theme/naive.ts` (reads the variables at runtime); pages import wrappers from `@/components/ui` whose names and props mirror shadcn-vue (so a later move to shadcn-vue swaps wrapper internals only) and use Tailwind for layout instead of `n-space`/`n-flex`; `AppShell` (`web/src/components/layout`) provides the grouped sidebar (Monitor / Build / System; viewers see Map, Tweets, Tracker) and the live status bar; routes with `meta.fullBleed` get no page gutter; `npm run check:ui` (`web/scripts/check-ui.sh`, run in CI) rejects hex colours, inline `style=` and `n-*` tags outside `components/ui` in converted paths (its `CONVERTED` list grows as pages migrate)
- **Pipeline builder** — `/pipelines/:id/build` (admin, fullBleed) edits a pipeline's graph on a Vue Flow canvas laid out left-to-right by dagre on every structural change (nodes aren't draggable; saved `pos_x`/`pos_y` are whatever the layout produced); steps are added from the "+" beside each output (Start, action, sub-pipeline, condition Pass/Fail), which opens a searchable picker grouped by kind (Condition, Ollama, Notification, Internal, ONNX, Sub-pipelines; active only unless "Show inactive"); dragging output → input still connects nodes (merges); the selected node or connection opens in an inspector (floating panel from lg, Sheet below) whose edits apply live; Run from here (with unsaved changes it asks to save first, then finds the node again by its position in the save response, since saving re-creates node rows), Discard (confirms) and an unsaved-changes leave guard; the Start node lists the trigger types linked to the pipeline; long graphs whose fit zoom would be below 0.5 open at Start instead of fitted
- **Stream auto-connect** — `stream.auto_connect: true` connects to X on startup without a CLI call
- **Bulk delete endpoints** — every resource with a single-delete-by-ID also has a collection-level `DELETE /api/{resource}` endpoint (body: optional `ids` array + resource-specific filter fields; all optional; empty body = delete all; response: `{"deleted": N}`); affected resources: rules, sources, pipelines, triggers/links, tracked-entities, entity-candidates, actions, ollama/backends, notification-providers, notification-channels, search jobs, digest jobs; each is exposed as a `*_bulk_delete` MCP tool; existing single-delete routes are unchanged
- **MCP server** — `tidewatch-mcp` is a separate pure-Go binary that exposes all REST API endpoints as MCP tools; table-driven: each endpoint is a `ToolDef` in `internal/mcpserver/tools.go`; a `Dispatcher` translates tool calls into HTTP requests against tidewatch; auth is credential-based: `--username`/`--password` (or env `TIDEWATCH_MCP_USERNAME`/`TIDEWATCH_MCP_PASSWORD`) triggers automatic login via `POST /api/auth/login`, token is cached in memory, and on 401 the token is invalidated and re-acquired transparently (no manual token management needed); supports stdio (for Claude Code and desktop MCP clients) and HTTP/SSE (for programmatic clients and the future brain pipeline) — both transports can run simultaneously; configured via `--base-url` (default `http://localhost:8080`) and `--http` (optional SSE bind address); env vars: `TIDEWATCH_MCP_BASE_URL`, `TIDEWATCH_MCP_HTTP`, `TIDEWATCH_MCP_USERNAME`, `TIDEWATCH_MCP_PASSWORD`; built alongside the main binary in the same Dockerfile stage
- **UI runtime config** — `API_BASE_URL` and `API_UPSTREAM` are injected at container startup by `web/docker-entrypoint.sh`: it writes `env-config.js` (SPA reads `window.__ENV__.API_BASE_URL`) and runs `envsubst` on `nginx.conf.template` to produce the live nginx config. `API_BASE_URL` defaults to `""` (same-origin) so the SPA calls `/api/...` as relative paths; nginx proxies those to `API_UPSTREAM` (default `http://tidewatch:8080`) via Docker network — API traffic never leaves the server. Set `API_UPSTREAM` to override the backend target. `MAP_CENTER` (`"lat,lon"`) and `MAP_ZOOM` are also written into `env-config.js` and set the map's initial view (default: world view `[20, 0]`, zoom 2).

## Testing

- Unit tests: mock interfaces, in-memory SQLite (`:memory:`)
- Integration tests: `testcontainers-go` for Redis, gated behind `//go:build integration`
- ONNX tests: small test model in `testdata/`
- Packages that import `daulet/tokenizers` need the prebuilt static lib to link: download `libtokenizers.linux-amd64.tar.gz` from the daulet/tokenizers releases (version pinned in `Dockerfile` / `test.yml`) and run with `CGO_LDFLAGS=-L<dir>`
- CI (`.github/workflows/test.yml`) runs `gofmt -l`, `go vet ./...` and `go test -race ./...`, plus `npm run check:ui` and `npm run build` in `web/`, on every push/PR to `main` — keep it green

## Documentation Requirements

After every fix or feature, update all affected docs before considering the task done:

- **CLAUDE.md** — update Project Structure, Key Design Decisions, Tech Stack, or Code Conventions if any changed
- **README.md** — update features, configuration tables, CLI or REST API reference if they changed
- **`configs/`** — update example/default config files if new config keys were added
- **CLI help strings** — keep `Use`, `Short`, and `Long` fields on Cobra commands in sync with actual behavior

If a new component, table, queue key, or API route is introduced, it must appear in CLAUDE.md's Project Structure or Key Design Decisions (whichever fits) before the task is committed.

## Build Verification

Before committing any change that touches Go code, frontend code, or Dockerfiles, build the local container image to verify the build succeeds:

```bash
docker compose build --build-arg BUILDKIT_INLINE_CACHE=1 tidewatch
# or tag it explicitly
docker build -t tidewatch:local .
```

Do not push changes that have not been verified to build successfully.
