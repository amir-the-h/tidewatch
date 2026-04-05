# Agentic Chat Design

**Date:** 2026-04-05  
**Status:** Approved

## Overview

Replace the tweet-context chat feature with a fully agentic assistant. The LLM receives all iran-watcher MCP tools and autonomously calls them to answer questions. Tool calls are visible to the user as collapsible steps in the UI, followed by the streamed final answer. No tweet pre-selection is required.

## Goals

- Remove the tweet pre-selection requirement; `/chat` is always accessible
- Give the model read and write access to all iran-watcher REST API tools
- Show tool calls and results transparently in the UI (collapsible)
- Keep the implementation server-side; the frontend is a renderer of a richer event stream

## Architecture

### Backend Agentic Loop

A new `AgentStream` method on `OllamaPipeline` drives the conversation:

1. Convert `AllTools()` ToolDefs to Ollama tool JSON schema (`BuildOllamaTools()`)
2. Send messages + tools to Ollama `/api/chat` with `stream: false` (tool-call turns need full responses)
3. If response contains `tool_calls`: for each call, write `tool_call` SSE event → execute via `ToolExecutor` → write `tool_result` SSE event → append both to messages → go to step 2
4. If response is plain text (no tool calls): stream it as `text_chunk` SSE events → write `[DONE]`

No hard cap on tool-call iterations; the loop runs until the model produces a text response.

Tool-call turns use `stream: false` because Ollama emits tool_calls only in the last chunk of a streaming response, requiring full buffering anyway. Only the final text answer turn uses streaming.

### `ToolExecutor` Interface

Defined in `internal/inference/pipeline.go`:

```go
type ToolExecutor interface {
    Execute(ctx context.Context, name string, args map[string]any) (string, error)
}
```

`mcpserver.Dispatcher` satisfies this interface via a new `Execute` method that looks up the `ToolDef` by name and delegates to the existing `do()` method.

### `ChatPipeline` Interface

```go
type ChatPipeline interface {
    Pipeline
    AgentStream(ctx context.Context, messages []ChatMessage, tools []OllamaTool, executor ToolExecutor, w io.Writer) error
}
```

`EstimateTokens` and `ChatStream` are removed. `AgentStream` receives the pre-built tool list from the caller to avoid a circular import between `internal/inference` and `internal/mcpserver`.

### Ollama Tool Types

New types in `internal/inference`:

```go
type OllamaTool struct {
    Type     string         `json:"type"` // always "function"
    Function OllamaToolFunc `json:"function"`
}
type OllamaToolFunc struct {
    Name        string                 `json:"name"`
    Description string                 `json:"description"`
    Parameters  map[string]interface{} `json:"parameters"` // JSON Schema object
}
```

### Tool Definition Conversion

`BuildOllamaTools()` in `internal/mcpserver` (alongside `schema.go`) iterates `AllTools()` and converts each `ToolDef` to `OllamaTool`. `PathParams`, `QueryParams`, and `Body` fields map to JSON Schema `properties`. Required params are collected into a `required` array. The function is exported so `internal/api` can call it without `internal/inference` importing `internal/mcpserver`.

### Dispatcher Wiring

