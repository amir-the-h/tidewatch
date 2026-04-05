# Specialized Chat Actions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow each Chat action to carry its own system prompt and curated tool list, and let the Chat page switch between them.

**Architecture:** Store `allowed_tools: []string` in the Ollama action config JSON. `BuildOllamaTools` gains an `allowed []string` filter parameter. The chat stream endpoint accepts an `action_id` to route to a specific action. The Chat page renders an action picker backed by a per-action conversation map in the Pinia store.

**Tech Stack:** Go 1.25, chi router, Vue 3, Naive UI, Pinia

---

## File Map

| File | Change |
|------|--------|
| `internal/mcpserver/ollama_tools.go` | `BuildOllamaTools(allowed []string)` — remove whitelist, accept filter |
| `internal/mcpserver/ollama_tools_test.go` | Update tests for new signature |
| `internal/api/handlers_chat.go` | Add `GET /api/chat/tools`; update `handleChatStream` + `findChatPipeline` |
| `internal/api/server.go` | Register new route |
| `web/src/api/chat.ts` | Add `listChatTools()`; update `streamChat` to accept `actionId` |
| `web/src/views/PipelinesView.vue` | Add tools multi-select; show system prompt for all Ollama usecases; allow multiple chat actions |
| `web/src/stores/chat.ts` | Per-action conversation map; `selectedActionId` with localStorage persistence |
| `web/src/views/ChatView.vue` | Action selector dropdown |

---

## Task 1: Refactor `BuildOllamaTools` — accept allow-list filter

**Files:**
- Modify: `internal/mcpserver/ollama_tools.go`
- Modify: `internal/mcpserver/ollama_tools_test.go`

- [ ] **Step 1: Update the test file first (TDD)**

Replace the entire contents of `internal/mcpserver/ollama_tools_test.go`:

```go
package mcpserver_test

import (
	"testing"

	"github.com/amir-the-h/iran-watcher/internal/mcpserver"
)

func TestBuildOllamaTools_EmptyAllowed_ReturnsAll(t *testing.T) {
	tools := mcpserver.BuildOllamaTools(nil)
	if len(tools) == 0 {
		t.Fatal("expected non-empty tool list")
	}
	// All 130+ tools should be present — just verify count is large
	if len(tools) < 50 {
		t.Errorf("expected at least 50 tools, got %d", len(tools))
	}
}

func TestBuildOllamaTools_FilteredAllowed(t *testing.T) {
	tools := mcpserver.BuildOllamaTools([]string{"tweets_list", "health_get"})
	if len(tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(tools))
	}
	names := make(map[string]bool)
	for _, tool := range tools {
		names[tool.Function.Name] = true
	}
	if !names["tweets_list"] {
		t.Error("expected tweets_list in filtered tools")
	}
	if !names["health_get"] {
		t.Error("expected health_get in filtered tools")
	}
}

func TestBuildOllamaTools_FilteredUnknownName_Ignored(t *testing.T) {
	tools := mcpserver.BuildOllamaTools([]string{"tweets_list", "does_not_exist"})
	if len(tools) != 1 {
		t.Errorf("expected 1 tool (unknown ignored), got %d", len(tools))
	}
}

func TestBuildOllamaTools_PathParamMapped(t *testing.T) {
	tools := mcpserver.BuildOllamaTools([]string{"tweets_get"})
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	tool := tools[0]
	props, ok := tool.Function.Parameters["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("parameters.properties is not a map")
	}
	idProp, ok := props["id"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'id' property")
	}
	if idProp["type"] != "string" {
		t.Errorf("expected type=string for id, got %v", idProp["type"])
	}
	req, ok := tool.Function.Parameters["required"].([]string)
	if !ok {
		t.Fatal("expected required array")
	}
	var hasID bool
	for _, r := range req {
		if r == "id" {
			hasID = true
		}
	}
	if !hasID {
		t.Error("expected 'id' in required array")
	}
}

func TestBuildOllamaTools_BodyMapped(t *testing.T) {
	tools := mcpserver.BuildOllamaTools([]string{"actions_create"})
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	props, ok := tools[0].Function.Parameters["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("parameters.properties is not a map")
	}
	if _, ok := props["body"]; !ok {
		t.Error("expected 'body' property for actions_create")
	}
}

func TestBuildOllamaTools_TypeField(t *testing.T) {
	tools := mcpserver.BuildOllamaTools(nil)
	for _, tool := range tools {
		if tool.Type != "function" {
			t.Errorf("tool %s: expected type=function, got %q", tool.Function.Name, tool.Type)
		}
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail (signature mismatch)**

```bash
go test ./internal/mcpserver/... 2>&1 | head -20
```

Expected: compile error — `too many arguments in call to mcpserver.BuildOllamaTools`

- [ ] **Step 3: Update `ollama_tools.go`**

Replace the entire `BuildOllamaTools` function, `chatToolWhitelist`, and `buildFilteredOllamaTools` in `internal/mcpserver/ollama_tools.go`. The file section from line 16 to line 79 becomes:

```go
// BuildOllamaTools converts registered ToolDefs to Ollama's JSON Schema format.
// allowed is a list of tool names to include; nil or empty means all tools.
// Called at request time by the chat handler.
func BuildOllamaTools(allowed []string) []OllamaTool {
	all := AllTools()
	if len(allowed) == 0 {
		out := make([]OllamaTool, 0, len(all))
		for _, td := range all {
			out = append(out, toolDefToOllama(td))
		}
		return out
	}
	filter := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		filter[name] = true
	}
	out := make([]OllamaTool, 0, len(allowed))
	for _, td := range all {
		if filter[td.Name] {
			out = append(out, toolDefToOllama(td))
		}
	}
	return out
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
go test ./internal/mcpserver/... -v 2>&1 | tail -20
```

Expected: all tests PASS

- [ ] **Step 5: Fix the compile error in `handlers_chat.go`**

In `internal/api/handlers_chat.go` line 77, change:
```go
tools := mcpserver.BuildOllamaTools()
```
to:
```go
tools := mcpserver.BuildOllamaTools(nil)
```

(This will be replaced properly in Task 2, but the code must compile now.)

- [ ] **Step 6: Verify the project still compiles**

```bash
go build ./... 2>&1
```

Expected: no errors

- [ ] **Step 7: Commit**

```bash
git add internal/mcpserver/ollama_tools.go internal/mcpserver/ollama_tools_test.go internal/api/handlers_chat.go
git commit -m "feat: BuildOllamaTools accepts allow-list filter; empty = all tools"
```

---

## Task 2: Backend — `GET /api/chat/tools` + update `handleChatStream`

**Files:**
- Modify: `internal/api/handlers_chat.go`
- Modify: `internal/api/server.go`

- [ ] **Step 1: Update `handlers_chat.go` — full replacement**

Replace the entire contents of `internal/api/handlers_chat.go` with:

```go
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/mcpserver"
)

