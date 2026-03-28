# Notification Pipeline Design

**Date:** 2026-03-28
**Status:** Approved

## Overview

Replace the `notifier.Dispatcher` event-driven notification system with a first-class `notification` pipeline type that participates fully in the action/chain system. Notifications are triggered exclusively by actions — no more system event subscriptions.

The existing `notifications` table is split into two new tables: `notification_providers` (credentials) and `notification_channels` (delivery config + template). A `notification` pipeline references a channel; the channel references a provider.

## Data Model

### `notification_providers` (renamed + reschema'd from `notifications`)

Stores provider credentials only.

| Column       | Type    | Notes                                    |
|--------------|---------|------------------------------------------|
| id           | INTEGER | PK                                       |
| name         | TEXT    | Human-readable label                     |
| type         | TEXT    | `"pushover"` or `"telegram"`             |
| config       | JSON    | Credentials — see below                  |
| active       | BOOLEAN |                                          |
| created_at   | DATETIME|                                          |
| updated_at   | DATETIME|                                          |

**Pushover config:** `{"app_token": "...", "user_key": "..."}`
**Telegram config:** `{"bot_token": "..."}`

### `notification_channels`

Stores delivery configuration and message template. Multiple channels can share one provider (e.g. two Telegram channels to different chat IDs on the same bot).

| Column           | Type    | Notes                                          |
|------------------|---------|------------------------------------------------|
| id               | INTEGER | PK                                             |
| name             | TEXT    | Human-readable label                           |
| provider_id      | INTEGER | FK → notification_providers.id                 |
| message_template | TEXT    | Template string with `${variable}` substitution|
| config           | JSON    | Provider-specific delivery settings            |
| active           | BOOLEAN |                                                |
| created_at       | DATETIME|                                                |
| updated_at       | DATETIME|                                                |

**Pushover channel config example:**
```json
{"title": "Iran Alert", "priority": 1, "sound": "siren", "url": "${tweet_url}", "url_title": "View Tweet", "ttl": 3600, "html": false}
```

**Telegram channel config example:**
```json
{"chat_id": "-1001234567890", "parse_mode": "HTML", "disable_web_page_preview": false, "disable_notification": false}
```

### `pipelines` table (existing, no schema change)

A notification pipeline stores:
```json
{"channel_id": 42}
```
in the existing `config` column, with `type = "notification"`.

## Template Variable System

`message_template` supports `${variable}` substitution. Variables are resolved from `mergedCtx` at execution time, which contains:

- `${tweet}` — full tweet text
- `${author}` — tweet author handle (`ev.AuthorHandle`)
- `${tweet_id}` — tweet ID (`ev.TweetID`)
- `${tweet_url}` — computed as `https://twitter.com/i/web/status/${tweet_id}`, injected by the worker alongside the other built-in keys
- Any key output by upstream pipeline executions in the chain (e.g. `${label}`, `${score}`, `${locations}`)

Unknown variables are left as-is (not substituted). No template engine — simple string replacement over `mergedCtx` keys.

## Go Code Structure

### `internal/model/`

- **`notification.go`** — replace `Notification` struct with:
  - `NotificationProvider` — `ID`, `Name`, `Type`, `Config json.RawMessage`, `Active`, timestamps
  - `NotificationChannel` — `ID`, `Name`, `ProviderID`, `MessageTemplate`, `Config json.RawMessage`, `Active`, timestamps
- **`pipeline.go`** — add `PipelineTypeNotification PipelineType = "notification"`

### `internal/notifier/`

- **Keep:** `pushover.go`, `telegram.go` — HTTP clients unchanged
- **Remove:** `dispatcher.go`, `event.go` — system event dispatch retired
- **Remove:** `template.go` — replaced by `${variable}` substitution in the pipeline
- **Add:** `provider.go` — `Provider` interface with `Send(ctx context.Context, msg string, config map[string]any) error`, implemented by both clients

### `internal/pipeline/notification.go` (rewritten)

`NotificationPipeline` implements `inference.Pipeline`:

```go
type NotificationPipeline struct {
    name     string
    channel  *model.NotificationChannel
    provider notifier.Provider
}
```

