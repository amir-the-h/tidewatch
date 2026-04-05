# Agentic Chat Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace tweet-context chat with an agentic assistant that autonomously calls iran-watcher MCP tools to answer questions.

**Architecture:** `OllamaPipeline.AgentStream` drives a tool-calling loop: send messages + all MCP tool definitions to Ollama (`stream:false`), execute any `tool_calls` via `mcpserver.Dispatcher`, write SSE events for each step, emit the final text answer as a `text_chunk` event. The frontend renders tool steps as collapsible `<details>` elements above the streamed reply.

**Tech Stack:** Go 1.25.5, Ollama `/api/chat` with `tools` array (no streaming until final turn), SSE, Vue 3 + Naive UI, Pinia

---

## File Map

| Action | File | Responsibility |
|--------|------|----------------|
| Modify | `internal/inference/pipeline.go` | Add `OllamaTool`, `OllamaToolFunc`, `ToolExecutor`; update `ChatPipeline` interface |
| Modify | `internal/inference/ollama.go` | Add package-level agent types; add `AgentStream`; remove `ChatStream`/`EstimateTokens`; update `OllamaChatSystem` |
| Create | `internal/inference/ollama_agent_test.go` | `AgentStream` unit tests |
| Create | `internal/mcpserver/ollama_tools.go` | `BuildOllamaTools()`, `toolIndex` map, `toolDefToOllama()` |
| Create | `internal/mcpserver/ollama_tools_test.go` | Tests for `BuildOllamaTools` |
| Modify | `internal/mcpserver/dispatcher.go` | Add `Execute()` method |
| Modify | `internal/api/handlers_chat.go` | Rewrite: remove estimate handler, rewrite stream handler |
| Modify | `internal/api/server.go` | Add `toolExec` field; update `NewServer` and `NewServerWithConcreteRules` |
| Modify | `internal/api/api_test.go` | Update `NewServer` call (add `nil` arg) |
| Modify | `cli/cmd/serve.go` | Construct `mcpserver.Dispatcher`; pass as `toolExec` |
| Modify | `web/src/api/chat.ts` | New event types; drop `tweet_ids`; new SSE parser |
| Modify | `web/src/stores/chat.ts` | Drop tweet context; add `toolSteps` to messages |
| Modify | `web/src/views/ChatView.vue` | Add tool step UI; remove token bar and session guard |
| Modify | `web/src/views/TweetsView.vue` | Remove bulk-select chat button |
| Modify | `web/src/views/TrackerView.vue` | Remove per-entity chat button |
| Modify | `CLAUDE.md` | Update "Tweet chat" key design decision |

---

### Task 1: Add `OllamaTool`, `OllamaToolFunc`, `ToolExecutor` to `internal/inference/pipeline.go`

**Files:**
- Modify: `internal/inference/pipeline.go`

No tests — pure type/interface additions. The existing `ChatPipeline` interface is **not changed yet** (that happens in Task 5 along with the handler rewrite).

- [ ] **Step 1: Add types and interface after the existing `ChatMessage` block (line 86)**

In `internal/inference/pipeline.go`, replace lines 88–93 with:

```go
// OllamaTool describes one function tool in Ollama's JSON Schema format.
type OllamaTool struct {
	Type     string         `json:"type"` // always "function"
	Function OllamaToolFunc `json:"function"`
}

// OllamaToolFunc is the function definition within an OllamaTool.
type OllamaToolFunc struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"` // JSON Schema object
}

// ToolExecutor executes a named tool with the given arguments and returns the
// result as a plain string. Used by AgentStream to execute MCP tool calls.
type ToolExecutor interface {
	Execute(ctx context.Context, name string, args map[string]any) (string, error)
}

// ChatPipeline is implemented by pipelines that support streaming chat.
type ChatPipeline interface {
	Pipeline
	ChatStream(ctx context.Context, tweetContext []string, messages []ChatMessage, w io.Writer) error
	EstimateTokens(ctx context.Context, texts []string) (estimated int, contextLimit int, err error)
}
```

- [ ] **Step 2: Verify the package compiles**

```bash
cd /mnt/storage/shared/projects/personal/iran-watcher
go build ./internal/inference/...
```

Expected: no output (success).

- [ ] **Step 3: Commit**

```bash
git add internal/inference/pipeline.go
git commit -m "feat: add OllamaTool, OllamaToolFunc, ToolExecutor types to inference"
```

---

### Task 2: `BuildOllamaTools()` with tests

**Files:**
- Create: `internal/mcpserver/ollama_tools.go`
- Create: `internal/mcpserver/ollama_tools_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/mcpserver/ollama_tools_test.go`:

```go
package mcpserver_test

import (
	"testing"

	"github.com/amir-the-h/iran-watcher/internal/mcpserver"
)

func TestBuildOllamaTools_Count(t *testing.T) {
	tools := mcpserver.BuildOllamaTools()
	if len(tools) == 0 {
		t.Fatal("expected non-empty tool list")
	}
}

