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
	"math"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// numericPattern matches any integer or decimal number (optionally signed)
// that could represent a coordinate value in tweet text.
var numericPattern = regexp.MustCompile(`[-+]?[0-9]+(?:\.[0-9]+)?`)

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

// ExtractionLocation holds a single geographic location extracted from a tweet.
type ExtractionLocation struct {
	Label       string     `json:"label,omitempty"`
	Coordinates [2]float64 `json:"coordinates"` // [latitude, longitude]
	Place       string     `json:"place,omitempty"`
}

// ExtractionResult is the structured output of the geo-event extraction pipeline.
type ExtractionResult struct {
	Description     string               `json:"description"`
	Locations       []ExtractionLocation `json:"locations"`
	GeolocatedBy    []string             `json:"geolocated_by"`
	GeolocationURLs []string             `json:"geolocation_urls"`
	ProofURLs       []string             `json:"proof_urls"`
	LocationURL     string               `json:"location_url"`
	SourceURLs      []string             `json:"source_urls"`
	Truncated       bool                 `json:"truncated,omitempty"`
}

// flexStrings unmarshals a JSON value that may be either a string or an array
// of strings. LLMs occasionally return a bare string instead of a one-element
// array, so we coerce it silently.
type flexStrings []string

func (f *flexStrings) UnmarshalJSON(b []byte) error {
	var arr []string
	if err := json.Unmarshal(b, &arr); err == nil {
		*f = arr
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	if s != "" {
		*f = []string{s}
	}
	return nil
}

// flexCoordinates unmarshals a [lat, lon] pair where the LLM may emit numbers
// or quoted strings for the coordinate values.
type flexCoordinates [2]float64

func (f *flexCoordinates) UnmarshalJSON(b []byte) error {
	// Entire field is null or wrong shape — mark both as invalid.
	if string(b) == "null" {
		f[0], f[1] = math.NaN(), math.NaN()
		return nil
	}
	var raw [2]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		f[0], f[1] = math.NaN(), math.NaN()
		return nil
	}
	for i, r := range raw {
		if string(r) == "null" {
			f[i] = math.NaN()
			continue
		}
		// Try as a JSON number first.
		var n json.Number
		if err := json.Unmarshal(r, &n); err == nil {
			v, err := n.Float64()
			if err != nil {
				f[i] = math.NaN()
				continue
			}
			f[i] = v
			continue
		}
		// Fall back to a quoted string.
		var s string
		if err := json.Unmarshal(r, &s); err == nil {
			v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
			if err != nil {
				f[i] = math.NaN()
				continue
			}
			f[i] = v
			continue
		}
		// Anything else (object, array, bool) — mark invalid.
		f[i] = math.NaN()
	}
	return nil
}

// rawExtractionLocation mirrors ExtractionLocation but uses flexCoordinates.
type rawExtractionLocation struct {
	Label       string          `json:"label,omitempty"`
	Coordinates flexCoordinates `json:"coordinates"`
	Place       string          `json:"place,omitempty"`
}

// rawExtractionResult is used for tolerant JSON decoding; its fields are then
// copied into ExtractionResult.
type rawExtractionResult struct {
	Description     string                  `json:"description"`
	Locations       []rawExtractionLocation `json:"locations"`
	GeolocatedBy    flexStrings             `json:"geolocated_by"`
	GeolocationURLs flexStrings             `json:"geolocation_urls"`
	ProofURLs       flexStrings             `json:"proof_urls"`
	LocationURL     string                  `json:"location_url"`
	SourceURLs      flexStrings             `json:"source_urls"`
	Truncated       bool                    `json:"truncated,omitempty"`
}

// ollamaBackend represents a single Ollama server instance.
// Each backend has its own HTTP client. healthy tracks whether the backend is
// currently reachable; inUse tracks whether a request holds it. State is
// managed under OllamaPipeline.mu.
type ollamaBackend struct {
	baseURL string
	client  *http.Client
	healthy bool // managed by health checker and request-time failures
	inUse   bool // true while a request holds this backend
}

