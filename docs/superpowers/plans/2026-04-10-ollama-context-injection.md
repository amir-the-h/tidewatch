# Ollama Context Injection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow Ollama actions to receive selected upstream action outputs as a structured `<context>` block alongside (or instead of) the raw tweet text.

**Architecture:** Add `ContextSummary`/`ContextSummaryLoader` types to `internal/inference`; extend `OllamaPipeline` with `inputMode`, `contextActionIDs`, and `loadSummaries`; wire the loader from the store in `action_cache.go`; add two fields to the Ollama action form in the UI.

**Tech Stack:** Go (`internal/inference`, `internal/worker`), Vue 3 + Naive UI (`web/src/views/PipelinesView.vue`)

---

## File Map

| File | Change |
|---|---|
| `internal/inference/ollama.go` | Add types, struct fields, `buildContextBlock`, `renderContextValue`, modify `NewOllamaPipelineFromConfig` and `Run` |
| `internal/inference/ollama_context_test.go` | New — unit tests for `buildContextBlock` and `Run` context modes |
| `internal/inference/ollama_agent_test.go` | Update two call sites of `NewOllamaPipelineFromConfig` (add nil loader arg) |
| `internal/worker/action_cache.go` | Add `buildOllamaContextLoader`, wire it into the Ollama case |
| `web/src/views/PipelinesView.vue` | Add `formInputMode`/`formContextActionIds` refs, reset, load, save, template fields |

---

## Task 1: Add `ContextSummary`, `ContextSummaryLoader`, and `buildContextBlock` to `inference`

**Files:**
- Modify: `internal/inference/ollama.go`
- Create: `internal/inference/ollama_context_test.go`

- [ ] **Step 1: Write failing tests for `buildContextBlock`**

Create `internal/inference/ollama_context_test.go`:

```go
package inference

import (
	"strings"
	"testing"
)

func TestBuildContextBlock_TweetVarsOnly(t *testing.T) {
	tweetVars := map[string]any{
		"tweet_id":     "123",
		"tweet_url":    "https://x.com/user/status/123",
		"tweet_author": "user",
		"tweet_short":  "hello world",
	}
	got := buildContextBlock(tweetVars, nil)
	if !strings.HasPrefix(got, "<context>\n") {
		t.Errorf("expected <context> prefix, got: %s", got)
	}
	if !strings.HasSuffix(got, "</context>") {
		t.Errorf("expected </context> suffix, got: %s", got)
	}
	for _, want := range []string{"tweet_id: 123", "tweet_url: https://x.com/user/status/123", "tweet_author: user", "tweet_short: hello world"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in output:\n%s", want, got)
		}
	}
}

func TestBuildContextBlock_ScalarMetadata(t *testing.T) {
	summaries := []ContextSummary{
		{ActionID: 1, ActionName: "Classifier", Metadata: map[string]any{
			"top_label":  "violence",
			"confidence": 0.97,
			"flagged":    true,
		}},
	}
	got := buildContextBlock(map[string]any{}, summaries)
	if !strings.Contains(got, "Classifier:") {
		t.Errorf("expected action section header")
	}
	if !strings.Contains(got, "  top_label: violence") {
		t.Errorf("expected top_label line")
	}
	if !strings.Contains(got, "  confidence: 0.97") {
		t.Errorf("expected confidence line")
	}
	if !strings.Contains(got, "  flagged: true") {
		t.Errorf("expected flagged line")
	}
}

func TestBuildContextBlock_NestedObject(t *testing.T) {
	summaries := []ContextSummary{
		{ActionID: 2, ActionName: "EmbedGen", Metadata: map[string]any{
			"discord_embed": map[string]any{"color": float64(123), "title": "Test"},
		}},
	}
	got := buildContextBlock(map[string]any{}, summaries)
	if !strings.Contains(got, "discord_embed: {") {
		t.Errorf("expected JSON-serialised object, got:\n%s", got)
	}
}

func TestBuildContextBlock_ArrayValue(t *testing.T) {
	summaries := []ContextSummary{
		{ActionID: 3, ActionName: "NER", Metadata: map[string]any{
			"entities": []any{"Iran", "Khamenei"},
		}},
	}
	got := buildContextBlock(map[string]any{}, summaries)
	if !strings.Contains(got, `entities: ["Iran","Khamenei"]`) {
		t.Errorf("expected JSON array, got:\n%s", got)
	}
}

func TestBuildContextBlock_EmptyMetadataOmitted(t *testing.T) {
	summaries := []ContextSummary{
		{ActionID: 4, ActionName: "NoOutput", Metadata: map[string]any{}},
	}
	got := buildContextBlock(map[string]any{}, summaries)
	if strings.Contains(got, "NoOutput") {
		t.Errorf("action with empty metadata should be omitted, got:\n%s", got)
	}
}

func TestBuildContextBlock_OrderPreserved(t *testing.T) {
	summaries := []ContextSummary{
		{ActionID: 1, ActionName: "First", Metadata: map[string]any{"x": "1"}},
		{ActionID: 2, ActionName: "Second", Metadata: map[string]any{"y": "2"}},
	}
	got := buildContextBlock(map[string]any{}, summaries)
	firstIdx := strings.Index(got, "First:")
	secondIdx := strings.Index(got, "Second:")
	if firstIdx == -1 || secondIdx == -1 || firstIdx > secondIdx {
		t.Errorf("expected First before Second, got:\n%s", got)
	}
}
```

