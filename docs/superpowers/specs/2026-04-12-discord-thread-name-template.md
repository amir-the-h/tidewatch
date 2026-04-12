# Discord Thread Name Template

**Date:** 2026-04-12
**Status:** Approved

## Problem

When a Discord notification action creates a forum thread, the thread name is derived from the first 100 characters of the rendered message body (`threadNameFromText`). This produces poor thread names because the message body is formatted for reading, not titling.

## Solution

Add a `thread_name_template` config field to Discord notification actions. It uses the same `${key}` substitution syntax as `message_template` and is hard-required for Discord provider actions.

## Data & Config

A new string key `thread_name_template` is added to the notification action's `config` JSON, at the same level as `message_template`:

```json
{
  "channel_id": 3,
  "message_template": "${tweet}\n\nSource: @${author}",
  "thread_name_template": "[${category}] ${author}"
}
```

Supported substitution variables: same as `message_template` — tweet fields (`${tweet}`, `${author}`, `${tweet_id}`, `${tweet_url}`) plus any action execution metadata key.

Discord enforces a 100-character maximum on thread names. The rendered value is truncated to 100 runes before delivery.

**Validation:** `buildNotificationPipeline()` returns an error at action-cache build time if the provider type is `discord` and `thread_name_template` is empty or missing. This prevents misconfigured actions from being activated.

## Backend

### `internal/pipeline/notification.go`

`NotificationPipeline` gains a `threadNameTemplate string` field. `NewNotificationPipeline` accepts it as a new parameter after `messageTemplate`.

In `Run()`, if `threadNameTemplate` is non-empty:
1. Render it via `substituteVars` using the same escaper and context map as `messageTemplate`.
2. Truncate the result to 100 runes.
3. Copy `p.deliveryConfig` into a local map (to avoid mutating shared state), set `"thread_name"` on the copy, and pass the copy to `provider.Deliver()`.

### `internal/worker/action_cache.go`

`buildNotificationPipeline()` extracts `thread_name_template` from the action config alongside `message_template`. Both are excluded from `deliveryConfig`. For Discord providers, an empty `thread_name_template` returns an error.

The new template string is passed as the third argument to `NewNotificationPipeline`.

### `internal/notifier/discord.go`

`deliverRoot()` reads `thread_name` from deliveryConfig:

```go
threadName, _ := deliveryConfig["thread_name"].(string)
reqBody := map[string]any{
    "name":    threadName,
    "message": msgBody,
}
```

The `threadNameFromText()` helper and its tests are deleted — they are no longer used.

## UI (`web/src/components/tabs/ActionsTab.vue`)

A new ref `formThreadNameTemplate string` is added alongside `formMessageTemplate`.

A **"Thread Name Template"** `<n-form-item>` is rendered only when `notifProviderType === 'discord'`, positioned between the Message Template field and any Discord-specific options. It uses a single-line `<n-input>` with a hint showing the available substitution variables.

Lifecycle:
- **Load** (`openEditAction`): `formThreadNameTemplate = row.config?.thread_name_template ?? ''`
- **Reset** (`resetForm`): `formThreadNameTemplate = ''`
- **Save** (`onSaveAction`): if provider is Discord and `formThreadNameTemplate` is empty, show error `'Thread name template is required for Discord'` and return. Otherwise: `config.thread_name_template = formThreadNameTemplate.value.trim()`

## Testing

- Update `NewNotificationPipeline` call sites in existing tests to pass `""` for the new `threadNameTemplate` parameter (no behavioral change for non-Discord providers).
- Add a unit test to `notification_test.go`: verify that when `threadNameTemplate` is set, `deliveryConfig["thread_name"]` contains the rendered and truncated value.
- Update `discord_test.go`: replace tests that relied on `threadNameFromText` behavior with tests that pass `thread_name` via deliveryConfig.
- Delete `threadNameFromText` and its unit tests.

## Files Changed

| File | Change |
|------|--------|
| `internal/pipeline/notification.go` | Add `threadNameTemplate` field; render and inject into deliveryConfig in `Run()` |
| `internal/worker/action_cache.go` | Extract `thread_name_template`; validate required for Discord; pass to `NewNotificationPipeline` |
| `internal/notifier/discord.go` | Read `thread_name` from deliveryConfig in `deliverRoot()`; delete `threadNameFromText` |
| `internal/pipeline/notification_test.go` | Update call sites; add thread name rendering test |
| `internal/notifier/discord_test.go` | Update to use deliveryConfig thread name; remove `threadNameFromText` tests |
| `web/src/components/tabs/ActionsTab.vue` | Add `formThreadNameTemplate` ref; render Discord-only field; validate on save |
