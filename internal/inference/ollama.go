package inference

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
	"strings"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/mcpserver"
)

const (
	defaultOllamaBaseURL = "http://localhost:11434"
	ollamaRequestTimeout = 120 * time.Second
	ollamaDigestTimeout  = 10 * time.Minute
	ollamaLoadTimeout    = 5 * time.Minute
)

// ollamaHTTPError is returned when Ollama responds with a non-200 status so
// that isBackendError can inspect the status code and distinguish 5xx backend
// failures from 4xx model/request errors.
type ollamaHTTPError struct {
	statusCode int
	body       string
}

func (e *ollamaHTTPError) Error() string {
	return fmt.Sprintf("ollama: HTTP %d: %s", e.statusCode, e.body)
}

// isBackendError reports whether err indicates the Ollama backend itself is
// unavailable (network failure or 5xx), as opposed to a model or request error.
func isBackendError(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var httpErr *ollamaHTTPError
	if errors.As(err, &httpErr) {
		return httpErr.statusCode >= 500
	}
	return false
}

// ollamaBackend represents a single Ollama server instance.
// Each backend has its own HTTP client. healthy tracks whether the backend is
// currently reachable; inUse tracks whether a request holds it. State is
// managed under BackendPool.mu.
type ollamaBackend struct {
	name         string    // display name; defaults to baseURL if empty
	baseURL      string
	client       *http.Client
	enabled      bool      // false means the backend is disabled and should not be used
	healthy      bool      // managed by health checker and request-time failures
	inUse        bool      // true while a request holds this backend
	score        float64   // tokens/sec, exponential moving average; 0 = not yet measured
	requestCount int64     // total completed requests
	lastActiveAt time.Time // zero value if never used
}

// updateScore updates the backend's performance score using an exponential
// moving average (alpha=0.3). Must be called with BackendPool.mu held.
func (b *ollamaBackend) updateScore(tokensPerSec float64) {
	if b.score == 0 {
		b.score = tokensPerSec
	} else {
		b.score = 0.3*tokensPerSec + 0.7*b.score
	}
}

// ollamaEval holds the eval stats returned in Ollama generate/chat responses.
type ollamaEval struct {
	EvalCount    int   `json:"eval_count"`
	EvalDuration int64 `json:"eval_duration"` // nanoseconds
}

// ollamaAgentMsg is the message format for AgentStream requests/responses.
type ollamaAgentMsg struct {
	Role       string             `json:"role"`
	Content    string             `json:"content,omitempty"`
	ToolCalls  []ollamaAgentTCall `json:"tool_calls,omitempty"`
	ToolCallID string             `json:"tool_call_id,omitempty"`
}

// ollamaAgentTCall is a single tool call returned by Ollama.
type ollamaAgentTCall struct {
	ID       string               `json:"id,omitempty"`
	Function ollamaAgentTCallFunc `json:"function"`
}

// ollamaAgentTCallFunc carries the name and arguments of a tool call.
type ollamaAgentTCallFunc struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// OllamaPipeline implements Pipeline by calling one or more locally-running
// Ollama servers via a shared BackendPool. The model Path stored in the
// pipelines table is treated as the Ollama model name (e.g. "qwen2.5:3b").
//
// assignedIDs is a priority-ordered list of backend IDs from the pool to use.
// An empty slice means use any backend from the full pool (AcquireAny).
type OllamaPipeline struct {
	name            string
	model           string
	systemPrompt    string
	pool            *BackendPool
	assignedIDs     []int64 // priority-ordered backend IDs; empty = use full pool
	fastWaitTimeout time.Duration
}

