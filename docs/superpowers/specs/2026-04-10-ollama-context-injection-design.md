# Ollama Context Injection Design

**Date:** 2026-04-10
**Status:** Approved

## Problem

Ollama pipeline actions currently receive only the raw tweet text as input to the LLM. Upstream action outputs (NER entities, classification labels, structured embeds, etc.) accumulate in `input.Context` but are never forwarded to the model. This prevents actions like the Discord embed generator (action 84) from including dynamic values such as `tweet_url` or `tweet_id` in their output, and prevents LLMs from reasoning over the full enriched context that prior actions have produced.

## Goal

Allow individual Ollama actions to be configured to pass selected upstream action outputs — plus built-in tweet variables — to the LLM as a structured `<context>` block appended to (or replacing) the tweet text.

## Scope

Ollama actions only. ONNX classifiers and NER models take fixed-format text and are unaffected.

---

## Config Schema

Two new optional keys in the Ollama action `config` JSON:

```json
{
  "model": "gemma4:e2b-it-q4_K_M",
  "system_prompt": "...",
  "input_mode": "text_and_context",
  "context_action_ids": [81, 82, 83]
}
```

### `input_mode` (string, default `"text_only"`)

| Value | LLM receives |
|---|---|
| `"text_only"` | Raw tweet text only (current behaviour, default when key absent) |
| `"context_only"` | `<context>` block only, no tweet text |
| `"text_and_context"` | Tweet text followed by `<context>` block |

### `context_action_ids` ([]int64, default empty)

Action IDs whose latest successful execution metadata for this tweet is included in the `<context>` block. Order in the block follows the array order. When empty or `input_mode` is `"text_only"`, no context loading occurs.

---

## Context Block Format

Built-in tweet variables are always prepended to the block when context mode is active, regardless of `context_action_ids`:

```
<context>
tweet_id: 1234567890
tweet_url: https://x.com/user/status/1234567890
tweet_author: user123
tweet_short: Iran state media reports...

War Analytics Classifier:
  top_label: assassination
  confidence: 0.97
  entities: ["Khamenei","IRGC","Tehran"]

War Analytics NER:
  discord_embed: {"color":15158332,"title":"🔴 Assassination","description":"Senior IRGC commander...","fields":[{"name":"Location","value":"Tehran","inline":true}]}
</context>
```

### Formatting rules

- **Simple scalars** (string, number, bool): rendered as plain text on the same line as the key (`key: value`)
- **Arrays and objects** (including deeply nested): JSON-serialised as compact single-line JSON (`key: [...]` or `key: {...}`)
- **Missing action outputs** (action ran but produced no metadata, or action hasn't run for this tweet): section omitted silently
- **Action name** used as section header (from `ActionSummary.ActionName`), not the action ID

Built-in tweet vars included: `tweet_id`, `tweet_url`, `tweet_author`, `tweet_short`.

---

## Implementation

### 1. `OllamaPipeline` struct (`internal/inference/ollama.go`)

`internal/pipeline` imports `internal/inference`, so the loader type must be defined in `inference` to avoid a circular import. Two new types added to `internal/inference`:

```go
// ContextSummary holds the metadata output of one action, used to build
// the <context> block passed to the LLM.
type ContextSummary struct {
    ActionID   int64
    ActionName string
    Metadata   map[string]any
}

// ContextSummaryLoader fetches ContextSummary records for the given tweetID,
// filtered to the provided action IDs. Injected at construction time.
type ContextSummaryLoader func(ctx context.Context, tweetID string, actionIDs []int64) ([]ContextSummary, error)
```

Three new fields on `OllamaPipeline`:

```go
type OllamaPipeline struct {
    // ... existing fields ...
    inputMode        string               // "text_only" | "context_only" | "text_and_context"
    contextActionIDs []int64
    loadSummaries    ContextSummaryLoader // nil = context disabled
}
```

`NewOllamaPipelineFromConfig` reads `input_mode` and `context_action_ids` from config. The `loadSummaries` callback is a new parameter injected at construction time.

### 2. `OllamaPipeline.Run` (`internal/inference/ollama.go`)

Before calling `sendChat`, `Run` builds the user message:

```
if inputMode != "text_only" && loadSummaries != nil:
    load summaries for tweetID
    filter to contextActionIDs
    build <context> block
    
    if inputMode == "context_only":
        userText = contextBlock
    else: // text_and_context
        userText = input.Text + "\n\n" + contextBlock
else:
    userText = input.Text  // unchanged
```

A new unexported function `buildContextBlock(tweetVars map[string]any, summaries []ContextSummary) string` handles formatting, kept in `ollama.go` alongside `sendChat`.

### 3. Wiring (`cli/cmd/serve.go`)

Where `NewOllamaPipelineFromConfig` is called, pass a `loadSummaries` closure wrapping `store.ListTweetActionSummaries` — identical pattern to `NotificationPipeline`'s `loadActions` injection today.

### 4. Store — no changes needed

`store.ListTweetActionSummaries(ctx, tweetID)` already returns `[]TweetActionSummary` with per-action metadata. In `serve.go`, the `ContextSummaryLoader` closure wraps `ListTweetActionSummaries`, converts `[]store.TweetActionSummary` → `[]inference.ContextSummary`, and filters to the requested `actionIDs` before returning.

### 5. UI (`web/src/views/PipelinesView.vue`)

Two new fields in the Ollama action form, below the system prompt:

- **Input Mode** — `<n-select>` with options: `Text only` (default), `Context only`, `Text + Context`
- **Context Actions** — `<n-select multiple>` visible only when mode is not `text_only`; populated from `actionsData` (name + ID), excluding the action being edited

Saved into `config.input_mode` and `config.context_action_ids`. Loaded back when opening edit form.

---

## Data Flow

```
Tweet arrives
    → worker builds mergedCtx (tweet vars + all upstream metadata)
    → worker calls pipeline.Run(input) where input.Context = mergedCtx
        → OllamaPipeline.Run checks inputMode
        → if context mode: calls loadSummaries(ctx, tweetID)
            → store.ListTweetActionSummaries returns per-action summaries
            → filter to contextActionIDs
            → buildContextBlock produces <context>...</context> string
        → sendChat(userText) with assembled message
        → LLM response parsed as JSON → returned as action outputs
```

---

## Error Handling

- `loadSummaries` error: log and fall back to `input_mode = "text_only"` for this execution (non-fatal; don't fail the task)
- Unknown `input_mode` value: treat as `"text_only"`
- Action ID in `context_action_ids` with no executions for this tweet: silently omit that action's section

---

## Testing

- Unit test `buildContextBlock`: scalars, arrays, nested objects, missing action IDs, empty context_action_ids
- Unit test `OllamaPipeline.Run` with a mock `loadSummaries`: verify correct user message for each mode
- Existing `sendChat` / `runRaw` tests unaffected (they bypass `Run`)