The API server (`internal/api/server.go`) constructs a `mcpserver.Dispatcher` at startup with:
- `baseURL = "http://localhost:{port}"` (the server's own bind address)
- Admin credentials from config

The Dispatcher is passed to `handleChatStream` as a `inference.ToolExecutor`. Tool calls loop back to the same server, reusing all existing auth and route logic.

### System Prompt

```
You are an intelligent assistant for the iran-watcher monitoring application. You have tools to query and manage all aspects of the system — tweets, rules, pipelines, actions, entities, and more. Use them to answer questions accurately and concisely.
```

## API Changes

### `POST /api/chat/stream`

**Request body (new):**
```json
{ "messages": [{"role": "user", "content": "..."}] }
```
`tweet_ids` field removed.

**SSE event stream (new format):**
```
data: {"type":"tool_call","name":"tweets_list","args":{"limit":10}}\n\n
data: {"type":"tool_result","name":"tweets_list","result":"{\"tweets\":[]}"}\n\n
data: {"type":"text_chunk","content":"In the last 24 hours..."}\n\n
data: [DONE]\n\n
```

Event types:
| Type | When emitted | Fields |
|------|-------------|--------|
| `tool_call` | Before tool execution | `name`, `args` |
| `tool_result` | After tool execution | `name`, `result` (string, max 8KB) |
| `text_chunk` | During final answer stream | `content` |
| `error` | On unrecoverable failure | `message` |

`[DONE]` sentinel retained for end-of-stream.

### `POST /api/chat/estimate` — **Removed**

### Router

`/api/chat/estimate` route removed from `internal/api/server.go`.

## Frontend Changes

### `web/src/api/chat.ts`

- Remove `estimateTokens`
- `streamChat` request body: drop `tweet_ids`
- SSE parser: parse `data: {...}` as JSON and dispatch by `type`; handle `tool_call`, `tool_result`, `text_chunk`, `error`; `[DONE]` sentinel unchanged

New type:
```ts
type ChatEvent =
  | { type: 'tool_call'; name: string; args: Record<string, unknown> }
  | { type: 'tool_result'; name: string; result: string }
  | { type: 'text_chunk'; content: string }
  | { type: 'error'; message: string }
```

### `web/src/stores/chat.ts`

**Removed:** `tweetIds`, `estimate`, `initSession`, `loadEstimate`, `hasSession`

**Updated:** `sendMessage` handles `tool_call`/`tool_result` events by accumulating into `toolSteps` on the current assistant message; `text_chunk` events append to message text content.

Message shape extended:
```ts
interface AssistantMessage {
  role: 'assistant'
  content: string
  toolSteps: { name: string; args: unknown; result: string }[]
}
```

### `web/src/views/ChatView.vue`

- Remove token bar and token estimate display
- Remove "no session" guard — `/chat` is always navigable
- Each assistant message renders:
  1. **Tool steps** — `<details>` elements (collapsed by default), one per tool call/result pair. Header: `▶ Called {name}`. Body: args + result.
  2. **Text content** — streams in below the steps
- Nav sidebar: add direct "Chat" link (no longer gated on tweet selection)

### Removed Entry Points

- Bulk-select chat button on `/tweets`
- Per-entity chat button on `/tracker`
- All `useChatStore.initSession()` call sites

## Code Removed

| Location | What is removed |
|----------|----------------|
| `internal/inference/ollama.go` | `ChatStream`, `EstimateTokens` methods |
| `internal/inference/pipeline.go` | `ChatPipeline.EstimateTokens`, `ChatStream` from interface |
| `internal/api/handlers_chat.go` | `handleChatEstimate`, `fetchTweetsByIDs`, `formatTweetsForChat` |
| `internal/api/server.go` | `POST /api/chat/estimate` route |
| `web/src/api/chat.ts` | `estimateTokens` function, `TokenEstimate` type |
| `web/src/stores/chat.ts` | `tweetIds`, `estimate`, `initSession`, `loadEstimate`, `hasSession` |
| `web/src/views/ChatView.vue` | Token bar, session guard |
| `web/src/views/TweetsView.vue` | Bulk-select chat button |
| `web/src/views/TrackerView.vue` | Per-entity chat button |

## Error Handling

- **Tool execution error** → emit `tool_result` with error text as content; model can see it and react
- **Tool name not found** → `Execute` returns error → same as above
- **Malformed tool call args** → JSON unmarshal failure → emit error `tool_result`
- **All Ollama backends exhausted** → emit `{"type":"error","message":"..."}` SSE event, close stream
- **Tool result too large** → truncate at 8KB with a note appended: `"[truncated]"`

## Testing

- Unit test `BuildOllamaTools()` — verify ToolDef fields produce correct JSON Schema properties and required arrays
- Unit test `Dispatcher.Execute()` — mock HTTP server, verify path substitution and body marshalling
- Unit test `OllamaPipeline.AgentStream` — mock `ToolExecutor` + mock Ollama server; verify:
  - `tool_call`/`tool_result` SSE events written before `text_chunk`
  - Loop terminates when Ollama returns no tool_calls
  - Error events written on tool execution failure
- Frontend: update `chat.ts` tests for new event format; remove estimate-related tests

## CLAUDE.md Updates Required

- Update "Tweet chat" bullet in Key Design Decisions to describe the new agentic chat
- Remove `EstimateTokens` reference from `ChatPipeline` description
- Add `ToolExecutor` interface and `buildOllamaTools` to relevant sections