func TestBuildOllamaTools_PathParamMapped(t *testing.T) {
	// Find tweets_get which has a required path param "id"
	tools := mcpserver.BuildOllamaTools()
	var found bool
	for _, tool := range tools {
		if tool.Function.Name != "tweets_get" {
			continue
		}
		found = true
		props, ok := tool.Function.Parameters["properties"].(map[string]interface{})
		if !ok {
			t.Fatal("parameters.properties is not a map")
		}
		idProp, ok := props["id"].(map[string]interface{})
		if !ok {
			t.Fatal("expected 'id' property")
		}
		if idProp["type"] != "integer" {
			t.Errorf("expected type=integer for id, got %v", idProp["type"])
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
	if !found {
		t.Fatal("tweets_get tool not found")
	}
}

func TestBuildOllamaTools_BodyMapped(t *testing.T) {
	// Find actions_create which has a body
	tools := mcpserver.BuildOllamaTools()
	for _, tool := range tools {
		if tool.Function.Name != "actions_create" {
			continue
		}
		props, ok := tool.Function.Parameters["properties"].(map[string]interface{})
		if !ok {
			t.Fatal("parameters.properties is not a map")
		}
		if _, ok := props["body"]; !ok {
			t.Error("expected 'body' property for actions_create")
		}
		return
	}
	t.Fatal("actions_create tool not found")
}

func TestBuildOllamaTools_TypeField(t *testing.T) {
	tools := mcpserver.BuildOllamaTools()
	for _, tool := range tools {
		if tool.Type != "function" {
			t.Errorf("tool %s: expected type=function, got %q", tool.Function.Name, tool.Type)
		}
	}
}
```

- [ ] **Step 2: Run test to confirm it fails**

```bash
cd /mnt/storage/shared/projects/personal/iran-watcher
go test ./internal/mcpserver/... -run TestBuildOllamaTools -v
```

Expected: FAIL — `BuildOllamaTools` undefined.

- [ ] **Step 3: Implement `BuildOllamaTools()`**

Create `internal/mcpserver/ollama_tools.go`:

```go
package mcpserver

import "github.com/amir-the-h/iran-watcher/internal/inference"

// BuildOllamaTools converts all registered ToolDefs to Ollama's JSON Schema
// tool format. Called at request time by the chat handler.
func BuildOllamaTools() []inference.OllamaTool {
	all := AllTools()
	out := make([]inference.OllamaTool, 0, len(all))
	for _, td := range all {
		out = append(out, toolDefToOllama(td))
	}
	return out
}

// toolIndex is a name→ToolDef lookup built once at package init.
var toolIndex map[string]ToolDef

func init() {
	all := AllTools()
	toolIndex = make(map[string]ToolDef, len(all))
	for _, td := range all {
		toolIndex[td.Name] = td
	}
}

func toolDefToOllama(td ToolDef) inference.OllamaTool {
	props := map[string]interface{}{}
	var required []string

	for _, p := range td.PathParams {
		props[p.Name] = paramToSchemaObj(p)
		if p.Required {
			required = append(required, p.Name)
		}
	}
	for _, p := range td.QueryParams {
		props[p.Name] = paramToSchemaObj(p)
		if p.Required {
			required = append(required, p.Name)
		}
	}
	if td.Body != nil {
		obj := map[string]interface{}{"type": "object"}
		if td.Body.Description != "" {
			obj["description"] = td.Body.Description
		}
		props["body"] = obj
	}

	params := map[string]interface{}{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		params["required"] = required
	}

	return inference.OllamaTool{
		Type: "function",
		Function: inference.OllamaToolFunc{
			Name:        td.Name,
			Description: td.Description,
			Parameters:  params,
		},
	}
}

func paramToSchemaObj(p ParamDef) map[string]interface{} {
	obj := map[string]interface{}{}
	switch p.Type {
	case "integer":
		obj["type"] = "integer"
	case "boolean":
		obj["type"] = "boolean"
	default:
		obj["type"] = "string"
	}
	if p.Description != "" {
		obj["description"] = p.Description
	}
	return obj
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
cd /mnt/storage/shared/projects/personal/iran-watcher
go test ./internal/mcpserver/... -run TestBuildOllamaTools -v
```

Expected: all four tests PASS.

- [ ] **Step 5: Run full test suite to check for regressions**

```bash
go test ./...
```

Expected: all pass (no failures).

- [ ] **Step 6: Commit**

```bash
git add internal/mcpserver/ollama_tools.go internal/mcpserver/ollama_tools_test.go
git commit -m "feat: add BuildOllamaTools() to convert ToolDefs to Ollama tool format"
```

---

### Task 3: `Dispatcher.Execute()` with tests

**Files:**
- Modify: `internal/mcpserver/dispatcher.go`
- Modify: `internal/mcpserver/dispatcher_test.go` (append new test)

- [ ] **Step 1: Write the failing test**

Append to `internal/mcpserver/dispatcher_test.go`:

```go
func TestDispatcher_Execute_CallsCorrectEndpoint(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"tweets":[]}`))
	}))
	defer srv.Close()

	d := mcpserver.NewDispatcher(srv.URL)
	result, err := d.Execute(context.Background(), "tweets_list", map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/tweets" {
		t.Errorf("expected /api/tweets, got %q", gotPath)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}
}

func TestDispatcher_Execute_UnknownTool(t *testing.T) {
	d := mcpserver.NewDispatcher("http://localhost:9999")
	_, err := d.Execute(context.Background(), "no_such_tool", map[string]any{})
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestDispatcher_Execute_WithPathParam(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":42}`))
	}))
	defer srv.Close()

	d := mcpserver.NewDispatcher(srv.URL)
	_, err := d.Execute(context.Background(), "tweets_get", map[string]any{"id": "abc123"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/tweets/abc123" {
		t.Errorf("expected /api/tweets/abc123, got %q", gotPath)
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
cd /mnt/storage/shared/projects/personal/iran-watcher
go test ./internal/mcpserver/... -run TestDispatcher_Execute -v
```

Expected: FAIL — `Execute` undefined on `*Dispatcher`.

- [ ] **Step 3: Implement `Dispatcher.Execute()`**

Add to the bottom of `internal/mcpserver/dispatcher.go`:

```go
// Execute looks up the ToolDef by name, builds a CallToolRequest from args,
// and calls Handle. Satisfies inference.ToolExecutor.
func (d *Dispatcher) Execute(ctx context.Context, name string, args map[string]any) (string, error) {
	td, ok := toolIndex[name]
	if !ok {
		return "", fmt.Errorf("dispatcher: unknown tool: %s", name)
	}
	var req mcp.CallToolRequest
	req.Params.Arguments = args
	result, err := d.Handle(ctx, td, req)
	if err != nil {
		return "", err
	}
	for _, c := range result.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			return tc.Text, nil
		}
	}
	if result.IsError {
		return "error from tool", nil
	}
	return "", nil
}
```

- [ ] **Step 4: Run tests to confirm they pass**

```bash
cd /mnt/storage/shared/projects/personal/iran-watcher
go test ./internal/mcpserver/... -run TestDispatcher_Execute -v
```

Expected: all three tests PASS.

- [ ] **Step 5: Verify all mcpserver tests still pass**

```bash
go test ./internal/mcpserver/...
```

Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/mcpserver/dispatcher.go internal/mcpserver/dispatcher_test.go
git commit -m "feat: add Dispatcher.Execute() satisfying inference.ToolExecutor interface"
```

