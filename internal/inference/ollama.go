package inference

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultOllamaBaseURL = "http://localhost:11434"
	ollamaRequestTimeout = 60 * time.Second
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

// rawExtractionResult is used for tolerant JSON decoding; its fields are then
// copied into ExtractionResult.
type rawExtractionResult struct {
	Description     string               `json:"description"`
	Locations       []ExtractionLocation `json:"locations"`
	GeolocatedBy    flexStrings          `json:"geolocated_by"`
	GeolocationURLs flexStrings          `json:"geolocation_urls"`
	ProofURLs       flexStrings          `json:"proof_urls"`
	LocationURL     string               `json:"location_url"`
	SourceURLs      flexStrings          `json:"source_urls"`
	Truncated       bool                 `json:"truncated,omitempty"`
}

// OllamaPipeline implements Pipeline by calling a locally-running Ollama server.
// The model Path stored in the models table is treated as the Ollama model name
// (e.g. "qwen2.5:3b"). The base URL comes from the application config.
type OllamaPipeline struct {
	name    string
	baseURL string
	model   string
	client  *http.Client
}

// NewOllamaPipeline creates a new OllamaPipeline. baseURL defaults to
// http://localhost:11434 if empty.
func NewOllamaPipeline(name, baseURL string) *OllamaPipeline {
	if baseURL == "" {
		baseURL = defaultOllamaBaseURL
	}
	return &OllamaPipeline{
		name:    name,
		baseURL: baseURL,
		client:  &http.Client{Timeout: ollamaRequestTimeout},
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
			{Role: "system", Content: ollamaExtractionSystem},
			{Role: "user", Content: text},
		},
		Stream: false,
		Format: "json",
	})
	if err != nil {
		return nil, fmt.Errorf("inference: ollama: marshal request: %w", err)
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
	return &ExtractionResult{
		Description:     raw.Description,
		Locations:       raw.Locations,
		GeolocatedBy:    []string(raw.GeolocatedBy),
		GeolocationURLs: []string(raw.GeolocationURLs),
		ProofURLs:       []string(raw.ProofURLs),
		LocationURL:     raw.LocationURL,
		SourceURLs:      []string(raw.SourceURLs),
		Truncated:       raw.Truncated,
	}, nil
}

const ollamaExtractionSystem = `You extract structured data from GeoConfirmed geo-confirmation posts on X (Twitter).

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
