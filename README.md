# iran-watcher

Monitors X (Twitter) Filtered Stream for Iran-related news. Stores tweets in SQLite, queues through Redis, categorizes via local ONNX models. Pure Go — no Python.

## Architecture

```
X Filtered Stream (SSE)
        │
        ▼
  stream.Client          ← connects, normalizes, saves raw tweet to SQLite
        │ LPUSH
        ▼
  Redis (iran-watcher:raw)
        │ BRPOP
        ▼
  worker.Pool            ← N goroutines, each runs all active pipelines
        │
        ├─► inference.Registry  ← ONNX models, hot-reloadable via API
        │
        ├─► SQLite (categorizations, digest_jobs)
        │
        ├─► Redis (iran-watcher:failed)  ← dead-letter for partial failures
        │
        └─► notifier.Dispatcher  ← async push notifications (Pushover / Telegram)

  api.Server             ← REST API for management (localhost only)
  cli                    ← thin Cobra CLI over the REST API
```

## Prerequisites

- Go 1.25.5+
- Redis (running on `localhost:6379` by default)
- [ONNX Runtime](https://github.com/microsoft/onnxruntime) shared library installed
- [Hugging Face CLI](https://huggingface.co/docs/huggingface_hub/guides/cli) (`hf`) for model downloads
- Rust toolchain (required by `daulet/tokenizers` CGO bindings)
- X (Twitter) API bearer token with Filtered Stream access

## Installation

```bash
git clone https://github.com/amir-the-h/iran-watcher
cd iran-watcher
go build ./cmd/iran-watcher/
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
  count: 0             # 0 = one worker per active model (min 1)

inference:
  models_dir: ./models

ollama:
  base_url: http://localhost:11434

log:
  level: info          # debug | info | warn | error
  format: text         # text | json

pushover:
  app_token: ""
  user_key: ""

telegram:
  bot_token: ""

stream:
  auto_connect: false  # auto-connect to X stream on startup

search:
  enabled: false
  interval: 15m
  max_results: 100
  limit: 0
```

All fields can also be set via environment variables:

| Env var | Config field |
|---|---|
| `IRAN_WATCHER_X_BEARER_TOKEN` | `x.bearer_token` |
| `IRAN_WATCHER_X_TIER` | `x.tier` |
| `IRAN_WATCHER_REDIS_ADDR` | `redis.addr` |
| `IRAN_WATCHER_REDIS_PASSWORD` | `redis.password` |
| `IRAN_WATCHER_REDIS_DB` | `redis.db` |
| `IRAN_WATCHER_SQLITE_PATH` | `sqlite.path` |
| `IRAN_WATCHER_API_BIND` | `api.bind` |
| `IRAN_WATCHER_WORKER_COUNT` | `worker.count` |
| `IRAN_WATCHER_ONNX_LIB_PATH` | `inference.onnx_lib_path` |
| `IRAN_WATCHER_MODELS_DIR` | `inference.models_dir` |
| `IRAN_WATCHER_OLLAMA_BASE_URL` | `ollama.base_url` |
| `IRAN_WATCHER_LOG_LEVEL` | `log.level` |
| `IRAN_WATCHER_LOG_FORMAT` | `log.format` |
| `IRAN_WATCHER_PUSHOVER_APP_TOKEN` | `pushover.app_token` |
| `IRAN_WATCHER_PUSHOVER_USER_KEY` | `pushover.user_key` |
| `IRAN_WATCHER_TELEGRAM_BOT_TOKEN` | `telegram.bot_token` |
| `IRAN_WATCHER_STREAM_AUTO_CONNECT` | `stream.auto_connect` |

## Running

```bash
./iran-watcher --config my-config.yaml
```

On startup the binary will:
1. Connect to SQLite (runs migrations automatically)
2. Connect to Redis
3. Load any active ONNX models into the inference registry
4. Start the stream client, worker pool, and REST API
5. Shut down gracefully on `SIGINT` / `SIGTERM`

## CLI

The `iran-watcher` binary also exposes a Cobra CLI to manage the running server:

```bash
# Sources (stream filter rules)
iran-watcher source list
iran-watcher source add --type account --value @username
iran-watcher source add --type keyword --value "nuclear"
iran-watcher source remove <id>

# Models
iran-watcher model list
iran-watcher model add --name sentiment --path ./models/sentiment.onnx --type classification
iran-watcher model activate <id>
iran-watcher model deactivate <id>
iran-watcher model remove <id>

# Tweets
iran-watcher tweet list [--source-id <id>] [--from <RFC3339>] [--to <RFC3339>] [--page 1] [--per-page 20]
iran-watcher tweet show <id>
iran-watcher tweet reprocess [--tweet-ids id1,id2] [--model-names model1,model2]

# Health
iran-watcher health

# Custom API base URL (default: http://localhost:8080)
iran-watcher --base-url http://remote-host:8080 health
```

## REST API

All routes are under `/api`. The server binds to `127.0.0.1:8080` by default (localhost only, no auth).

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/health` | Health check + active model names |
| `GET` | `/api/sources` | List sources |
| `POST` | `/api/sources` | Add a source `{type, value}` |
| `PUT` | `/api/sources/{id}` | Update a source |
| `DELETE` | `/api/sources/{id}` | Remove a source |
| `POST` | `/api/sources/{id}/deactivate` | Deactivate a source |
| `GET` | `/api/rules` | List stream filter rules |
| `POST` | `/api/rules` | Create a rule |
| `GET` | `/api/rules/{id}` | Get a rule |
| `PUT` | `/api/rules/{id}` | Update a rule |
| `DELETE` | `/api/rules/{id}` | Delete a rule |
| `POST` | `/api/rules/{id}/activate` | Activate a rule (syncs to X) |
| `POST` | `/api/rules/{id}/deactivate` | Deactivate a rule (removes from X) |
| `GET` | `/api/tweets` | List tweets (filter: source_id, from, to, page, per_page) |
| `GET` | `/api/tweets/{id}` | Get tweet + categorizations |
| `DELETE` | `/api/tweets` | Bulk delete tweets |
| `POST` | `/api/tweets/reprocess` | Re-enqueue tweets `{tweet_ids, model_names}` |
| `DELETE` | `/api/tweets/noise` | Delete tweets classified as noise |
| `GET` | `/api/models` | List registered models |
| `POST` | `/api/models` | Register a model `{name, version, path, type}` |
| `POST` | `/api/models/download` | Download from HuggingFace `{repo_id}` |
| `DELETE` | `/api/models/{id}` | Remove a model |
| `POST` | `/api/models/{id}/activate` | Activate + hot-reload registry |
| `POST` | `/api/models/{id}/deactivate` | Deactivate + hot-reload registry |
| `POST` | `/api/models/{id}/run` | Run a model pipeline on-demand |
| `GET` | `/api/models/{id}/labels` | List model labels |
| `POST` | `/api/models/{id}/labels` | Add a model label |
| `DELETE` | `/api/models/{id}/labels/{label}` | Remove a model label |
| `POST` | `/api/models/{id}/rules` | Associate a rule with a model |
| `DELETE` | `/api/models/{id}/rules/{rule_id}` | Remove rule association |
| `GET` | `/api/stream/status` | Stream connection status |
| `POST` | `/api/stream/connect` | Connect to X filtered stream |
| `POST` | `/api/stream/disconnect` | Disconnect from X filtered stream |
| `GET` | `/api/categorizations` | List categorizations (filter: tweet_id, model_name, from, to) |
| `GET` | `/api/tracked-entities` | List tracked entities |
| `POST` | `/api/tracked-entities` | Create a tracked entity |
| `GET` | `/api/tracked-entities/{id}` | Get a tracked entity |
| `PUT` | `/api/tracked-entities/{id}` | Update a tracked entity |
| `DELETE` | `/api/tracked-entities/{id}` | Delete a tracked entity |
| `POST` | `/api/tracked-entities/{id}/activate` | Activate a tracked entity |
| `POST` | `/api/tracked-entities/{id}/deactivate` | Deactivate a tracked entity |
| `GET` | `/api/tracked-entities/{id}/tracks` | List entity tracks |
| `GET` | `/api/summaries` | List tweet summaries (filter: from, to, rule_id, source_id, entity_id) |
| `POST` | `/api/summaries/run` | Trigger summary generation |
| `GET` | `/api/digest` | List digest jobs |
| `POST` | `/api/digest` | Create a digest job `{from, to, rule_id, source_id, entity_id, limit}` |
| `GET` | `/api/digest/{id}` | Get digest job status + result |
| `DELETE` | `/api/digest/{id}` | Delete a digest job |
| `GET` | `/api/failures` | List dead-letter queue items |
| `DELETE` | `/api/failures` | Clear dead-letter queue |
| `DELETE` | `/api/queue` | Flush the processing queue |
| `POST` | `/api/search/run` | Trigger a backfill search |
| `GET` | `/api/notifications` | List notification configs |
| `POST` | `/api/notifications` | Create a notification config |
| `POST` | `/api/notifications/test` | Test a notification config |
| `GET` | `/api/notifications/{id}` | Get a notification config |
| `PUT` | `/api/notifications/{id}` | Update a notification config |
| `DELETE` | `/api/notifications/{id}` | Delete a notification config |
| `POST` | `/api/notifications/{id}/activate` | Activate a notification config |
| `POST` | `/api/notifications/{id}/deactivate` | Deactivate a notification config |

## Adding Models

Download an ONNX model from HuggingFace:

```bash
iran-watcher model download  # POST /api/models/download {"repo_id": "org/model"}
```

Or manually place `.onnx` files under `./models/` and register via `model add`.

Activate the model to start inference:

```bash
iran-watcher model activate <id>
```

The worker pool picks up the new model immediately — no restart required.

## Development

```bash
# Unit tests
go test ./...

# Integration tests (requires Docker for Redis container)
go test -tags integration ./...

# Vet
go vet ./...

# Build
go build ./cmd/iran-watcher/
```

## Project Structure

```
cmd/iran-watcher/       binary entrypoint + lifecycle wiring
cli/cmd/                Cobra CLI subcommands
internal/
  config/               YAML config + env overrides
  model/                domain types (Tweet, Source, Model, Notification, DigestJob, etc.)
  store/                SQLite repository (WAL mode, embedded migrations)
  queue/                Redis list producer/consumer + dead-letter queue
  inference/            ONNX pipeline interface + hot-reload registry
  stream/               X Filtered Stream SSE client
  worker/               worker pool goroutines
  api/                  REST API (chi)
  notifier/             async push notifications (Pushover + Telegram)
  search/               X search API backfill scheduler
migrations/             embedded SQL migration files
configs/                default config file
models/                 ONNX model files (gitignored)
```