---

### Task 4: `OllamaPipeline.AgentStream()` with tests

**Files:**
- Modify: `internal/inference/ollama.go`
- Create: `internal/inference/ollama_agent_test.go`

- [ ] **Step 1: Add package-level types for agent messages**

In `internal/inference/ollama.go`, add these types after the `ollamaEval` struct (after line 84):

```go
// ollamaAgentMsg is the message format for AgentStream requests/responses.
type ollamaAgentMsg struct {
	Role      string             `json:"role"`
	Content   string             `json:"content,omitempty"`
	ToolCalls []ollamaAgentTCall `json:"tool_calls,omitempty"`
}

// ollamaAgentTCall is a single tool call returned by Ollama.
type ollamaAgentTCall struct {
	Function ollamaAgentTCallFunc `json:"function"`
}

// ollamaAgentTCallFunc carries the name and arguments of a tool call.
type ollamaAgentTCallFunc struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}
```

- [ ] **Step 2: Write the failing test**

Create `internal/inference/ollama_agent_test.go`:

```go
package inference

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// mockToolExecutor records Execute calls and returns a fixed result.
type mockToolExecutor struct {
	calls  []string
	result string
}

func (m *mockToolExecutor) Execute(_ context.Context, name string, _ map[string]any) (string, error) {
	m.calls = append(m.calls, name)
	if m.result == "" {
		return `{"ok":true}`, nil
	}
	return m.result, nil
}

// newAgentTestServer creates a mock Ollama server for AgentStream tests.
// On the first /api/chat call it returns a tool call; on the second it returns
// plain text. /api/ps always reports the model as loaded.
func newAgentTestServer(t *testing.T, model string) *httptest.Server {
	t.Helper()
	var callCount int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/ps":
			// Report model as loaded so ensureLoaded skips warmUp.
			json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]any{{"model": model}},
			})
		case "/api/chat":
			n := atomic.AddInt32(&callCount, 1)
			if n == 1 {
				// First call: return a tool_call
				json.NewEncoder(w).Encode(map[string]any{
					"message": map[string]any{
						"role":    "assistant",
						"content": "",
						"tool_calls": []map[string]any{
							{
								"function": map[string]any{
									"name":      "tweets_list",
									"arguments": map[string]any{"limit": float64(5)},
								},
							},
						},
					},
					"done": true,
				})
			} else {
				// Second call: return plain text (no tool_calls)
				json.NewEncoder(w).Encode(map[string]any{
					"message": map[string]any{
						"role":    "assistant",
						"content": "There are 5 recent tweets.",
					},
					"done": true,
				})
			}
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestAgentStream_ToolCallThenText(t *testing.T) {
	const model = "test-model"
	ollamaSrv := newAgentTestServer(t, model)
	defer ollamaSrv.Close()

	pool := NewBackendPool()
	defer pool.Close()
	pool.Load([]BackendRecord{{ID: 1, Name: "test", URL: ollamaSrv.URL, Enabled: true}})

	p := NewOllamaPipelineFromConfig("chat", map[string]any{"model": model}, pool, nil, 5*time.Second)

	executor := &mockToolExecutor{}
	tools := []OllamaTool{
		{
			Type: "function",
			Function: OllamaToolFunc{
				Name:        "tweets_list",
				Description: "list tweets",
				Parameters:  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
			},
		},
	}

	messages := []ChatMessage{{Role: "user", Content: "How many recent tweets?"}}

	var buf bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := p.AgentStream(ctx, messages, tools, executor, &buf)
	if err != nil {
		t.Fatalf("AgentStream returned error: %v", err)
	}

	output := buf.String()

	// Must contain a tool_call event
	if !strings.Contains(output, `"type":"tool_call"`) {
		t.Errorf("expected tool_call event in output, got:\n%s", output)
	}
	// Must contain a tool_result event
	if !strings.Contains(output, `"type":"tool_result"`) {
		t.Errorf("expected tool_result event in output, got:\n%s", output)
	}
	// Must contain a text_chunk event with the final answer
	if !strings.Contains(output, `"type":"text_chunk"`) {
		t.Errorf("expected text_chunk event in output, got:\n%s", output)
	}
	if !strings.Contains(output, "5 recent tweets") {
		t.Errorf("expected final answer in text_chunk, got:\n%s", output)
	}
	// Must end with [DONE]
	if !strings.Contains(output, "data: [DONE]") {
		t.Errorf("expected [DONE] sentinel in output, got:\n%s", output)
	}

	// Executor must have been called with tweets_list
	if len(executor.calls) != 1 || executor.calls[0] != "tweets_list" {
		t.Errorf("expected executor called with tweets_list, got %v", executor.calls)
	}
}

func TestAgentStream_DirectTextNoTools(t *testing.T) {
	const model = "test-model"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/ps" {
			json.NewEncoder(w).Encode(map[string]any{"models": []map[string]any{{"model": model}}})
			return
		}
		// Always return plain text, no tool calls
		json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]any{"role": "assistant", "content": "Hello!"},
			"done":    true,
		})
	}))
	defer srv.Close()

	pool := NewBackendPool()
	defer pool.Close()
	pool.Load([]BackendRecord{{ID: 1, Name: "test", URL: srv.URL, Enabled: true}})

	p := NewOllamaPipelineFromConfig("chat", map[string]any{"model": model}, pool, nil, 5*time.Second)
	executor := &mockToolExecutor{}

	var buf bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := p.AgentStream(ctx, []ChatMessage{{Role: "user", Content: "hi"}}, nil, executor, &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, `"type":"text_chunk"`) {
		t.Errorf("expected text_chunk, got:\n%s", output)
	}
	if strings.Contains(output, `"type":"tool_call"`) {
		t.Errorf("unexpected tool_call event, got:\n%s", output)
	}
	if !strings.Contains(output, "data: [DONE]") {
		t.Errorf("expected [DONE], got:\n%s", output)
	}
	if len(executor.calls) != 0 {
		t.Errorf("expected no executor calls, got %v", executor.calls)
	}
}
```