- [ ] **Step 2: Run to confirm tests fail**

```bash
cd /mnt/shared/projects/personal/iran-watcher
go test ./internal/inference/ -run TestBuildContextBlock -v 2>&1 | head -20
```

Expected: compile error — `ContextSummary`, `ContextSummaryLoader`, `buildContextBlock` undefined.

- [ ] **Step 3: Add types and `buildContextBlock` to `ollama.go`**

At the top of `internal/inference/ollama.go`, after the existing imports (which already include `encoding/json`, `fmt`, `sort`, `strings`), add these declarations after the `const` block and before `OllamaPipeline`:

```go
// ContextSummary holds the metadata output of one upstream action for use
// in the LLM <context> block.
type ContextSummary struct {
	ActionID   int64
	ActionName string
	Metadata   map[string]any
}

// ContextSummaryLoader fetches ContextSummary records for a tweet, filtered
// to the provided action IDs. Injected at construction time; nil = disabled.
type ContextSummaryLoader func(ctx context.Context, tweetID string, actionIDs []int64) ([]ContextSummary, error)
```

Update the import block in `ollama.go` to add `"sort"` and `"strconv"` (they are not currently present):

```go
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/mcpserver"
)
```

Then add at the end of `ollama.go` (before the closing of the file):

```go
// buildContextBlock formats a <context>...</context> string from built-in
// tweet vars and a list of upstream action summaries. Tweet vars are rendered
// first in a fixed order; action sections follow in the order supplied.
// Actions with empty Metadata are omitted. Complex values (arrays, objects)
// are JSON-serialised on a single line; scalars are rendered as plain text.
func buildContextBlock(tweetVars map[string]any, summaries []ContextSummary) string {
	var sb strings.Builder
	sb.WriteString("<context>\n")

	for _, key := range []string{"tweet_id", "tweet_url", "tweet_author", "tweet_short"} {
		if v, ok := tweetVars[key]; ok {
			sb.WriteString(key + ": " + renderContextValue(v) + "\n")
		}
	}

	for _, s := range summaries {
		if len(s.Metadata) == 0 {
			continue
		}
		sb.WriteString("\n" + s.ActionName + ":\n")
		keys := make([]string, 0, len(s.Metadata))
		for k := range s.Metadata {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			sb.WriteString("  " + k + ": " + renderContextValue(s.Metadata[k]) + "\n")
		}
	}

	sb.WriteString("</context>")
	return sb.String()
}

// renderContextValue converts a metadata value to a display string.
// Strings, numbers, and bools are rendered as plain text.
// Arrays and objects (including nested) are JSON-serialised as compact JSON.
func renderContextValue(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case bool:
		if val {
			return "true"
		}
		return "false"
	default:
		b, err := json.Marshal(val)
		if err != nil {
			return fmt.Sprintf("%v", val)
		}
		return string(b)
	}
}
```

Also add `"sort"` and `"strconv"` to the imports in `ollama.go` if not already present. Check with:

```bash
grep -n '"sort"\|"strconv"' /mnt/shared/projects/personal/iran-watcher/internal/inference/ollama.go
```

If missing, add them to the import block.

- [ ] **Step 4: Run tests to confirm they pass**