`Run(ctx, input)`:
1. Substitute `${key}` in `channel.MessageTemplate` using `mergedCtx`
2. Parse `channel.Config` into `map[string]any`
3. Call `provider.Send(ctx, renderedMessage, config)`
4. Return `map[string]any{"sent": true, "provider": providerType}`

### `internal/store/`

New store methods (replacing old `notifications` CRUD):

- `SaveNotificationProvider`, `GetNotificationProvider`, `ListNotificationProviders`, `UpdateNotificationProvider`, `DeleteNotificationProvider`, `SetNotificationProviderActive`
- `SaveNotificationChannel`, `GetNotificationChannel`, `ListNotificationChannels`, `UpdateNotificationChannel`, `DeleteNotificationChannel`, `SetNotificationChannelActive`

### `internal/worker/action_cache.go`

On cache rebuild, for pipelines with `type = "notification"`:
1. Parse `channel_id` from `pipeline.config`
2. Load `NotificationChannel` from store
3. Load `NotificationProvider` from store via `channel.ProviderID`
4. Instantiate the correct client from provider credentials
5. Build and cache a `NotificationPipeline`

### `internal/api/`

- **Remove:** `handlers_notifications.go`
- **Add:** `handlers_notification_providers.go`
  - `GET    /api/notification/providers`
  - `POST   /api/notification/providers`
  - `GET    /api/notification/providers/{id}`
  - `PUT    /api/notification/providers/{id}`
  - `DELETE /api/notification/providers/{id}`
  - `POST   /api/notification/providers/{id}/activate`
  - `POST   /api/notification/providers/{id}/deactivate`
- **Add:** `handlers_notification_channels.go`
  - `GET    /api/notification/channels`
  - `POST   /api/notification/channels`
  - `GET    /api/notification/channels/{id}`
  - `PUT    /api/notification/channels/{id}`
  - `DELETE /api/notification/channels/{id}`
  - `POST   /api/notification/channels/{id}/activate`
  - `POST   /api/notification/channels/{id}/deactivate`

### `migrations/`

Three migration steps (single migration file):
1. Create `notification_providers` table
2. Create `notification_channels` table
3. Migrate existing `notifications` rows: for each row, insert a `notification_providers` record (credentials from provider-specific fields) and a `notification_channels` record (delivery config + template), then drop `notifications` table

## Execution Flow

```
Action (trigger_type=new_tweet, pipeline_id=N)
  └─ Worker pops task from Redis
       └─ ActionCache.Get(action_id) → NotificationPipeline{channel, provider}
            └─ NotificationPipeline.Run(ctx, input)
                 ├─ Substitute ${variables} in message_template using mergedCtx
                 ├─ Parse channel.config for delivery settings
                 ├─ provider.Send(ctx, renderedMessage, config)
                 └─ Return {"sent": true, "provider": "telegram"}
                      └─ evaluateChains → push downstream tasks if conditions met
```

## What Is Removed

- `notifier.Dispatcher` — retired entirely
- `notifier.EventEmitter` interface — no longer needed
- `notifier/event.go` — event type constants removed
- `notifier/template.go` — replaced by pipeline-level substitution
- `notifier/dispatcher.go` and `notifier/dispatcher_test.go`
- Old `PushoverPipeline` and `TelegramPipeline` internal handlers (`notification_push`, `notification_tg`)
- Old `notifications` CRUD in store
- `handlers_notifications.go` and all `/api/notifications` routes
- References to `Dispatcher` in `Server`, wiring in `main.go`/`serve.go`

## Chain Usage Examples

**Standalone:** `Action(trigger=new_tweet) → NotificationPipeline`

**After categorization:**
```
Action(trigger=new_tweet) → ONNXPipeline → outputs {label, score}
  └─ Chain(condition: label == "protest") → Action(trigger=chain_only) → NotificationPipeline
       message_template: "Protest tweet by ${author}: ${tweet} [score: ${score}]"
```

**Multi-step:**
```
Action → EntityMatcher → outputs {entity_name}
  └─ Chain → Action → NotificationPipeline
       message_template: "Entity matched: ${entity_name} — ${tweet}"
```
