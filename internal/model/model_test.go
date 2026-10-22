package model

import (
	"encoding/json"
	"testing"
	"time"
)

// Test 1: Verify SourceType constants have correct string values.
func TestXTermTypeConstants(t *testing.T) {
	if XTermTypeAccount != "account" {
		t.Errorf("XTermTypeAccount = %q, want %q", XTermTypeAccount, "account")
	}
	if XTermTypeKeyword != "keyword" {
		t.Errorf("XTermTypeKeyword = %q, want %q", XTermTypeKeyword, "keyword")
	}
}

// Test 2: Verify Signal JSON marshal/unmarshal round-trips without data loss,
// and that RawJSON is excluded from JSON output.
func TestSignalJSONRoundTrip(t *testing.T) {
	publishedAt := time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)
	fetchedAt := time.Date(2026, 3, 23, 12, 1, 0, 0, time.UTC)
	lat := 35.7

	original := &Signal{
		ID:          7,
		Kind:        SignalKindPost,
		ExtNS:       "x",
		ExternalID:  "tweet-123",
		Author:      "somehandle",
		AuthorID:    "author-456",
		Text:        "Hello, world!",
		Lat:         &lat,
		Attrs:       map[string]any{"x_tweet_id": "tweet-123"},
		RawJSON:     []byte(`{"raw":"data"}`),
		PublishedAt: publishedAt,
		FetchedAt:   fetchedAt,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	// Verify RawJSON is excluded and nil coordinates are omitted.
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal into map failed: %v", err)
	}
	for _, k := range []string{"raw_json", "RawJSON", "lon"} {
		if _, ok := raw[k]; ok {
			t.Errorf("unexpected key %q in JSON output", k)
		}
	}

	var got Signal
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal into Signal failed: %v", err)
	}
	if got.ID != original.ID || got.Kind != original.Kind || got.ExternalID != original.ExternalID {
		t.Errorf("identity = %d/%s/%s", got.ID, got.Kind, got.ExternalID)
	}
	if got.Author != original.Author || got.Text != original.Text {
		t.Errorf("author/text = %q/%q", got.Author, got.Text)
	}
	if got.Lat == nil || *got.Lat != lat || got.Lon != nil {
		t.Errorf("lat/lon = %v/%v", got.Lat, got.Lon)
	}
	if got.Attrs["x_tweet_id"] != "tweet-123" {
		t.Errorf("attrs = %v", got.Attrs)
	}
	if !got.PublishedAt.Equal(original.PublishedAt) || !got.FetchedAt.Equal(original.FetchedAt) {
		t.Errorf("times = %v/%v", got.PublishedAt, got.FetchedAt)
	}
	if len(got.RawJSON) != 0 {
		t.Errorf("RawJSON should be empty after unmarshal, got %q", got.RawJSON)
	}
}