// lockedWriter wraps an http.ResponseWriter with a mutex so that the
// keep-alive goroutine and AgentStream can safely write concurrently.
type lockedWriter struct {
	mu sync.Mutex
	w  http.ResponseWriter
}

func (lw *lockedWriter) Write(b []byte) (int, error) {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	return lw.w.Write(b)
}

func (lw *lockedWriter) Flush() {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	if f, ok := lw.w.(http.Flusher); ok {
		f.Flush()
	}
}

// handleChatTools returns all available MCP tool names and descriptions.
//
// GET /api/chat/tools
// Response: [{"name":"...","description":"..."}]
func (s *Server) handleChatTools(w http.ResponseWriter, r *http.Request) {
	all := mcpserver.AllTools()
	type toolInfo struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	out := make([]toolInfo, len(all))
	for i, td := range all {
		out[i] = toolInfo{Name: td.Name, Description: td.Description}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleChatStream runs the agentic chat loop and streams events as SSE.
//
// POST /api/chat/stream
// Body: { "action_id": "123", "messages": [{"role":"user","content":"..."}] }
// Response: text/event-stream
//
// Event types:
//
//	{"type":"tool_call","name":"...","args":{...}}
//	{"type":"tool_result","name":"...","result":"..."}
//	{"type":"text_chunk","content":"..."}
//	[DONE]
func (s *Server) handleChatStream(w http.ResponseWriter, r *http.Request) {
	if s.actionCache == nil {
		writeError(w, http.StatusServiceUnavailable, "action cache not available")
		return
	}
	if s.toolExec == nil {
		writeError(w, http.StatusServiceUnavailable, "tool executor not configured")
		return
	}

	// Decode and validate request before committing to SSE response.
	var req struct {
		ActionID string                  `json:"action_id"`
		Messages []inference.ChatMessage `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "messages required")
		return
	}

	var actionID int64
	if req.ActionID != "" {
		var err error
		actionID, err = strconv.ParseInt(req.ActionID, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid action_id")
			return
		}
	}

	cp, allowedTools := s.findChatPipeline(actionID)
	if cp == nil {
		writeError(w, http.StatusUnprocessableEntity, "no active chat-capable model (requires an active Ollama action)")
		return
	}

	tools := mcpserver.BuildOllamaTools(allowedTools)

	// Commit to SSE — no more early returns after this point.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	// Wrap w so keep-alive goroutine and AgentStream can write concurrently.
	lw := &lockedWriter{w: w}
	fmt.Fprint(lw, ": keep-alive\n\n")
	lw.Flush()

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				fmt.Fprint(lw, ": keep-alive\n\n")
				lw.Flush()
			}
		}
	}()

	if err := cp.AgentStream(ctx, req.Messages, tools, s.toolExec, lw); err != nil {
		if !errors.Is(err, context.Canceled) {
			s.logger.Error("api: chat stream", "error", err)
		}
	}
}

// findChatPipeline returns the chat pipeline and its allowed tool list for the
// given action ID. If actionID is 0, falls back to the first active action with
// usecase=="chat", then any active ChatPipeline.
// Returns (nil, nil) if no suitable action is found.
func (s *Server) findChatPipeline(actionID int64) (inference.ChatPipeline, []string) {
	if s.actionCache == nil {
		return nil, nil
	}

	// Targeted lookup by action ID.
	if actionID > 0 {
		e := s.actionCache.Get(actionID)
		if e == nil {
			return nil, nil
		}
		cp, ok := e.Pipeline.(inference.ChatPipeline)
		if !ok {
			return nil, nil
		}
		return cp, actionAllowedTools(e.Action.Config)
	}

	// Fallback: find any active chat action.
	var fallback inference.ChatPipeline
	var fallbackTools []string
	for _, e := range s.actionCache.All() {
		cp, ok := e.Pipeline.(inference.ChatPipeline)
		if !ok {
			continue
		}
		if actionUsecase(e.Action.Config) == "chat" {
			return cp, actionAllowedTools(e.Action.Config)
		}
		if fallback == nil {
			fallback = cp
			fallbackTools = actionAllowedTools(e.Action.Config)
		}
	}
	return fallback, fallbackTools
}

// actionUsecase extracts the "usecase" field from an action's JSON config.
func actionUsecase(cfg json.RawMessage) string {
	if len(cfg) == 0 {
		return ""
	}
	var c struct {
		Usecase string `json:"usecase"`
	}
	if err := json.Unmarshal(cfg, &c); err != nil {
		return ""
	}
	return c.Usecase
}

// actionAllowedTools extracts the "allowed_tools" field from an action's JSON config.
// Returns nil if absent (meaning all tools are allowed).
func actionAllowedTools(cfg json.RawMessage) []string {
	if len(cfg) == 0 {
		return nil
	}
	var c struct {
		AllowedTools []string `json:"allowed_tools"`
	}
	if err := json.Unmarshal(cfg, &c); err != nil {
		return nil
	}
	return c.AllowedTools
}
```

- [ ] **Step 2: Register `GET /api/chat/tools` in `server.go`**

In `internal/api/server.go`, find the chat route block (around line 267):
```go
// Chat
r.Post("/chat/stream", srv.handleChatStream)
```

Replace with:
```go
// Chat
r.Get("/chat/tools", srv.handleChatTools)
r.Post("/chat/stream", srv.handleChatStream)
```

- [ ] **Step 3: Verify the project compiles**

```bash
go build ./... 2>&1
```

Expected: no errors

- [ ] **Step 4: Run all tests**

```bash
go test ./... 2>&1 | tail -20
```

Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/api/handlers_chat.go internal/api/server.go
git commit -m "feat: add GET /api/chat/tools; handleChatStream accepts action_id"
```

---

## Task 3: Frontend API layer — `listChatTools` + update `streamChat`

**Files:**
- Modify: `web/src/api/chat.ts`

- [ ] **Step 1: Replace `web/src/api/chat.ts`**

```typescript
const apiBase: string =
  (window as Window & { __ENV__?: { API_BASE_URL?: string } }).__ENV__?.API_BASE_URL ||
  (import.meta.env.VITE_API_BASE_URL as string) ||
  ''

export interface ChatMessage {
  role: 'user' | 'assistant' | 'system'
  content: string
}

export interface ChatToolInfo {
  name: string
  description: string
}

export type ChatEvent =
  | { type: 'tool_call'; name: string; args: Record<string, unknown> }
  | { type: 'tool_result'; name: string; result: string }
  | { type: 'text_chunk'; content: string }
  | { type: 'error'; message: string }

/** Returns all available MCP tool names and descriptions. */
export async function listChatTools(): Promise<ChatToolInfo[]> {
  const token = localStorage.getItem('iran_watcher_token')
  const headers: Record<string, string> = {}
  if (token) headers['Authorization'] = `Bearer ${token}`
  const res = await fetch(`${apiBase}/api/chat/tools`, { headers })
  if (!res.ok) throw new Error(`Failed to load tools: ${res.statusText}`)
  return res.json()
}

/**
 * Streams an agentic chat response via SSE (POST /api/chat/stream).
 * Returns an AbortController; call .abort() to cancel mid-stream.
 *
 * actionId: ID of the chat action to use. Empty string = server fallback.
 * onEvent is called for each parsed ChatEvent.
 * onDone is called once the [DONE] sentinel is received.
 * onError is called on network or server errors (not on abort).
 */
export function streamChat(
  actionId: string,
  messages: ChatMessage[],
  onEvent: (event: ChatEvent) => void,
  onDone: () => void,
  onError: (err: Error) => void,
): AbortController {
  const controller = new AbortController()

  ;(async () => {
    try {
      const token = localStorage.getItem('iran_watcher_token')
      const headers: Record<string, string> = { 'Content-Type': 'application/json' }
      if (token) headers['Authorization'] = `Bearer ${token}`

      const body: Record<string, unknown> = { messages }
      if (actionId) body['action_id'] = actionId

      const res = await fetch(`${apiBase}/api/chat/stream`, {
        method: 'POST',
        headers,
        body: JSON.stringify(body),
        signal: controller.signal,
      })

      if (res.status === 401) {
        localStorage.removeItem('iran_watcher_token')
        window.location.href = '/login'
        return
      }
      if (!res.ok) {
        const b = await res.json().catch(() => ({ error: res.statusText }))
        throw new Error((b as any).error ?? res.statusText)
      }

      const reader = res.body!.getReader()
      const decoder = new TextDecoder()
      let buffer = ''

      while (true) {
        const { done, value } = await reader.read()
        if (done) break

        buffer += decoder.decode(value, { stream: true })
        const lines = buffer.split('\n')
        buffer = lines.pop() ?? ''

        for (const line of lines) {
          if (!line.startsWith('data: ')) continue
          const data = line.slice(6).trim()
          if (data === '[DONE]') {
            onDone()
            return
          }
          if (data.startsWith(':')) continue // SSE comment / keep-alive
          try {
            const event = JSON.parse(data) as ChatEvent
            onEvent(event)
          } catch {
            // skip malformed event
          }
        }
      }
      onDone()
    } catch (err) {
      if ((err as any).name !== 'AbortError') {
        onError(err as Error)
      }
    }
  })()

  return controller
}
```

- [ ] **Step 2: Verify TypeScript compiles (via docker build dry-run)**

```bash
docker compose build iran-watcher-ui 2>&1 | tail -20
```

If no separate UI service, skip — the full build in Task 6 will catch it.

- [ ] **Step 3: Commit**

```bash
git add web/src/api/chat.ts
git commit -m "feat: add listChatTools API; streamChat accepts actionId param"
```

---

## Task 4: Frontend — Update action form for chat actions

**Files:**
- Modify: `web/src/views/PipelinesView.vue`

- [ ] **Step 1: Add state for chat tools in the script section**

After the line `const formSystemPrompt  = ref('')` (around line 65 in `PipelinesView.vue`), add:

```typescript
const formAllowedTools  = ref<string[]>([])
const chatTools         = ref<{ name: string; description: string }[]>([])
const chatToolsLoading  = ref(false)
```

Also add the import for `listChatTools` at the top of the imports from `../api/chat`:

Find the existing import block (there may not be one from `../api/chat` yet in this file). Add it near the other API imports:

```typescript
import { listChatTools } from '../api/chat'
```

- [ ] **Step 2: Add `chatToolOptions` computed**

After the `handlerOptions` computed (around line 130), add:

```typescript
const chatToolOptions = computed(() =>
  chatTools.value.map(t => ({ label: t.name, value: t.name, description: t.description }))
)
```

- [ ] **Step 3: Add tool loading function**

After `fetchActions()`, add:

```typescript
async function fetchChatTools() {
  if (chatTools.value.length > 0) return // already loaded
  chatToolsLoading.value = true
  try {
    chatTools.value = await listChatTools()
  } catch (e: any) {
    msg.error('Failed to load chat tools: ' + e.message)
  } finally {
    chatToolsLoading.value = false
  }
}
```

- [ ] **Step 4: Reset `formAllowedTools` in `resetActionForm`**

In `resetActionForm()`, after the line `formSystemPrompt.value = ''`, add:

```typescript
formAllowedTools.value = []
```

- [ ] **Step 5: Populate `formAllowedTools` in `openEditAction`**

In `openEditAction()`, after the line `formSystemPrompt.value = row.config?.system_prompt ?? ''`, add:

```typescript
formAllowedTools.value = Array.isArray(row.config?.allowed_tools) ? [...row.config.allowed_tools] : []
if (row.type === 'ollama' && row.config?.usecase === 'chat') {
  fetchChatTools()
}
```

- [ ] **Step 6: Trigger chat tool load when usecase switches to "chat" in create mode**

After `openCreateAction()`, add a watcher. Find where the `watch` or `onMounted` calls are, or add near the end of the `<script setup>` block:

```typescript
watch(formUsecase, (val) => {
  if (val === 'chat' && formType.value === 'ollama') {
    fetchChatTools()
  }
})
```

- [ ] **Step 7: Update `onSaveAction` for Ollama actions**

In `onSaveAction`, find the `if (formType.value === 'ollama')` block (around line 313). Replace it with:

```typescript
if (formType.value === 'ollama') {
  if (!formModel.value.trim()) {
    msg.error('Model name is required')
    return
  }
  config.model = formModel.value.trim()
  config.usecase = formUsecase.value
  if (formSystemPrompt.value.trim()) {
    config.system_prompt = formSystemPrompt.value.trim()
  }
  if (formUsecase.value === 'chat' && formAllowedTools.value.length > 0) {
    config.allowed_tools = formAllowedTools.value
  }
  // Only enforce uniqueness for digest (allow multiple chat actions)
  if (editingActionId.value === null && formUsecase.value === 'digest') {
    const conflict = actionsData.value.find(a => a.config?.usecase === 'digest')
    if (conflict) {
      msg.error(`A digest action already exists ("${conflict.name}"). Delete it first.`)
      return
    }
  }
}
```

- [ ] **Step 8: Update the Ollama form template**

Find the Ollama-specific template block (around line 981):

```vue
<!-- Ollama-specific -->
<template v-if="formType === 'ollama'">
  <n-form-item label="Model">
    <n-input v-model:value="formModel" placeholder="e.g. llama3.2, mistral, gemma3" />
  </n-form-item>
  <n-form-item label="Use Case">
    <n-select v-model:value="formUsecase" :options="[
      { label: 'Custom (action)', value: 'custom' },
      { label: 'Digest',          value: 'digest' },
      { label: 'Chat',            value: 'chat'   },
    ]" />
  </n-form-item>
  <n-form-item v-if="formUsecase === 'custom'" label="System Prompt">
    <n-input
      v-model:value="formSystemPrompt"
      type="textarea" :rows="4"
      placeholder="Leave empty to use the default generic extraction prompt"
      style="font-family: monospace; font-size: 12px"
    />
  </n-form-item>
</template>
```

Replace with:

```vue
<!-- Ollama-specific -->
<template v-if="formType === 'ollama'">
  <n-form-item label="Model">
    <n-input v-model:value="formModel" placeholder="e.g. llama3.2, mistral, gemma3" />
  </n-form-item>
  <n-form-item label="Use Case">
    <n-select v-model:value="formUsecase" :options="[
      { label: 'Custom (action)', value: 'custom' },
      { label: 'Digest',          value: 'digest' },
      { label: 'Chat',            value: 'chat'   },
    ]" />
  </n-form-item>
  <n-form-item label="System Prompt">
    <n-input
      v-model:value="formSystemPrompt"
      type="textarea" :rows="4"
      placeholder="Leave empty to use the default system prompt"
      style="font-family: monospace; font-size: 12px"
    />
  </n-form-item>
  <!-- Chat tools selector (chat usecase only) -->
  <template v-if="formUsecase === 'chat'">
    <n-divider>Chat Tools</n-divider>
    <n-form-item label="Allowed Tools">
      <n-select
        v-model:value="formAllowedTools"
        multiple
        filterable
        clearable
        :options="chatToolOptions"
        :loading="chatToolsLoading"
        placeholder="Leave empty to allow all tools"
        :render-label="(opt: any) => opt.label"
      />
    </n-form-item>
    <n-text depth="3" style="font-size: 12px; margin-top: -8px; display: block">
      Restrict the tools available to this chat assistant. Leave empty to allow all {{ chatTools.length }} tools.
    </n-text>
  </template>
</template>
```

- [ ] **Step 9: Verify Docker build passes**

```bash
docker compose build iran-watcher 2>&1 | tail -30
```

Expected: build succeeds

- [ ] **Step 10: Commit**

```bash
git add web/src/views/PipelinesView.vue
git commit -m "feat: action form — tools multi-select for chat actions; system prompt for all Ollama usecases"
```

---

## Task 5: Frontend — Refactor chat store for per-action conversation history

**Files:**
- Modify: `web/src/stores/chat.ts`

- [ ] **Step 1: Replace `web/src/stores/chat.ts`**

```typescript
import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { streamChat } from '../api/chat'
import type { ChatMessage, ChatEvent } from '../api/chat'

export type { ChatMessage }

export interface ToolStep {
  name: string
  args: Record<string, unknown>
  result: string
}

export interface DisplayMessage {
  role: 'user' | 'assistant'
  content: string
  toolSteps: ToolStep[]
}

const STORAGE_KEY = 'chat_action_id'

export const useChatStore = defineStore('chat', () => {
  // Per-action conversation history keyed by action ID string.
  const conversations  = ref<Record<string, DisplayMessage[]>>({})
  const selectedActionId = ref<string>(localStorage.getItem(STORAGE_KEY) ?? '')
  const isStreaming    = ref(false)

  let abortController: AbortController | null = null
  let pendingStep: { name: string; args: Record<string, unknown> } | null = null

  /** Messages for the currently selected action. */
  const messages = computed<DisplayMessage[]>(
    () => conversations.value[selectedActionId.value] ?? []
  )

  /** Switch to a different chat action; preserves that action's history. */
  function selectAction(id: string) {
    selectedActionId.value = id
    localStorage.setItem(STORAGE_KEY, id)
  }

  function ensureConversation(id: string) {
    if (!conversations.value[id]) {
      conversations.value[id] = []
    }
  }

  /**
   * Append a user message and stream the assistant reply.
   * Resolves when the stream ends cleanly; rejects on error.
   */
  async function sendMessage(content: string): Promise<void> {
    if (isStreaming.value) return
    const actionId = selectedActionId.value
    ensureConversation(actionId)

    const conv = conversations.value[actionId]

    // Build history to send: only role+content (no toolSteps)
    const history: ChatMessage[] = [
      ...conv.map(m => ({ role: m.role, content: m.content })),
      { role: 'user' as const, content },
    ]

    conv.push({ role: 'user', content, toolSteps: [] })
    const assistantMsg: DisplayMessage = { role: 'assistant', content: '', toolSteps: [] }
    conv.push(assistantMsg)
    const assistantIdx = conv.length - 1

    isStreaming.value = true
    pendingStep = null

    return new Promise((resolve, reject) => {
      abortController = streamChat(
        actionId,
        history,
        (event: ChatEvent) => {
          const msg = conversations.value[actionId][assistantIdx]
          if (event.type === 'tool_call') {
            pendingStep = { name: event.name, args: event.args }
          } else if (event.type === 'tool_result' && pendingStep) {
            conversations.value[actionId][assistantIdx] = {
              ...msg,
              toolSteps: [
                ...msg.toolSteps,
                { name: pendingStep.name, args: pendingStep.args, result: event.result },
              ],
            }
            pendingStep = null
          } else if (event.type === 'text_chunk') {
            conversations.value[actionId][assistantIdx] = {
              ...msg,
              content: msg.content + event.content,
            }
          }
        },
        () => {
          isStreaming.value = false
          abortController = null
          pendingStep = null
          resolve()
        },
        (err) => {
          isStreaming.value = false
          abortController = null
          pendingStep = null
          reject(err)
        },
      )
    })
  }

  /** Cancel an in-progress stream. */
  function stopStreaming() {
    abortController?.abort()
    abortController = null
    isStreaming.value = false
    pendingStep = null
  }

  /** Clear the conversation for the currently selected action. */
  function clearSession() {
    stopStreaming()
    if (selectedActionId.value) {
      conversations.value[selectedActionId.value] = []
    }
  }

  return {
    messages,
    conversations,
    selectedActionId,
    isStreaming,
    selectAction,
    sendMessage,
    stopStreaming,
    clearSession,
  }
})
```

- [ ] **Step 2: Verify Docker build passes**

```bash
docker compose build iran-watcher 2>&1 | tail -30
```

Expected: build succeeds

- [ ] **Step 3: Commit**

```bash
git add web/src/stores/chat.ts
git commit -m "feat: chat store — per-action conversation history with localStorage-persisted action selection"
```

---

## Task 6: Frontend — Add action selector to `ChatView.vue`

**Files:**
- Modify: `web/src/views/ChatView.vue`

- [ ] **Step 1: Replace `web/src/views/ChatView.vue`**

```vue
<!-- web/src/views/ChatView.vue -->
<script setup lang="ts">
import { ref, computed, nextTick, watch, onMounted } from 'vue'
import { useMessage } from 'naive-ui'
import { SendOutline, StopCircleOutline } from '@vicons/ionicons5'
import { useChatStore } from '../stores/chat'
import { listActions, type Action } from '../api/actions'

const msg   = useMessage()
const store = useChatStore()

const input      = ref('')
const messagesEl = ref<HTMLElement | null>(null)
const inputEl    = ref<HTMLTextAreaElement | null>(null)

// ── Chat actions ─────────────────────────────────────────────────────────────

const chatActions     = ref<Action[]>([])
const actionsLoading  = ref(false)

const actionOptions = computed(() =>
  chatActions.value.map(a => ({ label: a.name, value: String(a.id) }))
)

async function fetchChatActions() {
  actionsLoading.value = true
  try {
    const all = await listActions({ active: true })
    chatActions.value = all.filter(
      a => a.type === 'ollama' && a.config?.usecase === 'chat'
    )
    // If nothing is selected yet (or the stored ID is no longer valid), pick first.
    const ids = chatActions.value.map(a => String(a.id))
    if (!ids.includes(store.selectedActionId) && chatActions.value.length > 0) {
      store.selectAction(String(chatActions.value[0].id))
    }
  } catch (e: any) {
    msg.error('Failed to load chat actions: ' + e.message)
  } finally {
    actionsLoading.value = false
  }
}

onMounted(fetchChatActions)

// ── Scroll to bottom on new messages or streaming content ──────────────────

function scrollToBottom() {
  nextTick(() => {
    if (messagesEl.value) {
      messagesEl.value.scrollTop = messagesEl.value.scrollHeight
    }
  })
}
watch(() => store.messages[store.messages.length - 1]?.content, scrollToBottom)
watch(() => store.messages.length, scrollToBottom)

// ── Send ──────────────────────────────────────────────────────────────────

async function send() {
  const content = input.value.trim()
  if (!content || store.isStreaming) return
  input.value = ''
  try {
    await store.sendMessage(content)
  } catch (e: any) {
    msg.error(e.message ?? 'Chat error')
  }
  nextTick(() => inputEl.value?.focus())
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    send()
  }
}

function formatArgs(args: Record<string, unknown>): string {
  return JSON.stringify(args, null, 2)
}

function truncateResult(result: string): string {
  const max = 500
  return result.length > max ? result.slice(0, max) + '…' : result
}
</script>

<template>
  <div style="
    display: flex;
    flex-direction: column;
    height: calc(100vh - 48px - 48px);
    min-height: 400px;
  ">
    <!-- Header bar -->
    <div style="
      display: flex;
      align-items: center;
      gap: 12px;
      padding: 0 0 12px 0;
      flex-shrink: 0;
    ">
      <n-text style="font-size: 16px; font-weight: 600; color: #cdd6f4;">Chat</n-text>

      <!-- Action selector -->
      <n-select
        :value="store.selectedActionId"
        :options="actionOptions"
        :loading="actionsLoading"
        placeholder="Select a chat action…"
        size="small"
        style="width: 220px;"
        :disabled="store.isStreaming"
        @update:value="(v: string) => store.selectAction(v)"
      />

      <n-button
        v-if="store.messages.length > 0"
        size="small"
        quaternary
        style="margin-left: auto; color: #6c7086;"
        @click="store.clearSession()"
      >
        Clear chat
      </n-button>
    </div>

    <!-- No actions warning -->
    <n-alert
      v-if="!actionsLoading && chatActions.length === 0"
      type="warning"
      style="margin-bottom: 12px; flex-shrink: 0;"
    >
      No active chat actions found. Create an Ollama action with usecase "Chat" in the Pipelines page.
    </n-alert>

    <!-- Messages area -->
    <div
      ref="messagesEl"
      style="
        flex: 1;
        overflow-y: auto;
        display: flex;
        flex-direction: column;
        gap: 12px;
        padding-right: 4px;
        margin-bottom: 12px;
      "
    >
      <!-- Empty state -->
      <div
        v-if="store.messages.length === 0"
        style="
          flex: 1;
          display: flex;
          flex-direction: column;
          align-items: center;
          justify-content: center;
          gap: 8px;
          color: #6c7086;
        "
      >
        <n-text depth="3" style="font-size: 13px;">Ask anything about your iran-watcher data…</n-text>
        <n-text depth="3" style="font-size: 12px;">The assistant can query tweets, rules, pipelines, entities, and more.</n-text>
      </div>

      <!-- Message bubbles -->
      <div
        v-for="(m, i) in store.messages"
        :key="i"
        :style="{
          display: 'flex',
          justifyContent: m.role === 'user' ? 'flex-end' : 'flex-start',
        }"
      >
        <div :style="{
          maxWidth: '80%',
          display: 'flex',
          flexDirection: 'column',
          gap: '6px',
        }">
          <!-- Tool steps (assistant only) -->
          <template v-if="m.role === 'assistant'">
            <details
              v-for="(step, si) in m.toolSteps"
              :key="si"
              style="
                font-size: 12px;
                border: 1px solid rgba(255,255,255,0.08);
                border-radius: 8px;
                padding: 6px 10px;
                background: rgba(255,255,255,0.03);
                color: #a6adc8;
                cursor: pointer;
              "
            >
              <summary style="list-style: none; display: flex; align-items: center; gap: 6px; user-select: none;">
                <span style="color: #89b4fa;">⚙</span>
                <span>Called <code style="color: #cba6f7;">{{ step.name }}</code></span>
              </summary>
              <div style="margin-top: 8px; display: flex; flex-direction: column; gap: 6px;">
                <div>
                  <n-text depth="3" style="font-size: 11px; text-transform: uppercase; letter-spacing: 0.05em;">Arguments</n-text>
                  <pre style="margin: 4px 0 0; font-size: 11px; white-space: pre-wrap; word-break: break-all; color: #cdd6f4;">{{ formatArgs(step.args) }}</pre>
                </div>
                <div>
                  <n-text depth="3" style="font-size: 11px; text-transform: uppercase; letter-spacing: 0.05em;">Result</n-text>
                  <pre style="margin: 4px 0 0; font-size: 11px; white-space: pre-wrap; word-break: break-all; color: #cdd6f4;">{{ truncateResult(step.result) }}</pre>
                </div>
              </div>
            </details>
          </template>

          <!-- Message bubble -->
          <div
            v-if="m.role === 'user' || m.content || (store.isStreaming && i === store.messages.length - 1)"
            :style="{
              padding: '10px 14px',
              borderRadius: m.role === 'user' ? '14px 14px 4px 14px' : '14px 14px 14px 4px',
              background: m.role === 'user' ? 'rgba(137,180,250,0.18)' : 'rgba(255,255,255,0.05)',
              border: m.role === 'user' ? '1px solid rgba(137,180,250,0.25)' : '1px solid rgba(255,255,255,0.08)',
              fontSize: '13px',
              lineHeight: '1.6',
              color: '#cdd6f4',
              whiteSpace: 'pre-wrap',
              wordBreak: 'break-word',
            }"
          >
            <span v-if="m.content">{{ m.content }}</span>
            <!-- Spinner while waiting for first chunk (no content yet) -->
            <n-spin
              v-if="!m.content && m.role === 'assistant' && store.isStreaming && i === store.messages.length - 1"
              size="small"
            />
            <!-- Streaming cursor (shown after first token arrives) -->
            <span
              v-else-if="m.content && store.isStreaming && i === store.messages.length - 1 && m.role === 'assistant'"
              style="
                display: inline-block;
                width: 2px; height: 14px;
                background: #89b4fa;
                margin-left: 2px;
                vertical-align: text-bottom;
                animation: blink 1s step-end infinite;
              "
            />
          </div>
        </div>
      </div>
    </div>

    <!-- Input row -->
    <div style="
      flex-shrink: 0;
      display: flex;
      gap: 8px;
      align-items: flex-end;
      border-top: 1px solid rgba(255,255,255,0.08);
      padding-top: 12px;
    ">
      <n-input
        ref="inputEl"
        v-model:value="input"
        type="textarea"
        :autosize="{ minRows: 1, maxRows: 5 }"
        placeholder="Ask anything… (Enter to send, Shift+Enter for newline)"
        style="flex: 1;"
        :disabled="!store.selectedActionId || store.isStreaming"
        @keydown="onKeydown"
      />
      <n-tooltip v-if="!store.isStreaming" trigger="hover">
        <template #trigger>
          <n-button
            type="primary"
            :disabled="!input.trim() || !store.selectedActionId"
            @click="send"
          >
            <template #icon><n-icon><SendOutline /></n-icon></template>
          </n-button>
        </template>
        Send (Enter)
      </n-tooltip>
      <n-tooltip v-else trigger="hover">
        <template #trigger>
          <n-button type="error" @click="store.stopStreaming()">
            <template #icon><n-icon><StopCircleOutline /></n-icon></template>
          </n-button>
        </template>
        Stop generating
      </n-tooltip>
    </div>
  </div>
