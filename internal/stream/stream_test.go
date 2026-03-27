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
	saveTweetCalled     bool
	saveTweetRuleCalled bool
	rulesByXRuleID      map[string]*model.Rule
}

func (m *mockStore) SaveTweet(_ context.Context, _ *model.Tweet) error {
	m.saveTweetCalled = true
	return nil
}
func (m *mockStore) GetRuleByXRuleID(_ context.Context, xRuleID string) (*model.Rule, error) {
	if r, ok := m.rulesByXRuleID[xRuleID]; ok {
		return r, nil
	}
	return nil, fmt.Errorf("rule not found: %s", xRuleID)
}
func (m *mockStore) SaveTweetRule(_ context.Context, _ *model.TweetRule) error {
	m.saveTweetRuleCalled = true
	return nil
}
func (m *mockStore) GetTweet(_ context.Context, _ string) (*model.Tweet, error)  { return nil, nil }
func (m *mockStore) ListTweets(_ context.Context, _ store.TweetFilter) ([]*model.Tweet, error) {
	return nil, nil
}
func (m *mockStore) SaveSource(_ context.Context, _ *model.Source) error { return nil }
func (m *mockStore) GetSource(_ context.Context, _ int64) (*model.Source, error) {
	return nil, nil
}
func (m *mockStore) ListSources(_ context.Context, _ store.SourceFilter) ([]*model.Source, error) {
	return nil, nil
}
func (m *mockStore) DeleteSource(_ context.Context, _ int64) error                        { return nil }
func (m *mockStore) SetSourceActive(_ context.Context, _ int64, _ bool) error             { return nil }
func (m *mockStore) ActiveRuleRefCount(_ context.Context, _ int64) (int, error)           { return 0, nil }
func (m *mockStore) RuleRefCount(_ context.Context, _ int64) (int, error)                 { return 0, nil }
func (m *mockStore) SaveRule(_ context.Context, _ *model.Rule) error                      { return nil }
func (m *mockStore) GetRule(_ context.Context, _ int64) (*model.Rule, error)              { return nil, nil }
func (m *mockStore) ListRules(_ context.Context, _ store.RuleFilter) ([]*model.Rule, error) {
	return nil, nil
}
func (m *mockStore) UpdateRule(_ context.Context, _ *model.Rule) error                    { return nil }
func (m *mockStore) DeleteRule(_ context.Context, _ int64) error                          { return nil }
func (m *mockStore) SetRuleActive(_ context.Context, _ int64, _ bool, _ string) error     { return nil }
func (m *mockStore) TweetRuleCount(_ context.Context, _ int64) (int, error)               { return 0, nil }
func (m *mockStore) SaveModel(_ context.Context, _ *model.Model) error                    { return nil }
func (m *mockStore) GetModel(_ context.Context, _ int64) (*model.Model, error)            { return nil, nil }
func (m *mockStore) ListModels(_ context.Context) ([]*model.Model, error)                 { return nil, nil }
func (m *mockStore) DeleteModel(_ context.Context, _ int64) error                         { return nil }
func (m *mockStore) SetModelActive(_ context.Context, _ int64, _ bool) error              { return nil }
func (m *mockStore) SaveCategorization(_ context.Context, _ *model.Categorization) error  { return nil }
func (m *mockStore) ListCategorizations(_ context.Context, _ store.CategorizationFilter) ([]*model.Categorization, error) {
	return nil, nil
}
func (m *mockStore) ListUncategorizedTweets(_ context.Context, _ []string) ([]*model.Tweet, error) {
	return nil, nil
}
func (m *mockStore) SaveNotification(_ context.Context, _ *model.Notification) error { return nil }
func (m *mockStore) GetNotification(_ context.Context, _ int64) (*model.Notification, error) {
	return nil, nil
}
func (m *mockStore) ListNotifications(_ context.Context, _ store.NotificationFilter) ([]*model.Notification, error) {
	return nil, nil
}
func (m *mockStore) UpdateNotification(_ context.Context, _ *model.Notification) error { return nil }
func (m *mockStore) DeleteNotification(_ context.Context, _ int64) error               { return nil }
func (m *mockStore) SetNotificationActive(_ context.Context, _ int64, _ bool) error    { return nil }
func (m *mockStore) AddModelRule(_ context.Context, _, _ int64) error                  { return nil }
func (m *mockStore) RemoveModelRule(_ context.Context, _, _ int64) error               { return nil }
func (m *mockStore) ListTweetRuleIDs(_ context.Context, _ string) ([]int64, error)     { return nil, nil }
func (m *mockStore) CountEntityTracks(_ context.Context, _ store.EntityTrackFilter) (int, error) { return 0, nil }
func (m *mockStore) ListModelLabels(_ context.Context, _ string) ([]string, error)     { return nil, nil }
func (m *mockStore) AddModelLabel(_ context.Context, _, _ string) error                { return nil }
func (m *mockStore) RemoveModelLabel(_ context.Context, _, _ string) error             { return nil }
func (m *mockStore) SaveUser(_ context.Context, _ *model.User) error                   { return nil }
func (m *mockStore) GetUser(_ context.Context, _ int64) (*model.User, error)           { return nil, nil }
func (m *mockStore) GetUserByUsername(_ context.Context, _ string) (*model.User, error) { return nil, nil }
func (m *mockStore) ListUsers(_ context.Context) ([]*model.User, error)                { return nil, nil }
func (m *mockStore) UpdateUser(_ context.Context, _ *model.User) error                 { return nil }
func (m *mockStore) DeleteUser(_ context.Context, _ int64) error                       { return nil }
func (m *mockStore) ListSummaries(_ context.Context, _ store.SummaryFilter) ([]*model.SummaryItem, error) { return nil, nil }
func (m *mockStore) CountSummaries(_ context.Context, _ store.SummaryFilter) (int, error) { return 0, nil }
func (m *mockStore) CountTweets(_ context.Context, _ store.TweetFilter) (int, error) { return 0, nil }
func (m *mockStore) DeleteTweets(_ context.Context, _ []string) (int64, error)       { return 0, nil }
func (m *mockStore) DeleteNoiseTweets(_ context.Context) (int64, error)              { return 0, nil }
func (m *mockStore) UpdateSource(_ context.Context, _ *model.Source) error           { return nil }
func (m *mockStore) ListRulesDependingOn(_ context.Context, _ int64) ([]*model.Rule, error) { return nil, nil }
func (m *mockStore) SaveTrackedEntity(_ context.Context, _ *model.TrackedEntity) error { return nil }
func (m *mockStore) GetTrackedEntity(_ context.Context, _ int64) (*model.TrackedEntity, error) { return nil, fmt.Errorf("not found") }
func (m *mockStore) ListTrackedEntities(_ context.Context, _ store.TrackedEntityFilter) ([]*model.TrackedEntity, error) { return nil, nil }
func (m *mockStore) UpdateTrackedEntity(_ context.Context, _ *model.TrackedEntity) error { return nil }
func (m *mockStore) DeleteTrackedEntity(_ context.Context, _ int64) error            { return nil }
func (m *mockStore) SetTrackedEntityActive(_ context.Context, _ int64, _ bool) error { return nil }
func (m *mockStore) ListActiveTrackedEntities(_ context.Context) ([]*model.TrackedEntity, error) { return nil, nil }
func (m *mockStore) SaveEntityTrack(_ context.Context, _ *model.EntityTrack) error  { return nil }
func (m *mockStore) ListEntityTracks(_ context.Context, _ store.EntityTrackFilter) ([]*model.EntityTrack, error) { return nil, nil }
func (m *mockStore) ListNERSuggestionsForEntity(_ context.Context, _ int64) ([]string, error) { return nil, nil }
func (m *mockStore) SearchNERSuggestions(_ context.Context, _, _ string) ([]string, error) { return nil, nil }
func (m *mockStore) SaveDigestJob(_ context.Context, _ *model.DigestJob) error       { return nil }
func (m *mockStore) GetDigestJob(_ context.Context, _ string) (*model.DigestJob, error) { return nil, fmt.Errorf("not found") }
func (m *mockStore) UpdateDigestJob(_ context.Context, _ *model.DigestJob) error     { return nil }
func (m *mockStore) ListDigestJobs(_ context.Context, _ int) ([]*model.DigestJob, error) { return nil, nil }
func (m *mockStore) DeleteDigestJob(_ context.Context, _ string) error               { return nil }
func (m *mockStore) SaveSearchJob(_ context.Context, _ *model.SearchJob) error       { return nil }
func (m *mockStore) GetSearchJob(_ context.Context, _ string) (*model.SearchJob, error) { return nil, fmt.Errorf("not found") }
func (m *mockStore) UpdateSearchJob(_ context.Context, _ *model.SearchJob) error     { return nil }
func (m *mockStore) ListSearchJobs(_ context.Context, _ int) ([]*model.SearchJob, error) { return nil, nil }
func (m *mockStore) DeleteSearchJob(_ context.Context, _ string) error               { return nil }
func (m *mockStore) CountAdmins(_ context.Context) (int, error)                        { return 1, nil }
func (m *mockStore) SaveAction(_ context.Context, a *model.Action) error               { return nil }
func (m *mockStore) GetAction(_ context.Context, _ int64) (*model.Action, error)       { return nil, fmt.Errorf("not found") }
func (m *mockStore) ListActions(_ context.Context, _ store.ActionFilter) ([]*model.Action, error) { return nil, nil }
func (m *mockStore) UpdateAction(_ context.Context, _ *model.Action) error             { return nil }
func (m *mockStore) DeleteAction(_ context.Context, _ int64) error                     { return nil }
func (m *mockStore) SetActionActive(_ context.Context, _ int64, _ bool) error          { return nil }
func (m *mockStore) SaveActionExecution(_ context.Context, _ *model.ActionExecution) error { return nil }
func (m *mockStore) ListActionExecutions(_ context.Context, _ store.ExecutionFilter) ([]*model.ActionExecution, error) { return nil, nil }
func (m *mockStore) ListActionExecutionMetadata(_ context.Context, _ int64) (map[string]string, error) { return nil, nil }
func (m *mockStore) SaveActionExecutionMetadata(_ context.Context, _ int64, _, _ string) error { return nil }
func (m *mockStore) SaveTwitterCredentials(_ context.Context, _ *model.TwitterCredentials) error { return nil }
func (m *mockStore) GetTwitterCredentials(_ context.Context) (*model.TwitterCredentials, error) { return nil, fmt.Errorf("not found") }
func (m *mockStore) DeleteTwitterCredentials(_ context.Context) error                  { return nil }
func (m *mockStore) Close() error                                                       { return nil }

