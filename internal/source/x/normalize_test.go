package x

import (
	"testing"
	"time"

	"github.com/amir-the-h/tidewatch/internal/model"
)

var validEventJSON = []byte(`{"data":{"id":"1234567890","text":"Some tweet text","author_id":"987654321","created_at":"2026-03-23T12:00:00.000Z"},"includes":{"users":[{"id":"987654321","username":"handle_without_at"}]},"matching_rules":[{"id":"rule_id_1","tag":"my_tag"}]}`)

// ---------------------------------------------------------------------------
// Tests: Normalize
// ---------------------------------------------------------------------------

func TestNormalize_ValidEvent(t *testing.T) {
	st, err := Normalize(validEventJSON)
	if err != nil {
		t.Fatalf("Normalize returned unexpected error: %v", err)
	}
	if st.Signal.ExternalID != "1234567890" || st.Signal.ExtNS != ExtNS || st.Signal.Kind != model.SignalKindPost {
		t.Errorf("identity: got %s/%s/%s", st.Signal.Kind, st.Signal.ExtNS, st.Signal.ExternalID)
	}
	if st.Signal.AuthorID != "987654321" {
		t.Errorf("AuthorID: got %q, want %q", st.Signal.AuthorID, "987654321")
	}
	if st.Signal.Author != "handle_without_at" {
		t.Errorf("Author: got %q, want %q", st.Signal.Author, "handle_without_at")
	}
	if st.Signal.URL != "https://x.com/handle_without_at/status/1234567890" {
		t.Errorf("URL: got %q", st.Signal.URL)
	}
	if st.Signal.Attrs["x_tweet_id"] != "1234567890" || st.Signal.Attrs["x_author_id"] != "987654321" {
		t.Errorf("attrs: got %v", st.Signal.Attrs)
	}
	if st.Signal.Text != "Some tweet text" {
		t.Errorf("Text: got %q, want %q", st.Signal.Text, "Some tweet text")
	}
	if st.Signal.FetchedAt.IsZero() {
		t.Error("FetchedAt should not be zero")
	}
	wantCreated := time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)
	if !st.Signal.PublishedAt.Equal(wantCreated) {
		t.Errorf("CreatedAt: got %v, want %v", st.Signal.PublishedAt, wantCreated)
	}
	if len(st.MatchingRules) != 1 {
		t.Fatalf("MatchingRules len: got %d, want 1", len(st.MatchingRules))
	}
	if st.MatchingRules[0].ID != "rule_id_1" || st.MatchingRules[0].Tag != "my_tag" {
		t.Errorf("MatchingRules[0]: got %+v", st.MatchingRules[0])
	}
	if len(st.Signal.RawJSON) == 0 {
		t.Error("RawJSON should not be empty")
	}
}

func TestNormalize_MissingUserFallback(t *testing.T) {
	data := []byte(`{"data":{"id":"111","text":"hello","author_id":"999","created_at":"2026-03-23T12:00:00.000Z"},"includes":{"users":[]},"matching_rules":[]}`)
	st, err := Normalize(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if st.Signal.Author != "999" {
		t.Errorf("Author fallback: got %q, want %q", st.Signal.Author, "999")
	}
}

func TestNormalize_MalformedJSON(t *testing.T) {
	_, err := Normalize([]byte(`{not valid json`))
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}

func TestNormalize_RetweetExpandsFullText(t *testing.T) {
	data := []byte(`{
		"data": {
			"id": "999",
			"text": "RT @GeoConfirmed: GeoConfirmed Iran.\n\nShort truncated text\u2026",
			"author_id": "111",
			"created_at": "2026-03-23T12:00:00.000Z",
			"referenced_tweets": [{"id": "888", "type": "retweeted"}]
		},
		"includes": {
			"users": [{"id": "111", "username": "retweeter"}],
			"tweets": [{"id": "888", "text": "GeoConfirmed Iran.\n\nFull original text with coordinates 36.42, 43.10\n\nGeoLocated by\n@someuser"}]
		},
		"matching_rules": []
	}`)

	st, err := Normalize(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if st.Signal.Text != "GeoConfirmed Iran.\n\nFull original text with coordinates 36.42, 43.10\n\nGeoLocated by\n@someuser" {
		t.Errorf("expected full original text, got: %q", st.Signal.Text)
	}
}

func TestNormalize_NoteTweetUsesFullText(t *testing.T) {
	data := []byte(`{
		"data": {
			"id": "1001",
			"text": "This is the first 280 characters of a long note tweet\u2026",
			"author_id": "111",
			"created_at": "2026-03-23T12:00:00.000Z",
			"note_tweet": {"text": "This is the complete full text of a long note tweet that exceeds 280 characters and contains much more detail."}
		},
		"includes": {
			"users": [{"id": "111", "username": "author"}]
		},
		"matching_rules": []
	}`)

	st, err := Normalize(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "This is the complete full text of a long note tweet that exceeds 280 characters and contains much more detail."
	if st.Signal.Text != want {
		t.Errorf("expected note_tweet full text, got: %q", st.Signal.Text)
	}
}

func TestNormalize_RetweetOfNoteTweetUsesFullText(t *testing.T) {
	data := []byte(`{
		"data": {
			"id": "1002",
			"text": "RT @author: This is the first 280 characters\u2026",
			"author_id": "222",
			"created_at": "2026-03-23T12:00:00.000Z",
			"referenced_tweets": [{"id": "1001", "type": "retweeted"}]
		},
		"includes": {
			"users": [{"id": "222", "username": "retweeter"}],
			"tweets": [{
				"id": "1001",
				"text": "This is the first 280 characters\u2026",
				"note_tweet": {"text": "Full text of the retweeted note tweet with all its details intact."}
			}]
		},
		"matching_rules": []
	}`)

	st, err := Normalize(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "Full text of the retweeted note tweet with all its details intact."
	if st.Signal.Text != want {
		t.Errorf("expected full note_tweet text from retweet, got: %q", st.Signal.Text)
	}
}

func TestNormalize_QuoteTweetOnlyUsesQuoteText(t *testing.T) {
	data := []byte(`{
		"data": {
			"id": "777",
			"text": "This is a direct tweet, not an RT",
			"author_id": "111",
			"created_at": "2026-03-23T12:00:00.000Z",
			"referenced_tweets": [{"id": "666", "type": "quoted"}]
		},
		"includes": {
			"users": [{"id": "111", "username": "author"}],
			"tweets": [{"id": "666", "text": "quoted tweet text"}]
		},
		"matching_rules": []
	}`)

	st, err := Normalize(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "This is a direct tweet, not an RT"
	if st.Signal.Text != want {
		t.Errorf("quote tweet should only use the quote's own text, got: %q", st.Signal.Text)
	}
}

func TestNormalize_ReferencedAttr(t *testing.T) {
	data := []byte(`{"data":{"id":"1","text":"q","author_id":"2","created_at":"2026-03-23T12:00:00.000Z",
		"referenced_tweets":[{"id":"666","type":"quoted"}]},"includes":{"users":[{"id":"2","username":"u"}]}}`)
	evt, err := Normalize(data)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	ref, _ := evt.Signal.Attrs["x_referenced"].(map[string]string)
	if ref["quoted"] != "666" {
		t.Errorf("x_referenced: got %v", evt.Signal.Attrs["x_referenced"])
	}
}