```bash
cd /mnt/shared/projects/personal/iran-watcher
go test ./internal/inference/ -run TestBuildContextBlock -v
```

Expected: all 6 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/inference/ollama.go internal/inference/ollama_context_test.go
git commit -m "feat(inference): add ContextSummary types and buildContextBlock"
```

---

## Task 2: Extend `OllamaPipeline` struct and modify `Run`

**Files:**
- Modify: `internal/inference/ollama.go`
- Modify: `internal/inference/ollama_context_test.go`
- Modify: `internal/inference/ollama_agent_test.go`

- [ ] **Step 1: Write failing tests for `Run` context modes**

Append to `internal/inference/ollama_context_test.go`:

```go
func TestOllamaPipelineRun_TextOnly_NoLoader(t *testing.T) {
	// text_only with nil loader — must pass raw tweet text unchanged.
	var sentText string
	p := &OllamaPipeline{
		name:      "test",
		inputMode: "text_only",
		sendChatFn: func(_ context.Context, text string) (string, error) {
			sentText = text
			return `{"result":"ok"}`, nil
		},
	}
	_, err := p.Run(context.Background(), PipelineInput{
		Text:    "raw tweet",
		TweetID: "t1",
		Context: map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sentText != "raw tweet" {
		t.Errorf("expected raw tweet text, got: %s", sentText)
	}
}

func TestOllamaPipelineRun_ContextOnly(t *testing.T) {
	loader := func(_ context.Context, tweetID string, actionIDs []int64) ([]ContextSummary, error) {
		return []ContextSummary{
			{ActionID: 1, ActionName: "Clf", Metadata: map[string]any{"top_label": "violence"}},
		}, nil
	}
	var sentText string
	p := &OllamaPipeline{
		name:             "test",
		inputMode:        "context_only",
		contextActionIDs: []int64{1},
		loadSummaries:    loader,
		sendChatFn: func(_ context.Context, text string) (string, error) {
			sentText = text
			return `{"result":"ok"}`, nil
		},
	}
	_, err := p.Run(context.Background(), PipelineInput{
		Text:    "raw tweet",
		TweetID: "t1",
		Context: map[string]any{"tweet_id": "t1", "tweet_url": "https://x.com/u/status/t1", "tweet_author": "u", "tweet_short": "raw tweet"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(sentText, "raw tweet\n") {
		t.Errorf("context_only should not include raw tweet text, got:\n%s", sentText)
	}
	if !strings.Contains(sentText, "<context>") {
		t.Errorf("expected <context> block, got:\n%s", sentText)
	}
	if !strings.Contains(sentText, "top_label: violence") {
		t.Errorf("expected top_label in context, got:\n%s", sentText)
	}
}

func TestOllamaPipelineRun_TextAndContext(t *testing.T) {
	loader := func(_ context.Context, tweetID string, actionIDs []int64) ([]ContextSummary, error) {
		return []ContextSummary{
			{ActionID: 2, ActionName: "NER", Metadata: map[string]any{"entities": []any{"Iran"}}},
		}, nil
	}
	var sentText string
	p := &OllamaPipeline{
		name:             "test",
		inputMode:        "text_and_context",
		contextActionIDs: []int64{2},
		loadSummaries:    loader,
		sendChatFn: func(_ context.Context, text string) (string, error) {
			sentText = text
			return `{"result":"ok"}`, nil
		},
	}
	_, err := p.Run(context.Background(), PipelineInput{
		Text:    "raw tweet",
		TweetID: "t1",
		Context: map[string]any{"tweet_id": "t1", "tweet_url": "https://x.com/u/status/t1", "tweet_author": "u", "tweet_short": "raw tweet"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(sentText, "raw tweet") {
		t.Errorf("expected raw tweet text first, got:\n%s", sentText)
	}
	if !strings.Contains(sentText, "<context>") {
		t.Errorf("expected <context> block, got:\n%s", sentText)
	}
}

func TestOllamaPipelineRun_LoaderError_FallsBackToText(t *testing.T) {
	loader := func(_ context.Context, tweetID string, actionIDs []int64) ([]ContextSummary, error) {
		return nil, fmt.Errorf("db error")
	}
	var sentText string
	p := &OllamaPipeline{
		name:             "test",
		inputMode:        "text_and_context",
		contextActionIDs: []int64{1},
		loadSummaries:    loader,
		sendChatFn: func(_ context.Context, text string) (string, error) {
			sentText = text
			return `{"result":"ok"}`, nil
		},
	}
	_, err := p.Run(context.Background(), PipelineInput{
		Text:    "raw tweet",
		TweetID: "t1",
		Context: map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sentText != "raw tweet" {
		t.Errorf("expected fallback to raw tweet, got: %s", sentText)
	}
}
```

- [ ] **Step 2: Run to confirm new tests fail**

```bash
cd /mnt/shared/projects/personal/iran-watcher
go test ./internal/inference/ -run TestOllamaPipelineRun_ -v 2>&1 | head -30
```

Expected: compile errors — `sendChatFn` field and new struct fields undefined.

- [ ] **Step 3: Add `sendChatFn` hook and new fields to `OllamaPipeline` struct**

In `internal/inference/ollama.go`, update the `OllamaPipeline` struct. Find the current struct definition (around line 111) and add the new fields:

```go
type OllamaPipeline struct {
	name            string
	model           string
	systemPrompt    string
	pool            *BackendPool
	assignedIDs     []int64
	fastWaitTimeout time.Duration
	// Context injection
	inputMode        string               // "text_only" | "context_only" | "text_and_context"
	contextActionIDs []int64
	loadSummaries    ContextSummaryLoader // nil = context disabled
	// Testability hook: if non-nil, overrides the real sendChat HTTP call.
	sendChatFn func(ctx context.Context, text string) (string, error)
}
```

- [ ] **Step 4: Update `NewOllamaPipelineFromConfig` to accept the loader and read new config keys**

Replace the existing `NewOllamaPipelineFromConfig` function (around line 128) with:

```go
func NewOllamaPipelineFromConfig(name string, cfg map[string]any, pool *BackendPool, assignedIDs []int64, fastWaitTimeout time.Duration, loadSummaries ContextSummaryLoader) *OllamaPipeline {
	systemPrompt, _ := cfg["system_prompt"].(string)
	if fastWaitTimeout == 0 {
		fastWaitTimeout = 5 * time.Second
	}
	model, _ := cfg["model"].(string)
	inputMode, _ := cfg["input_mode"].(string)
	if inputMode == "" {
		inputMode = "text_only"
	}
	var contextActionIDs []int64
	if ids, ok := cfg["context_action_ids"].([]any); ok {
		for _, id := range ids {
			if fid, ok := id.(float64); ok {
				contextActionIDs = append(contextActionIDs, int64(fid))
			}
		}
	}
	return &OllamaPipeline{
		name:             name,
		model:            model,
		systemPrompt:     systemPrompt,
		pool:             pool,
		assignedIDs:      assignedIDs,
		fastWaitTimeout:  fastWaitTimeout,
		inputMode:        inputMode,
		contextActionIDs: contextActionIDs,
		loadSummaries:    loadSummaries,
	}
}
```

- [ ] **Step 5: Update `runRaw` to use `sendChatFn` hook when set**

The testability hook must be wired into `runRaw` before the `Run` tests can pass. Find the existing `runRaw` function (around line 227) and replace it:

```go
func (p *OllamaPipeline) runRaw(ctx context.Context, text []byte) (json.RawMessage, error) {
	if isTruncated(string(text)) {
		return json.RawMessage(`{"truncated":true}`), nil
	}
	var content string
	var err error
	if p.sendChatFn != nil {
		content, err = p.sendChatFn(ctx, string(text))
	} else {
		content, err = p.sendChat(ctx, string(text))
	}
	if err != nil {
		return nil, err
	}
	return json.RawMessage(content), nil
}
```

- [ ] **Step 6: Update `Run` to build the user message based on `inputMode`**

Replace the existing `Run` method (around line 238):

```go
func (p *OllamaPipeline) Run(ctx context.Context, input PipelineInput) (map[string]any, error) {
	userText := input.Text

	if p.inputMode != "text_only" && p.loadSummaries != nil && len(p.contextActionIDs) > 0 {
		summaries, err := p.loadSummaries(ctx, input.TweetID, p.contextActionIDs)
		if err == nil {
			tweetVars := map[string]any{
				"tweet_id":     input.Context["tweet_id"],
				"tweet_url":    input.Context["tweet_url"],
				"tweet_author": input.Context["tweet_author"],
				"tweet_short":  input.Context["tweet_short"],
			}
			contextBlock := buildContextBlock(tweetVars, summaries)
			switch p.inputMode {
			case "context_only":
				userText = contextBlock
			case "text_and_context":
				userText = input.Text + "\n\n" + contextBlock
			}
		}
		// On loader error: fall through with userText = input.Text (text_only fallback)
	}

	raw, err := p.runRaw(ctx, []byte(userText))
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{"raw": string(raw)}, nil
	}
	return out, nil
}
```

- [ ] **Step 7: Fix the two broken call sites in `ollama_agent_test.go`**

In `internal/inference/ollama_agent_test.go`, both calls to `NewOllamaPipelineFromConfig` need a `nil` loader argument. Find them (around lines 93 and 166) and update:

```go
// line ~93
p := NewOllamaPipelineFromConfig("chat", map[string]any{"model": model}, pool, nil, 5*time.Second, nil)

// line ~166
p := NewOllamaPipelineFromConfig("chat", map[string]any{"model": model}, pool, nil, 5*time.Second, nil)
```

- [ ] **Step 8: Run all inference tests**

```bash
cd /mnt/shared/projects/personal/iran-watcher
go test ./internal/inference/ -v -run "TestBuildContextBlock|TestOllamaPipelineRun_" 2>&1 | tail -20
```

Expected: all tests PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/inference/ollama.go internal/inference/ollama_context_test.go internal/inference/ollama_agent_test.go
git commit -m "feat(inference): extend OllamaPipeline with context injection modes"
```

---

## Task 3: Wire the `ContextSummaryLoader` in `action_cache.go`

**Files:**
- Modify: `internal/worker/action_cache.go`

- [ ] **Step 1: Add `buildOllamaContextLoader` helper and update the Ollama case**

In `internal/worker/action_cache.go`, add this function after `buildNotificationPipeline` (around line 284):

```go
// buildOllamaContextLoader returns a ContextSummaryLoader that fetches per-action
// metadata from the store, filtered to the requested action IDs in the supplied order.
func buildOllamaContextLoader(st store.Store) inference.ContextSummaryLoader {
	return func(ctx context.Context, tweetID string, actionIDs []int64) ([]inference.ContextSummary, error) {
		all, err := st.ListTweetActionSummaries(ctx, tweetID)
		if err != nil {
			return nil, err
		}
		byID := make(map[int64]inference.ContextSummary, len(all))
		for _, s := range all {
			byID[s.ActionID] = inference.ContextSummary{
				ActionID:   s.ActionID,
				ActionName: s.ActionName,
				Metadata:   s.Metadata,
			}
		}
		out := make([]inference.ContextSummary, 0, len(actionIDs))
		for _, id := range actionIDs {
			if s, ok := byID[id]; ok {
				out = append(out, s)
			}
		}
		return out, nil
	}
}
```

Then update the Ollama case in `buildPipeline` (around line 164):

```go
case model.ActionTypeOllama:
	// Always build fresh from DB config so that updates to model name,
	// system_prompt, or backend assignments take effect without a restart.
	assignedIDs := actionBackendIDs[a.ID] // nil → use full pool
	p = inference.NewOllamaPipelineFromConfig(a.Name, cfg, c.pool, assignedIDs, 0, buildOllamaContextLoader(c.store))
	c.infReg.Register(p)
```

- [ ] **Step 2: Build to verify no compile errors**

```bash
cd /mnt/shared/projects/personal/iran-watcher
go build ./internal/worker/ 2>&1
```

Expected: no output (clean build).

- [ ] **Step 3: Run worker tests**

```bash
cd /mnt/shared/projects/personal/iran-watcher
go test ./internal/worker/ -v 2>&1 | tail -20
```

Expected: all tests PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/worker/action_cache.go
git commit -m "feat(worker): wire ContextSummaryLoader into Ollama pipeline construction"
```

---

## Task 4: Add Input Mode and Context Actions fields to the UI

**Files:**
- Modify: `web/src/views/PipelinesView.vue`

- [ ] **Step 1: Add the two new refs**

In the refs block (around line 64), after `formAllowedTools`:

```ts
const formInputMode         = ref<string>('text_only')
const formContextActionIds  = ref<number[]>([])
```

- [ ] **Step 2: Reset the new refs in `resetActionForm`**

In `resetActionForm` (around line 233), after the line `formAllowedTools.value = []`:

```ts
formInputMode.value = 'text_only'
formContextActionIds.value = []
```

- [ ] **Step 3: Load the new fields when opening edit form**

In the edit form load block (around line 278), after the line `formAllowedTools.value = ...`:

```ts
formInputMode.value = row.config?.input_mode ?? 'text_only'
formContextActionIds.value = Array.isArray(row.config?.context_action_ids)
  ? [...row.config.context_action_ids]
  : []
```

- [ ] **Step 4: Save the new fields when submitting**

In the ollama config build block (around line 342), after the `allowed_tools` block:

```ts
if (formInputMode.value && formInputMode.value !== 'text_only') {
  config.input_mode = formInputMode.value
  if (formContextActionIds.value.length > 0) {
    config.context_action_ids = formContextActionIds.value
  }
}
```

- [ ] **Step 5: Add form fields to the template**

In `PipelinesView.vue`, find the Ollama section in the template (around line 1067, just after the system prompt `</n-form-item>` and before the `<!-- Chat tools selector -->` block). Insert:

```html
      <!-- Context injection (custom usecase only) -->
      <template v-if="formUsecase !== 'chat'">
        <n-form-item label="Input Mode">
          <n-select
            v-model:value="formInputMode"
            :options="[
              { label: 'Text only (default)',  value: 'text_only'        },
              { label: 'Context only',         value: 'context_only'     },
              { label: 'Text + Context',       value: 'text_and_context' },
            ]"
          />
        </n-form-item>
        <n-form-item v-if="formInputMode !== 'text_only'" label="Context Actions">
          <n-select
            v-model:value="formContextActionIds"
            multiple
            filterable
            clearable
            :options="actionsData
              .filter(a => a.id !== editingActionId && a.type === 'ollama' || a.type === 'onnx' || a.type === 'internal')
              .map(a => ({ label: a.name, value: a.id }))"
            placeholder="Select upstream actions whose outputs to include"
          />
        </n-form-item>
        <n-text v-if="formInputMode !== 'text_only'" depth="3" style="font-size: 12px; margin-top: -8px; display: block">
          The selected actions' latest outputs for each tweet will be passed to the LLM in a &lt;context&gt; block.
          Built-in tweet variables (tweet_id, tweet_url, tweet_author, tweet_short) are always included.
        </n-text>
      </template>
```

Note on the filter expression — the intent is: show all actions except the one being edited. The expression above has a precedence issue; replace with a computed option:

Instead of an inline filter, add a computed property after the existing `chatToolOptions` computed (search for `chatToolOptions` around line 190):

```ts
const contextActionOptions = computed(() =>
  actionsData.value
    .filter(a => a.id !== editingActionId.value)
    .map(a => ({ label: `${a.name} (${a.type})`, value: a.id }))
)
```

Then use `:options="contextActionOptions"` in the `<n-select>` above instead of the inline filter.

- [ ] **Step 6: Build the frontend to verify no errors**

```bash
cd /mnt/shared/projects/personal/iran-watcher
docker compose build --build-arg BUILDKIT_INLINE_CACHE=1 iran-watcher 2>&1 | tail -20
```

Expected: build completes without errors.

- [ ] **Step 7: Commit**

```bash
git add web/src/views/PipelinesView.vue
git commit -m "feat(ui): add input mode and context actions fields to Ollama action form"
```

---

## Task 5: Full build verification

- [ ] **Step 1: Run all Go tests**

```bash
cd /mnt/shared/projects/personal/iran-watcher
go test ./internal/inference/ ./internal/worker/ ./internal/pipeline/ -v 2>&1 | grep -E "^(ok|FAIL|---)" | head -30
```

Expected: all packages report `ok`.

- [ ] **Step 2: Build the Docker image**

```bash
cd /mnt/shared/projects/personal/iran-watcher
docker compose build --build-arg BUILDKIT_INLINE_CACHE=1 iran-watcher 2>&1 | tail -10
```

Expected: `Successfully built` or `writing image`.

- [ ] **Step 3: Commit if anything was left uncommitted**

```bash
git status
```

If clean, nothing to do. If there are stragglers:

```bash
git add -p
git commit -m "chore: final cleanup for ollama context injection"
```