- [ ] **Step 3: Run tests to confirm they fail**

```bash
cd /mnt/storage/shared/projects/personal/iran-watcher
go test ./internal/inference/... -run TestAgentStream -v
```

Expected: FAIL — `AgentStream` undefined.

- [ ] **Step 4: Update `OllamaChatSystem` constant**

In `internal/inference/ollama.go`, find `OllamaChatSystem` (around line 730) and update its value:

```go
// OllamaChatSystem is the default system prompt for the agentic chat assistant.
const OllamaChatSystem = `You are an intelligent assistant for the iran-watcher monitoring application. You have tools to query and manage all aspects of the system — tweets, rules, pipelines, actions, tracked entities, and more. Use your tools to look up information before answering. Be concise and accurate.`
```

- [ ] **Step 5: Implement `OllamaPipeline.AgentStream()`**

Add the following method to `internal/inference/ollama.go`, after the `EstimateTokens` method (after line 657):

```go
// AgentStream runs the agentic tool-calling loop. It sends messages + tool
// definitions to Ollama (stream:false), executes any tool_calls via executor,
// writes SSE events for each step, and emits the final text as a text_chunk
// event followed by [DONE].
//
// SSE event format:
//
//	data: {"type":"tool_call","name":"...","args":{...}}\n\n
//	data: {"type":"tool_result","name":"...","result":"..."}\n\n
//	data: {"type":"text_chunk","content":"..."}\n\n
//	data: [DONE]\n\n
//
// On Ollama backend errors the backend is marked unhealthy and a new one is
// acquired; all previously accumulated tool call history is preserved across
// retries.
func (p *OllamaPipeline) AgentStream(
	ctx context.Context,
	messages []ChatMessage,
	tools []OllamaTool,
	executor ToolExecutor,
	w io.Writer,
) error {
	type agentChatReq struct {
		Model    string           `json:"model"`
		Messages []ollamaAgentMsg `json:"messages"`
		Tools    []OllamaTool     `json:"tools,omitempty"`
		Stream   bool             `json:"stream"`
	}
	type agentChatResp struct {
		Message ollamaAgentMsg `json:"message"`
		Done    bool           `json:"done"`
	}

	flusher, canFlush := w.(interface{ Flush() })
	emit := func(event map[string]any) {
		data, _ := json.Marshal(event)
		fmt.Fprintf(w, "data: %s\n\n", data)
		if canFlush {
			flusher.Flush()
		}
	}

	// Build initial message history with system message prepended.
	systemContent := OllamaChatSystem
	if p.systemPrompt != "" {
		systemContent = p.systemPrompt
	}
	msgs := make([]ollamaAgentMsg, 0, len(messages)+1)
	msgs = append(msgs, ollamaAgentMsg{Role: "system", Content: systemContent})
	for _, m := range messages {
		msgs = append(msgs, ollamaAgentMsg{Role: m.Role, Content: m.Content})
	}

	for { // backend retry loop
		b, err := p.acquire(ctx)
		if err != nil {
			return fmt.Errorf("inference: ollama: agent stream: acquire: %w", err)
		}

		if err := p.ensureLoaded(b); err != nil {
			if isBackendError(err) {
				p.pool.MarkUnhealthy(b)
				continue
			}
			p.pool.Release(b)
			return fmt.Errorf("inference: ollama: agent stream: ensure loaded: %w", err)
		}

		var loopErr error
		for { // tool-calling loop on this backend
			reqBody, err := json.Marshal(agentChatReq{
				Model:    p.model,
				Messages: msgs,
				Tools:    tools,
				Stream:   false,
			})
			if err != nil {
				loopErr = fmt.Errorf("inference: ollama: agent: marshal: %w", err)
				break
			}

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.baseURL+"/api/chat", bytes.NewReader(reqBody))
			if err != nil {
				loopErr = fmt.Errorf("inference: ollama: agent: build request: %w", err)
				break
			}
			req.Header.Set("Content-Type", "application/json")

			resp, err := b.client.Do(req)
			if err != nil {
				loopErr = err
				break
			}

			if resp.StatusCode != http.StatusOK {
				raw, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				loopErr = &ollamaHTTPError{statusCode: resp.StatusCode, body: string(raw)}
				break
			}

			var chatResp agentChatResp
			if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
				resp.Body.Close()
				loopErr = fmt.Errorf("inference: ollama: agent: decode: %w", err)
				break
			}
			resp.Body.Close()

			if len(chatResp.Message.ToolCalls) == 0 {
				// No tool calls: emit the final text and finish.
				emit(map[string]any{"type": "text_chunk", "content": chatResp.Message.Content})
				fmt.Fprintf(w, "data: [DONE]\n\n")
				if canFlush {
					flusher.Flush()
				}
				p.pool.Release(b)
				return nil
			}

			// Append assistant message (with tool_calls) to history.
			msgs = append(msgs, chatResp.Message)

			// Execute each tool call.
			for _, tc := range chatResp.Message.ToolCalls {
				emit(map[string]any{
					"type": "tool_call",
					"name": tc.Function.Name,
					"args": tc.Function.Arguments,
				})

				result, execErr := executor.Execute(ctx, tc.Function.Name, tc.Function.Arguments)
				if execErr != nil {
					result = fmt.Sprintf("error: %v", execErr)
				}
				const maxResultBytes = 8 * 1024
				if len(result) > maxResultBytes {
					result = result[:maxResultBytes] + "[truncated]"
				}

				emit(map[string]any{
					"type":   "tool_result",
					"name":   tc.Function.Name,
					"result": result,
				})

				msgs = append(msgs, ollamaAgentMsg{Role: "tool", Content: result})
			}
			// Loop back: send updated conversation to Ollama.
		}

		// loopErr is set — decide whether to retry with a new backend.
		if loopErr != nil {
			if isBackendError(loopErr) {
				p.pool.MarkUnhealthy(b)
				continue
			}
			p.pool.Release(b)
			return loopErr
		}
	}
}
```

