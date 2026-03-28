# Actions System Design

**Date:** 2026-03-27
**Status:** Superseded

> **Superseded by:** [Event-Driven Pipeline Refactor Design](./2026-03-28-event-driven-refactor-design.md) (2026-03-28)
>
> This spec described the initial actions system with `internal/action/` package, `ActionRegistry`, `PipelineAction`, and `TwitterAction`. That implementation has been replaced by the event-driven refactor: `internal/action/` is deleted, all work goes through `iran-watcher:tasks` Redis queue, pipelines are stored in the `pipelines` table, and `ActionCache` replaces `ActionRegistry`.

## Overview

Replace the pipeline-only dispatch layer with a generic action system. Every unit of work triggered by an incoming tweet is an **action**. Running an ONNX/Ollama pipeline is one action type; interacting with Twitter (retweet, like, quote) is another. Both sit at the same level in the dispatch stack.

## Architecture

```
Tweet arrives → Worker Manager → ActionRegistry.AllForRules(ruleIDs)
                                        ↓
                           []Action (fair-share scheduled)
                                        ↓
                    ┌───────────────────┴───────────────────┐
                    │                                       │
             PipelineAction                          TwitterAction
         (run ONNX/Ollama model)          (retweet | like | quote tweet)
                    │                                       │
             save categorization                    call Twitter API
             run entity matching                    log execution
             dead-letter on failure
```

## Core Action Interface

New package: `internal/action/`

```go
type ActionType string

const (
    ActionTypePipeline ActionType = "pipeline"
    ActionTypeRetweet  ActionType = "retweet"
    ActionTypeLike     ActionType = "like"
    ActionTypeQuote    ActionType = "quote"
)

type Action interface {
    ID() int64
    Name() string
    Type() ActionType
    RuleIDs() []string  // empty = fires on all rules
    Execute(ctx context.Context, tweet model.Tweet) error
}
```

### PipelineAction (`internal/action/pipeline_action.go`)

- Wraps an `inference.Pipeline` resolved from its `model_id`
- `Execute`: run pipeline → save categorization → run entity matching → dead-letter on failure
- Absorbs the categorization/entity-matching logic currently in the worker loop, keeping the worker thin
- Failures are logged to `action_executions` and pushed to Redis dead-letter queue (`iran-watcher:failed`) for reprocessability

### TwitterAction (`internal/action/twitter_action.go`)

- Holds action type, config (template + optional `pipeline_model_id`), and a `*twitter.Client`
- `Execute` for `retweet`/`like`: single Twitter API call
- `Execute` for `quote`:
  - If `pipeline_model_id` is set: run that pipeline on tweet text, use output as quote text
  - Otherwise: render static template (supports `{text}`, `{author}` placeholders)
- HTTP 429 rate limit errors: logged as `error` in execution log with `rate_limit_reset` metadata key — no retry, moves on

### ActionRegistry (`internal/action/registry.go`)

- Replaces `inference.Registry` as what the worker manager holds for dispatch
- Loads actions from store on startup and on hot-reload signal
- `AllForRules(ruleIDs []string) []Action` — filters active actions by rule overlap
- Composes with `inference.Registry` (which stays for model loading/hot-reload): pipeline actions are constructed by resolving `model_id` through the inference registry

## Database Schema

### `actions` table

```sql
CREATE TABLE actions (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT NOT NULL,
    type        TEXT NOT NULL,           -- pipeline | retweet | like | quote
    active      INTEGER NOT NULL DEFAULT 1,
    rule_ids    TEXT NOT NULL DEFAULT '[]',  -- JSON array, empty = all rules
    config      TEXT NOT NULL DEFAULT '{}',  -- type-specific JSON
    created_at  DATETIME NOT NULL
);
```

Config JSON by type:
- `pipeline`: `{"model_id": 42}`
- `retweet`: `{}`
- `like`: `{}`
- `quote`: `{"template": "Breaking: {text}", "pipeline_model_id": null}`

### `action_executions` table

Core log entry per action fired:

```sql
CREATE TABLE action_executions (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    action_id   INTEGER NOT NULL REFERENCES actions(id),
    tweet_id    TEXT NOT NULL,
    status      TEXT NOT NULL,   -- success | error
    error       TEXT,
    executed_at DATETIME NOT NULL
);
```

### `action_execution_metadata` table

Arbitrary key-value context per execution — extensible to any future action type with no schema changes:

```sql
CREATE TABLE action_execution_metadata (
    execution_id  INTEGER NOT NULL REFERENCES action_executions(id),
    key           TEXT NOT NULL,
    value         TEXT NOT NULL,
    PRIMARY KEY (execution_id, key)
);
```

