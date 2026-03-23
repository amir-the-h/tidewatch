# Pushover Notifier — Design Spec

**Date:** 2026-03-23
**Status:** Approved

## Overview

Add a dynamic push notification system to iran-watcher using Pushover. Notifications are configured at runtime via REST API and CLI, stored in SQLite, and dispatched asynchronously via a buffered channel. Each notification config targets a specific event type and can optionally filter by rule, model name, or model result. All Pushover message parameters (priority, sound, device, URL, TTL, HTML, etc.) are configurable per notification.

## Requirements

- Dynamic CRUD for notification configs (SQLite + REST API + CLI)
- Six event types: `tweet_matched`, `tweet_categorized`, `categorization_failed`, `stream_connected`, `stream_disconnected`, `stream_error`
- Optional AND-combined filters: rule ID, model name, model result (shallow JSON match)
- Full Pushover parameter customization per notification config
- Async dispatch — never blocks tweet processing or stream handling
- Fire-and-forget on failure — log errors, no retries
- Go `text/template` for message bodies with sensible defaults
- Test endpoint to verify a notification config works

## Architecture: Async Channel Dispatch

```
stream.Client ──emit──┐
                       ├──→ chan NotificationEvent (buffered, 256) ──→ Dispatcher goroutine
worker.Pool ───emit────┘                                                   │
                                                                           ├─ query active configs from Store
                                                                           ├─ evaluate filters
                                                                           ├─ render message template
                                                                           └─ PushoverClient.Send()
```

Components emit lightweight `NotificationEvent` structs onto a buffered channel. The dispatcher consumes them in its own goroutine, matches against notification configs, and sends Pushover messages. If the channel is full, the event is dropped with a log warning.

## Data Model

### `notifications` table (migration 004)

```sql
CREATE TABLE notifications (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    name             TEXT    NOT NULL,
    event_type       TEXT    NOT NULL,
    active           BOOLEAN NOT NULL DEFAULT 1,
    filter_rule_id   INTEGER NULL REFERENCES rules(id),
    filter_model_name    TEXT NULL,
    filter_model_result  TEXT NULL,  -- JSON, shallow key-value match
    pushover_priority    INTEGER NOT NULL DEFAULT 0,
    pushover_sound       TEXT NULL,
    pushover_device      TEXT NULL,
    pushover_title       TEXT NULL,
    pushover_url         TEXT NULL,
    pushover_url_title   TEXT NULL,
    pushover_ttl         INTEGER NULL,
    pushover_html        BOOLEAN NOT NULL DEFAULT 0,
    pushover_retry       INTEGER NULL,
    pushover_expire      INTEGER NULL,
    message_template     TEXT NULL,
    created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_notifications_event_type ON notifications(event_type);
CREATE INDEX idx_notifications_active ON notifications(active);
```

### Filter Logic

All non-null filters are AND'd. A notification with no filters fires for every event of its `event_type`.

- **`filter_rule_id`**: Event's matching rules must include this rule ID.
- **`filter_model_name`**: Event's model name must equal this value.
- **`filter_model_result`**: Shallow JSON match — each key in the filter must exist in the categorization results with the same value. Not a query language; designed for `{"label": "sanctions"}` style matching.

## Event System

### NotificationEvent

```go
type NotificationEvent struct {
    Type         string               // event_type enum
    Tweet        *model.Tweet         // nil for stream events
    Rules        []model.MatchingRule // nil for stream events
    ModelName    string               // only for categorization events
    ModelVersion string               // only for categorization events
    Results      json.RawMessage      // only for tweet_categorized
    Error        string               // only for failure/error events
    Timestamp    time.Time
}
```

### Event Type Constants

```go
const (
    EventTweetMatched         = "tweet_matched"
    EventTweetCategorized     = "tweet_categorized"
    EventCategorizationFailed = "categorization_failed"
    EventStreamConnected      = "stream_connected"
    EventStreamDisconnected   = "stream_disconnected"
    EventStreamError          = "stream_error"
)
```

### Emit Points

| Component | Event | Location |
|-----------|-------|----------|
| `stream.Client.processEvent` | `tweet_matched` | After saving tweet + rules to SQLite |
| `worker.worker` | `tweet_categorized` | After `SaveCategorization` succeeds |
| `worker.worker` | `categorization_failed` | Per failed model in the pipeline loop |
| `stream.Client.Connect` | `stream_connected` | After successful HTTP connection |
| `stream.Client.Disconnect` | `stream_disconnected` | After stream goroutine exits |
| `stream.Client.runLoop` | `stream_error` | On connection error before backoff retry |

## Dispatcher

### Behavior

- Owns a `chan NotificationEvent` (buffered, default 256, configurable)
- Runs a single goroutine via `Start(ctx context.Context)`
- Per event:
  1. Query active notification configs from store where `event_type` matches
  2. For each config, evaluate filters against event data
  3. If all filters pass, render message template (custom or default), expand URL placeholders, build `PushoverMessage`
  4. Call `PushoverClient.Send(ctx, msg)`
  5. On error: log via slog, continue to next config
- On context cancellation: drain remaining channel events (best-effort), then exit

### Emit Function

```go
func (d *Dispatcher) Emit(event NotificationEvent)
```

Non-blocking send. If the channel buffer is full, log a warning and drop the event.

## Pushover Client

### Configuration

```yaml
pushover:
  app_token: ""   # IRAN_WATCHER_PUSHOVER_APP_TOKEN
  user_key: ""    # IRAN_WATCHER_PUSHOVER_USER_KEY
```

Global app token and user key. Per-notification device targeting via `pushover_device` field.

### PushoverClient

