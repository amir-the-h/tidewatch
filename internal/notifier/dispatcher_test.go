package notifier

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

// --- minimal mock store --------------------------------------------------

type mockStore struct {
	mu            sync.Mutex
	notifications map[int64]*model.Notification
	nextID        int64
}

func newMockStore() *mockStore {
	return &mockStore{
		notifications: make(map[int64]*model.Notification),
		nextID:        1,
	}
}

func (m *mockStore) addNotification(n *model.Notification) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if n.ID == 0 {
		n.ID = m.nextID
		m.nextID++
	}
	m.notifications[n.ID] = n
}

func (m *mockStore) SaveNotification(_ context.Context, n *model.Notification) error {
	m.addNotification(n)
	return nil
}

func (m *mockStore) GetNotification(_ context.Context, id int64) (*model.Notification, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, ok := m.notifications[id]
	if !ok {
		return nil, fmt.Errorf("notification %d not found", id)
	}
	return n, nil
}

func (m *mockStore) ListNotifications(_ context.Context, f store.NotificationFilter) ([]*model.Notification, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*model.Notification
	for _, n := range m.notifications {
		if f.EventType != nil && n.EventType != *f.EventType {
			continue
		}
		if f.Active != nil && n.Active != *f.Active {
			continue
		}
		out = append(out, n)
	}
	return out, nil
}

func (m *mockStore) UpdateNotification(_ context.Context, n *model.Notification) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notifications[n.ID] = n
	return nil
}

func (m *mockStore) DeleteNotification(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.notifications, id)
	return nil
}

func (m *mockStore) SetNotificationActive(_ context.Context, id int64, active bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if n, ok := m.notifications[id]; ok {
		n.Active = active
	}
	return nil
}

