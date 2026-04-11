package pipeline

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/amir-the-h/iran-watcher/internal/inference"
)

// mockGeoStore implements geocodeCacheStore for testing.
type mockGeoStore struct {
	cache map[[2]float64]string
}

func newMockGeoStore() *mockGeoStore {
	return &mockGeoStore{cache: map[[2]float64]string{}}
}

func (m *mockGeoStore) GetGeocodeCache(_ context.Context, lat, lon float64) (string, bool, error) {
	v, ok := m.cache[[2]float64{lat, lon}]
	return v, ok, nil
}

func (m *mockGeoStore) SetGeocodeCache(_ context.Context, lat, lon float64, name string) error {
	m.cache[[2]float64{lat, lon}] = name
	return nil
}

// nominatimSrv starts an httptest server that always returns the given display_name.
func nominatimSrv(displayName string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"display_name": displayName})
	}))
}

func newTestGeocoder(st *mockGeoStore, nominatimURL string) *ReverseGeocoderPipeline {
	return &ReverseGeocoderPipeline{
		name:  "test-geocoder",
		store: st,
		cfg: reverseGeocoderConfig{
			CoordinateKey:  "locations",
			CoordinateType: "array_of_pairs",
			OutputKey:      "location_names",
			NominatimURL:   nominatimURL,
		},
		client: &http.Client{},
	}
}

func TestReverseGeocoder_MissingContextKey(t *testing.T) {
	p := newTestGeocoder(newMockGeoStore(), "http://unused")

	out, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	names := out["location_names"].([]string)
	if len(names) != 0 {
		t.Errorf("want empty slice for missing key, got %v", names)
	}
}

func TestReverseGeocoder_CacheHit(t *testing.T) {
	st := newMockGeoStore()
	// roundCoord(35.68921) = 35.6892; roundCoord(51.38901) = 51.389
	st.cache[[2]float64{35.6892, 51.389}] = "Tehran, Iran"

	p := newTestGeocoder(st, "http://unused")

	out, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{"locations": "[[35.68921,51.38901]]"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	names := out["location_names"].([]string)
	if len(names) != 1 || names[0] != "Tehran, Iran" {
		t.Errorf("want ['Tehran, Iran'], got %v", names)
	}
}

func TestReverseGeocoder_NominatimCall(t *testing.T) {
	srv := nominatimSrv("Karaj, Alborz Province, Iran")
	defer srv.Close()

	st := newMockGeoStore()
	p := &ReverseGeocoderPipeline{
		name:  "test-geocoder",
		store: st,
		cfg: reverseGeocoderConfig{
			CoordinateKey:  "locations",
			CoordinateType: "array_of_pairs",
			OutputKey:      "location_names",
			NominatimURL:   srv.URL,
		},
		client: srv.Client(),
	}

	out, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{"locations": "[[35.8324,50.9985]]"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	names := out["location_names"].([]string)
	if len(names) != 1 || names[0] != "Karaj, Alborz Province, Iran" {
		t.Errorf("want ['Karaj, Alborz Province, Iran'], got %v", names)
	}

	// Verify result was cached using rounded coords.
	lat := roundCoord(35.8324)
	lon := roundCoord(50.9985)
	cached, found, _ := st.GetGeocodeCache(context.Background(), lat, lon)
	if !found || cached != "Karaj, Alborz Province, Iran" {
		t.Errorf("want cached result, got %q (found=%v)", cached, found)
	}
}

func TestReverseGeocoder_NominatimErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Unable to geocode"})
	}))
	defer srv.Close()

	st := newMockGeoStore()
	p := &ReverseGeocoderPipeline{
		name:  "test-geocoder",
		store: st,
		cfg: reverseGeocoderConfig{
			CoordinateKey:  "locations",
			CoordinateType: "array_of_pairs",
			OutputKey:      "location_names",
			NominatimURL:   srv.URL,
		},
		client: srv.Client(),
	}

	out, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{"locations": "[[0.0,0.0]]"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	names := out["location_names"].([]string)
	if len(names) != 1 || names[0] != "" {
		t.Errorf("want [''] for Nominatim error, got %v", names)
	}
	// Verify empty string was cached (so we don't re-query).
	lat, lon := roundCoord(0.0), roundCoord(0.0)
	cached, found, _ := st.GetGeocodeCache(context.Background(), lat, lon)
	if !found || cached != "" {
		t.Errorf("want cached empty string, got %q (found=%v)", cached, found)
	}
}

func TestReverseGeocoder_NominatimDown(t *testing.T) {
	// Point to a non-listening address; handler must not return an error.
	st := newMockGeoStore()
	p := newTestGeocoder(st, "http://127.0.0.1:19999")

	out, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{"locations": "[[35.0,51.0]]"},
	})
	if err != nil {
		t.Fatalf("Run must not fail when Nominatim is unreachable, got: %v", err)
	}
	names := out["location_names"].([]string)
	if len(names) != 1 || names[0] != "" {
		t.Errorf("want [''] when Nominatim down, got %v", names)
	}
}

func TestReverseGeocoder_CustomOutputKey(t *testing.T) {
	srv := nominatimSrv("Isfahan, Iran")
	defer srv.Close()

	st := newMockGeoStore()
	p := &ReverseGeocoderPipeline{
		name:  "test-geocoder",
		store: st,
		cfg: reverseGeocoderConfig{
			CoordinateKey:  "coords",
			CoordinateType: "array_of_pairs",
			OutputKey:      "places",
			NominatimURL:   srv.URL,
		},
		client: srv.Client(),
	}

	out, err := p.Run(context.Background(), inference.PipelineInput{
		Context: map[string]any{"coords": "[[32.6539,51.6660]]"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, ok := out["places"]; !ok {
		t.Error("want output key 'places'")
	}
	names := out["places"].([]string)
	if len(names) != 1 || names[0] != "Isfahan, Iran" {
		t.Errorf("want ['Isfahan, Iran'], got %v", names)
	}
}