</template>

<style scoped>
@keyframes blink {
  0%, 100% { opacity: 1; }
  50%       { opacity: 0; }
}

details > summary::-webkit-details-marker { display: none; }
</style>
```

- [ ] **Step 2: Verify Docker build passes**

```bash
docker compose build iran-watcher 2>&1 | tail -30
```

Expected: build succeeds

- [ ] **Step 3: Commit**

```bash
git add web/src/views/ChatView.vue
git commit -m "feat: ChatView — action selector with per-action conversation history"
```

---

## Task 7: Update CLAUDE.md docs

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Update the Agentic chat section in CLAUDE.md**

Find the **Agentic chat** bullet in the Key Design Decisions section. Update the relevant portion:

The existing text about `BuildOllamaTools()` being called with a hardcoded whitelist and `chatToolWhitelist` should be replaced with:

```
**Agentic chat** — ... `BuildOllamaTools(allowed []string)` in `internal/mcpserver` converts `AllTools()` ToolDefs to Ollama JSON Schema format, filtered to `allowed` (empty = all tools); the `chatToolWhitelist` constant has been removed — per-action tool selection replaces it; each Chat action stores `allowed_tools: []string` in its config JSON; `POST /api/chat/stream` accepts an optional `action_id` string field to target a specific chat action; `GET /api/chat/tools` returns all tool name+description pairs for the action form picker; the Chat page renders an action selector (active Ollama actions with `usecase: "chat"`), persisting the selection in `localStorage`; per-action conversation history is stored as `conversations: Record<string, DisplayMessage[]>` in the chat Pinia store; ...
```

Update the precise text in-place to reflect:
1. `BuildOllamaTools(allowed []string)` replaces the old no-arg version
2. `chatToolWhitelist` removed
3. `GET /api/chat/tools` endpoint added
4. `POST /api/chat/stream` body gains optional `action_id`
5. Multiple chat actions are now allowed (uniqueness check removed for chat usecase)
6. Chat store uses per-action conversations keyed by action ID

- [ ] **Step 2: Commit docs**

```bash
git add CLAUDE.md
git commit -m "docs: update CLAUDE.md for specialized chat actions"
```

---

## Task 8: Final build verification

- [ ] **Step 1: Run all Go tests**

```bash
go test ./... 2>&1
```

Expected: all PASS

- [ ] **Step 2: Full Docker build**

```bash
docker compose build iran-watcher 2>&1 | tail -30
```

Expected: build succeeds with no errors

- [ ] **Step 3: Smoke test the new endpoint (optional — if Docker is running)**

```bash
# Start the stack
docker compose up -d

# Get a token
TOKEN=$(curl -s -X POST http://localhost:8080/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin"}' | python3 -c "import sys,json; print(json.load(sys.stdin)['token'])")

# List chat tools
curl -s http://localhost:8080/api/chat/tools \
  -H "Authorization: Bearer $TOKEN" | python3 -m json.tool | head -20
```

Expected: JSON array of tool objects with `name` and `description` fields