// Unimplemented Store methods — panic if called unexpectedly.
func (m *mockStore) SaveTweet(_ context.Context, _ *model.Tweet) error { panic("not implemented") }
func (m *mockStore) GetTweet(_ context.Context, _ string) (*model.Tweet, error) {
	panic("not implemented")
}
func (m *mockStore) ListTweets(_ context.Context, _ store.TweetFilter) ([]*model.Tweet, error) {
	panic("not implemented")
}
func (m *mockStore) SaveSource(_ context.Context, _ *model.Source) error {
	panic("not implemented")
}
func (m *mockStore) GetSource(_ context.Context, _ int64) (*model.Source, error) {
	panic("not implemented")
}
func (m *mockStore) ListSources(_ context.Context, _ store.SourceFilter) ([]*model.Source, error) {
	panic("not implemented")
}
func (m *mockStore) DeleteSource(_ context.Context, _ int64) error   { panic("not implemented") }
func (m *mockStore) SetSourceActive(_ context.Context, _ int64, _ bool) error {
	panic("not implemented")
}
func (m *mockStore) ActiveRuleRefCount(_ context.Context, _ int64) (int, error) {
	panic("not implemented")
}
func (m *mockStore) RuleRefCount(_ context.Context, _ int64) (int, error) {
	panic("not implemented")
}
func (m *mockStore) SaveRule(_ context.Context, _ *model.Rule) error { panic("not implemented") }
func (m *mockStore) GetRule(_ context.Context, _ int64) (*model.Rule, error) {
	panic("not implemented")
}
func (m *mockStore) GetRuleByXRuleID(_ context.Context, _ string) (*model.Rule, error) {
	panic("not implemented")
}
func (m *mockStore) ListRules(_ context.Context, _ store.RuleFilter) ([]*model.Rule, error) {
	panic("not implemented")
}
func (m *mockStore) UpdateRule(_ context.Context, _ *model.Rule) error { panic("not implemented") }
func (m *mockStore) DeleteRule(_ context.Context, _ int64) error       { panic("not implemented") }
func (m *mockStore) SetRuleActive(_ context.Context, _ int64, _ bool, _ string) error {
	panic("not implemented")
}
func (m *mockStore) TweetRuleCount(_ context.Context, _ int64) (int, error) {
	panic("not implemented")
}
func (m *mockStore) SaveTweetRule(_ context.Context, _ *model.TweetRule) error {
	panic("not implemented")
}
func (m *mockStore) SavePipeline(_ context.Context, _ *model.Pipeline) error { return nil }
func (m *mockStore) GetPipeline(_ context.Context, _ int64) (*model.Pipeline, error) {
	return nil, fmt.Errorf("not found")
}
func (m *mockStore) ListPipelines(_ context.Context, _ store.PipelineFilter) ([]*model.Pipeline, error) {
	return nil, nil
}
func (m *mockStore) UpdatePipeline(_ context.Context, _ *model.Pipeline) error             { return nil }
func (m *mockStore) DeletePipeline(_ context.Context, _ int64) error                       { return nil }
func (m *mockStore) SetPipelineActive(_ context.Context, _ int64, _ bool) error            { return nil }
func (m *mockStore) ListPipelineLabels(_ context.Context, _ int64) ([]string, error)       { return nil, nil }
func (m *mockStore) AddPipelineLabel(_ context.Context, _ int64, _ string) error           { return nil }
func (m *mockStore) RemovePipelineLabel(_ context.Context, _ int64, _ string) error        { return nil }
func (m *mockStore) SaveActionChain(_ context.Context, _ *model.ActionChain) error         { return nil }
func (m *mockStore) GetActionChain(_ context.Context, _ int64) (*model.ActionChain, error) {
	return nil, fmt.Errorf("not found")
}
func (m *mockStore) ListActionChains(_ context.Context, _ store.ActionChainFilter) ([]*model.ActionChain, error) {
	return nil, nil
}
func (m *mockStore) UpdateActionChain(_ context.Context, _ *model.ActionChain) error { return nil }
func (m *mockStore) DeleteActionChain(_ context.Context, _ int64) error              { return nil }
func (m *mockStore) SaveCategorization(_ context.Context, _ *model.Categorization) error {
	panic("not implemented")
}
func (m *mockStore) ListCategorizations(_ context.Context, _ store.CategorizationFilter) ([]*model.Categorization, error) {
	panic("not implemented")
}
func (m *mockStore) ListUncategorizedTweets(_ context.Context, _ []string) ([]*model.Tweet, error) {
	panic("not implemented")
}
func (m *mockStore) ListTweetRuleIDs(_ context.Context, _ string) ([]int64, error) { return nil, nil }
func (m *mockStore) SaveUser(_ context.Context, _ *model.User) error                    { return nil }
func (m *mockStore) GetUser(_ context.Context, _ int64) (*model.User, error)            { return nil, nil }
func (m *mockStore) GetUserByUsername(_ context.Context, _ string) (*model.User, error) { return nil, nil }
func (m *mockStore) ListUsers(_ context.Context) ([]*model.User, error)                 { return nil, nil }
func (m *mockStore) UpdateUser(_ context.Context, _ *model.User) error                  { return nil }
func (m *mockStore) DeleteUser(_ context.Context, _ int64) error                        { return nil }
func (m *mockStore) CountAdmins(_ context.Context) (int, error) { return 1, nil }
func (m *mockStore) CountTweets(_ context.Context, _ store.TweetFilter) (int, error) { return 0, nil }
func (m *mockStore) DeleteTweets(_ context.Context, _ []string) (int64, error)       { return 0, nil }
func (m *mockStore) DeleteNoiseTweets(_ context.Context) (int64, error)              { return 0, nil }
func (m *mockStore) ListRulesDependingOn(_ context.Context, _ int64) ([]*model.Rule, error) { return nil, nil }
func (m *mockStore) UpdateSource(_ context.Context, _ *model.Source) error           { return nil }
func (m *mockStore) SaveTrackedEntity(_ context.Context, _ *model.TrackedEntity) error { return nil }
func (m *mockStore) GetTrackedEntity(_ context.Context, _ int64) (*model.TrackedEntity, error) { return nil, fmt.Errorf("not found") }
func (m *mockStore) ListTrackedEntities(_ context.Context, _ store.TrackedEntityFilter) ([]*model.TrackedEntity, error) { return nil, nil }
func (m *mockStore) UpdateTrackedEntity(_ context.Context, _ *model.TrackedEntity) error { return nil }
func (m *mockStore) DeleteTrackedEntity(_ context.Context, _ int64) error            { return nil }
func (m *mockStore) SetTrackedEntityActive(_ context.Context, _ int64, _ bool) error { return nil }
func (m *mockStore) ListActiveTrackedEntities(_ context.Context) ([]*model.TrackedEntity, error) { return nil, nil }
func (m *mockStore) SaveEntityTrack(_ context.Context, _ *model.EntityTrack) error { return nil }
func (m *mockStore) ListEntityTracks(_ context.Context, _ store.EntityTrackFilter) ([]*model.EntityTrack, error) { return nil, nil }
func (m *mockStore) CountEntityTracks(_ context.Context, _ store.EntityTrackFilter) (int, error) { return 0, nil }
func (m *mockStore) ListNERSuggestionsForEntity(_ context.Context, _ int64) ([]string, error) { return nil, nil }
func (m *mockStore) SearchNERSuggestions(_ context.Context, _, _ string) ([]string, error) { return nil, nil }
func (m *mockStore) ListSummaries(_ context.Context, _ store.SummaryFilter) ([]*model.SummaryItem, error) { return nil, nil }
func (m *mockStore) CountSummaries(_ context.Context, _ store.SummaryFilter) (int, error) { return 0, nil }
func (m *mockStore) SaveDigestJob(_ context.Context, _ *model.DigestJob) error { return nil }
func (m *mockStore) GetDigestJob(_ context.Context, _ string) (*model.DigestJob, error) { return nil, fmt.Errorf("not found") }
func (m *mockStore) UpdateDigestJob(_ context.Context, _ *model.DigestJob) error { return nil }
func (m *mockStore) ListDigestJobs(_ context.Context, _ int) ([]*model.DigestJob, error) { return nil, nil }
func (m *mockStore) DeleteDigestJob(_ context.Context, _ string) error { return nil }
func (m *mockStore) SaveSearchJob(_ context.Context, _ *model.SearchJob) error { return nil }
func (m *mockStore) GetSearchJob(_ context.Context, _ string) (*model.SearchJob, error) { return nil, fmt.Errorf("not found") }
func (m *mockStore) UpdateSearchJob(_ context.Context, _ *model.SearchJob) error { return nil }
func (m *mockStore) ListSearchJobs(_ context.Context, _ int) ([]*model.SearchJob, error) { return nil, nil }
func (m *mockStore) DeleteSearchJob(_ context.Context, _ string) error { return nil }
func (m *mockStore) SaveAction(_ context.Context, _ *model.Action) error { return nil }
func (m *mockStore) GetAction(_ context.Context, _ int64) (*model.Action, error) { return nil, fmt.Errorf("not found") }
func (m *mockStore) ListActions(_ context.Context, _ store.ActionFilter) ([]*model.Action, error) { return nil, nil }
func (m *mockStore) UpdateAction(_ context.Context, _ *model.Action) error { return nil }
func (m *mockStore) DeleteAction(_ context.Context, _ int64) error { return nil }
func (m *mockStore) SetActionActive(_ context.Context, _ int64, _ bool) error { return nil }
func (m *mockStore) SaveActionExecution(_ context.Context, _ *model.ActionExecution) error { return nil }
func (m *mockStore) ListActionExecutions(_ context.Context, _ store.ExecutionFilter) ([]*model.ActionExecution, error) { return nil, nil }
func (m *mockStore) ListActionExecutionMetadata(_ context.Context, _ int64) (map[string]string, error) { return nil, nil }
func (m *mockStore) SaveActionExecutionMetadata(_ context.Context, _ int64, _, _ string) error { return nil }
func (m *mockStore) SaveTwitterCredentials(_ context.Context, _ *model.TwitterCredentials) error { return nil }
func (m *mockStore) GetTwitterCredentials(_ context.Context) (*model.TwitterCredentials, error) { return nil, fmt.Errorf("not found") }
func (m *mockStore) DeleteTwitterCredentials(_ context.Context) error { return nil }
func (m *mockStore) Close() error { return nil }

