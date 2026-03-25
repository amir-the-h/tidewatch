package inference

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// numericPattern matches any integer or decimal number (optionally signed)
// that could represent a coordinate value in tweet text.
var numericPattern = regexp.MustCompile(`[-+]?[0-9]+(?:\.[0-9]+)?`)

const (
	defaultOllamaBaseURL = "http://localhost:11434"
	ollamaRequestTimeout = 120 * time.Second
	ollamaLoadTimeout    = 5 * time.Minute
)

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
	GeolocationURLs flexStrings          `json:"geolocation_urls"`
	ProofURLs       flexStrings          `json:"proof_urls"`
	LocationURL     string               `json:"location_url"`
	SourceURLs      flexStrings          `json:"source_urls"`
	Truncated       bool                 `json:"truncated,omitempty"`
}

// OllamaPipeline implements Pipeline by calling a locally-running Ollama server.
// The model Path stored in the models table is treated as the Ollama model name
// (e.g. "qwen2.5:3b"). The base URL comes from the application config.
//
// A semaphore (sem) limits concurrent in-flight requests to 1. Ollama queues
// requests internally anyway, so firing N concurrent requests just causes the
// Nth one to time out waiting. Serialising here prevents that.
type OllamaPipeline struct {
	name         string
	baseURL      string
	model        string
	systemPrompt string
	client       *http.Client
	sem          chan struct{} // capacity 1 — only one call in flight at a time
}

// NewOllamaPipeline creates a new OllamaPipeline. baseURL defaults to
// http://localhost:11434 if empty. systemPrompt selects the extraction
// instruction set; pass OllamaGeoConfirmedSystem or OllamaGenericSystem.
// An empty systemPrompt falls back to OllamaGenericSystem.
func NewOllamaPipeline(name, baseURL, systemPrompt string) *OllamaPipeline {
	if baseURL == "" {
		baseURL = defaultOllamaBaseURL
	}
	if systemPrompt == "" {
		systemPrompt = OllamaGenericSystem
	}
	return &OllamaPipeline{
		name:         name,
		baseURL:      baseURL,
		systemPrompt: systemPrompt,
		client:       &http.Client{}, // no global timeout — each request carries its own context deadline
		sem:          make(chan struct{}, 1),
	}
}

// Name returns the pipeline name as registered in the models table.
func (p *OllamaPipeline) Name() string { return p.name }

// Load treats modelPath as the Ollama model name (e.g. "qwen2.5:3b").
// It pings /api/tags to verify the server is reachable.
func (p *OllamaPipeline) Load(modelPath string) error {
	p.model = modelPath
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/api/tags", nil)
	if err != nil {
		return fmt.Errorf("inference: ollama: build ping: %w", err)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("inference: ollama: ping %s: %w", p.baseURL, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("inference: ollama: ping returned %d", resp.StatusCode)
	}
	return nil
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

// Close is a no-op; the HTTP client has no persistent state to release.
func (p *OllamaPipeline) Close() error { return nil }

// ensureLoaded checks /api/ps to see if the model is currently loaded in Ollama.
// If it is not, it calls warmUp to load it before returning.
// Must be called with the semaphore held.
func (p *OllamaPipeline) ensureLoaded() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/api/ps", nil)
	if err != nil {
		return fmt.Errorf("inference: ollama: ps request: %w", err)
	}
	resp, err := p.client.Do(req)
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

	return p.warmUp()
}

// warmUp sends a no-op generate request to load the model into memory before
// actual inference requests begin. Uses ollamaLoadTimeout to accommodate cold
// starts. Ollama's default keep_alive (5 min idle) applies — the model unloads
// naturally when not in use. Must be called with the semaphore held.
func (p *OllamaPipeline) warmUp() error {
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

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("inference: ollama: warmup request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
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

	// Acquire semaphore — only one Ollama request in flight at a time.
	p.sem <- struct{}{}
	defer func() { <-p.sem }()

	// Ensure the model is loaded before sending the real request.
	// If it is not running, warmUp blocks until Ollama has loaded it.
	if err := p.ensureLoaded(); err != nil {
		return nil, fmt.Errorf("inference: ollama: ensure loaded: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), ollamaRequestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("inference: ollama: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("inference: ollama: chat: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("inference: ollama: chat returned %d: %s", resp.StatusCode, raw)
	}

	var chatResp chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("inference: ollama: decode response: %w", err)
	}

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

// OllamaGeoConfirmedSystem is the extraction prompt for structured GeoConfirmed posts.
const OllamaGeoConfirmedSystem = `You extract structured data from GeoConfirmed geo-confirmation posts on X (Twitter).

REPORT FORMAT — posts follow this exact layout:
  GeoConfirmed Iran.

  <description paragraph(s)>

  [Optional label - ]<lat>, <lon>
  <PlusCode> <City, Country>

  GeoLocated by
  @<handle>

  Geolocation:
  [Geo]
  https://...

  [Proof:
  https://...]

  Location:
  https://geoconfirmed.org/...

  Sources:
  https://...

  Visit our map: https://geoconfirmed.org/...

EXTRACTION RULES:

description:
  All narrative paragraph(s) describing the event. Stop at the first coordinate line or metadata line. Do NOT include the "GeoConfirmed Iran." header.

locations (array):
  Each location block contains a coordinate line and a plus-code line.
  - "label": text before " - " on the coordinate line (e.g. "Left building"). Empty string if no label.
  - "coordinates": [latitude, longitude] parsed as numbers from the "<lat>, <lon>" line. Coordinates are two decimal numbers separated by ", ". ONLY extract explicit coordinates from the text — do not guess.
  - "place": the human-readable part after the plus code on the next line (e.g. "Mosul, Iraq").
  If no coordinate lines are present, return [].

geolocated_by:
  @handles listed after "GeoLocated by". Include the @ prefix. Do NOT include @GeoConfirmed itself — only the person(s) who performed the geolocation.

geolocation_urls:
  All https:// URLs under the "Geolocation:" section. Ignore bare words like "Geo".

proof_urls:
  All https:// URLs under the "Proof:" section. [] if section is absent.

location_url:
  The single https://geoconfirmed.org/... URL under the "Location:" section. "" if absent.

source_urls:
  All https:// URLs under the "Sources:" section.

OUTPUT: Return only a valid JSON object with these exact keys. No markdown, no explanation.
Use [] for missing arrays and "" for missing strings.`

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
