package inference

import (
	"bufio"
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

// ChatStream sends a streaming multi-turn chat to Ollama with tweet context
// prepended to the system message. It writes SSE lines to w as chunks arrive:
//
//	data: "chunk text"\n\n
//	data: [DONE]\n\n
//
// The caller must set Content-Type: text/event-stream and flush headers before
// calling this method. w is type-asserted to http.Flusher; if the assertion
// succeeds each chunk is flushed immediately for real-time streaming.
// On backend errors (network or 5xx) it marks the backend unhealthy and retries.
func (p *OllamaPipeline) ChatStream(ctx context.Context, tweetContext []string, messages []ChatMessage, w io.Writer) error {
	var sb strings.Builder
	sb.WriteString(OllamaChatSystem)
	if len(tweetContext) > 0 {
		sb.WriteString("\n\nCONTEXT TWEETS:\n")
		for i, t := range tweetContext {
			fmt.Fprintf(&sb, "[%d] %s\n\n", i+1, t)
		}
	}

	type ollamaMsg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type chatReq struct {
		Model    string      `json:"model"`
		Messages []ollamaMsg `json:"messages"`
		Stream   bool        `json:"stream"`
	}

	msgs := make([]ollamaMsg, 0, len(messages)+1)
	msgs = append(msgs, ollamaMsg{Role: "system", Content: sb.String()})
	for _, m := range messages {
		msgs = append(msgs, ollamaMsg{Role: m.Role, Content: m.Content})
	}

	body, err := json.Marshal(chatReq{Model: p.model, Messages: msgs, Stream: true})
	if err != nil {
		return fmt.Errorf("inference: ollama: chat stream: marshal: %w", err)
	}

	for {
		b, err := p.acquire(ctx)
		if err != nil {
			return fmt.Errorf("inference: ollama: chat stream: %w", err)
		}

		if err := p.ensureLoaded(b); err != nil {
			if isBackendError(err) {
				p.pool.MarkUnhealthy(b)
				continue
			}
			p.pool.Release(b)
			return fmt.Errorf("inference: ollama: chat stream: ensure loaded: %w", err)
		}

		req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, b.baseURL+"/api/chat", bytes.NewReader(body))
		if reqErr != nil {
			p.pool.Release(b)
			return fmt.Errorf("inference: ollama: chat stream: build request: %w", reqErr)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, doErr := b.client.Do(req)
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
			return httpErr
		}

		type streamChunk struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Done         bool  `json:"done"`
			EvalCount    int   `json:"eval_count"`
			EvalDuration int64 `json:"eval_duration"`
		}

		flusher, canFlush := w.(interface{ Flush() })

		scanner := bufio.NewScanner(resp.Body)
		var scanErr error
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}
			var chunk streamChunk
			if err := json.Unmarshal(line, &chunk); err != nil {
				continue
			}
			if chunk.Done {
				p.recordEval(b, ollamaEval{EvalCount: chunk.EvalCount, EvalDuration: chunk.EvalDuration})
				fmt.Fprintf(w, "data: [DONE]\n\n")
				if canFlush {
					flusher.Flush()
				}
				resp.Body.Close()
				p.pool.Release(b)
				return nil
			}
			if chunk.Message.Content != "" {
				encoded, _ := json.Marshal(chunk.Message.Content)
				fmt.Fprintf(w, "data: %s\n\n", encoded)
				if canFlush {
					flusher.Flush()
				}
			}
		}
		scanErr = scanner.Err()
		resp.Body.Close()
		p.pool.Release(b)
		return scanErr
	}
}

// EstimateTokens returns the approximate token count for the given texts and
// the model's context-window limit. It calls Ollama's /api/tokenize endpoint
// for an accurate count; if that fails it falls back to len(text)/4.
// Uses any available backend — token counts and context limits are model
// properties, identical across all backends.
// On backend errors (network or 5xx) it marks the backend unhealthy and retries.
func (p *OllamaPipeline) EstimateTokens(ctx context.Context, texts []string) (int, int, error) {
	combined := strings.Join(texts, "\n\n")

	for {
		b, err := p.acquire(ctx)
		if err != nil {
			return 0, 0, fmt.Errorf("inference: ollama: estimate tokens: %w", err)
		}

		limit := p.getContextLimit(ctx, b)
		tokens, err := p.tokenize(ctx, b, combined)
		if err != nil {
			if isBackendError(err) {
				p.pool.MarkUnhealthy(b)
				continue
			}
			p.pool.Release(b)
			// Rough fallback: ~4 chars per token
			return len(combined) / 4, limit, nil
		}
		p.pool.Release(b)
		return tokens, limit, nil
	}
}

// getContextLimit queries Ollama's /api/show on the given backend for the
// model's context window size. Falls back to 8192 if unavailable.
func (p *OllamaPipeline) getContextLimit(ctx context.Context, b *ollamaBackend) int {
	const defaultLimit = 8192

	body, _ := json.Marshal(map[string]string{"model": p.model})
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, b.baseURL+"/api/show", bytes.NewReader(body))
	if err != nil {
		return defaultLimit
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.client.Do(req)
	if err != nil {
		return defaultLimit
	}
	defer resp.Body.Close()

	var result struct {
		ModelInfo map[string]json.RawMessage `json:"model_info"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return defaultLimit
	}
	for k, v := range result.ModelInfo {
		if strings.Contains(k, "context_length") {
			var n int
			if err := json.Unmarshal(v, &n); err == nil && n > 0 {
				return n
			}
		}
	}
	return defaultLimit
}

// tokenize calls Ollama's /api/tokenize endpoint on the given backend and
// returns the token count.
func (p *OllamaPipeline) tokenize(ctx context.Context, b *ollamaBackend, text string) (int, error) {
	body, _ := json.Marshal(map[string]string{"model": p.model, "prompt": text})

	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, b.baseURL+"/api/tokenize", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, &ollamaHTTPError{statusCode: resp.StatusCode, body: "tokenize failed"}
	}

	var result struct {
		Tokens []int `json:"tokens"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}
	return len(result.Tokens), nil
}

// OllamaChatSystem is the system prompt for interactive chat sessions over tweets.
const OllamaChatSystem = `You are an intelligence analyst specialising in Iran. You have been provided with a set of tweets from open-source monitoring as context.

Answer questions about these tweets accurately and concisely. If a question cannot be answered from the provided tweets, say so clearly. Do not fabricate information not present in the tweets.`

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