// --- test helpers --------------------------------------------------------

// fakePushoverServer returns an httptest.Server that records received messages
// and always responds with status=1.
func fakePushoverServer(t *testing.T, received *[]string, mu *sync.Mutex) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if mu != nil {
			mu.Lock()
		}
		*received = append(*received, string(body))
		if mu != nil {
			mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":1}`)
	}))
}

func newTestDispatcher(t *testing.T, s *mockStore, srv *httptest.Server) *Dispatcher {
	t.Helper()
	client := NewPushoverClient("app-token", "user-key", newNopLogger())
	client.apiURL = srv.URL
	return NewDispatcher(s, client, nil, newNopLogger())
}

func newNopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func ptr[T any](v T) *T { return &v }

// --- tests ---------------------------------------------------------------

func TestDispatcher_EmitAndSend(t *testing.T) {
	var (
		received []string
		mu       sync.Mutex
	)
	srv := fakePushoverServer(t, &received, &mu)
	defer srv.Close()

	s := newMockStore()
	s.addNotification(&model.Notification{
		ID:        1,
		Name:      "test",
		EventType: EventTweetMatched,
		Active:    true,
	})

	d := newTestDispatcher(t, s, srv)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		d.Start(ctx)
		close(done)
	}()

	tweet := &model.Tweet{
		ID:           "123",
		AuthorHandle: "testuser",
		Text:         "hello world",
		CreatedAt:    time.Now(),
	}
	d.Emit(NotificationEvent{
		Type:      EventTweetMatched,
		Tweet:     tweet,
		Timestamp: time.Now(),
	})

	// Give the dispatcher time to process, then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("expected 1 pushover request, got %d", len(received))
	}
}