// NewOllamaPipelineFromConfig creates an Ollama pipeline from DB pipeline config.
// config must contain a "model" key (Ollama model name) and optionally a
// "system_prompt" key. pool is the shared BackendPool; assignedIDs restricts
// which backends this pipeline uses (empty = any). fastWaitTimeout controls
// how long acquirePrioritized waits for each backend before trying the next;
// values <= 0 default to 5s.
func NewOllamaPipelineFromConfig(name string, cfg map[string]any, pool *BackendPool, assignedIDs []int64, fastWaitTimeout time.Duration) *OllamaPipeline {
	systemPrompt, _ := cfg["system_prompt"].(string)
	if fastWaitTimeout == 0 {
		fastWaitTimeout = 5 * time.Second
	}
	model, _ := cfg["model"].(string)
	return &OllamaPipeline{
		name:            name,
		model:           model,
		systemPrompt:    systemPrompt,
		pool:            pool,
		assignedIDs:     assignedIDs,
		fastWaitTimeout: fastWaitTimeout,
	}
}

// acquire gets a backend for this pipeline.
func (p *OllamaPipeline) acquire(ctx context.Context) (*ollamaBackend, error) {
	if len(p.assignedIDs) == 0 {
		return p.pool.AcquireAny(ctx)
	}
	return p.acquirePrioritized(ctx)
}