// OllamaPipeline implements Pipeline by calling one or more locally-running
// Ollama servers. The model Path stored in the models table is treated as the
// Ollama model name (e.g. "qwen2.5:3b"). Base URLs come from the application
// config; multiple URLs form a pool so requests are spread across backends.
//
// Backends are selected via acquire/release under mu+cond. Each backend
// handles at most one in-flight request at a time — Ollama queues internally
// and firing concurrent requests to the same instance just causes timeouts.
// Unhealthy backends are skipped; the background health checker probes them
// for recovery every 30 seconds.
type OllamaPipeline struct {
	name         string
	model        string
	systemPrompt string
	backends     []*ollamaBackend // ordered slice for Load iteration

	mu           sync.Mutex
	cond         *sync.Cond        // broadcast whenever any backend state changes
	cancelHealth context.CancelFunc
}

// NewOllamaPipeline creates a new OllamaPipeline. baseURLs is the list of
// Ollama server addresses; an empty slice or a single empty string both fall
// back to http://localhost:11434. systemPrompt selects the extraction
// instruction set; an empty value falls back to OllamaGenericSystem.
func NewOllamaPipeline(name string, baseURLs []string, systemPrompt string) *OllamaPipeline {
	if len(baseURLs) == 0 {
		baseURLs = []string{defaultOllamaBaseURL}
	}
	if systemPrompt == "" {
		systemPrompt = OllamaGenericSystem
	}
	backends := make([]*ollamaBackend, 0, len(baseURLs))
	for _, u := range baseURLs {
		if u == "" {
			u = defaultOllamaBaseURL
		}
		backends = append(backends, &ollamaBackend{
			baseURL: u,
			client:  &http.Client{}, // no global timeout — each request carries its own context deadline
			healthy: true,
			inUse:   false,
		})
	}
	p := &OllamaPipeline{
		name:         name,
		systemPrompt: systemPrompt,
		backends:     backends,
	}
	p.cond = sync.NewCond(&p.mu)
	return p
}

// acquire blocks until a healthy, idle backend is available or ctx is cancelled.
func (p *OllamaPipeline) acquire(ctx context.Context) (*ollamaBackend, error) {
	stop := context.AfterFunc(ctx, func() { p.cond.Broadcast() })
	defer stop()
	p.mu.Lock()
	defer p.mu.Unlock()
	for {
		for _, b := range p.backends {
			if b.healthy && !b.inUse {
				b.inUse = true
				return b, nil
			}
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		p.cond.Wait()
	}
}

// release marks a backend as no longer in use and notifies waiters.
func (p *OllamaPipeline) release(b *ollamaBackend) {
	p.mu.Lock()
	b.inUse = false
	p.mu.Unlock()
	p.cond.Broadcast()
}

// markUnhealthy marks a backend as unhealthy and not in use, then wakes waiters
// so they can try a different backend.
func (p *OllamaPipeline) markUnhealthy(b *ollamaBackend) {
	p.mu.Lock()
	b.healthy = false
	b.inUse = false
	p.mu.Unlock()
	p.cond.Broadcast()
	slog.Warn("ollama: backend marked unhealthy", "url", b.baseURL)
}

// Name returns the pipeline name as registered in the models table.
func (p *OllamaPipeline) Name() string { return p.name }

// MaxConcurrency returns the count of currently healthy backends, satisfying
// ConcurrencyLimiter. Each backend handles at most one request at a time, so
// the total throughput scales with the number of healthy backends.
func (p *OllamaPipeline) MaxConcurrency() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, b := range p.backends {
		if b.healthy {
			n++
		}
	}
	return n
}

// pingBackend pings GET /api/tags with a 5s timeout. Returns true if reachable.
func (p *OllamaPipeline) pingBackend(b *ollamaBackend) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.baseURL+"/api/tags", nil)
	if err != nil {
		return false
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// Load treats modelPath as the Ollama model name (e.g. "qwen2.5:3b").
// It pings /api/tags on every backend with a 5s timeout; unreachable backends
// are marked unhealthy rather than causing Load to fail. Load only fails for
// non-backend errors (e.g. bad request construction). After pinging, it starts
// the background health checker.
func (p *OllamaPipeline) Load(modelPath string) error {
	p.model = modelPath
	healthy := 0
	for _, b := range p.backends {
		if p.pingBackend(b) {
			p.mu.Lock()
			b.healthy = true
			p.mu.Unlock()
			healthy++
		} else {
			slog.Warn("ollama: backend unreachable on load, marked unhealthy", "url", b.baseURL)
			p.mu.Lock()
			b.healthy = false
			p.mu.Unlock()
		}
	}
	slog.Info("ollama: backends healthy after load", "healthy", healthy, "total", len(p.backends))

	healthCtx, cancel := context.WithCancel(context.Background())
	p.cancelHealth = cancel
	go p.runHealthChecker(healthCtx)
	return nil
}

// Close stops the background health checker and releases any pipeline state.
func (p *OllamaPipeline) Close() error {
	if p.cancelHealth != nil {
		p.cancelHealth()
	}
	return nil
}

// runHealthChecker runs two tickers: one to re-probe unhealthy backends for
// recovery (every 30s), and one to re-probe healthy backends for failure
// (every 60s). Stops when ctx is cancelled.
func (p *OllamaPipeline) runHealthChecker(ctx context.Context) {
	unhealthyTicker := time.NewTicker(30 * time.Second)
	healthyTicker := time.NewTicker(60 * time.Second)
	defer unhealthyTicker.Stop()
	defer healthyTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-unhealthyTicker.C:
			p.probeUnhealthy()
		case <-healthyTicker.C:
			p.probeHealthy()
		}
	}
}

