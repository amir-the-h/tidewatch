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
- **Live UI** — Vue 3 + Naive UI dashboard with WebSocket push for queue depth, worker status and execution results; JWT auth with admin/viewer roles.

See [`docs/examples/geopolitical-monitoring`](docs/examples/geopolitical-monitoring) for a complete set of real-world detector → extractor → notification pipelines.

## Architecture

```
X Filtered Stream (SSE) ─┐   X search backfill ─┐
                         ▼                      ▼
                  stream.Client  ── saves tweet to SQLite
                         │ LPUSH one Task per subscribed action
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

## Quickstart (Docker)

Requirements: Docker, an X API bearer token with Filtered Stream access. Optional: an [Ollama](https://ollama.com) server, an NVIDIA GPU.

```bash
git clone https://github.com/amir-the-h/tidewatch && cd tidewatch
cp .env.example .env
# edit .env: TIDEWATCH_X_BEARER_TOKEN, TIDEWATCH_AUTH_JWT_SECRET (openssl rand -hex 32),
#            TIDEWATCH_AUTH_ADMIN_PASSWORD

# (optional) prepare a Nominatim extract for reverse geocoding
GEOFABRIK_REGIONS="europe/germany" ./scripts/prepare-nominatim.sh

docker compose up -d --build                 # CPU
# docker compose -f docker-compose.yml -f docker-compose.gpu.yml up -d --build   # NVIDIA GPU
```

Open http://localhost:3000 and sign in as the admin user. ONNX models go in `./models` (download with the HuggingFace `hf` CLI); Ollama backends are added at runtime from the UI.

### Native build prerequisites

Docker is the preferred way to build and run tidewatch. Native builds require Go 1.26+, CGO, the prebuilt [`libtokenizers`](https://github.com/daulet/tokenizers/releases) static library, the ONNX Runtime shared library, and Redis.

## Configuration

The server reads a YAML config (`configs/default.yaml`, copied into the image as `/configs/config.yaml`); every key can be overridden with a `TIDEWATCH_*` environment variable.

```yaml
x:
  tier: basic          # "basic" or "pro"

redis:
  addr: localhost:6379
  db: 0

sqlite:
  path: ./data/tidewatch.db

api:
  bind: 127.0.0.1:8080

worker:
  count: 0             # 0 = one worker per active action (min 1)

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

stream:
  auto_connect: false  # auto-connect to X stream on startup

search:
  enabled: false
  interval: 15m
  max_results: 100
  limit: 0
```

Key environment variables:

| Env var | Config field |
|---|---|
| `TIDEWATCH_X_BEARER_TOKEN` | `x.bearer_token` |
| `TIDEWATCH_X_TIER` | `x.tier` |
| `TIDEWATCH_REDIS_ADDR` | `redis.addr` |
| `TIDEWATCH_REDIS_PASSWORD` | `redis.password` |
| `TIDEWATCH_SQLITE_PATH` | `sqlite.path` |
| `TIDEWATCH_API_BIND` | `api.bind` |
| `TIDEWATCH_WORKER_COUNT` | `worker.count` |
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
| `TIDEWATCH_STREAM_AUTO_CONNECT` | `stream.auto_connect` |

UI container (`tidewatch-ui`) variables: `API_UPSTREAM` (backend URL, default `http://tidewatch:8080`), `API_BASE_URL` (default same-origin), `MAP_CENTER` (`"lat,lon"`) and `MAP_ZOOM` for the map's initial view, `UI_PORT` (host port, default `3000`).

## CLI

The `tidewatch` binary exposes a Cobra CLI to manage the running server:

