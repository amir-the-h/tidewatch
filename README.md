# iran-watcher

Monitors X (Twitter) Filtered Stream for Iran-related news. Stores tweets in SQLite, queues through Redis, categorizes via local ONNX models, and runs LLM summarization via Ollama. Pure Go — no Python.

## Architecture

```
X Filtered Stream (SSE)
        │
        ▼
  stream.Client          ← connects, normalizes, saves raw tweet to SQLite
        │ LPUSH Task per matching action
        ▼
  Redis (iran-watcher:tasks:{action_id})
        │ BRPOP
        ▼
  worker.Manager         ← N goroutines, each independently BRPOP from ready action queues
        │
        ├─► ActionCache  ← hot-reloadable in-memory map of action_id → (Action + Pipeline)
        │
        ├─► ONNX / Ollama / Internal pipelines
        │
        ├─► SQLite (action_executions, action_execution_metadata)
        │
        ├─► Pipeline DAG edge evaluation → downstream tasks pushed to Redis
        │
        ├─► Redis (iran-watcher:failed)  ← dead-letter for failures
        │
        └─► hub.Hub → WebSocket broadcast to connected UI clients

  api.Server             ← REST API (chi), JWT-authenticated
  iran-watcher-mcp       ← MCP server (stdio + HTTP/SSE), pure Go
  cli                    ← thin Cobra CLI over the REST API
```

## Prerequisites

Docker is the preferred way to build and run iran-watcher. Native builds require:

- Go 1.25.5+
- CGO enabled
- Rust toolchain (required by `daulet/tokenizers`)
- ONNX Runtime shared library installed on system
- [Hugging Face CLI](https://huggingface.co/docs/huggingface_hub/guides/cli) (`hf`) for model downloads
- Redis
- X (Twitter) API bearer token with Filtered Stream access

## Running with Docker (recommended)

```bash
# Build and start all services
docker compose build
docker compose up -d

# Rebuild after code changes
docker compose build iran-watcher
docker compose up -d --no-deps iran-watcher

# View logs
docker compose logs -f iran-watcher

# Stop
docker compose down
```

## Configuration

Copy the default config and set your bearer token:

```bash
cp configs/iran-watcher.yaml my-config.yaml
export IRAN_WATCHER_X_BEARER_TOKEN="your-bearer-token"
```

`configs/iran-watcher.yaml` defaults:

```yaml
x:
  tier: basic          # "basic" or "pro"

redis:
  addr: localhost:6379
  db: 0

sqlite:
  path: ./data/iran-watcher.db

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
  jwt_secret: ""       # required; set via IRAN_WATCHER_AUTH_JWT_SECRET
  admin_username: admin
  admin_password: ""   # required; set via ADMIN_PASSWORD

twitter:
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
| `IRAN_WATCHER_X_BEARER_TOKEN` | `x.bearer_token` |
| `IRAN_WATCHER_X_TIER` | `x.tier` |
| `IRAN_WATCHER_REDIS_ADDR` | `redis.addr` |
| `IRAN_WATCHER_REDIS_PASSWORD` | `redis.password` |
| `IRAN_WATCHER_SQLITE_PATH` | `sqlite.path` |
| `IRAN_WATCHER_API_BIND` | `api.bind` |
| `IRAN_WATCHER_WORKER_COUNT` | `worker.count` |
| `IRAN_WATCHER_ONNX_LIB_PATH` | `inference.onnx_lib_path` |
| `IRAN_WATCHER_MODELS_DIR` | `inference.models_dir` |
| `IRAN_WATCHER_OLLAMA_BASE_URL` | `ollama.base_url` |
| `IRAN_WATCHER_NOMINATIM_URL` | `nominatim_url` |
| `IRAN_WATCHER_AUTH_JWT_SECRET` | `auth.jwt_secret` |
| `ADMIN_USERNAME` | `auth.admin_username` |
| `ADMIN_PASSWORD` | `auth.admin_password` |
| `IRAN_WATCHER_TWITTER_CLIENT_ID` | `twitter.client_id` |
| `IRAN_WATCHER_TWITTER_CALLBACK_URL` | `twitter.callback_url` |
| `IRAN_WATCHER_LOG_LEVEL` | `log.level` |
| `IRAN_WATCHER_LOG_FORMAT` | `log.format` |
| `IRAN_WATCHER_STREAM_AUTO_CONNECT` | `stream.auto_connect` |

## CLI

The `iran-watcher` binary exposes a Cobra CLI to manage the running server:

```bash
# Authentication
iran-watcher auth login --username admin --password secret
iran-watcher auth logout

# Sources (stream filter rules)
iran-watcher source list
iran-watcher source add --type account --value @username
iran-watcher source add --type keyword --value "nuclear"
iran-watcher source remove <id>

# Stream filter rules
iran-watcher rule list
iran-watcher rule add --value "keyword lang:fa"
iran-watcher rule activate <id>
iran-watcher rule deactivate <id>

# Actions (execution units)
iran-watcher action list
iran-watcher action activate <id>
iran-watcher action deactivate <id>
iran-watcher action backends list <action-id>
iran-watcher action backends set <action-id> <backend-id> --priority 1

# Pipelines (DAG workflows)
iran-watcher pipeline list
iran-watcher pipeline activate <id>
iran-watcher pipeline deactivate <id>
iran-watcher pipeline run <id>

# Ollama backends
iran-watcher ollama backends list
iran-watcher ollama backends add --url http://localhost:11434 --max-concurrent 2
iran-watcher ollama backends enable <id>
iran-watcher ollama backends disable <id>

# Tweets
iran-watcher tweet list [--source-id <id>] [--from <RFC3339>] [--to <RFC3339>] [--page 1] [--per-page 20]
iran-watcher tweet show <id>
iran-watcher tweet reprocess [--tweet-ids id1,id2]

# Health
iran-watcher health

# Custom API base URL (default: http://localhost:8080)
iran-watcher --base-url http://remote-host:8080 health
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
| `GET` | `/api/queue` | Per-action queue breakdown |
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
cmd/iran-watcher/       → binary entrypoint + lifecycle wiring (CGO)
cmd/iran-watcher-mcp/   → MCP server binary entrypoint (pure Go, no CGO)
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
  notifier/             → Async push notifications (Pushover + Telegram)
  search/               → X search API backfill scheduler
  auth/                 → JWT (HS256) + bcrypt helpers + HTTP middleware
  hub/                  → WebSocket hub (fan-out broadcaster)
  mcpserver/            → MCP server: ToolDef registry, Dispatcher, stdio/SSE transports
migrations/             → Embedded SQL migration files
configs/                → Default config files
models/                 → ONNX model files (gitignored)
web/                    → Vue 3 frontend (Vite, Pinia, Tailwind)
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

`iran-watcher-mcp` exposes all REST API endpoints as MCP tools for use with Claude Code and other MCP clients.

```bash
# stdio (Claude Code)
iran-watcher-mcp --base-url http://localhost:8080 --username admin --password secret

# HTTP/SSE
iran-watcher-mcp --base-url http://localhost:8080 --http :9090 --username admin --password secret
```

Environment variables: `IRAN_WATCHER_MCP_BASE_URL`, `IRAN_WATCHER_MCP_HTTP`, `IRAN_WATCHER_MCP_USERNAME`, `IRAN_WATCHER_MCP_PASSWORD`.