// probeUnhealthy pings all unhealthy backends; marks them healthy on success.
// Snapshots targets under lock, probes outside lock, updates state under lock.
func (p *OllamaPipeline) probeUnhealthy() {
	p.mu.Lock()
	var targets []*ollamaBackend
	for _, b := range p.backends {
		if !b.healthy {
			targets = append(targets, b)
		}
	}
	p.mu.Unlock()

	for _, b := range targets {
		if p.pingBackend(b) {
			p.mu.Lock()
			b.healthy = true
			p.mu.Unlock()
			p.cond.Broadcast()
			slog.Info("ollama: backend recovered", "url", b.baseURL)
		}
	}
}

// probeHealthy pings all healthy backends; marks them unhealthy on failure.
func (p *OllamaPipeline) probeHealthy() {
	p.mu.Lock()
	var targets []*ollamaBackend
	for _, b := range p.backends {
		if b.healthy {
			targets = append(targets, b)
		}
	}
	p.mu.Unlock()

	for _, b := range targets {
		if !p.pingBackend(b) {
			p.markUnhealthy(b)
		}
	}
}

// Run sends the tweet text to Ollama and returns the ExtractionResult as JSON.
func (p *OllamaPipeline) Run(text []byte) (json.RawMessage, error) {
	result, err := p.Extract(string(text))
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

// RunBatch loops Run over each text sequentially.
func (p *OllamaPipeline) RunBatch(texts [][]byte) ([]json.RawMessage, error) {
	results := make([]json.RawMessage, len(texts))
	for i, t := range texts {
		r, err := p.Run(t)
		if err != nil {
			return nil, err
		}
		results[i] = r
	}
	return results, nil
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

// coordinateAppearsInText returns true when coord is present as a literal number
// in text (within 0.001° ≈ 100 m). This guards against the LLM geocoding place
// names instead of extracting explicit numeric coordinates from the text.
func coordinateAppearsInText(coord float64, text string) bool {
	for _, m := range numericPattern.FindAllString(text, -1) {
		v, err := strconv.ParseFloat(m, 64)
		if err != nil {
			continue
		}
		if math.Abs(v-coord) < 0.001 {
			return true
		}
	}
	return false
}

// isTruncated reports whether text appears to be a truncated retweet.
func isTruncated(text string) bool {
	return strings.HasPrefix(text, "RT @") && strings.HasSuffix(strings.TrimSpace(text), "\u2026")
}

// Extract calls Ollama's chat endpoint and unmarshals the structured result.
// On backend errors (network or 5xx) it marks the backend unhealthy and retries
// on another backend.
func (p *OllamaPipeline) Extract(text string) (*ExtractionResult, error) {
	if isTruncated(text) {
		return &ExtractionResult{Truncated: true}, nil
	}
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
		Message message `json:"message"`
	}

	body, err := json.Marshal(chatRequest{
		Model: p.model,
		Messages: []message{
			{Role: "system", Content: p.systemPrompt},
			{Role: "user", Content: text},
		},
		Stream: false,
		Format: "json",
	})
	if err != nil {
		return nil, fmt.Errorf("inference: ollama: marshal request: %w", err)
	}

	for {
		b, err := p.acquire(context.Background())
		if err != nil {
			return nil, fmt.Errorf("inference: ollama: acquire: %w", err)
		}

		// Ensure the model is loaded before sending the real request.
		if err := p.ensureLoaded(b); err != nil {
			if isBackendError(err) {
				p.markUnhealthy(b)
				continue
			}
			p.release(b)
			return nil, fmt.Errorf("inference: ollama: ensure loaded: %w", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), ollamaRequestTimeout)
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, b.baseURL+"/api/chat", bytes.NewReader(body))
		if reqErr != nil {
			cancel()
			p.release(b)
			return nil, fmt.Errorf("inference: ollama: build request: %w", reqErr)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, doErr := b.client.Do(req)
		cancel()
		if doErr != nil {
			p.markUnhealthy(b)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			httpErr := &ollamaHTTPError{statusCode: resp.StatusCode, body: string(raw)}
			if isBackendError(httpErr) {
				p.markUnhealthy(b)
				continue
			}
			p.release(b)
			return nil, httpErr
		}

		var chatResp chatResponse
		decErr := json.NewDecoder(resp.Body).Decode(&chatResp)
		resp.Body.Close()
		if decErr != nil {
			p.release(b)
			return nil, fmt.Errorf("inference: ollama: decode response: %w", decErr)
		}
		p.release(b)

		var raw rawExtractionResult
		if err := json.Unmarshal([]byte(chatResp.Message.Content), &raw); err != nil {
			return nil, fmt.Errorf("inference: ollama: parse extraction JSON: %w", err)
		}
		locs := make([]ExtractionLocation, 0, len(raw.Locations))
		for _, l := range raw.Locations {
			if math.IsNaN(l.Coordinates[0]) || math.IsNaN(l.Coordinates[1]) {
				continue // LLM returned null or unparseable value — skip this location
			}
			// Reject coordinates the LLM inferred from place names rather than reading
			// from the text. Both values must appear as literal numbers in the tweet.
			if !coordinateAppearsInText(l.Coordinates[0], text) ||
				!coordinateAppearsInText(l.Coordinates[1], text) {
				continue
			}
			locs = append(locs, ExtractionLocation{
				Label:       l.Label,
				Coordinates: [2]float64(l.Coordinates),
				Place:       l.Place,
			})
		}
		return &ExtractionResult{
			Description:     raw.Description,
			Locations:       locs,
			GeolocatedBy:    []string(raw.GeolocatedBy),
			GeolocationURLs: []string(raw.GeolocationURLs),
			ProofURLs:       []string(raw.ProofURLs),
			LocationURL:     raw.LocationURL,
			SourceURLs:      []string(raw.SourceURLs),
			Truncated:       raw.Truncated,
		}, nil
	}
}

// Digest calls Ollama with the OllamaDigestSystem prompt and a numbered list of
// pre-formatted tweet strings, returning a single plain-text briefing.
// On backend errors (network or 5xx) it marks the backend unhealthy and retries.
func (p *OllamaPipeline) Digest(tweets []string) (string, error) {
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
		Message message `json:"message"`
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
		b, err := p.acquire(context.Background())
		if err != nil {
			return "", fmt.Errorf("inference: ollama: digest: acquire: %w", err)
		}

		if err := p.ensureLoaded(b); err != nil {
			if isBackendError(err) {
				p.markUnhealthy(b)
				continue
			}
			p.release(b)
			return "", fmt.Errorf("inference: ollama: digest: ensure loaded: %w", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), ollamaDigestTimeout)
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, b.baseURL+"/api/chat", bytes.NewReader(body))
		if reqErr != nil {
			cancel()
			p.release(b)
			return "", fmt.Errorf("inference: ollama: digest: build request: %w", reqErr)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, doErr := b.client.Do(req)
		cancel()
		if doErr != nil {
			p.markUnhealthy(b)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			httpErr := &ollamaHTTPError{statusCode: resp.StatusCode, body: string(raw)}
			if isBackendError(httpErr) {
				p.markUnhealthy(b)
				continue
			}
			p.release(b)
			return "", httpErr
		}

		var chatResp chatResponse
		decErr := json.NewDecoder(resp.Body).Decode(&chatResp)
		resp.Body.Close()
		if decErr != nil {
			p.release(b)
			return "", fmt.Errorf("inference: ollama: digest: decode response: %w", decErr)
		}
		p.release(b)
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

// OllamaGeoConfirmedSystem is the extraction prompt for structured GeoConfirmed posts.
const OllamaGeoConfirmedSystem = `You extract structured data from GeoConfirmed geo-confirmation posts on X (Twitter).

REPORT FORMAT — posts follow this general layout (some sections may be absent or reordered):
  GeoConfirmed Iran.

  <description paragraph(s)>

  [Optional label - ]<lat>, <lon>
  [<PlusCode> <City, Country>]

  GeoLocated by
  @<handle>

  Geolocation:
  https://...

  [Proof:
  https://...]

  Location:
  https://...

  [Sources:
  https://...]

EXTRACTION RULES:

description:
  All narrative paragraph(s) describing the event. Stop at the first coordinate line or metadata line.
  Do NOT include the "GeoConfirmed Iran." header.
  Do NOT include video timestamp markers such as "Vid1", "Vid2", "0:09-0:12", etc.

locations (array):
  Scan the entire text for explicit decimal coordinate pairs (<lat>, <lon>).
  Coordinates appear as two numbers separated by ", " — e.g. "28.941203, 50.8128".
  The label (if any) may appear on the same line before " - ", OR on the immediately preceding line.
  For each coordinate pair found:
  - "label": any descriptive text associated with the coordinates. Empty string if none.
  - "coordinates": [latitude, longitude] as numbers exactly as written. ONLY extract explicit numeric coordinates — never guess from place names.
  - "place": the human-readable location on the line after the coordinates (after the PlusCode, if present). Empty string if absent.
  If no numeric coordinates appear in the text, return [].

geolocated_by:
  @handles listed after "GeoLocated by". Include the @ prefix.
  Do NOT include @GeoConfirmed itself.
  IMPORTANT: "Vid1", "Vid2", "Vid3" etc. are video clip labels — they are NOT @handles. Never add "@" to them.

geolocation_urls:
  All URLs under the "Geolocation:" section.

proof_urls:
  All URLs under the "Proof:" section. [] if section is absent.

location_url:
  The single URL under the "Location:" section. "" if absent.

source_urls:
  All URLs under the "Sources:" section.

OUTPUT: Return only a valid JSON object with these exact keys. No markdown, no explanation.
Use [] for missing arrays and "" for missing strings.`

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
				p.markUnhealthy(b)
				continue
			}
			p.release(b)
			return fmt.Errorf("inference: ollama: chat stream: ensure loaded: %w", err)
		}

		req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, b.baseURL+"/api/chat", bytes.NewReader(body))
		if reqErr != nil {
			p.release(b)
			return fmt.Errorf("inference: ollama: chat stream: build request: %w", reqErr)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, doErr := b.client.Do(req)
		if doErr != nil {
			p.markUnhealthy(b)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			httpErr := &ollamaHTTPError{statusCode: resp.StatusCode, body: string(raw)}
			if isBackendError(httpErr) {
				p.markUnhealthy(b)
				continue
			}
			p.release(b)
			return httpErr
		}

		type streamChunk struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Done bool `json:"done"`
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
				fmt.Fprintf(w, "data: [DONE]\n\n")
				if canFlush {
					flusher.Flush()
				}
				resp.Body.Close()
				p.release(b)
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
		p.release(b)
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
				p.markUnhealthy(b)
				continue
			}
			p.release(b)
			// Rough fallback: ~4 chars per token
			return len(combined) / 4, limit, nil
		}
		p.release(b)
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