```go
type PushoverClient struct {
    appToken   string
    userKey    string
    httpClient *http.Client  // 10s timeout
    logger     *slog.Logger
}
```

### PushoverMessage

```go
type PushoverMessage struct {
    Message  string
    Title    string
    Priority int
    Sound    string
    Device   string
    URL      string
    URLTitle string
    TTL      int
    HTML     bool
    Retry    int   // required if Priority == 2, min 30
    Expire   int   // required if Priority == 2, max 10800
}
```

### Send

`POST https://api.pushover.net/1/messages.json` with form-encoded body. Validates before sending:
- `Message` non-empty
- If `Priority == 2`: `Retry >= 30` and `0 < Expire <= 10800`

Returns wrapped error with Pushover error messages on failure. No retries.

## Message Templating

### Template Engine

Go `text/template` with data context from `NotificationEvent`:

```
{{.Tweet.ID}}, {{.Tweet.AuthorHandle}}, {{.Tweet.Text}}
{{.Rules}}
{{.ModelName}}, {{.ModelVersion}}
{{.Results}}
{{.Error}}
{{.Timestamp}}
```

### Default Templates

| Event Type | Default |
|------------|---------|
| `tweet_matched` | `New tweet from @{{.Tweet.AuthorHandle}}: {{.Tweet.Text}}` |
| `tweet_categorized` | `Tweet by @{{.Tweet.AuthorHandle}} categorized by {{.ModelName}}: {{.Results}}` |
| `categorization_failed` | `Failed to categorize tweet {{.Tweet.ID}} with {{.ModelName}}: {{.Error}}` |
| `stream_connected` | `Stream connected` |
| `stream_disconnected` | `Stream disconnected` |
| `stream_error` | `Stream error: {{.Error}}` |

### URL Placeholder Expansion

`pushover_url` supports `{tweet_id}`, `{author_handle}` via `strings.NewReplacer`. Not Go templates — keeps URLs clean in JSON without escaping.

## REST API

### Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/notifications` | List (filters: `event_type`, `active`, `rule_id`) |
| `POST` | `/api/notifications` | Create |
| `GET` | `/api/notifications/{id}` | Get |
| `PUT` | `/api/notifications/{id}` | Update |
| `DELETE` | `/api/notifications/{id}` | Delete |
| `POST` | `/api/notifications/{id}/activate` | Enable |
| `POST` | `/api/notifications/{id}/deactivate` | Disable |
| `POST` | `/api/notifications/test` | Send test notification (`{"notification_id": 123}`) |

### Request Body (Create/Update)

```json
{
  "name": "Sanctions alert",
  "event_type": "tweet_categorized",
  "filter_rule_id": 5,
  "filter_model_name": "topic-classifier",
  "filter_model_result": {"label": "sanctions"},
  "pushover_priority": 1,
  "pushover_sound": "siren",
  "pushover_device": null,
  "pushover_title": "Iran Watcher Alert",
  "pushover_url": "https://x.com/{author_handle}/status/{tweet_id}",
  "pushover_url_title": "View Tweet",
  "pushover_ttl": null,
  "pushover_html": true,
  "pushover_retry": null,
  "pushover_expire": null,
  "message_template": "{{.Tweet.AuthorHandle}}: {{.Tweet.Text}}"
}
```

### Validation

- `name` and `event_type` required
- `event_type` must be one of six valid values
- `filter_rule_id` must reference an existing rule if set
- Priority 2: `retry >= 30`, `expire <= 10800`
- `message_template` parsed as Go template to catch syntax errors

## CLI Commands

```
iran-watcher notification list [--event-type=] [--active]
iran-watcher notification create --name "..." --event-type "..." [flags]
iran-watcher notification get <id>
iran-watcher notification update <id> [flags]
iran-watcher notification delete <id>
iran-watcher notification activate <id>
iran-watcher notification deactivate <id>
iran-watcher notification test <id>
```

Thin REST client calls, consistent with existing CLI commands.

## File Layout

### New Files

```
internal/notifier/pushover.go       — PushoverClient, PushoverMessage, Send()
internal/notifier/dispatcher.go     — Dispatcher, event channel, goroutine loop, filter matching
internal/notifier/event.go          — NotificationEvent, event type constants
internal/notifier/template.go       — default templates, rendering, URL placeholder expansion
internal/model/notification.go      — Notification domain struct, NotificationFilter
internal/api/handlers_notifications.go — CRUD + activate/deactivate + test handlers
cli/cmd/notification.go             — Cobra commands
migrations/004_notifications.sql    — CREATE TABLE
```

### Modified Files

| File | Change |
|------|--------|
| `internal/config/config.go` | Add `Pushover` struct (`AppToken`, `UserKey`) |
| `cli/cmd/serve.go` | Create PushoverClient + Dispatcher, pass to components |
| `internal/stream/client.go` | Accept dispatcher, emit tweet + stream events |
| `internal/worker/pool.go` + `worker.go` | Accept dispatcher, emit categorization events |
| `internal/api/server.go` | Accept dispatcher, register notification routes |
| `internal/store/store.go` | Add notification methods to Store interface |
| `configs/iran-watcher.yaml` | Add `pushover` section |

## Design Decisions

1. **Pushover-only, no abstraction layer** — YAGNI. Refactor to interface if a second provider is ever needed.
2. **Async channel dispatch** — decouples notifications from the critical tweet-processing path.
3. **Fire-and-forget** — notifications are informational. Tweets are already safe in SQLite.
4. **No receipt tracking** — emergency notifications use Pushover's built-in retry/expire mechanism.
5. **Shallow JSON match for model results** — simple and sufficient for label-based filtering without building a query language.
6. **Global Pushover credentials in config** — single user system, no need for per-notification auth.
