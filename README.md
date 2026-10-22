# tidewatch

[![Test](https://github.com/amir-the-h/tidewatch/actions/workflows/test.yml/badge.svg)](https://github.com/amir-the-h/tidewatch/actions/workflows/test.yml)
[![Docker](https://github.com/amir-the-h/tidewatch/actions/workflows/docker-publish.yml/badge.svg)](https://github.com/amir-the-h/tidewatch/actions/workflows/docker-publish.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

**Real-time social-stream intelligence, self-hosted.** tidewatch ingests the X (Twitter) Filtered Stream, runs every post through user-defined DAG pipelines of local ONNX models, Ollama LLMs and built-in handlers, and turns the results into a live map, an entity graph, digests and push alerts — all on your own hardware, in a single Go binary.

## Features

- **Visual DAG pipelines** — compose actions (ONNX classifier/NER, Ollama prompt, internal handler) into graphs with conditional edges (`==`, `contains`, `exists`, …), sub-pipelines and fan-in; hot-reloaded without restart.
- **Local inference** — ONNX Runtime (CUDA when available, CPU fallback) with HuggingFace tokenizers; Ollama backend pool with per-backend concurrency limits, health scoring and per-action backend priority.
- **Event-driven workers** — per-action Redis queues, `BRPOP` worker pool that only listens on queues it has capacity for (zero Redis traffic while idle), dead-letter queue, pipeline-completion tracking via reference counting.
- **Entity tracking** — NER-driven candidate discovery with auto-promotion thresholds, alias suggestions, and a co-occurrence graph explorer.
- **Geo** — coordinate extraction from LLM output, self-hosted Nominatim reverse geocoding with SQLite cache, date-heat map of events.
- **Alerts** — Pushover, Telegram and Discord Forum threads (with LLM-generated tags), templated with `${key}` substitution.
- **Agentic chat & MCP** — a tool-calling chat assistant over your data, plus a standalone MCP server exposing the full REST API to Claude and other MCP clients.
- **Live UI** — Vue 3 + Naive UI operations console (see [docs/ui-standard.md](docs/ui-standard.md)) with WebSocket push for queue depth, worker status and execution results; JWT auth with admin/viewer roles.

See [`docs/examples/geopolitical-monitoring`](docs/examples/geopolitical-monitoring) for a complete set of real-world detector → extractor → notification pipelines.

## Architecture

```
sources (source.Manager: x_stream = X Filtered Stream, x_search = X search, …)
                         │ connectors emit signals
                         ▼
                  source.Ingester ── saves each item as a signal in SQLite (deduped by ext_ns + external_id),
                         │           records which source saw it (signal_sources)
                         │ new signals: LPUSH one Task per linked new_signal pipeline entry
                         ▼
          Redis  tidewatch:tasks:{action_id}   (one list per action)
                         │ BRPOP across "ready" keys
                         ▼
                  worker.Manager  (N goroutines)
                    │  Ollama keys only polled while backend capacity is free
                    ├─► ActionCache (hot-reloadable action → pipeline map)
                    ├─► ONNX / Ollama / internal handler
                    ├─► SQLite: action_executions + metadata
                    ├─► evaluate DAG edges → LPUSH downstream tasks
                    ├─► tidewatch:failed  (dead-letter list)
                    └─► hub.Hub → WebSocket → UI

  api.Server      REST API (chi), JWT auth
  tidewatch-mcp   MCP server (stdio + HTTP/SSE), pure Go
  cli             Cobra CLI over the REST API
```

## Pipeline context

Every task an action runs gets a flat context map: the outputs of earlier actions on the same signal, then these keys, which actions can't overwrite. Notification templates and Ollama prompts use them as `${key}`.

| Key | Value |
|---|---|
| `signal_id` | integer signal ID |
| `signal_kind` | `post` or `article` |
| `source_id`, `source_name`, `source_type`, `source_tier` | the source that dispatched the signal; on reprocess and pipeline-run backfills, the first source that saw it; `source_tier` is absent for unrated sources |
| `author`, `title`, `text`, `url`, `lang`, `published_at` | from the signal (`title` is empty for posts) |
| `text_short` | `text` cut to 280 characters (`title` when there is no text) |
| X signals only | `x_tweet_id`, `x_author_id`, `x_referenced` (retweeted/quoted/replied_to IDs), `x_rule_ids` |

ONNX and Ollama actions receive the text for posts and `title + "\n\n" + text` for articles. The `twitter_retweet`, `twitter_like` and `twitter_quote` handlers act on `x_tweet_id`; the `rule_matches` condition checks `x_rule_ids`.

## Quickstart (Docker)

Requirements: Docker, an X API bearer token (Filtered Stream access for `x_stream`). Optional: an [Ollama](https://ollama.com) server, an NVIDIA GPU.

```bash
git clone https://github.com/amir-the-h/tidewatch && cd tidewatch
cp .env.example .env
# edit .env: TIDEWATCH_AUTH_JWT_SECRET (openssl rand -hex 32),
#            TIDEWATCH_AUTH_ADMIN_PASSWORD

# (optional) prepare a Nominatim extract for reverse geocoding
GEOFABRIK_REGIONS="europe/germany" ./scripts/prepare-nominatim.sh

docker compose up -d --build                 # CPU
# docker compose -f docker-compose.yml -f docker-compose.gpu.yml up -d --build   # NVIDIA GPU
```

Open http://localhost:3000 and sign in as the admin user. ONNX models go in `./models` (download with the HuggingFace `hf` CLI); Ollama backends are added at runtime from the UI.

X is configured as sources. Until the Sources page arrives, create them with the CLI (or the MCP `sources_create` tool):

```bash
tidewatch auth login --username admin --password …
tidewatch source create --type x_stream --name "X stream" --set bearer_token=$X_BEARER_TOKEN --set tier=pro --active
tidewatch source create --type x_search --name "X search" --set bearer_token=$X_BEARER_TOKEN --active   # optional
tidewatch source status
```

An active `x_stream` source is a connected stream: it syncs the active X rules to X when it starts and stays connected (reconnecting with backoff) until it is deactivated.

Later schema changes are numbered migrations (`002_sources.sql`, `003_drop_search_jobs.sql`, …) that apply on startup.

### Upgrading from phase 1 step 2

`x.bearer_token`, `x.tier`, `stream.auto_connect` and the `search.*` block (and `TIDEWATCH_X_BEARER_TOKEN`, `TIDEWATCH_X_TIER`, `TIDEWATCH_STREAM_AUTO_CONNECT`) are gone and ignored if still set. Create an `x_stream` source with the bearer token (and an `x_search` source if the search scheduler was enabled) as above.

### Upgrading from v0.2 or earlier

The schema was reset when tweets became signals (one `migrations/001_schema.sql` replaces the old 29 migrations) and an old database is not upgraded: `serve` refuses to start on one. Stop the stack, move `data/tidewatch.db` aside (nothing reads it any more) and start again; the admin user is seeded again from config, and actions, pipelines, rules and the rest need to be recreated.

### Native build prerequisites

Docker is the preferred way to build and run tidewatch. Native builds require Go 1.26+, CGO, the prebuilt [`libtokenizers`](https://github.com/daulet/tokenizers/releases) static library, the ONNX Runtime shared library, and Redis.

## Configuration

The server reads a YAML config (`configs/default.yaml`, copied into the image as `/configs/config.yaml`); every key can be overridden with a `TIDEWATCH_*` environment variable.

```yaml
redis:
  addr: localhost:6379
  db: 0

sqlite:
  path: ./data/tidewatch.db

api:
  bind: 127.0.0.1:8080

worker:
  count: 0             # 0 = one worker per active action (min 1)

sources:
  max_concurrent_polls: 8  # poll-source fetches running at once, across all sources

inference:
  models_dir: ./models

ollama:
  base_url: http://localhost:11434

nominatim_url: http://nominatim:8080

auth:
  jwt_secret: ""       # REQUIRED — the server refuses to start without it
  admin_username: admin
  admin_password: ""   # initial admin, seeded on first startup only

twitter:               # optional: OAuth 2.0 PKCE for retweet/like/quote actions
  client_id: ""
  callback_url: ""

log:
  level: info          # debug | info | warn | error
  format: text         # text | json
```

Key environment variables:

| Env var | Config field |
|---|---|
| `TIDEWATCH_REDIS_ADDR` | `redis.addr` |
| `TIDEWATCH_REDIS_PASSWORD` | `redis.password` |
| `TIDEWATCH_SQLITE_PATH` | `sqlite.path` |
| `TIDEWATCH_API_BIND` | `api.bind` |
| `TIDEWATCH_WORKER_COUNT` | `worker.count` |
| `TIDEWATCH_SOURCES_MAX_CONCURRENT_POLLS` | `sources.max_concurrent_polls` |
| `TIDEWATCH_ONNX_LIB_PATH` | `inference.onnx_lib_path` |
| `TIDEWATCH_MODELS_DIR` | `inference.models_dir` |
| `TIDEWATCH_OLLAMA_BASE_URL` | `ollama.base_url` |
| `TIDEWATCH_NOMINATIM_URL` | `nominatim_url` |
| `TIDEWATCH_AUTH_JWT_SECRET` | `auth.jwt_secret` |
| `TIDEWATCH_AUTH_ADMIN_USERNAME` | `auth.admin_username` |
| `TIDEWATCH_AUTH_ADMIN_PASSWORD` | `auth.admin_password` |
| `TIDEWATCH_TWITTER_CLIENT_ID` | `twitter.client_id` |
| `TIDEWATCH_TWITTER_CALLBACK_URL` | `twitter.callback_url` |
| `TIDEWATCH_LOG_LEVEL` | `log.level` |
| `TIDEWATCH_LOG_FORMAT` | `log.format` |

UI container (`tidewatch-ui`) variables: `API_UPSTREAM` (backend URL, default `http://tidewatch:8080`), `API_BASE_URL` (default same-origin), `MAP_CENTER` (`"lat,lon"`) and `MAP_ZOOM` for the map's initial view, `UI_PORT` (host port, default `3000`).

## CLI

The `tidewatch` binary exposes a Cobra CLI to manage the running server:

```bash
# Authentication
tidewatch auth login --username admin --password secret
tidewatch auth logout

# Sources (configured connector instances)
tidewatch source types
tidewatch source list [--type rss] [--active true]
tidewatch source create --type <type> --name "BBC World" --set url=https://… --tier 2 --active
tidewatch source update <id> --set key=value --interval 600
tidewatch source activate|deactivate|delete <id>
tidewatch source fetch <id>                 # poll once now
tidewatch source backfill <id> --params '{"start_time":"2026-10-01T00:00:00Z","end_time":"2026-10-08T00:00:00Z","limit":500}'   # x_search
tidewatch source status
tidewatch source runs <id>
tidewatch source run <run-id>

# X rule builder: terms (rule atoms) and Filtered Stream rules
tidewatch x term list
tidewatch x term add --type account --value @username
tidewatch x term add --type keyword --value "nuclear"
tidewatch x term remove <id>
tidewatch x rule list
tidewatch x rule add iran --expr '{"OR":[{"src":1},{"src":2}]}'
tidewatch x rule activate <id>
tidewatch x rule deactivate <id>

# Actions (execution units)
tidewatch action list
tidewatch action activate <id>
tidewatch action deactivate <id>
tidewatch action backends list <action-id>
tidewatch action backends set <action-id> <backend-id> --priority 1

# Pipelines (DAG workflows)
tidewatch pipeline list
tidewatch pipeline activate <id>
tidewatch pipeline deactivate <id>
tidewatch pipeline run <id>

# Ollama backends
tidewatch ollama backends list
tidewatch ollama backends add --url http://localhost:11434 --max-concurrent 2
tidewatch ollama backends enable <id>
tidewatch ollama backends disable <id>

# Signals (ingested posts and articles)
tidewatch signal list [--kind post|article] [--rule-id <id>] [--from <RFC3339>] [--to <RFC3339>] [--page 1] [--per-page 20]
tidewatch signal show <id>
tidewatch signal reprocess --signal-ids 1,2 [--action-ids 5,6]
tidewatch signal delete --ids 1,2

# Health
tidewatch health

# Custom API base URL (default: http://localhost:8080)
tidewatch --base-url http://remote-host:8080 health
```

## REST API

All routes are under `/api`. JWT bearer token required (except `POST /api/auth/login`).

### Auth & Users
| Method | Path | Description |
|---|---|---|
| `POST` | `/api/auth/login` | Login, returns JWT |
| `GET` | `/api/users` | List users (admin) |
| `POST` | `/api/users` | Create user (admin) |
| `PUT` | `/api/users/{id}` | Update user (admin) |
| `DELETE` | `/api/users/{id}` | Delete user (admin) |

### Health
| Method | Path | Description |
|---|---|---|
| `GET` | `/api/health` | Health check |
| `GET` | `/api/ws` | WebSocket live-update feed (`?token=<jwt>`) |

### Sources
A source is a configured instance of a connector type (admin only). Active sources run in the server: stream sources stay connected and restart with backoff (5 s doubling to 5 min), poll sources are fetched every interval ±10%. Secret config fields come back as `"••••"`; a `PUT` that sends `"••••"` keeps the stored value. Connector types are registered in code; the RSS connector arrives later in phase 1.

| Type | Mode | Config | Notes |
|---|---|---|---|
| `x_stream` | stream | `bearer_token` (secret, required), `tier` (`basic`, `pro`, `enterprise`: the X API plan) | X Filtered Stream; at most one active. Syncs the active non-local X rules to X when it starts (keeps matching ones, adopts rules already on X by value, adds the rest, deletes rules nothing claims); rule changes through `/api/x/rules` reach X through it. A connection silent for 90 s is dropped and reconnected |
| `x_search` | poll (default 15 min, minimum 5 min) | `bearer_token` (secret, required), `tier` (`pro`/`enterprise` use full-archive search), `query` or `rule_ids` (neither = every active non-local rule), `max_results`, `limit` (per query per poll), `start_time` (until a query has a `since_id`), `tweet_fields`, `user_fields`, `expansions` | The cursor keeps the newest tweet ID per query, so restarts don't re-fetch. A poll fails only when every query fails. Backfill params: `start_time`, `end_time` (RFC 3339), `limit` |

Both emit `kind=post` signals in the `x` namespace, so a tweet seen by both is stored once with two `signal_sources` rows.

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/sources` | List sources (filter: `type`, `active`) |
| `POST` | `/api/sources` | Create a source: `name`, `type`, `config`, `poll_interval_s`, `tier` (1–4), `state_affiliated`, `region`, `language`, `active` |
| `GET` | `/api/sources/{id}` | Get a source |
| `PUT` | `/api/sources/{id}` | Update the fields sent (`clear_poll_interval`, `clear_tier` reset those); the type can't change |
| `DELETE` | `/api/sources/{id}` | Delete a source (its signals stay) |
| `DELETE` | `/api/sources` | Bulk delete (`ids`, `active`, `type`) |
| `POST` | `/api/sources/{id}/activate` | Start a source (refused past the connector's `max_instances`) |
| `POST` | `/api/sources/{id}/deactivate` | Stop a source |
| `POST` | `/api/sources/{id}/fetch` | Poll once now, active or not → 202 with a source run |
| `POST` | `/api/sources/{id}/backfill` | Historical fetch with connector-defined params → 202 with a source run |
| `GET` | `/api/sources/types` | Connector types: mode, config fields, intervals, backfill support |
| `GET` | `/api/sources/status` | Runtime status per source: state (`running`, `idle`, `backoff`, `error`, `stopped`), last run/success/error, next run, fetched/new counters |
| `GET` | `/api/sources/{id}/runs` | A source's runs, newest first (`limit`) |
| `GET` | `/api/source-runs/{id}` | One run: `status` (`pending`, `running`, `done`, `error`), `fetched_count`, `new_count` |

### X rules and terms
Terms are the rule atoms (keyword, hashtag, `from:` account…) that X Filtered Stream rules combine.

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/x/terms` | List terms |
| `POST` | `/api/x/terms` | Add a term |
| `PUT` | `/api/x/terms/{id}` | Update a term |
| `DELETE` | `/api/x/terms/{id}` | Remove a term |
| `DELETE` | `/api/x/terms` | Bulk delete terms |
| `POST` | `/api/x/terms/{id}/deactivate` | Deactivate a term |
| `GET` | `/api/x/rules` | List rules |
| `POST` | `/api/x/rules` | Create a rule |
| `GET` | `/api/x/rules/{id}` | Get a rule |
| `PUT` | `/api/x/rules/{id}` | Update a rule |
| `DELETE` | `/api/x/rules/{id}` | Delete a rule |
| `DELETE` | `/api/x/rules` | Bulk delete rules |
| `POST` | `/api/x/rules/{id}/activate` | Activate a rule (adds it on X) |
| `POST` | `/api/x/rules/{id}/deactivate` | Deactivate a rule (removes it from X) |

Changes to non-local rules reach X through the active `x_stream` source (a refusal from X is a `502`). With no active `x_stream` source they are only stored, and the stream syncs them when it next starts. Rule length is validated for that source's `tier` (`basic` without one).

### Signals
A signal is one ingested item (an X post today; RSS articles later in phase 1). `{id}` is the integer signal ID; the upstream ID (e.g. the tweet ID) is `external_id`.

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/signals` | List signals, newest first (filter: kind, rule_id, source_id, source_type, from, to, page, per_page) → `{signals, total}` |
| `GET` | `/api/signals/{id}` | Get a signal, the sources that saw it, its matched X rules and its action executions |
| `DELETE` | `/api/signals` | Delete signals by `ids` (executions, metadata, rule matches and entity tracks cascade) |
| `POST` | `/api/signals/reprocess` | Re-enqueue `signal_ids` through the `new_signal` pipelines, or through `action_ids` |
| `DELETE` | `/api/signals/noise` | Delete X signals that match no rule, or only local rules |

### Actions (execution units)
| Method | Path | Description |
|---|---|---|
| `GET` | `/api/actions` | List actions |
| `POST` | `/api/actions` | Create an action |
| `GET` | `/api/actions/{id}` | Get an action |
| `PUT` | `/api/actions/{id}` | Update an action |
| `DELETE` | `/api/actions/{id}` | Delete an action |
| `DELETE` | `/api/actions` | Bulk delete actions |
| `POST` | `/api/actions/{id}/activate` | Activate an action |
| `POST` | `/api/actions/{id}/deactivate` | Deactivate an action |
| `GET` | `/api/actions/handlers` | List available internal handler types |
| `GET` | `/api/actions/{id}/executions` | List executions for an action |
| `GET` | `/api/action-backends` | List action→backend assignments |
| `POST` | `/api/action-backends` | Assign backend to action |
| `DELETE` | `/api/action-backends/{id}` | Remove assignment |

### Pipelines (DAG workflows)
| Method | Path | Description |
|---|---|---|
| `GET` | `/api/pipelines` | List pipelines |
| `POST` | `/api/pipelines` | Create a pipeline |
| `GET` | `/api/pipelines/{id}` | Get a pipeline |
| `PUT` | `/api/pipelines/{id}` | Update a pipeline |
| `DELETE` | `/api/pipelines/{id}` | Delete a pipeline |
| `DELETE` | `/api/pipelines` | Bulk delete pipelines |
| `POST` | `/api/pipelines/{id}/activate` | Activate a pipeline |
| `POST` | `/api/pipelines/{id}/deactivate` | Deactivate a pipeline |
| `POST` | `/api/pipelines/{id}/run` | Manual backfill run |
| `GET` | `/api/pipelines/{id}/graph` | Get pipeline DAG |
| `PUT` | `/api/pipelines/{id}/graph` | Save pipeline DAG |

### Triggers
| Method | Path | Description |
|---|---|---|
| `GET` | `/api/triggers` | List trigger types |
| `GET` | `/api/triggers/links` | List trigger→pipeline links |
| `POST` | `/api/triggers/links` | Create a link |
| `DELETE` | `/api/triggers/links/{id}` | Delete a link |
| `DELETE` | `/api/triggers/links` | Bulk delete links |
| `POST` | `/api/triggers/links/{id}/activate` | Activate a link |
| `POST` | `/api/triggers/links/{id}/deactivate` | Deactivate a link |

### Executions
| Method | Path | Description |
|---|---|---|
| `GET` | `/api/executions` | List action executions (filter: signal_id, action_id, status, from, to) |
| `DELETE` | `/api/executions` | Bulk delete executions (`signal_ids`, `action_ids`, `pipeline_id`) |
| `GET` | `/api/executions/{id}/metadata` | Get execution metadata |

### Ollama Backends
| Method | Path | Description |
|---|---|---|
| `GET` | `/api/ollama/backends` | List Ollama backends |
| `POST` | `/api/ollama/backends` | Add a backend |
| `PUT` | `/api/ollama/backends/{id}` | Update a backend |
| `DELETE` | `/api/ollama/backends/{id}` | Remove a backend |
| `DELETE` | `/api/ollama/backends` | Bulk delete backends |
| `POST` | `/api/ollama/backends/{id}/enable` | Enable a backend |
| `POST` | `/api/ollama/backends/{id}/disable` | Disable a backend |

### ONNX Models
| Method | Path | Description |
|---|---|---|
| `GET` | `/api/models` | List registered ONNX models |
| `POST` | `/api/models` | Register a model |
| `POST` | `/api/models/download` | Download from HuggingFace |
| `DELETE` | `/api/models/{id}` | Remove a model |
| `POST` | `/api/models/{id}/activate` | Activate + hot-reload |
| `POST` | `/api/models/{id}/deactivate` | Deactivate + hot-reload |
| `POST` | `/api/models/{id}/run` | Run on-demand |
| `GET` | `/api/models/{id}/labels` | List model labels |
| `POST` | `/api/models/{id}/labels` | Add a label |
| `DELETE` | `/api/models/{id}/labels/{label}` | Remove a label |

### Tracked Entities & Candidates
| Method | Path | Description |
|---|---|---|
| `GET` | `/api/tracked-entities` | List tracked entities |
| `POST` | `/api/tracked-entities` | Create a tracked entity |
| `GET` | `/api/tracked-entities/{id}` | Get a tracked entity |
| `PUT` | `/api/tracked-entities/{id}` | Update a tracked entity |
| `DELETE` | `/api/tracked-entities/{id}` | Delete a tracked entity |
| `DELETE` | `/api/tracked-entities` | Bulk delete entities |
| `POST` | `/api/tracked-entities/{id}/activate` | Activate |
| `POST` | `/api/tracked-entities/{id}/deactivate` | Deactivate |
| `GET` | `/api/tracked-entities/{id}/tracks` | List entity tracks |
| `GET` | `/api/tracked-entities/{id}/alias-suggestions` | NER-derived alias candidates |
| `GET` | `/api/alias-suggestions` | Alias suggestions by name+type |
| `GET` | `/api/entity-candidates` | List entity candidates |
| `GET` | `/api/entity-candidates/{id}` | Get a candidate |
| `DELETE` | `/api/entity-candidates/{id}` | Delete a candidate |
| `DELETE` | `/api/entity-candidates` | Bulk delete candidates |
| `POST` | `/api/entity-candidates/{id}/promote` | Promote to tracked entity |
| `POST` | `/api/entity-candidates/{id}/reject` | Reject candidate |
| `POST` | `/api/entity-candidates/{id}/merge` | Merge as alias |
| `GET` | `/api/entity-connections` | List co-occurrence connections |
| `DELETE` | `/api/entity-connections` | Clear all connections |
| `GET` | `/api/entity-connections/subgraph` | BFS subgraph for a given entity |

### Notifications
Provider credentials (`app_token`, `user_key`, `bot_token`) come back as `"••••"`; an update that sends `"••••"` keeps the stored value.

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/notification-providers` | List providers |
| `POST` | `/api/notification-providers` | Create provider (Pushover/Telegram) |
| `GET` | `/api/notification-providers/{id}` | Get provider |
| `PUT` | `/api/notification-providers/{id}` | Update provider |
| `DELETE` | `/api/notification-providers/{id}` | Delete provider |
| `DELETE` | `/api/notification-providers` | Bulk delete |
| `POST` | `/api/notification-providers/{id}/activate` | Activate |
| `POST` | `/api/notification-providers/{id}/deactivate` | Deactivate |
| `GET` | `/api/notification-channels` | List channels |
| `POST` | `/api/notification-channels` | Create channel |
| `GET` | `/api/notification-channels/{id}` | Get channel |
| `PUT` | `/api/notification-channels/{id}` | Update channel |
| `DELETE` | `/api/notification-channels/{id}` | Delete channel |
| `DELETE` | `/api/notification-channels` | Bulk delete |
| `POST` | `/api/notification-channels/{id}/activate` | Activate |
| `POST` | `/api/notification-channels/{id}/deactivate` | Deactivate |

### Summaries & Digests
| Method | Path | Description |
|---|---|---|
| `GET` | `/api/summaries` | List signal summaries |
| `POST` | `/api/summaries/run` | Trigger summary generation |
| `GET` | `/api/digest` | List digest jobs |
| `POST` | `/api/digest` | Create a digest job |
| `GET` | `/api/digest/{id}` | Get digest job status + result |
| `DELETE` | `/api/digest/{id}` | Delete a digest job |
| `DELETE` | `/api/digest` | Bulk delete digest jobs |

### Map
| Method | Path | Description |
|---|---|---|
| `GET` | `/api/map` | List geo-event pins |

### Queue & Failures
| Method | Path | Description |
|---|---|---|
| `GET` | `/api/queue` | Global queue status: pending count + per-worker details |
| `DELETE` | `/api/queue` | Flush all pending task queues |
| `DELETE` | `/api/workers/buffer` | Cancel actively-executing workers |
| `GET` | `/api/failures` | List dead-letter queue items |
| `DELETE` | `/api/failures` | Clear dead-letter queue |

### Twitter OAuth & Chat
| Method | Path | Description |
|---|---|---|
| `GET` | `/api/twitter/auth/start` | Begin OAuth 2.0 PKCE flow |
| `GET` | `/api/twitter/auth/callback` | OAuth callback |
| `GET` | `/api/twitter/auth/status` | Connection status |
| `DELETE` | `/api/twitter/auth` | Disconnect Twitter account |
| `POST` | `/api/chat/stream` | Streaming chat with tool use |
| `GET` | `/api/chat/tools` | List tools available to a chat action |

## Project Structure

```
cmd/tidewatch/       → binary entrypoint + lifecycle wiring (CGO)
cmd/tidewatch-mcp/   → MCP server binary entrypoint (pure Go, no CGO)
cli/                    → Cobra CLI subcommands (thin REST client)
internal/
  config/               → YAML config + env overrides
  model/                → Domain types (Signal, Source, Action, Pipeline, etc.)
  store/                → SQLite repository (DELETE journal mode, embedded migrations)
  queue/                → Redis list producer/consumer + dead-letter queue
  inference/            → ONNX pipeline interface, Ollama pipeline, BackendPool
  pipeline/             → Internal pipeline registry (entity_matcher, entity_cooccurrence,
                          reverse_geocoder, twitter_*, notification_*, entity_discovery)
  twitter/              → Twitter OAuth 2.0 PKCE client + API calls (Retweet/Like/Quote)
  source/               → Connector framework: registry, manager (supervised source goroutines), shared ingest
    x/                  → X connectors (x_stream, x_search), rule compiler, validator, rules API client
  worker/               → Worker Manager + ActionCache (hot-reloadable)
  api/                  → REST API (chi)
  notifier/             → Async push notifications (Pushover, Telegram, Discord Bot)
  transform/            → Output transforms (e.g. NER entity grouping)
  auth/                 → JWT (HS256) + bcrypt helpers + HTTP middleware
  hub/                  → WebSocket hub (fan-out broadcaster)
  mcpserver/            → MCP server: ToolDef registry, Dispatcher, stdio/SSE transports
migrations/             → Embedded SQL migration files
configs/                → Default config files
models/                 → ONNX model files (gitignored)
web/                    → Vue 3 frontend (Vite, Pinia, Naive UI, Tailwind v4)
```

## Development

```bash
# Unit tests
go test ./...

# Integration tests (requires Docker for Redis container)
go test -tags integration ./...

# Vet
go vet ./...
```

Native `go build` / `go run` is not recommended — the binary requires CGO, Rust toolchain, and ONNX Runtime shared libraries. Use Docker instead.

## MCP Server

`tidewatch-mcp` exposes all REST API endpoints as MCP tools for use with Claude Code and other MCP clients.

```bash
# stdio (Claude Code)
tidewatch-mcp --base-url http://localhost:8080 --username admin --password secret

# HTTP/SSE
tidewatch-mcp --base-url http://localhost:8080 --http :9090 --username admin --password secret
```

Environment variables: `TIDEWATCH_MCP_BASE_URL`, `TIDEWATCH_MCP_HTTP`, `TIDEWATCH_MCP_USERNAME`, `TIDEWATCH_MCP_PASSWORD`.

## License

[MIT](LICENSE)