// OllamaGenericSystem is the extraction prompt for any tweet that may contain
// geographic information, with no assumptions about post format.
const OllamaGenericSystem = `You extract explicit geographic coordinates from tweet text.

EXTRACTION RULES:

description:
  The full narrative text of the tweet. Do NOT include URLs or metadata lines.

locations (array):
  ONLY add an entry here if the tweet contains explicit numeric coordinates (decimal degrees, DMS, or similar).
  Do NOT infer or guess coordinates from place names — if no numeric coordinates appear in the text, return [].
  For each coordinate pair found:
  - "label": a short label the tweet associates with this coordinate (e.g. "strike site"). Empty string if none.
  - "coordinates": [latitude, longitude] parsed as numbers exactly as written in the tweet.
  - "place": a place name the tweet associates with this coordinate. Empty string if none.

geolocated_by:
  @handles that claim to have geolocated or verified the location. Include the @ prefix. [] if none.

geolocation_urls:
  URLs linking to geolocation evidence (maps, satellite imagery, etc.). [] if none.

proof_urls:
  URLs to supporting proof or sources for the geolocation. [] if none.

location_url:
  A single URL specifically linking to a map pin or location marker. "" if absent.

source_urls:
  Any other source URLs mentioned in the tweet. [] if none.

OUTPUT: Return only a valid JSON object with these exact keys. No markdown, no explanation.
Use [] for missing arrays and "" for missing strings.`
