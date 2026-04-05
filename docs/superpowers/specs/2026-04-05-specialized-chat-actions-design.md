# Specialized Chat Actions Design

**Date:** 2026-04-05
**Status:** Approved

## Overview

Replace the single monolithic chat action (found by `usecase: "chat"` fallback) with a system where each chat action is fully self-contained — it has its own system prompt and its own curated set of MCP tools. The Chat page gains an action picker so users can switch between specialized assistants.

## Data Model

No DB migration required. The Ollama action config JSON gains one new optional field:

```json
{
  "model": "qwen2.5:3b",
  "system_prompt": "You are a specialist in...",
  "usecase": "chat",
  "allowed_tools": ["tweets_list", "health_get", "pipelines_list"]
}
```

- `allowed_tools`: `[]string` of tool names from the full 130+ tool set.
- Empty/absent means all tools are available (backward compatible — existing chat actions keep full access).
- `system_prompt` was already supported; no change to its semantics.

## Backend

### `BuildOllamaTools(allowed []string)`

Signature change in `internal/mcpserver/ollama_tools.go`:

- Empty `allowed` → return all 130+ tools (converted from `AllTools()`).
- Non-empty `allowed` → filter to only tool names present in `allowed`.
- The `chatToolWhitelist` constant is removed; it becomes a per-action config concern.

### `GET /api/chat/tools`

New endpoint in `internal/api/handlers_chat.go`. Returns all available tool definitions so the frontend can populate the multi-select picker without hardcoding names.

Response shape:
```json
[
  { "name": "tweets_list", "description": "List tweets with optional filters" },
  ...
]
```

### `POST /api/chat/stream`

Request body gains an optional `action_id` field:

```json
{
  "action_id": "abc123",
  "messages": [...]
}
```

Behavior:
- If `action_id` is provided: look up that specific action (must be active, Ollama type, `usecase: "chat"`), read `allowed_tools` from its config, pass to `BuildOllamaTools`.
- If `action_id` is absent: fall back to current behavior — find any active chat action, read its `allowed_tools` from config (empty = all tools), and pass to `BuildOllamaTools`.

The `findChatPipeline` helper is updated to accept an optional action ID and perform the targeted lookup when present.

## Frontend

### Action Form (`ActionsForm.vue` or equivalent)

When an Ollama action has `usecase: "chat"` in its config, a **Tools** section appears:

- Fetches tool list from `GET /api/chat/tools` when the form opens.
- Renders a searchable multi-select component (consistent with other multi-select patterns in the UI).
- Each item shows tool name and description.
- "Select all" / "Clear all" convenience buttons.
- Selected tool names saved into `config.allowed_tools`.
- If no tools are selected the field is omitted from the saved config (empty = all tools).

### Chat Page (`ChatView.vue`)

An action selector is added at the top of the chat page:

- Dropdown listing all active Ollama actions with `usecase: "chat"`, fetched from `GET /api/actions` and filtered client-side.
- Selected action ID persisted in `localStorage` key `chat_action_id` so it survives page refresh.
- Conversation history keyed by `action_id` in the chat Pinia store (`conversations: Record<string, DisplayMessage[]>`).
- Switching actions swaps in that action's history (empty array if first time).
- `sendMessage` includes `action_id` in the POST body.

### Chat Store (`stores/chat.ts`)

- `conversations: Record<string, DisplayMessage[]>` replaces the flat `messages` array.
- Active conversation derived from `conversations[selectedActionId] ?? []`.
- `selectedActionId` ref kept in sync with `localStorage`.

## API Summary

| Method | Path | Change |
|--------|------|--------|
| GET | `/api/chat/tools` | New — returns all tool name+description pairs |
| POST | `/api/chat/stream` | Body gains optional `action_id` field |

No other API routes change. No DB migration.

## Backward Compatibility

- Existing chat actions with no `allowed_tools` in config continue to work unchanged (all tools available).
- `action_id` is optional in the stream request; omitting it preserves current fallback behavior.