- [ ] **Step 6: Run tests to confirm they pass**

```bash
cd /mnt/storage/shared/projects/personal/iran-watcher
go test ./internal/inference/... -run TestAgentStream -v
```

Expected: both tests PASS.

- [ ] **Step 7: Run full test suite**

```bash
go test ./...
```

Expected: all pass.

- [ ] **Step 8: Commit**

```bash
git add internal/inference/ollama.go internal/inference/ollama_agent_test.go
git commit -m "feat: implement OllamaPipeline.AgentStream with tool-calling loop"
```

---

### Task 5: Atomic refactor — update interface, rewrite handler, wire server

This task replaces the old chat system. All changes must be made together and committed as one to keep the codebase compiling throughout.

**Files:**
- Modify: `internal/inference/pipeline.go` (update `ChatPipeline` interface)
- Modify: `internal/inference/ollama.go` (remove `ChatStream`, `EstimateTokens`)
- Modify: `internal/api/handlers_chat.go` (rewrite)
- Modify: `internal/api/server.go` (add `toolExec` field, remove estimate route, update constructors)
- Modify: `internal/api/api_test.go` (update `NewServer` call)
- Modify: `cli/cmd/serve.go` (construct dispatcher, pass as `toolExec`)

- [ ] **Step 1: Update `ChatPipeline` interface in `pipeline.go`**

In `internal/inference/pipeline.go`, replace the `ChatPipeline` block (lines 88–93, which currently still has `ChatStream`/`EstimateTokens`) with:

```go
// ChatPipeline is implemented by pipelines that support the agentic chat loop.
type ChatPipeline interface {
	Pipeline
	AgentStream(ctx context.Context, messages []ChatMessage, tools []OllamaTool, executor ToolExecutor, w io.Writer) error
}
```

- [ ] **Step 2: Remove `ChatStream` and `EstimateTokens` from `ollama.go`**

In `internal/inference/ollama.go`:
1. Delete the entire `ChatStream` method (lines 500–626 approximately, starting from the comment `// ChatStream sends a streaming multi-turn chat...` through the closing `}`)
2. Delete the entire `EstimateTokens` method (starting from `// EstimateTokens returns the approximate token count...` through its closing `}`)

Keep `getContextLimit` and `tokenize` only if they're referenced elsewhere (check with grep first):

```bash
grep -n "getContextLimit\|tokenize\b" internal/inference/ollama.go
```

If only referenced from `EstimateTokens`, delete them too. If referenced from other methods, keep them.

- [ ] **Step 3: Rewrite `internal/api/handlers_chat.go`**

Replace the entire file content with:

```go
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/mcpserver"
)

// handleChatStream runs the agentic chat loop and streams events as SSE.
//
// POST /api/chat/stream
// Body: { "messages": [{"role":"user","content":"..."}] }
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
	cp := s.findChatPipeline()
	if cp == nil {
		writeError(w, http.StatusUnprocessableEntity, "no active chat-capable model (requires an active Ollama action)")
		return
	}
	if s.toolExec == nil {
		writeError(w, http.StatusServiceUnavailable, "tool executor not configured")
		return
	}

	var req struct {
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

	tools := mcpserver.BuildOllamaTools()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	flusher, canFlush := w.(http.Flusher)
	fmt.Fprint(w, ": keep-alive\n\n")
	if canFlush {
		flusher.Flush()
	}

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
				fmt.Fprint(w, ": keep-alive\n\n")
				if canFlush {
					flusher.Flush()
				}
			}
		}
	}()

	if err := cp.AgentStream(ctx, req.Messages, tools, s.toolExec, w); err != nil {
		if !errors.Is(err, context.Canceled) {
			s.logger.Error("api: chat stream", "error", err)
		}
	}
}

// findChatPipeline prefers the pipeline named "digest"; falls back to any ChatPipeline.
// Uses the action cache (active actions only) so deactivated Ollama pipelines are excluded.
func (s *Server) findChatPipeline() inference.ChatPipeline {
	if s.actionCache == nil {
		return nil
	}
	var fallback inference.ChatPipeline
	for _, e := range s.actionCache.All() {
		cp, ok := e.Pipeline.(inference.ChatPipeline)
		if !ok {
			continue
		}
		if e.Pipeline.Name() == "digest" {
			return cp
		}
		if fallback == nil {
			fallback = cp
		}
	}
	return fallback
}
```

