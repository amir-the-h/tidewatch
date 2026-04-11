# Discord Threaded Notifications Design

**Date:** 2026-04-11
**Status:** Approved

## Overview

Extend the Discord notification provider to support Forum channel threading. The first notification action for a tweet (the "root") creates a new thread in a Discord Forum channel and saves the thread ID as metadata. Downstream "reply" actions in the same pipeline DAG pick up that thread ID from the execution context and post their messages into the same thread, producing a structured, per-tweet notification thread in Discord.

## Background

The existing Telegram provider already supports a similar threading pattern: a root action's `Deliver` call returns `telegram_message_id`, which is saved as `action_execution_metadata`. Downstream reply actions find this ID in `input.Context` (merged via `MergeAllTweetExecutionMetadata`), and `NotificationPipeline.Run` forwards it to `TelegramClient.Deliver` which sends the message as a reply. This design mirrors that pattern exactly.

Discord Forum channels work as follows:
- A webhook POST to a Forum channel with a `thread_name` field and `?wait=true` query param creates a new thread and returns the created message object. The `channel_id` field in the response is the thread ID.
- Subsequent webhook POSTs to `{webhookURL}?thread_id=<thread_id>` deliver into that thread.
- No bot token is required — the existing webhook URL is sufficient.

## Changes

### 1. `internal/notifier/discord.go` — `DiscordClient.Deliver`

Two behaviours, driven by config keys passed in by `NotificationPipeline.Run`:

**Root send** (no `discord_thread_id` in config):
- Appends `?wait=true` to the webhook URL.
- Adds `thread_name` to the JSON body: the first 100 characters of the rendered message text, with any embedded JSON object stripped out (using the same `extractEmbedBody` scan already present). Trailing whitespace is trimmed.
- Decodes the Discord response body to extract `channel_id` (which is the thread ID for Forum channel webhooks).
- Returns `map[string]any{"discord_thread_id": <channel_id>}`.
- If the response body does not contain a non-zero `channel_id`, returns an error: `"notifier: discord: thread_id missing from wait response"`.

**Reply send** (config contains `discord_thread_id`):
- Appends `?thread_id=<id>` to the webhook URL.
- If `discord_thread_id` is present but zero/empty, returns an error: `"notifier: discord: discord_thread_id missing from context"`.
- Posts without `?wait=true`; response handling unchanged (204 or 200 = success).

The `webhookURL` field is stored verbatim; query params are constructed per call and never stored.

### 2. `internal/pipeline/notification.go` — `NotificationPipeline.Run`

Add one forwarding block alongside the existing Telegram block:

```go
// Forward discord_thread_id so reply actions post into the correct Forum thread.
// Absence means this is a root send; DiscordClient handles the thread creation.
if v, ok := input.Context["discord_thread_id"]; ok {
    cfg["discord_thread_id"] = v
}
```

Add `"discord_thread_id": true` to `internalMetaKeys` so it is excluded from `${actions}` display blocks.

### 3. No schema or UI changes

No new database columns, no new action config fields, no UI changes. The root/reply distinction is purely implicit — determined by whether `discord_thread_id` is present in the execution context at the time the action runs.

## Data Flow

```
Tweet arrives
    │
    ▼
Root notification action (Discord)
    │  POST {webhookURL}?wait=true
    │  body: { ..., thread_name: "first 100 chars of message" }
    │  ◄── response: { channel_id: <thread_id>, ... }
    │  saves: action_execution_metadata { discord_thread_id: <thread_id> }
    │
    ▼  (pipeline DAG edge)
Reply notification action (Discord)
    │  input.Context contains discord_thread_id (merged from root's metadata)
    │  NotificationPipeline.Run forwards it to cfg
    │  POST {webhookURL}?thread_id=<thread_id>
    │  saves: action_execution_metadata { sent: true, provider: "..." }
```

## Error Handling

| Scenario | Behaviour |
|----------|-----------|
| Discord API non-2xx on root send | Error propagated; `discord_thread_id` never saved; downstream replies will also error and dead-letter |
| `?wait=true` response missing `channel_id` | Error: `"notifier: discord: thread_id missing from wait response"` |
| Reply fires but `discord_thread_id` absent from context | Error from `DiscordClient.Deliver`; task dead-letters |
| Reply fires but `discord_thread_id` is zero/empty | Same error |

## Backward Compatibility

Existing Discord notification actions that do not have `discord_thread_id` in their execution context continue to function as root sends (the `?wait=true` + `thread_name` path). There is no breaking change for setups that do not use threading.

## User Configuration

No special configuration is required. The user:
1. Changes their Discord channel type to Forum.
2. Creates a root notification action (Discord provider) at the start of their pipeline.
3. Creates reply notification actions downstream in the DAG, connected after the root.
4. The system automatically threads all replies into the root's Forum thread.

## Testing

- `TestDiscordClient_RootSend_ReturnsThreadID` — mock HTTP server; verify request has `?wait=true` and `thread_name` in body; return mock response with `channel_id`; assert returned map contains `discord_thread_id`.
- `TestDiscordClient_ReplySend_UsesThreadID` — mock HTTP; provide `discord_thread_id` in config; verify request URL contains `?thread_id=<id>`; assert no error.
- `TestDiscordClient_ReplySend_MissingThreadID_Errors` — zero `discord_thread_id` in config → expect error containing `"discord_thread_id missing from context"`.
- `TestDiscordClient_RootSend_MissingChannelID_Errors` — mock returns body without `channel_id` → expect error containing `"thread_id missing from wait response"`.
- `TestNotificationPipeline_ForwardsDiscordThreadID` — context contains `discord_thread_id`; verify it is forwarded to the mock provider's config (mirrors `TestNotificationPipeline_ForwardsMessageIDToProvider`).
