package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

// geocodeCacheStore is the subset of store.Store needed by ReverseGeocoderPipeline.
// Using a minimal interface makes the handler easy to test with a simple mock.
type geocodeCacheStore interface {
	GetGeocodeCache(ctx context.Context, lat, lon float64) (string, bool, error)
	SetGeocodeCache(ctx context.Context, lat, lon float64, name string) error
}

// ReverseGeocoderPipeline resolves coordinate pairs to human-readable location names
// via a local Nominatim instance, caching results in SQLite.
//
// Input context key (configurable, default "locations"): raw JSON coordinate value
// in any format supported by model.ParseCoordinates.
//
// Output (configurable, default "location_names"): []string parallel to the input
// coordinate array. Empty string means no place name was found for that coordinate.
type ReverseGeocoderPipeline struct {
	name   string
	store  geocodeCacheStore
	cfg    reverseGeocoderConfig
	client *http.Client
}

type reverseGeocoderConfig struct {
	CoordinateKey  string `json:"coordinate_key"`
	CoordinateType string `json:"coordinate_type"`
	OutputKey      string `json:"output_key"`
	NominatimURL   string `json:"nominatim_url"` // optional per-action override
}

// NewReverseGeocoderFactory returns a Factory that creates ReverseGeocoderPipeline.
// defaultURL is the app-level Nominatim base URL (from config); individual actions
// can override it via their own "nominatim_url" config field.
func NewReverseGeocoderFactory(st store.Store, defaultURL string) Factory {
	return func(name string, config map[string]any) (inference.Pipeline, error) {
		cfg := reverseGeocoderConfig{
			CoordinateKey:  "locations",
			CoordinateType: string(model.CoordinateTypeArrayOfPairs),
			OutputKey:      "location_names",
			NominatimURL:   defaultURL,
		}
		if len(config) > 0 {
			if b, err := json.Marshal(config); err == nil {
				_ = json.Unmarshal(b, &cfg)
			}
		}
		if cfg.NominatimURL == "" {
			cfg.NominatimURL = defaultURL
		}
		return &ReverseGeocoderPipeline{
			name:   name,
			store:  st,
			cfg:    cfg,
			client: &http.Client{Timeout: 10 * time.Second},
		}, nil
	}
}

func (p *ReverseGeocoderPipeline) Name() string                 { return p.name }
func (p *ReverseGeocoderPipeline) Type() inference.PipelineType { return inference.PipelineTypeInternal }
func (p *ReverseGeocoderPipeline) Close() error                 { return nil }

func (p *ReverseGeocoderPipeline) Run(ctx context.Context, input inference.PipelineInput) (map[string]any, error) {
	raw, ok := input.Context[p.cfg.CoordinateKey]
	if !ok {
		return map[string]any{p.cfg.OutputKey: []string{}}, nil
	}

	// Normalise context value to a raw JSON string.
	// Context values are strings when they come from MergeAllTweetExecutionMetadata,
	// but may be other types after an in-process JSON round-trip.
	var rawStr string
	if s, ok := raw.(string); ok {
		rawStr = s
	} else {
		b, err := json.Marshal(raw)
		if err != nil {
			return nil, fmt.Errorf("reverse_geocoder: marshal coordinates: %w", err)
		}
		rawStr = string(b)
	}

	pairs, err := model.ParseCoordinates(rawStr, model.CoordinateType(p.cfg.CoordinateType))
	if err != nil {
		return nil, fmt.Errorf("reverse_geocoder: parse coordinates: %w", err)
	}

	names := make([]string, len(pairs))
	for i, pair := range pairs {
		lat := roundCoord(pair[0])
		lon := roundCoord(pair[1])

		if cached, found, err := p.store.GetGeocodeCache(ctx, lat, lon); err == nil && found {
			names[i] = cached
			continue
		}

		name, err := p.callNominatim(ctx, lat, lon)
		if err != nil {
			// Network failure or decode error: leave name empty, do not cache.
			// The next run will retry Nominatim.
			names[i] = ""
			continue
		}

		// Cache both successful names and confirmed empty results (error responses
		// from Nominatim) so we don't re-query the same dead coordinate repeatedly.
		_ = p.store.SetGeocodeCache(ctx, lat, lon, name)
		names[i] = name
	}

	return map[string]any{p.cfg.OutputKey: names}, nil
}

// callNominatim calls the Nominatim /reverse endpoint and returns the display_name.
// Returns ("", nil) when Nominatim responds with an error field (coordinate not found).
// Returns ("", err) on network or decode failure.
func (p *ReverseGeocoderPipeline) callNominatim(ctx context.Context, lat, lon float64) (string, error) {
	url := fmt.Sprintf("%s/reverse?lat=%f&lon=%f&format=json", p.cfg.NominatimURL, lat, lon)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build nominatim request: %w", err)
	}
	req.Header.Set("User-Agent", "iran-watcher/1.0")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("nominatim request: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		DisplayName string `json:"display_name"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode nominatim response: %w", err)
	}
	if result.Error != "" {
		// Coordinate exists but Nominatim has no name for it (open water, empty desert, etc.)
		return "", nil
	}
	return result.DisplayName, nil
}

// roundCoord rounds a coordinate to 4 decimal places (~11m precision) to maximise
// SQLite cache hits for nearby coordinates.
func roundCoord(v float64) float64 {
	return math.Round(v*10000) / 10000
}
