package stream

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/queue"
	"github.com/amir-the-h/iran-watcher/internal/store"
	"log/slog"
	"os"
)

// ---------------------------------------------------------------------------
// Mock implementations
// ---------------------------------------------------------------------------

type mockStore struct {
	saveTweetCalled       bool
	saveTweetSourceCalled bool
	listSourcesResult     []*model.Source
}

func (m *mockStore) SaveTweet(ctx context.Context, tweet *model.Tweet) error {
	m.saveTweetCalled = true
	return nil
}
func (m *mockStore) SaveTweetSource(ctx context.Context, ts *model.TweetSource) error {
	m.saveTweetSourceCalled = true
	return nil
}
func (m *mockStore) GetTweet(ctx context.Context, id string) (*model.Tweet, error) { return nil, nil }
func (m *mockStore) ListTweets(ctx context.Context, filter store.TweetFilter) ([]*model.Tweet, error) {
	return nil, nil
}
func (m *mockStore) SaveSource(ctx context.Context, source *model.Source) error { return nil }
func (m *mockStore) GetSource(ctx context.Context, id int64) (*model.Source, error) {
	return nil, nil
}
func (m *mockStore) ListSources(ctx context.Context, filter store.SourceFilter) ([]*model.Source, error) {
	return m.listSourcesResult, nil
}
func (m *mockStore) DeleteSource(ctx context.Context, id int64) error { return nil }
func (m *mockStore) UpdateSourceXRuleID(ctx context.Context, id int64, xRuleID string) error {
	return nil
}
func (m *mockStore) SaveModel(ctx context.Context, mod *model.Model) error { return nil }
func (m *mockStore) GetModel(ctx context.Context, id int64) (*model.Model, error) {
	return nil, nil
}
func (m *mockStore) ListModels(ctx context.Context) ([]*model.Model, error) { return nil, nil }
func (m *mockStore) DeleteModel(ctx context.Context, id int64) error        { return nil }
func (m *mockStore) SetModelActive(ctx context.Context, id int64, active bool) error {
	return nil
}
func (m *mockStore) SaveCategorization(ctx context.Context, c *model.Categorization) error {
	return nil
}
func (m *mockStore) ListCategorizations(ctx context.Context, filter store.CategorizationFilter) ([]*model.Categorization, error) {
	return nil, nil
}
func (m *mockStore) ListUncategorizedTweets(ctx context.Context, modelNames []string) ([]*model.Tweet, error) {
	return nil, nil
}
func (m *mockStore) Close() error { return nil }

type mockProducer struct {
	pushCalled bool
}

func (m *mockProducer) Push(ctx context.Context, q string, tweet *model.StreamTweet) error {
	m.pushCalled = true
	return nil
}
func (m *mockProducer) PushFailed(ctx context.Context, ft *queue.FailedTweet) error { return nil }

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

var validEventJSON = []byte(`{"data":{"id":"1234567890","text":"Some tweet text","author_id":"987654321","created_at":"2026-03-23T12:00:00.000Z"},"includes":{"users":[{"id":"987654321","username":"handle_without_at"}]},"matching_rules":[{"id":"rule_id_1","tag":"my_tag"}]}`)

// ---------------------------------------------------------------------------
// Tests: Normalize
// ---------------------------------------------------------------------------

func TestNormalize_ValidEvent(t *testing.T) {
	st, err := Normalize(validEventJSON)
	if err != nil {
		t.Fatalf("Normalize returned unexpected error: %v", err)
	}
	if st.Tweet.ID != "1234567890" {
		t.Errorf("ID: got %q, want %q", st.Tweet.ID, "1234567890")
	}
	if st.Tweet.AuthorID != "987654321" {
		t.Errorf("AuthorID: got %q, want %q", st.Tweet.AuthorID, "987654321")
	}
	if st.Tweet.AuthorHandle != "handle_without_at" {
		t.Errorf("AuthorHandle: got %q, want %q", st.Tweet.AuthorHandle, "handle_without_at")
	}
	if st.Tweet.Text != "Some tweet text" {
		t.Errorf("Text: got %q, want %q", st.Tweet.Text, "Some tweet text")
	}
	if st.Tweet.FetchedAt.IsZero() {
		t.Error("FetchedAt should not be zero")
	}
	wantCreated := time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)
	if !st.Tweet.CreatedAt.Equal(wantCreated) {
		t.Errorf("CreatedAt: got %v, want %v", st.Tweet.CreatedAt, wantCreated)
	}
	if len(st.MatchingRules) != 1 {
		t.Fatalf("MatchingRules len: got %d, want 1", len(st.MatchingRules))
	}
	if st.MatchingRules[0].ID != "rule_id_1" || st.MatchingRules[0].Tag != "my_tag" {
		t.Errorf("MatchingRules[0]: got %+v", st.MatchingRules[0])
	}
	if len(st.Tweet.RawJSON) == 0 {
		t.Error("RawJSON should not be empty")
	}
}