- [ ] **Step 4: Add `toolExec` to Server struct in `internal/api/server.go`**

Add `toolExec inference.ToolExecutor` as a field to the `Server` struct (after the existing `dispatcher *notifier.Dispatcher` field):

```go
toolExec inference.ToolExecutor
```

Then update the `NewServer` signature: add `toolExec inference.ToolExecutor` as the last parameter (after `modelsDir string`):

```go
func NewServer(
    bind string,
    // ... all existing params unchanged ...
    modelsDir string,
    toolExec inference.ToolExecutor,
) *Server {
```

And in the `NewServer` body, add to the `srv := &Server{...}` initialiser:

```go
toolExec: toolExec,
```

Also update `NewServerWithConcreteRules` to add `toolExec inference.ToolExecutor` as the last parameter and pass it through:

```go
func NewServerWithConcreteRules(
    // ... existing params ...
    modelsDir string,
    toolExec inference.ToolExecutor,
) *Server {
    return NewServer(bind, s, q, reg, cache, pipeReg, tc, rules, sc, tier, backendPool, ollamaFastWaitTimeout, jwtSecret, logger, dispatcher, ss, pool, sizer, lister, modelsDir, toolExec)
}
```

- [ ] **Step 5: Remove the estimate route from `server.go`**

In `internal/api/server.go`, find and delete:

```go
r.Post("/chat/estimate", srv.handleChatEstimate)
```

(Keep `r.Post("/chat/stream", srv.handleChatStream)`.)

- [ ] **Step 6: Update `NewServer` call in `internal/api/api_test.go`**

Find line 534:

```go
srv := NewServer("127.0.0.1:0", ms, mp, nil, nil, nil, nil, mr, nil, "basic", nil, 0, "", logger, nil, nil, nil, nil, nil, "")
```

Add `nil` at the end (for `toolExec`):

```go
srv := NewServer("127.0.0.1:0", ms, mp, nil, nil, nil, nil, mr, nil, "basic", nil, 0, "", logger, nil, nil, nil, nil, nil, "", nil)
```

- [ ] **Step 7: Update `cli/cmd/serve.go`**

First, add imports at the top:

```go
import (
    // ... existing imports ...
    "net"

    "github.com/amir-the-h/iran-watcher/internal/mcpserver"
)
```

Then, just before the `srv := api.NewServerWithConcreteRules(...)` call, add:

```go
// Build the tool executor: a Dispatcher that calls back into this API server.
// Tool calls in the chat loop are made as authenticated HTTP requests to localhost.
_, apiPort, _ := net.SplitHostPort(cfg.API.Bind)
if apiPort == "" {
    apiPort = "8080"
}
toolExec := mcpserver.NewDispatcherWithCredentials(
    "http://127.0.0.1:"+apiPort,
    cfg.Auth.AdminUsername,
    cfg.Auth.AdminPassword,
)
```

Then update the `NewServerWithConcreteRules` call to append `toolExec`:

```go
srv := api.NewServerWithConcreteRules(cfg.API.Bind, s, q, infReg, cache, pipeReg, twitterClient, rules, streamClient, cfg.X.Tier, pool, cfg.Ollama.FastWaitTimeout, jwtSecret, logger, dispatcher, searchScheduler, mgr, q, q, cfg.Inference.ModelsDir, toolExec)
```

- [ ] **Step 8: Verify the whole Go codebase compiles**

```bash
cd /mnt/storage/shared/projects/personal/iran-watcher
go build ./...
```

Expected: no output (success). Fix any compilation errors before proceeding.

- [ ] **Step 9: Run the full test suite**

```bash
go test ./...
```

Expected: all pass.

- [ ] **Step 10: Commit**

```bash
git add internal/inference/pipeline.go internal/inference/ollama.go \
        internal/api/handlers_chat.go internal/api/server.go \
        internal/api/api_test.go cli/cmd/serve.go
git commit -m "feat: replace tweet-context chat with agentic tool-calling chat"
```

---

### Task 6: Update frontend chat API (`web/src/api/chat.ts`)

**Files:**
- Modify: `web/src/api/chat.ts`

- [ ] **Step 1: Rewrite `web/src/api/chat.ts`**

Replace the entire file:

```typescript
import { apiFetch } from './client'

const apiBase: string =
  (window as Window & { __ENV__?: { API_BASE_URL?: string } }).__ENV__?.API_BASE_URL ||
  (import.meta.env.VITE_API_BASE_URL as string) ||
  ''

export interface ChatMessage {
  role: 'user' | 'assistant' | 'system'
  content: string
}

export type ChatEvent =
  | { type: 'tool_call'; name: string; args: Record<string, unknown> }
  | { type: 'tool_result'; name: string; result: string }
  | { type: 'text_chunk'; content: string }
  | { type: 'error'; message: string }

/**
 * Streams an agentic chat response via SSE (POST /api/chat/stream).
 * Returns an AbortController; call .abort() to cancel mid-stream.
 *
 * onEvent is called for each parsed ChatEvent.
 * onDone is called once the [DONE] sentinel is received.
 * onError is called on network or server errors (not on abort).
 */
export function streamChat(
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

      const res = await fetch(`${apiBase}/api/chat/stream`, {
        method: 'POST',
        headers,
        body: JSON.stringify({ messages }),
        signal: controller.signal,
      })

      if (res.status === 401) {
        localStorage.removeItem('iran_watcher_token')
        window.location.href = '/login'
        return
      }
      if (!res.ok) {
        const body = await res.json().catch(() => ({ error: res.statusText }))
        throw new Error((body as any).error ?? res.statusText)
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

- [ ] **Step 2: Verify TypeScript compiles**

```bash
cd /mnt/storage/shared/projects/personal/iran-watcher/web
npx tsc --noEmit
```

Expected: no errors on `chat.ts` (other unrelated errors are OK if they existed before).

- [ ] **Step 3: Commit**

```bash
git add web/src/api/chat.ts
git commit -m "feat: update chat API for agentic event stream (drop tweet_ids, add ChatEvent type)"
```

---

### Task 7: Update frontend chat store (`web/src/stores/chat.ts`)

**Files:**
- Modify: `web/src/stores/chat.ts`

- [ ] **Step 1: Rewrite `web/src/stores/chat.ts`**

Replace the entire file:

```typescript
import { defineStore } from 'pinia'
import { ref } from 'vue'
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