// acquirePrioritized cycles through assignedIDs in priority order,
// waiting up to fastWaitTimeout per backend before trying the next.
func (p *OllamaPipeline) acquirePrioritized(ctx context.Context) (*ollamaBackend, error) {
	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		for _, id := range p.assignedIDs {
			b := p.pool.Get(id)
			if b == nil {
				continue // disabled or removed
			}
			ok, err := p.pool.AcquireWithTimeout(ctx, b, p.fastWaitTimeout)
			if err != nil {
				return nil, err
			}
			if ok {
				return b, nil
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// recordEval updates the backend's performance score based on eval stats from
// a completed Ollama response. Safe to call with nil or zero eval stats.
func (p *OllamaPipeline) recordEval(b *ollamaBackend, eval ollamaEval) {
	if eval.EvalDuration == 0 || eval.EvalCount == 0 {
		return
	}
	tokensPerSec := float64(eval.EvalCount) / (float64(eval.EvalDuration) / 1e9)
	p.pool.mu.Lock()
	b.updateScore(tokensPerSec)
	b.requestCount++
	b.lastActiveAt = time.Now()
	p.pool.mu.Unlock()
}

// Name returns the pipeline name as registered in the pipelines table.
func (p *OllamaPipeline) Name() string { return p.name }

// MaxConcurrency returns the count of all enabled backends in the pool,
// satisfying ConcurrencyLimiter. Each backend handles at most one request
// at a time, so the total throughput scales with pool size.
func (p *OllamaPipeline) MaxConcurrency() int {
	return len(p.pool.All())
}

// IsAvailable reports whether a backend is immediately acquirable for this
// pipeline. Respects priority-ordered assignedIDs; empty slice falls back to
// the full pool sorted by score. Satisfies inference.AvailabilityChecker.
func (p *OllamaPipeline) IsAvailable() bool {
	return p.pool.IsAnyAvailable(p.assignedIDs)
}

// Load treats modelPath as the Ollama model name (e.g. "qwen2.5:3b").
// The pool manages health checking; this method just stores the model name.
func (p *OllamaPipeline) Load(modelPath string) error {
	p.model = modelPath
	return nil
}

// Close is a no-op; the pool manages backend lifecycle.
func (p *OllamaPipeline) Close() error {
	return nil
}

// Type returns PipelineTypeOllama.
func (p *OllamaPipeline) Type() PipelineType { return PipelineTypeOllama }

// runRaw sends text to Ollama and returns the model's JSON response as-is.
func (p *OllamaPipeline) runRaw(ctx context.Context, text []byte) (json.RawMessage, error) {
	if isTruncated(string(text)) {
		return json.RawMessage(`{"truncated":true}`), nil
	}
	content, err := p.sendChat(ctx, string(text))
	if err != nil {
		return nil, err
	}
	return json.RawMessage(content), nil
}

// Run executes the Ollama pipeline on input.Text and returns structured outputs.
func (p *OllamaPipeline) Run(ctx context.Context, input PipelineInput) (map[string]any, error) {
	raw, err := p.runRaw(ctx, []byte(input.Text))
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{"raw": string(raw)}, nil
	}
	return out, nil
}

// ensureLoaded checks /api/ps on the given backend to see if the model is
// currently loaded in Ollama. If it is not, it calls warmUp to load it.
// Must be called with the backend acquired.
func (p *OllamaPipeline) ensureLoaded(b *ollamaBackend) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.baseURL+"/api/ps", nil)
	if err != nil {
		return fmt.Errorf("inference: ollama: ps request: %w", err)
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("inference: ollama: ps: %w", err)
	}
	defer resp.Body.Close()

	var ps struct {
		Models []struct {
			Model string `json:"model"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ps); err != nil {
		return fmt.Errorf("inference: ollama: ps decode: %w", err)
	}
	for _, m := range ps.Models {
		if m.Model == p.model {
			return nil // already loaded
		}
	}

	return p.warmUp(b)
}

// warmUp sends a no-op generate request to load the model into memory on the
// given backend before actual inference requests begin. Uses ollamaLoadTimeout
// to accommodate cold starts. Ollama's default keep_alive (5 min idle) applies
// — the model unloads naturally when not in use.
// Must be called with the backend acquired.
func (p *OllamaPipeline) warmUp(b *ollamaBackend) error {
	type generateRequest struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	body, err := json.Marshal(generateRequest{Model: p.model, Stream: false})
	if err != nil {
		return fmt.Errorf("inference: ollama: warmup marshal: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), ollamaLoadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.baseURL+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("inference: ollama: warmup request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("inference: ollama: warmup: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("inference: ollama: warmup returned %d", resp.StatusCode)
	}
	return nil
}

// isTruncated reports whether text appears to be a truncated retweet.
func isTruncated(text string) bool {
	return strings.HasPrefix(text, "RT @") && strings.HasSuffix(strings.TrimSpace(text), "\u2026")
}

// sendChat sends a single user message to Ollama's /api/chat endpoint using
// p.systemPrompt and returns the raw content string from the assistant reply.
// On backend errors (network or 5xx) it marks the backend unhealthy and retries.
func (p *OllamaPipeline) sendChat(ctx context.Context, userText string) (string, error) {
	type message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type chatRequest struct {
		Model    string    `json:"model"`
		Messages []message `json:"messages"`
		Stream   bool      `json:"stream"`
		Format   string    `json:"format"`
	}
	type chatResponse struct {
		Message      message `json:"message"`
		EvalCount    int     `json:"eval_count"`
		EvalDuration int64   `json:"eval_duration"`
	}

	body, err := json.Marshal(chatRequest{
		Model: p.model,
		Messages: []message{
			{Role: "system", Content: p.systemPrompt},
			{Role: "user", Content: userText},
		},
		Stream: false,
		Format: "json",
	})
	if err != nil {
		return "", fmt.Errorf("inference: ollama: marshal request: %w", err)
	}

	notifyBackend(ctx, "waiting")
	for {
		b, err := p.acquire(ctx)
		if err != nil {
			return "", fmt.Errorf("inference: ollama: acquire: %w", err)
		}
		notifyBackend(ctx, b.name)

		if err := p.ensureLoaded(b); err != nil {
			if isBackendError(err) {
				p.pool.MarkUnhealthy(b)
				continue
			}
			p.pool.Release(b)
			return "", fmt.Errorf("inference: ollama: ensure loaded: %w", err)
		}

		reqCtx, cancel := context.WithTimeout(ctx, ollamaRequestTimeout)
		req, reqErr := http.NewRequestWithContext(reqCtx, http.MethodPost, b.baseURL+"/api/chat", bytes.NewReader(body))
		if reqErr != nil {
			cancel()
			p.pool.Release(b)
			return "", fmt.Errorf("inference: ollama: build request: %w", reqErr)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, doErr := b.client.Do(req)
		cancel()
		if doErr != nil {
			p.pool.MarkUnhealthy(b)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			httpErr := &ollamaHTTPError{statusCode: resp.StatusCode, body: string(raw)}
			if isBackendError(httpErr) {
				p.pool.MarkUnhealthy(b)
				continue
			}
			p.pool.Release(b)
			return "", httpErr
		}

		var chatResp chatResponse
		decErr := json.NewDecoder(resp.Body).Decode(&chatResp)
		resp.Body.Close()
		if decErr != nil {
			p.pool.Release(b)
			return "", fmt.Errorf("inference: ollama: decode response: %w", decErr)
		}
		p.recordEval(b, ollamaEval{EvalCount: chatResp.EvalCount, EvalDuration: chatResp.EvalDuration})
		p.pool.Release(b)
		return chatResp.Message.Content, nil
	}
}

// Digest calls Ollama with the OllamaDigestSystem prompt and a numbered list of
// pre-formatted tweet strings, returning a single plain-text briefing.
// On backend errors (network or 5xx) it marks the backend unhealthy and retries.
func (p *OllamaPipeline) Digest(ctx context.Context, tweets []string) (string, error) {
	var sb strings.Builder
	for i, t := range tweets {
		fmt.Fprintf(&sb, "[%d] %s\n\n", i+1, t)
	}

	type message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type chatRequest struct {
		Model    string    `json:"model"`
		Messages []message `json:"messages"`
		Stream   bool      `json:"stream"`
	}
	type chatResponse struct {
		Message      message `json:"message"`
		EvalCount    int     `json:"eval_count"`
		EvalDuration int64   `json:"eval_duration"`
	}

	body, err := json.Marshal(chatRequest{
		Model: p.model,
		Messages: []message{
			{Role: "system", Content: OllamaDigestSystem},
			{Role: "user", Content: sb.String()},
		},
		Stream: false,
	})
	if err != nil {
		return "", fmt.Errorf("inference: ollama: digest: marshal request: %w", err)
	}

	for {
		b, err := p.acquire(ctx)
		if err != nil {
			return "", fmt.Errorf("inference: ollama: digest: acquire: %w", err)
		}

		if err := p.ensureLoaded(b); err != nil {
			if isBackendError(err) {
				p.pool.MarkUnhealthy(b)
				continue
			}
			p.pool.Release(b)
			return "", fmt.Errorf("inference: ollama: digest: ensure loaded: %w", err)
		}

		reqCtx, cancel := context.WithTimeout(ctx, ollamaDigestTimeout)
		req, reqErr := http.NewRequestWithContext(reqCtx, http.MethodPost, b.baseURL+"/api/chat", bytes.NewReader(body))
		if reqErr != nil {
			cancel()
			p.pool.Release(b)
			return "", fmt.Errorf("inference: ollama: digest: build request: %w", reqErr)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, doErr := b.client.Do(req)
		cancel()
		if doErr != nil {
			p.pool.MarkUnhealthy(b)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			httpErr := &ollamaHTTPError{statusCode: resp.StatusCode, body: string(raw)}
			if isBackendError(httpErr) {
				p.pool.MarkUnhealthy(b)
				continue
			}
			p.pool.Release(b)
			return "", httpErr
		}

		var chatResp chatResponse
		decErr := json.NewDecoder(resp.Body).Decode(&chatResp)
		resp.Body.Close()
		if decErr != nil {
			p.pool.Release(b)
			return "", fmt.Errorf("inference: ollama: digest: decode response: %w", decErr)
		}
		p.recordEval(b, ollamaEval{EvalCount: chatResp.EvalCount, EvalDuration: chatResp.EvalDuration})
		p.pool.Release(b)
		return strings.TrimSpace(chatResp.Message.Content), nil
	}
}

// OllamaDigestSystem is the prompt for multi-tweet digest/briefing generation.
const OllamaDigestSystem = `You are an intelligence analyst specialising in Iran. You will receive a numbered list of tweets from open-source social media monitoring.

Produce a structured intelligence briefing that:
- Opens with a one-sentence executive summary of the overall situation
- Groups events by theme (military, political, economic, civil unrest, etc.)
- Within each theme, summarises the key developments in bullet points
- Concludes with any notable patterns, escalations, or trends

Write in plain text with clear section headings. Be concise and factual. Synthesise across tweets — do not list them individually.`


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
	tools []mcpserver.OllamaTool,
	executor ToolExecutor,
	w io.Writer,
) error {
	type agentChatReq struct {
		Model    string                 `json:"model"`
		Messages []ollamaAgentMsg       `json:"messages"`
		Tools    []mcpserver.OllamaTool `json:"tools,omitempty"`
		Stream   bool                   `json:"stream"`
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

			reqCtx, cancel := context.WithTimeout(ctx, ollamaRequestTimeout)
			req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, b.baseURL+"/api/chat", bytes.NewReader(reqBody))
			if err != nil {
				cancel()
				loopErr = fmt.Errorf("inference: ollama: agent: build request: %w", err)
				break
			}
			req.Header.Set("Content-Type", "application/json")

			resp, err := b.client.Do(req)
			cancel()
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

			// Ollama < 0.3 (e.g. 0.20.x) does not parse Qwen2.5's <tool_call>
			// special tokens into the tool_calls field; they appear as garbled
			// Unicode + raw JSON + "</tool_call>" in the content.  Extract them.
			if len(chatResp.Message.ToolCalls) == 0 && strings.Contains(chatResp.Message.Content, "</tool_call>") {
				if parsed := extractEmbeddedToolCalls(chatResp.Message.Content); len(parsed) > 0 {
					chatResp.Message.ToolCalls = parsed
					chatResp.Message.Content = ""
				}
			}

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

				msgs = append(msgs, ollamaAgentMsg{Role: "tool", Content: result, ToolCallID: tc.ID})
			}
			// Loop back: send updated conversation to Ollama.
		}

		// loopErr is always set when we exit the inner loop via break.
		if isBackendError(loopErr) {
			p.pool.MarkUnhealthy(b)
			continue
		}
		p.pool.Release(b)
		return loopErr
	}
}

// extractEmbeddedToolCalls parses tool calls that Ollama left as raw text in
// the content field instead of the tool_calls field (Ollama < 0.3 / qwen2.5).
// The model emits: [garbled open-token] {"name":"...","arguments":{...}} \n </tool_call>
// possibly multiple times. We extract all valid JSON blocks before each </tool_call>.
func extractEmbeddedToolCalls(content string) []ollamaAgentTCall {
	const closeTag = "</tool_call>"
	var out []ollamaAgentTCall
	rest := content
	for {
		idx := strings.Index(rest, closeTag)
		if idx < 0 {
			break
		}
		chunk := rest[:idx]
		rest = rest[idx+len(closeTag):]

		// Find the outermost JSON object in chunk.
		start := strings.Index(chunk, "{")
		if start < 0 {
			continue
		}
		// Walk forward to find the matching closing brace.
		depth := 0
		end := -1
		for i := start; i < len(chunk); i++ {
			switch chunk[i] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					end = i
				}
			}
			if depth == 0 && end >= 0 {
				break
			}
		}
		if end < 0 {
			continue
		}
		jsonStr := chunk[start : end+1]

		var call struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal([]byte(jsonStr), &call); err != nil || call.Name == "" {
			continue
		}
		out = append(out, ollamaAgentTCall{
			Function: ollamaAgentTCallFunc{
				Name:      call.Name,
				Arguments: call.Arguments,
			},
		})
	}
	return out
}

// OllamaChatSystem is the default system prompt for the agentic chat assistant.
const OllamaChatSystem = `You are an intelligent assistant for the iran-watcher OSINT monitoring application.

RULES:
- ALWAYS call a tool to fetch live data before answering. Never answer from memory.
- NEVER say a tool is missing or unavailable — always try.
- NEVER invent data. If a tool returns nothing, say so.
- When asked to "summarize tweets": call tweets_list (per_page = requested count), then write your own summary of the returned texts. Do not call summaries_run for this.`

// Summarize calls Ollama with a summarization prompt over the given text.
// On backend errors (network or 5xx) it marks the backend unhealthy and retries.
func (p *OllamaPipeline) Summarize(ctx context.Context, text string) (string, error) {
	slog.Debug("ollama: summarize", "pipeline", p.name, "model", p.model)
	content, err := p.sendChat(ctx, text)
	if err != nil {
		return "", err
	}
	return content, nil
}
