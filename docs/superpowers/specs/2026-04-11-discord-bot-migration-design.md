# Discord Bot Migration + Thread Tagging Design

**Date:** 2026-04-11
**Status:** Approved

## Overview

Migrate the Discord notification system from webhook-based delivery to the Discord Bot API. This enables dynamic thread tagging: a new `discord_tag_thread` internal pipeline handler reads tag names from context (produced by an Ollama generator) and applies them to an existing Forum thread via `PATCH /channels/{thread_id}`.

The webhook approach only supports creating threads and posting messages. The bot API adds:
- `applied_tags` at thread creation time
- `PatchThreadTags` to update tags on an existing thread
- `GetAvailableTags` to resolve tag names → snowflake IDs at runtime

## Architecture

### DiscordBotClient (replaces DiscordClient)

**File:** `internal/notifier/discord.go`

Replace `DiscordClient` (webhook-based) with `DiscordBotClient` (bot-based). The `notifier.Provider` interface is unchanged — only the underlying HTTP calls change.

**Fields:**
- `botToken string` — Discord Bot token (prefixed `Bot `)
- `forumChannelID string` — Snowflake ID of the Forum channel
- `httpClient *http.Client` — shared HTTP client with 10s timeout

**Methods:**

```
Deliver(ctx, text string, config map[string]any) (map[string]any, error)
```
- If `discord_thread_id` present in config: post to existing thread via `POST /channels/{thread_id}/messages`; returns `{"sent": true}`
- Otherwise: create new Forum thread via `POST /channels/{forum_channel_id}/threads` with body `{"name": "<first 100 chars>", "message": {"content|embeds": ...}, "applied_tags": [...]}` (tags optional); returns `{"discord_thread_id": "<id>"}`

```
GetAvailableTags(ctx context.Context) ([]ForumTag, error)
```
- `GET /channels/{forum_channel_id}` — returns `available_tags` array from the channel object
- `ForumTag` struct: `{ID string, Name string}`

```
PatchThreadTags(ctx context.Context, threadID string, tagIDs []string) error
```
- `PATCH /channels/{thread_id}` with body `{"applied_tags": ["id1", "id2"]}`

**Rate limiting:** Reuse existing `doRequest` retry logic (3 attempts, honours `X-RateLimit-Reset-After`).

**Auth header:** All requests include `Authorization: Bot <token>`.

### NotificationProvider Config Change

Discord provider config changes from:
```json
{"webhook_url": "https://discord.com/api/webhooks/..."}
```
to:
```json
{"bot_token": "Bot ...", "forum_channel_id": "1234567890"}
```

`internal/worker/action_cache.go` — discord case parses `bot_token` + `forum_channel_id`, constructs `notifier.NewDiscordBotClient(creds.BotToken, creds.ForumChannelID)`.

**Breaking change:** Existing Discord providers must have their config updated via the UI after deployment. No SQL migration — config is a JSON blob.

### discord_tag_thread Internal Handler

**File:** `internal/pipeline/discord_tag_thread.go`

A new internal pipeline handler registered as `"discord_tag_thread"`. Runs as a separate action node after the Ollama tag generator in the pipeline graph.

**Action config (stored in DB):**
```json
{
  "handler": "discord_tag_thread",
  "bot_token": "Bot ...",
  "forum_channel_id": "1234567890",
  "tag_names_key": "discord_tag_names"
}
```

- `bot_token` — required; Discord Bot token
- `forum_channel_id` — required; used to look up available tags
- `tag_names_key` — optional; context key holding generated tag names; defaults to `"discord_tag_names"`

**Runtime behaviour:**
1. Read `discord_thread_id` from `input.Context` (set by upstream Discord notification action via `action_execution_metadata`)
2. Read tag names from `input.Context[tag_names_key]` — accepts `[]string` or `[]any` (JSON-decoded array)
3. Call `GetAvailableTags` on the forum channel — build case-insensitive name→ID map
4. Resolve tag names to IDs; silently skip names that don't match any available tag
5. Call `PatchThreadTags(ctx, threadID, resolvedIDs)`
6. Return `{"tags_applied": ["id1", "id2"], "tag_count": 2}`

**Factory:** `NewDiscordTagThreadFactory() Factory` — constructs a `DiscordBotClient` from the action config at registry build time. No global client; no serve-time wiring.

**Registration** (`cli/cmd/serve.go`):
```go
pipeReg.Register("discord_tag_thread", pipeline.NewDiscordTagThreadFactory())
```

## Pipeline Graph

Typical graph for Discord thread notification with tags:

```
Start
  └─► Discord Notification (notification pipeline, creates thread → discord_thread_id)
        └─► Ollama Tag Generator (produces discord_tag_names in context)
              └─► discord_tag_thread (resolves names → IDs, patches thread)
```

`discord_thread_id` flows through `action_execution_metadata`. Workers call `MergeAllTweetExecutionMetadata` before each task, so it is available to `discord_tag_thread` without any special wiring.

## What Changes

| File | Change |
|------|--------|
| `internal/notifier/discord.go` | Replace `DiscordClient` + `NewDiscordClient` with `DiscordBotClient` + `NewDiscordBotClient`; rewrite `deliverRoot`/`deliverToThread` for bot API; add `GetAvailableTags` + `PatchThreadTags` |
| `internal/worker/action_cache.go` | Discord provider case: parse `bot_token` + `forum_channel_id`, construct `DiscordBotClient` |
| `internal/pipeline/discord_tag_thread.go` | New file: `DiscordTagThreadPipeline` + `NewDiscordTagThreadFactory` |
| `cli/cmd/serve.go` | Register `discord_tag_thread` handler |

## What Does Not Change

- `NotificationPipeline.Run` — context forwarding of `discord_thread_id` unchanged
- `extractEmbedBody`, `threadNameFromText` — reused as-is by `DiscordBotClient`
- `NotificationProvider` DB table schema — config is a JSON blob
- All other notifiers (Pushover, Telegram)
- No new SQL migrations

## Error Handling

- `discord_thread_id` missing from context in `discord_tag_thread` → error (task dead-lettered)
- Tag name not found in available tags → silently skipped (partial apply is acceptable)
- `GetAvailableTags` fails → error (task dead-lettered; no partial apply)
- Bot token missing from action config → error at action cache build time (action fails to load)
- Rate limit: same `doRequest` retry logic with 30s cap

## Testing

- Unit tests for `DiscordBotClient` methods using `httptest.Server`
- Unit test for `discord_tag_thread` handler: mock `GetAvailableTags` + `PatchThreadTags` via interface
- Existing `notification_test.go` updated to use `DiscordBotClient` where relevant
