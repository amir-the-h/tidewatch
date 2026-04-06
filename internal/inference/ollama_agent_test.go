package inference

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/mcpserver"
)

// mockToolExecutor records Execute calls and returns a fixed result.
type mockToolExecutor struct {
	mu     sync.Mutex
	calls  []string
	result string
}

func (m *mockToolExecutor) Execute(_ context.Context, name string, _ map[string]any) (string, error) {
	m.mu.Lock()
	m.calls = append(m.calls, name)
	m.mu.Unlock()
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
	tools := []mcpserver.OllamaTool{
		{
			Type: "function",
			Function: mcpserver.OllamaToolFunc{
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

// TestAgentStream_EmbeddedToolCallInContent simulates Ollama < 0.3 behaviour
// where Qwen2.5 tool calls appear as raw text in the content field instead of
// in the tool_calls array. The embedded extractor should parse them and drive
// the normal tool-calling loop.
func TestAgentStream_EmbeddedToolCallInContent(t *testing.T) {
	const model = "test-model"
	// Simulate what Ollama < 0.3 emits: garbled prefix + JSON + </tool_call>.
	embeddedContent := "\xef\xbf\xbd" + `{"name":"tweets_list","arguments":{"limit":3}}` + "</tool_call>"

	var callCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/ps" {
			json.NewEncoder(w).Encode(map[string]any{"models": []map[string]any{{"model": model}}})
			return
		}
		n := atomic.AddInt32(&callCount, 1)
		if n == 1 {
			// First call: tool call embedded in content, no tool_calls field.
			json.NewEncoder(w).Encode(map[string]any{
				"message": map[string]any{
					"role":    "assistant",
					"content": embeddedContent,
				},
				"done": true,
			})
		} else {
			// Second call: plain text answer.
			json.NewEncoder(w).Encode(map[string]any{
				"message": map[string]any{
					"role":    "assistant",
					"content": "Here are 3 tweets.",
				},
				"done": true,
			})
		}
	}))
	defer srv.Close()

	pool := NewBackendPool()
	defer pool.Close()
	pool.Load([]BackendRecord{{ID: 1, Name: "test", URL: srv.URL, Enabled: true}})

	p := NewOllamaPipelineFromConfig("chat", map[string]any{"model": model}, pool, nil, 5*time.Second)
	executor := &mockToolExecutor{result: `[{"id":1},{"id":2},{"id":3}]`}

	var buf bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := p.AgentStream(ctx, []ChatMessage{{Role: "user", Content: "list 3 tweets"}}, nil, executor, &buf)
	if err != nil {
		t.Fatalf("AgentStream returned error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, `"type":"tool_call"`) {
		t.Errorf("expected tool_call event, got:\n%s", output)
	}
	if !strings.Contains(output, `"tweets_list"`) {
		t.Errorf("expected tweets_list tool call, got:\n%s", output)
	}
	if !strings.Contains(output, `"type":"tool_result"`) {
		t.Errorf("expected tool_result event, got:\n%s", output)
	}
	if !strings.Contains(output, `"type":"text_chunk"`) {
		t.Errorf("expected text_chunk event, got:\n%s", output)
	}
	if !strings.Contains(output, "data: [DONE]") {
		t.Errorf("expected [DONE], got:\n%s", output)
	}
	if len(executor.calls) != 1 || executor.calls[0] != "tweets_list" {
		t.Errorf("expected executor called once with tweets_list, got %v", executor.calls)
	}
}

// TestExtractEmbeddedToolCalls covers the pure parsing function.
func TestExtractEmbeddedToolCalls(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string // expected tool names in order
	}{
		{
			name:    "no tool calls",
			content: "Just plain text.",
			want:    nil,
		},
		{
			name:    "single tool call",
			content: `{"name":"tweets_list","arguments":{"limit":5}}` + "</tool_call>",
			want:    []string{"tweets_list"},
		},
		{
			name: "two tool calls",
			content: `{"name":"rules_list","arguments":{}}` + "</tool_call>" +
				`{"name":"sources_list","arguments":{}}` + "</tool_call>",
			want: []string{"rules_list", "sources_list"},
		},
		{
			name:    "garbled prefix before JSON",
			content: "\xef\xbf\xbd" + `{"name":"health_get","arguments":{}}` + "</tool_call>",
			want:    []string{"health_get"},
		},
		{
			name:    "invalid JSON skipped",
			content: `not-json</tool_call>{"name":"tweets_list","arguments":{}}` + "</tool_call>",
			want:    []string{"tweets_list"},
		},
		{
			name:    "missing name skipped",
			content: `{"arguments":{}}` + "</tool_call>",
			want:    nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractEmbeddedToolCalls(tc.content)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d tool calls, want %d: %+v", len(got), len(tc.want), got)
			}
			for i, name := range tc.want {
				if got[i].Function.Name != name {
					t.Errorf("call[%d]: got name %q, want %q", i, got[i].Function.Name, name)
				}
			}
		})
	}
}