Example metadata by type:
- `pipeline`: `model_name`, `result` (JSON output)
- `retweet`: `retweet_id`
- `like`: (none required)
- `quote`: `quote_tweet_id`, `text`
- `rate_limit_reset` (any Twitter type on 429)

### `twitter_credentials` table

Single connected account (enforced by `DEFAULT 1` primary key):

```sql
CREATE TABLE twitter_credentials (
    id              INTEGER PRIMARY KEY DEFAULT 1,
    access_token    TEXT NOT NULL,
    refresh_token   TEXT NOT NULL,
    expires_at      DATETIME NOT NULL,
    twitter_user_id TEXT NOT NULL,
    username        TEXT NOT NULL,
    updated_at      DATETIME NOT NULL
);
```

## Twitter OAuth 2.0 PKCE

New package: `internal/twitter/`

### Flow

1. Admin clicks "Connect Twitter Account" in UI → `GET /api/twitter/auth/start`
2. Backend generates PKCE code verifier/challenge + state token; stores in in-memory cache (10min TTL); returns authorization URL
3. Browser redirects to Twitter — user approves
4. Twitter redirects to `GET /api/twitter/auth/callback?code=...&state=...`
5. Backend validates state, exchanges code for access + refresh tokens, stores in `twitter_credentials`, redirects to settings page
6. Token refresh happens transparently when `expires_at` is near

The callback URL (`/api/twitter/auth/callback`) must be registered in the Twitter developer portal.

### Twitter API Calls (`internal/twitter/client.go`)

- `Retweet(ctx, tweetID)` → `POST /2/users/:my_id/retweets`
- `Like(ctx, tweetID)` → `POST /2/users/:my_id/likes`
- `QuoteTweet(ctx, tweetID, text)` → `POST /2/tweets` with `quote_tweet_id`

## Worker Manager Refactor

The manager's structure is unchanged; only the type it dispatches changes:

- **Dispatcher**: calls `actionRegistry.AllForRules(ruleIDs)` — fans tweet out to per-action channels
- **Scheduler**: round-robins over action channels — structurally identical to today
- **Worker**: calls `action.Execute(ctx, tweet)` — thin, knows nothing about action internals
- `ConcurrencyLimiter` interface still applies — Ollama-backed `PipelineAction` can cap concurrent workers

The `completionTracker` (today coordinates entity matching + dead-letter after all pipelines finish) moves into `PipelineAction.Execute`. Each pipeline failure is logged independently. The Redis dead-letter queue is preserved for pipeline action failures only, keeping the reprocess endpoint functional.

## REST API

All endpoints admin-only.

### Action Management

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/actions` | List actions (filterable by type, active) |
| `POST` | `/api/actions` | Create action |
| `PUT` | `/api/actions/{id}` | Update name, config, rule_ids |
| `DELETE` | `/api/actions/{id}` | Delete action |
| `POST` | `/api/actions/{id}/activate` | Activate action |
| `POST` | `/api/actions/{id}/deactivate` | Deactivate action |

### Execution Log

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/actions/{id}/executions` | Paginated log for one action |
| `GET` | `/api/executions` | Global log (filterable by tweet_id, status, date range) |

### Twitter OAuth

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/twitter/auth/start` | Returns authorization URL |
| `GET` | `/api/twitter/auth/callback` | OAuth callback (redirect response) |
| `GET` | `/api/twitter/auth/status` | Connected account info or 404 |
| `DELETE` | `/api/twitter/auth` | Disconnect account |

## Frontend

- **`/actions` page** — lists all actions grouped by type; create/edit/delete/toggle active; shows recent execution count and last error inline; execution log accessible via drawer per action
- **Settings / Twitter Connect** — connected account username + avatar, connect/disconnect button; lives on a `/settings` page or dedicated `/settings/twitter`
- **`/models` page** — unchanged; model management (ONNX config, hot-reload, labels) stays; creating a `pipeline` action references a model from here

## Migration

On first startup after the update, a one-time migration step runs:

- For every active model in the `models` table, create a corresponding `pipeline` action with the same name, rule IDs, and `config: {"model_id": <id>}`
- Idempotent: skipped if any `pipeline` actions already exist
- Logged clearly at startup

Existing behaviour is preserved with no manual reconfiguration. Users see their pipelines on the new `/actions` page immediately.

## What Does Not Change

- `models` table and model management API
- ONNX/Ollama inference internals (`internal/inference/`)
- Hot-reload mechanism
- Reprocess endpoint (queues uncategorized tweets; pipeline actions handle them)
- Redis dead-letter queue key (`iran-watcher:failed`)
- Fair-share scheduler logic in `internal/worker/`