func TestDispatcher_FilterRuleID(t *testing.T) {
	var (
		received []string
		mu       sync.Mutex
	)
	srv := fakePushoverServer(t, &received, &mu)
	defer srv.Close()

	s := newMockStore()
	s.addNotification(&model.Notification{
		ID:           1,
		EventType:    EventTweetMatched,
		Active:       true,
		FilterRuleID: ptr(int64(5)),
	})

	d := newTestDispatcher(t, s, srv)

	tweet := &model.Tweet{ID: "1", AuthorHandle: "u", Text: "t", CreatedAt: time.Now()}

	// Matching: RuleIDs contains 5.
	ctx := context.Background()
	d.handle(ctx, NotificationEvent{
		Type:    EventTweetMatched,
		Tweet:   tweet,
		RuleIDs: []int64{3, 5, 7},
	})

	mu.Lock()
	gotMatch := len(received)
	mu.Unlock()
	if gotMatch != 1 {
		t.Fatalf("expected 1 send for matching rule IDs, got %d", gotMatch)
	}

	// Non-matching: RuleIDs does not contain 5.
	d.handle(ctx, NotificationEvent{
		Type:    EventTweetMatched,
		Tweet:   tweet,
		RuleIDs: []int64{1, 2},
	})

	mu.Lock()
	gotNoMatch := len(received)
	mu.Unlock()
	if gotNoMatch != 1 {
		t.Fatalf("expected still 1 send after non-matching rule IDs, got %d", gotNoMatch)
	}
}

func TestDispatcher_FilterModelName(t *testing.T) {
	var (
		received []string
		mu       sync.Mutex
	)
	srv := fakePushoverServer(t, &received, &mu)
	defer srv.Close()

	s := newMockStore()
	s.addNotification(&model.Notification{
		ID:              1,
		EventType:       EventTweetCategorized,
		Active:          true,
		FilterModelName: ptr("classifier"),
	})

	d := newTestDispatcher(t, s, srv)
	tweet := &model.Tweet{ID: "1", AuthorHandle: "u", Text: "t", CreatedAt: time.Now()}
	ctx := context.Background()

	// Matching model name.
	d.handle(ctx, NotificationEvent{
		Type:      EventTweetCategorized,
		Tweet:     tweet,
		ModelName: "classifier",
		Results:   json.RawMessage(`{}`),
	})
	mu.Lock()
	gotMatch := len(received)
	mu.Unlock()
	if gotMatch != 1 {
		t.Fatalf("expected 1 send for matching model name, got %d", gotMatch)
	}

	// Non-matching model name.
	d.handle(ctx, NotificationEvent{
		Type:      EventTweetCategorized,
		Tweet:     tweet,
		ModelName: "other",
		Results:   json.RawMessage(`{}`),
	})
	mu.Lock()
	gotNoMatch := len(received)
	mu.Unlock()
	if gotNoMatch != 1 {
		t.Fatalf("expected still 1 send after non-matching model name, got %d", gotNoMatch)
	}
}

