package queue

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/redis/go-redis/v9"
)

// ---------------------------------------------------------------------------
// Test 1: FailedTweet JSON round-trip
// ---------------------------------------------------------------------------

func TestFailedTweet_JSONRoundTrip(t *testing.T) {
	original := &FailedTweet{
		Tweet: &model.StreamTweet{
			Tweet: &model.Tweet{
				ID:           "123",
				AuthorID:     "456",
				AuthorHandle: "testuser",
				Text:         "Hello Iran",
			},
			MatchingRules: []model.MatchingRule{
				{ID: "rule1", Tag: "iran"},
			},
		},
		FailedModels: []FailedModel{
			{Name: "sentiment", Error: "model not loaded", Timestamp: "2026-03-23T00:00:00Z"},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got FailedTweet
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Tweet == nil || got.Tweet.Tweet == nil {
		t.Fatal("tweet is nil after round-trip")
	}
	if got.Tweet.Tweet.ID != original.Tweet.Tweet.ID {
		t.Errorf("tweet ID: got %q, want %q", got.Tweet.Tweet.ID, original.Tweet.Tweet.ID)
	}
	if got.Tweet.Tweet.Text != original.Tweet.Tweet.Text {
		t.Errorf("tweet text: got %q, want %q", got.Tweet.Tweet.Text, original.Tweet.Tweet.Text)
	}
	if len(got.Tweet.MatchingRules) != 1 {
		t.Fatalf("matching rules: got %d, want 1", len(got.Tweet.MatchingRules))
	}
	if got.Tweet.MatchingRules[0].Tag != "iran" {
		t.Errorf("matching rule tag: got %q, want %q", got.Tweet.MatchingRules[0].Tag, "iran")
	}
	if len(got.FailedModels) != 1 {
		t.Fatalf("failed models: got %d, want 1", len(got.FailedModels))
	}
	if got.FailedModels[0].Name != "sentiment" {
		t.Errorf("failed model name: got %q, want %q", got.FailedModels[0].Name, "sentiment")
	}
	if got.FailedModels[0].Error != "model not loaded" {
		t.Errorf("failed model error: got %q, want %q", got.FailedModels[0].Error, "model not loaded")
	}
}

// ---------------------------------------------------------------------------
// Test 2: StreamTweet JSON round-trip
// ---------------------------------------------------------------------------

func TestStreamTweet_JSONRoundTrip(t *testing.T) {
	now := time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)
	original := &model.StreamTweet{
		Tweet: &model.Tweet{
			ID:           "789",
			AuthorID:     "101",
			AuthorHandle: "newsbot",
			Text:         "Breaking: Iran nuclear talks",
			CreatedAt:    now,
			FetchedAt:    now,
		},
		MatchingRules: []model.MatchingRule{
			{ID: "r1", Tag: "nuclear"},
			{ID: "r2", Tag: "iran"},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got model.StreamTweet
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Tweet == nil {
		t.Fatal("tweet is nil after round-trip")
	}
	if got.Tweet.ID != original.Tweet.ID {
		t.Errorf("ID: got %q, want %q", got.Tweet.ID, original.Tweet.ID)
	}
	if got.Tweet.AuthorHandle != original.Tweet.AuthorHandle {
		t.Errorf("AuthorHandle: got %q, want %q", got.Tweet.AuthorHandle, original.Tweet.AuthorHandle)
	}
	if got.Tweet.Text != original.Tweet.Text {
		t.Errorf("Text: got %q, want %q", got.Tweet.Text, original.Tweet.Text)
	}
	if !got.Tweet.CreatedAt.Equal(original.Tweet.CreatedAt) {
		t.Errorf("CreatedAt: got %v, want %v", got.Tweet.CreatedAt, original.Tweet.CreatedAt)
	}
	if len(got.MatchingRules) != 2 {
		t.Fatalf("MatchingRules: got %d, want 2", len(got.MatchingRules))
	}
}

// ---------------------------------------------------------------------------
// Test 3: Pop returns (nil, nil) on timeout (redis.Nil error from BRPop)
// ---------------------------------------------------------------------------

// fakeRedisClient is a minimal redisClient implementation for testing.
type fakeRedisClient struct {
	brpopErr    error
	brpopResult []string
}

func (f *fakeRedisClient) LPush(_ context.Context, _ string, _ ...interface{}) *redis.IntCmd {
	return redis.NewIntResult(0, nil)
}

func (f *fakeRedisClient) BRPop(_ context.Context, _ time.Duration, _ ...string) *redis.StringSliceCmd {
	return redis.NewStringSliceResult(f.brpopResult, f.brpopErr)
}

func TestPop_ReturnsNilOnTimeout(t *testing.T) {
	fake := &fakeRedisClient{
		brpopErr:    redis.Nil, // BRPop returns redis.Nil on timeout
		brpopResult: nil,
	}
	q := &RedisQueue{client: fake}

	tweet, err := q.Pop(context.Background(), QueueRaw, 1*time.Second)
	if err != nil {
		t.Fatalf("expected nil error on timeout, got: %v", err)
	}
	if tweet != nil {
		t.Fatalf("expected nil tweet on timeout, got: %+v", tweet)
	}
}

func TestPopFailed_ReturnsNilOnTimeout(t *testing.T) {
	fake := &fakeRedisClient{
		brpopErr:    redis.Nil,
		brpopResult: nil,
	}
	q := &RedisQueue{client: fake}

	ft, err := q.PopFailed(context.Background(), 1*time.Second)
	if err != nil {
		t.Fatalf("expected nil error on timeout, got: %v", err)
	}
	if ft != nil {
		t.Fatalf("expected nil FailedTweet on timeout, got: %+v", ft)
	}
}

func TestPop_ReturnsNilOnEmptyResult(t *testing.T) {
	// BRPop can also return an empty slice with no error in some edge cases.
	fake := &fakeRedisClient{
		brpopErr:    nil,
		brpopResult: []string{}, // empty — fewer than 2 elements
	}
	q := &RedisQueue{client: fake}

	tweet, err := q.Pop(context.Background(), QueueRaw, 1*time.Second)
	if err != nil {
		t.Fatalf("expected nil error on empty result, got: %v", err)
	}
	if tweet != nil {
		t.Fatalf("expected nil tweet on empty result, got: %+v", tweet)
	}
}