func TestNormalize_MissingUserFallback(t *testing.T) {
	data := []byte(`{"data":{"id":"111","text":"hello","author_id":"999","created_at":"2026-03-23T12:00:00.000Z"},"includes":{"users":[]},"matching_rules":[]}`)
	st, err := Normalize(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if st.Tweet.AuthorHandle != "999" {
		t.Errorf("AuthorHandle fallback: got %q, want %q", st.Tweet.AuthorHandle, "999")
	}
}

func TestNormalize_MalformedJSON(t *testing.T) {
	_, err := Normalize([]byte(`{not valid json`))
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}

// ---------------------------------------------------------------------------
// Tests: Client
// ---------------------------------------------------------------------------

// sseEvent formats a single SSE data line followed by a blank line.
func sseEvent(data string) string {
	return fmt.Sprintf("data: %s\n\n", data)
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func TestClient_Start_ProcessesEvent(t *testing.T) {
	ms := &mockStore{
		listSourcesResult: []*model.Source{
			{ID: 1, XRuleID: "rule_id_1", Value: "iran", Active: true},
		},
	}
	mp := &mockProducer{}

	// Serve a single SSE event then keep the connection open briefly.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseEvent(string(validEventJSON)))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Hold connection open long enough for the client to read the event.
		time.Sleep(2 * time.Second)
	}))
	defer srv.Close()

	rm := NewRuleManager("token", srv.Client())
	rm.apiBase = srv.URL

	c := NewClient("token", ms, mp, rm, newTestLogger())
	// Override the stream URL to point at our test server.
	c.streamURL = srv.URL + "/stream"

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- c.Start(ctx) }()

	// Wait for processing to occur or timeout.
	deadline := time.After(3 * time.Second)
	for {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Start returned error: %v", err)
			}
			goto check
		case <-deadline:
			cancel()
			<-done
			goto check
		case <-time.After(50 * time.Millisecond):
			if ms.saveTweetCalled && ms.saveTweetSourceCalled && mp.pushCalled {
				cancel()
				<-done
				goto check
			}
		}
	}
check:
	if !ms.saveTweetCalled {
		t.Error("expected SaveTweet to be called")
	}
	if !ms.saveTweetSourceCalled {
		t.Error("expected SaveTweetSource to be called")
	}
	if !mp.pushCalled {
		t.Error("expected Producer.Push to be called")
	}
}

func TestClient_Start_ReconnectsAfterDisconnect(t *testing.T) {
	var requestCount int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		// Close the connection immediately every time.
		// The client should reconnect.
	}))
	defer srv.Close()

	ms := &mockStore{}
	mp := &mockProducer{}

	rm := NewRuleManager("token", srv.Client())
	rm.apiBase = srv.URL

	c := NewClient("token", ms, mp, rm, newTestLogger())
	c.streamURL = srv.URL + "/stream"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- c.Start(ctx) }()

	// Wait until at least 2 requests have been made (reconnect happened).
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			cancel()
			<-done
			if atomic.LoadInt32(&requestCount) < 2 {
				t.Errorf("expected at least 2 requests (reconnect), got %d", atomic.LoadInt32(&requestCount))
			}
			return
		case <-time.After(100 * time.Millisecond):
			if atomic.LoadInt32(&requestCount) >= 2 {
				cancel()
				<-done
				return
			}
		}
	}
}