```bash
# Authentication
tidewatch auth login --username admin --password secret
tidewatch auth logout

# Sources (stream filter rules)
tidewatch source list
tidewatch source add --type account --value @username
tidewatch source add --type keyword --value "nuclear"
tidewatch source remove <id>

# Stream filter rules
tidewatch rule list
tidewatch rule add --value "keyword lang:fa"
tidewatch rule activate <id>
tidewatch rule deactivate <id>

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

# Tweets
tidewatch tweet list [--source-id <id>] [--from <RFC3339>] [--to <RFC3339>] [--page 1] [--per-page 20]
tidewatch tweet show <id>
tidewatch tweet reprocess [--tweet-ids id1,id2]

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

### Health & Stream
| Method | Path | Description |
|---|---|---|
| `GET` | `/api/health` | Health check |
| `GET` | `/api/ws` | WebSocket live-update feed (`?token=<jwt>`) |
| `GET` | `/api/stream/status` | Stream connection status |
| `POST` | `/api/stream/connect` | Connect to X filtered stream |
| `POST` | `/api/stream/disconnect` | Disconnect from X filtered stream |

### Sources & Rules
| Method | Path | Description |
|---|---|---|
| `GET` | `/api/sources` | List sources |
| `POST` | `/api/sources` | Add a source |
| `PUT` | `/api/sources/{id}` | Update a source |
| `DELETE` | `/api/sources/{id}` | Remove a source |
| `DELETE` | `/api/sources` | Bulk delete sources |
| `POST` | `/api/sources/{id}/deactivate` | Deactivate a source |
| `GET` | `/api/rules` | List stream filter rules |
| `POST` | `/api/rules` | Create a rule |
| `GET` | `/api/rules/{id}` | Get a rule |
| `PUT` | `/api/rules/{id}` | Update a rule |
| `DELETE` | `/api/rules/{id}` | Delete a rule |
| `DELETE` | `/api/rules` | Bulk delete rules |
| `POST` | `/api/rules/{id}/activate` | Activate a rule (syncs to X) |
| `POST` | `/api/rules/{id}/deactivate` | Deactivate a rule (removes from X) |

### Tweets
| Method | Path | Description |
|---|---|---|
| `GET` | `/api/tweets` | List tweets (filter: source_id, from, to, page, per_page) |
| `GET` | `/api/tweets/{id}` | Get tweet + categorizations |
| `DELETE` | `/api/tweets` | Bulk delete tweets |
| `POST` | `/api/tweets/reprocess` | Re-enqueue tweets |
| `DELETE` | `/api/tweets/noise` | Delete tweets classified as noise |

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
| `GET` | `/api/executions` | List action executions |
| `DELETE` | `/api/executions/{id}` | Delete an execution |
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
| `GET` | `/api/summaries` | List tweet summaries |
| `POST` | `/api/summaries/run` | Trigger summary generation |
| `GET` | `/api/digest` | List digest jobs |
| `POST` | `/api/digest` | Create a digest job |
| `GET` | `/api/digest/{id}` | Get digest job status + result |
| `DELETE` | `/api/digest/{id}` | Delete a digest job |
| `DELETE` | `/api/digest` | Bulk delete digest jobs |

### Search & Map
| Method | Path | Description |
|---|---|---|
| `POST` | `/api/search/run` | Trigger a backfill search |
| `GET` | `/api/search/{id}` | Get search job status |
| `DELETE` | `/api/search/{id}` | Delete a search job |
| `DELETE` | `/api/search` | Bulk delete search jobs |
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
  model/                → Domain types (Tweet, Source, Action, Pipeline, etc.)
  store/                → SQLite repository (DELETE journal mode, embedded migrations)
  queue/                → Redis list producer/consumer + dead-letter queue
  inference/            → ONNX pipeline interface, Ollama pipeline, BackendPool
  pipeline/             → Internal pipeline registry (entity_matcher, entity_cooccurrence,
                          reverse_geocoder, twitter_*, notification_*, entity_discovery)
  twitter/              → Twitter OAuth 2.0 PKCE client + API calls (Retweet/Like/Quote)
  stream/               → X Filtered Stream SSE client
  worker/               → Worker Manager + ActionCache (hot-reloadable)
  api/                  → REST API (chi)
  notifier/             → Async push notifications (Pushover, Telegram, Discord Bot)
  search/               → X search API backfill scheduler
  transform/            → Output transforms (e.g. NER entity grouping)
  auth/                 → JWT (HS256) + bcrypt helpers + HTTP middleware
  hub/                  → WebSocket hub (fan-out broadcaster)
  mcpserver/            → MCP server: ToolDef registry, Dispatcher, stdio/SSE transports
migrations/             → Embedded SQL migration files
configs/                → Default config files
models/                 → ONNX model files (gitignored)
web/                    → Vue 3 frontend (Vite, Pinia, Naive UI)
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