type mockProducer struct {
	pushCalled bool
}

func (m *mockProducer) Push(_ context.Context, _ string, _ *model.StreamTweet) error {
	m.pushCalled = true
	return nil
}
func (m *mockProducer) PushFailed(_ context.Context, _ *queue.FailedTweet) error { return nil }

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
	if st.Tweet.Text != "GeoConfirmed Iran.\n\nFull original text with coordinates 36.42, 43.10\n\nGeoLocated by\n@someuser" {
		t.Errorf("expected full original text, got: %q", st.Tweet.Text)
	}
}

func TestNormalize_NonRetweetKeepsText(t *testing.T) {
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
	if st.Tweet.Text != "This is a direct tweet, not an RT" {
		t.Errorf("non-retweet text should be unchanged, got: %q", st.Tweet.Text)
	}
}

// ---------------------------------------------------------------------------
// Tests: Client
// ---------------------------------------------------------------------------

func sseEvent(data string) string {
	return fmt.Sprintf("data: %s\n\n", data)
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func TestClient_Start_ProcessesEvent(t *testing.T) {
	ms := &mockStore{
		rulesByXRuleID: map[string]*model.Rule{
			"rule_id_1": {ID: 1, Name: "test-rule", XRuleID: "rule_id_1", Active: true},
		},
	}
	mp := &mockProducer{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseEvent(string(validEventJSON)))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		time.Sleep(2 * time.Second)
	}))
	defer srv.Close()

	rm := NewRuleManager("token", srv.Client())
	rm.apiBase = srv.URL

	c := NewClient("token", ms, mp, rm, nil, newTestLogger())
	c.streamURL = srv.URL + "/stream"

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- c.Start(ctx) }()

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
			if ms.saveTweetCalled && ms.saveTweetRuleCalled && mp.pushCalled {
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
	if !ms.saveTweetRuleCalled {
		t.Error("expected SaveTweetRule to be called")
	}
	if !mp.pushCalled {
		t.Error("expected Producer.Push to be called")
	}
}

func TestClient_Start_ReconnectsAfterDisconnect(t *testing.T) {
	var requestCount int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
	}))
	defer srv.Close()

	ms := &mockStore{rulesByXRuleID: map[string]*model.Rule{}}
	mp := &mockProducer{}

	rm := NewRuleManager("token", srv.Client())
	rm.apiBase = srv.URL

	c := NewClient("token", ms, mp, rm, nil, newTestLogger())
	c.streamURL = srv.URL + "/stream"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- c.Start(ctx) }()

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