func TestDispatcher_FilterModelResult(t *testing.T) {
	filterJSON := json.RawMessage(`{"label":"sanctions"}`)

	var (
		received []string
		mu       sync.Mutex
	)
	srv := fakePushoverServer(t, &received, &mu)
	defer srv.Close()

	s := newMockStore()
	s.addNotification(&model.Notification{
		ID:                1,
		EventType:         EventTweetCategorized,
		Active:            true,
		FilterModelResult: &filterJSON,
	})

	d := newTestDispatcher(t, s, srv)
	tweet := &model.Tweet{ID: "1", AuthorHandle: "u", Text: "t", CreatedAt: time.Now()}
	ctx := context.Background()

	// Matching: results contain the required key-value pair.
	d.handle(ctx, NotificationEvent{
		Type:      EventTweetCategorized,
		Tweet:     tweet,
		ModelName: "model",
		Results:   json.RawMessage(`{"label":"sanctions","score":0.9}`),
	})
	mu.Lock()
	gotMatch := len(received)
	mu.Unlock()
	if gotMatch != 1 {
		t.Fatalf("expected 1 send for matching model result, got %d", gotMatch)
	}

	// Non-matching: label differs.
	d.handle(ctx, NotificationEvent{
		Type:      EventTweetCategorized,
		Tweet:     tweet,
		ModelName: "model",
		Results:   json.RawMessage(`{"label":"unrelated","score":0.9}`),
	})
	mu.Lock()
	gotNoMatch := len(received)
	mu.Unlock()
	if gotNoMatch != 1 {
		t.Fatalf("expected still 1 send after non-matching model result, got %d", gotNoMatch)
	}
}

func TestDispatcher_ChannelFull(t *testing.T) {
	var (
		received []string
		mu       sync.Mutex
	)
	srv := fakePushoverServer(t, &received, &mu)
	defer srv.Close()

	s := newMockStore()
	d := newTestDispatcher(t, s, srv)

	// Fill the channel to capacity without starting the dispatcher.
	for i := 0; i < channelBufferSize; i++ {
		d.ch <- NotificationEvent{Type: EventTweetMatched}
	}

	// This extra emit should not block or panic.
	done := make(chan struct{})
	go func() {
		defer close(done)
		d.Emit(NotificationEvent{Type: EventTweetMatched})
	}()

	select {
	case <-done:
		// success — did not block
	case <-time.After(time.Second):
		t.Fatal("Emit blocked when channel was full")
	}
}

func TestMatchModelResult(t *testing.T) {
	cases := []struct {
		name    string
		filter  string
		results string
		want    bool
	}{
		{
			name:    "exact match",
			filter:  `{"label":"sanctions"}`,
			results: `{"label":"sanctions"}`,
			want:    true,
		},
		{
			name:    "results superset",
			filter:  `{"label":"sanctions"}`,
			results: `{"label":"sanctions","score":0.95}`,
			want:    true,
		},
		{
			name:    "value mismatch",
			filter:  `{"label":"sanctions"}`,
			results: `{"label":"unrelated"}`,
			want:    false,
		},
		{
			name:    "missing key in results",
			filter:  `{"label":"sanctions"}`,
			results: `{"score":0.9}`,
			want:    false,
		},
		{
			name:    "empty filter matches all",
			filter:  `{}`,
			results: `{"label":"anything"}`,
			want:    true,
		},
		{
			name:    "numeric value match",
			filter:  `{"score":1}`,
			results: `{"score":1}`,
			want:    true,
		},
		{
			name:    "numeric value mismatch",
			filter:  `{"score":1}`,
			results: `{"score":2}`,
			want:    false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := matchModelResult(json.RawMessage(tc.filter), json.RawMessage(tc.results))
			if got != tc.want {
				t.Errorf("matchModelResult(%q, %q) = %v, want %v",
					tc.filter, tc.results, got, tc.want)
			}
		})
	}
}