export const useChatStore = defineStore('chat', () => {
  const messages   = ref<DisplayMessage[]>([])
  const isStreaming = ref(false)

  let abortController: AbortController | null = null
  // Pending tool step: tool_call received but tool_result not yet
  let pendingStep: { name: string; args: Record<string, unknown> } | null = null

  /**
   * Append a user message and stream the assistant reply.
   * Resolves when the stream ends cleanly; rejects on error.
   */
  async function sendMessage(content: string): Promise<void> {
    if (isStreaming.value) return

    // Build history to send: only role+content (no toolSteps)
    const history: ChatMessage[] = [
      ...messages.value.map(m => ({ role: m.role, content: m.content })),
      { role: 'user' as const, content },
    ]

    messages.value.push({ role: 'user', content, toolSteps: [] })
    const assistantMsg: DisplayMessage = { role: 'assistant', content: '', toolSteps: [] }
    messages.value.push(assistantMsg)
    const assistantIdx = messages.value.length - 1

    isStreaming.value = true
    pendingStep = null

    return new Promise((resolve, reject) => {
      abortController = streamChat(
        history,
        (event: ChatEvent) => {
          const msg = messages.value[assistantIdx]
          if (event.type === 'tool_call') {
            pendingStep = { name: event.name, args: event.args }
          } else if (event.type === 'tool_result' && pendingStep) {
            msg.toolSteps = [
              ...msg.toolSteps,
              { name: pendingStep.name, args: pendingStep.args, result: event.result },
            ]
            messages.value[assistantIdx] = { ...msg }
            pendingStep = null
          } else if (event.type === 'text_chunk') {
            messages.value[assistantIdx] = {
              ...msg,
              content: msg.content + event.content,
            }
          }
          // error events: log but don't crash the UI
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

  /** Clear the conversation. */
  function clearSession() {
    stopStreaming()
    messages.value = []
  }

  return {
    messages,
    isStreaming,
    sendMessage,
    stopStreaming,
    clearSession,
  }
})
```

- [ ] **Step 2: Verify TypeScript compiles**

```bash
cd /mnt/storage/shared/projects/personal/iran-watcher/web
npx tsc --noEmit
```

Expected: no new errors introduced.

- [ ] **Step 3: Commit**

```bash
git add web/src/stores/chat.ts
git commit -m "feat: update chat store for agentic tool steps, remove tweet context"
```

---

### Task 8: Rewrite `ChatView.vue`

**Files:**
- Modify: `web/src/views/ChatView.vue`

- [ ] **Step 1: Rewrite `web/src/views/ChatView.vue`**

Replace the entire file content with:

```vue
<!-- web/src/views/ChatView.vue -->
<script setup lang="ts">
import { ref, nextTick, watch } from 'vue'
import { useMessage } from 'naive-ui'
import { SendOutline, StopCircleOutline } from '@vicons/ionicons5'
import { useChatStore } from '../stores/chat'
import type { ToolStep } from '../stores/chat'

const msg   = useMessage()
const store = useChatStore()

const input      = ref('')
const messagesEl = ref<HTMLElement | null>(null)
const inputEl    = ref<HTMLTextAreaElement | null>(null)

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
            <!-- Streaming cursor -->
            <span
              v-if="store.isStreaming && i === store.messages.length - 1 && m.role === 'assistant'"
              style="
                display: inline-block;
                width: 2px; height: 14px;
                background: #89b4fa;
                margin-left: 2px;
                vertical-align: text-bottom;
                animation: blink 1s step-end infinite;
              "
            />
            <!-- Spinner while waiting for first chunk (no content yet) -->
            <n-spin
              v-else-if="!m.content && m.role === 'assistant' && store.isStreaming && i === store.messages.length - 1"
              size="small"
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
        @keydown="onKeydown"
      />
      <n-tooltip v-if="!store.isStreaming" trigger="hover">
        <template #trigger>
          <n-button
            type="primary"
            :disabled="!input.trim()"
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

- [ ] **Step 2: Verify TypeScript in the component compiles**

```bash
cd /mnt/storage/shared/projects/personal/iran-watcher/web
npx tsc --noEmit
```

Expected: no new errors.

- [ ] **Step 3: Commit**

```bash
git add web/src/views/ChatView.vue
git commit -m "feat: rewrite ChatView with collapsible tool steps, remove tweet context UI"
```

---

### Task 9: Remove chat entry points from `TweetsView.vue` and `TrackerView.vue`

**Files:**
- Modify: `web/src/views/TweetsView.vue`
- Modify: `web/src/views/TrackerView.vue`

- [ ] **Step 1: Remove chat code from `TweetsView.vue`**

In `web/src/views/TweetsView.vue`, remove:

1. The import of `ChatbubblesOutline` from `@vicons/ionicons5` (remove it from the destructured import on that line; keep all other icons)
2. The import of `useChatStore` from `../stores/chat`
3. The import of `estimateTokens` from `../api/chat`
4. The line `const chatStore = useChatStore()`
5. The `const estimatingForChat = ref(false)` declaration
6. The entire `chatWithSelected()` function (lines 279–296 approx)
7. The chat button in the template:
```html
<n-tooltip trigger="hover">
  <template #trigger>
    <n-button size="small" :loading="estimatingForChat" @click="chatWithSelected">
      <template #icon><n-icon><ChatbubblesOutline /></n-icon></template>
    </n-button>
  </template>
  Chat with selected tweets
</n-tooltip>
```

- [ ] **Step 2: Remove chat code from `TrackerView.vue`**

In `web/src/views/TrackerView.vue`, remove:

1. The import of `ChatbubblesOutline` from `@vicons/ionicons5` (remove from destructured import; keep others)
2. The import of `useChatStore` from `../stores/chat`
3. The import of `estimateTokens` from `../api/chat`
4. The line `const chatStore = useChatStore()`
5. The `const chatLoadingId = ref<number | null>(null)` declaration
6. The entire `chatWithEntity()` function
7. The chat button in the entity list template:
```html
<n-button
  size="small"
  quaternary
  :loading="chatLoadingId === e.id"
  :disabled="(e.track_count ?? 0) === 0"
  @click="chatWithEntity(e)"
>
  <template #icon><n-icon><ChatbubblesOutline /></n-icon></template>
</n-button>
```
And its wrapping `<n-tooltip>...</n-tooltip>` block.

- [ ] **Step 3: Verify TypeScript compiles**

```bash
cd /mnt/storage/shared/projects/personal/iran-watcher/web
npx tsc --noEmit
```

Expected: no errors related to the removed code.

- [ ] **Step 4: Commit**

```bash
git add web/src/views/TweetsView.vue web/src/views/TrackerView.vue
git commit -m "feat: remove tweet-selection chat entry points from Tweets and Tracker views"
```

---

### Task 10: Update `CLAUDE.md`

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Update the "Tweet chat" key design decision**

Find the **Tweet chat** bullet point in the "Key Design Decisions" section of `CLAUDE.md`. It currently starts with:

> **Tweet chat** — interactive multi-turn LLM chat over a selected tweet set...

Replace that entire bullet with:

```
- **Agentic chat** — interactive multi-turn LLM assistant at `/chat`; the model has access to all iran-watcher MCP tools (100+ tools covering every REST endpoint) and autonomously calls them to answer questions; `OllamaPipeline.AgentStream` drives the tool-calling loop: send messages + `[]OllamaTool` definitions to Ollama (`stream:false`), execute any `tool_calls` via `ToolExecutor` (satisfied by `mcpserver.Dispatcher`), write SSE events (`tool_call`, `tool_result`, `text_chunk`, `[DONE]`), repeat until no tool_calls remain; `BuildOllamaTools()` in `internal/mcpserver` converts `AllTools()` ToolDefs to Ollama JSON Schema format; `Dispatcher.Execute()` looks up the ToolDef by name and calls the API server's own loopback URL with admin credentials; the `ToolExecutor` interface in `internal/inference` keeps `inference` free of `mcpserver` imports; the `Server` holds a `toolExec inference.ToolExecutor` field constructed in `cli/cmd/serve.go` via `mcpserver.NewDispatcherWithCredentials("http://127.0.0.1:{port}", adminUser, adminPass)`; the frontend renders tool steps as collapsible `<details>` elements (args + result) above the streamed text answer; no tweet pre-selection or token estimation; both `ChatStream`/`EstimateTokens` methods and `POST /api/chat/estimate` are removed
```

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: update CLAUDE.md — agentic chat replaces tweet-context chat"
```

---

### Task 11: Docker build verification

**Files:** None (verification only)

- [ ] **Step 1: Build the Docker image**

```bash
cd /mnt/storage/shared/projects/personal/iran-watcher
docker compose build --build-arg BUILDKIT_INLINE_CACHE=1 iran-watcher
```

Expected: build completes without errors. Fix any compilation or build errors found.

- [ ] **Step 2: Build the web image**

```bash
docker compose build --build-arg BUILDKIT_INLINE_CACHE=1 iran-watcher-web
```

Expected: build completes without errors.

- [ ] **Step 3: Run Go tests one final time**

```bash
go test ./...
```

Expected: all pass.

---

## Self-Review Checklist

**Spec coverage:**
- ✅ Backend agentic loop: Task 4 (`AgentStream`)
- ✅ `ToolExecutor` interface: Task 1
- ✅ `BuildOllamaTools()`: Task 2
- ✅ `Dispatcher.Execute()`: Task 3
- ✅ SSE event protocol (`tool_call`, `tool_result`, `text_chunk`, `[DONE]`): Task 4 + Task 5
- ✅ `POST /api/chat/estimate` removed: Task 5
- ✅ `tweet_ids` removed from request: Task 5 + Task 6
- ✅ Transparent tool call UI (collapsible steps): Task 8
- ✅ No tweet pre-selection guard: Task 8
- ✅ Remove entry points (TweetsView, TrackerView): Task 9
- ✅ `toolExec` wired in server: Task 5
- ✅ Admin credentials from config for Dispatcher: Task 5
- ✅ 8KB result truncation: Task 4
- ✅ No hard cap on tool iterations: Task 4
- ✅ CLAUDE.md update: Task 10
- ✅ Build verification: Task 11

**Type consistency:**
- `OllamaTool` / `OllamaToolFunc` defined in Task 1, used in Tasks 2, 4, 5, 6
- `ToolExecutor` defined in Task 1, implemented by `Dispatcher` in Task 3, used in Tasks 4, 5
- `ChatEvent` defined in Task 6, used in Tasks 7, 8
- `DisplayMessage` / `ToolStep` defined in Task 7, used in Task 8
- `ollamaAgentMsg` / `ollamaAgentTCall` / `ollamaAgentTCallFunc` defined in Task 4, used only in `AgentStream`
