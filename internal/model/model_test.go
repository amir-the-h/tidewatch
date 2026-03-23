package model

import (
	"encoding/json"
	"testing"
	"time"
)

// Test 1: Verify SourceType constants have correct string values.
func TestSourceTypeConstants(t *testing.T) {
	if SourceTypeAccount != "account" {
		t.Errorf("SourceTypeAccount = %q, want %q", SourceTypeAccount, "account")
	}
	if SourceTypeKeyword != "keyword" {
		t.Errorf("SourceTypeKeyword = %q, want %q", SourceTypeKeyword, "keyword")
	}
}

// Test 2: Verify Tweet JSON marshal/unmarshal round-trips without data loss,
// and that RawJSON is excluded from JSON output.
func TestTweetJSONRoundTrip(t *testing.T) {
	createdAt := time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)
	fetchedAt := time.Date(2026, 3, 23, 12, 1, 0, 0, time.UTC)

	original := &Tweet{
		ID:           "tweet-123",
		AuthorID:     "author-456",
		AuthorHandle: "somehandle",
		Text:         "Hello, world!",
		RawJSON:      []byte(`{"raw":"data"}`),
		CreatedAt:    createdAt,
		FetchedAt:    fetchedAt,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	// Verify RawJSON is excluded from the JSON output.
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal into map failed: %v", err)
	}
	if _, ok := raw["raw_json"]; ok {
		t.Error("RawJSON should not appear in JSON output but found key 'raw_json'")
	}
	if _, ok := raw["RawJSON"]; ok {
		t.Error("RawJSON should not appear in JSON output but found key 'RawJSON'")
	}

	// Unmarshal back and verify fields.
	var got Tweet
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal into Tweet failed: %v", err)
	}

	if got.ID != original.ID {
		t.Errorf("ID = %q, want %q", got.ID, original.ID)
	}
	if got.AuthorID != original.AuthorID {
		t.Errorf("AuthorID = %q, want %q", got.AuthorID, original.AuthorID)
	}
	if got.AuthorHandle != original.AuthorHandle {
		t.Errorf("AuthorHandle = %q, want %q", got.AuthorHandle, original.AuthorHandle)
	}
	if got.Text != original.Text {
		t.Errorf("Text = %q, want %q", got.Text, original.Text)
	}
	if !got.CreatedAt.Equal(original.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, original.CreatedAt)
	}
	if !got.FetchedAt.Equal(original.FetchedAt) {
		t.Errorf("FetchedAt = %v, want %v", got.FetchedAt, original.FetchedAt)
	}
	// RawJSON should be nil/empty after unmarshal since it is excluded.
	if len(got.RawJSON) != 0 {
		t.Errorf("RawJSON should be empty after unmarshal, got %q", got.RawJSON)
	}
}

// Test 3: Verify StreamTweet can be created with a Tweet pointer and MatchingRules slice.
func TestStreamTweetCreation(t *testing.T) {
	tweet := &Tweet{
		ID:   "tweet-789",
		Text: "Iran news",
	}
	rules := []MatchingRule{
		{ID: "rule-1", Tag: "iran"},
		{ID: "rule-2", Tag: "tehran"},
	}

	st := StreamTweet{
		Tweet:         tweet,
		MatchingRules: rules,
	}

	if st.Tweet != tweet {
		t.Error("StreamTweet.Tweet pointer does not match original")
	}
	if len(st.MatchingRules) != 2 {
		t.Errorf("MatchingRules len = %d, want 2", len(st.MatchingRules))
	}
	if st.MatchingRules[0].ID != "rule-1" || st.MatchingRules[0].Tag != "iran" {
		t.Errorf("MatchingRules[0] = %+v, want {ID:rule-1 Tag:iran}", st.MatchingRules[0])
	}
	if st.MatchingRules[1].ID != "rule-2" || st.MatchingRules[1].Tag != "tehran" {
		t.Errorf("MatchingRules[1] = %+v, want {ID:rule-2 Tag:tehran}", st.MatchingRules[1])
	}
}

// Test 4: Verify Categorization.Results preserves raw JSON exactly without re-encoding artifacts.
func TestCategorizationResultsPreservesRawJSON(t *testing.T) {
	rawResults := json.RawMessage(`{"label":"test","score":0.95}`)
	createdAt := time.Date(2026, 3, 23, 10, 0, 0, 0, time.UTC)

	original := Categorization{
		ID:           1,
		TweetID:      "tweet-001",
		ModelName:    "sentiment-model",
		ModelVersion: "v1.0",
		Results:      rawResults,
		CreatedAt:    createdAt,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var got Categorization
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if string(got.Results) != string(rawResults) {
		t.Errorf("Results = %q, want %q", string(got.Results), string(rawResults))
	}
}
