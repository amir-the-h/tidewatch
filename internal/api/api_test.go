package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/queue"
	"github.com/amir-the-h/iran-watcher/internal/store"
	"github.com/amir-the-h/iran-watcher/internal/stream"
)

// ---------------------------------------------------------------------------
// mockStore
// ---------------------------------------------------------------------------

type mockStore struct {
	sources    map[int64]*model.Source
	rules      map[int64]*model.Rule
	tweets     map[string]*model.Tweet
	tweetRules []*model.TweetRule
	nextID     int64
}

func newMockStore() *mockStore {
	return &mockStore{
		sources: make(map[int64]*model.Source),
		rules:   make(map[int64]*model.Rule),
		tweets:  make(map[string]*model.Tweet),
	}
}

func (m *mockStore) SaveTweet(_ context.Context, t *model.Tweet) error {
	m.tweets[t.ID] = t
	return nil
}
func (m *mockStore) GetTweet(_ context.Context, id string) (*model.Tweet, error) {
	t, ok := m.tweets[id]
	if !ok {
		return nil, fmt.Errorf("tweet not found: %s", id)
	}
	return t, nil
}
func (m *mockStore) ListTweets(_ context.Context, _ store.TweetFilter) ([]*model.Tweet, error) {
	out := make([]*model.Tweet, 0, len(m.tweets))
	for _, t := range m.tweets {
		out = append(out, t)
	}
	return out, nil
}

func (m *mockStore) SaveSource(_ context.Context, s *model.Source) error {
	m.nextID++
	s.ID = m.nextID
	m.sources[s.ID] = s
	return nil
}
func (m *mockStore) GetSource(_ context.Context, id int64) (*model.Source, error) {
	s, ok := m.sources[id]
	if !ok {
		return nil, fmt.Errorf("source not found: %d", id)
	}
	return s, nil
}
func (m *mockStore) ListSources(_ context.Context, _ store.SourceFilter) ([]*model.Source, error) {
	out := make([]*model.Source, 0, len(m.sources))
	for _, s := range m.sources {
		out = append(out, s)
	}
	return out, nil
}
func (m *mockStore) DeleteSource(_ context.Context, id int64) error {
	delete(m.sources, id)
	return nil
}
func (m *mockStore) SetSourceActive(_ context.Context, id int64, active bool) error {
	if s, ok := m.sources[id]; ok {
		s.Active = active
	}
	return nil
}
func (m *mockStore) ActiveRuleRefCount(_ context.Context, _ int64) (int, error) { return 0, nil }
func (m *mockStore) RuleRefCount(_ context.Context, _ int64) (int, error)       { return 0, nil }

func (m *mockStore) SaveRule(_ context.Context, r *model.Rule) error {
	m.nextID++
	r.ID = m.nextID
	m.rules[r.ID] = r
	return nil
}
func (m *mockStore) GetRule(_ context.Context, id int64) (*model.Rule, error) {
	r, ok := m.rules[id]
	if !ok {
		return nil, fmt.Errorf("rule not found: %d", id)
	}
	return r, nil
}
func (m *mockStore) GetRuleByXRuleID(_ context.Context, xRuleID string) (*model.Rule, error) {
	for _, r := range m.rules {
		if r.XRuleID == xRuleID {
			return r, nil
		}
	}
	return nil, fmt.Errorf("rule not found: %s", xRuleID)
}
func (m *mockStore) ListRules(_ context.Context, _ store.RuleFilter) ([]*model.Rule, error) {
	out := make([]*model.Rule, 0, len(m.rules))
	for _, r := range m.rules {
		out = append(out, r)
	}
	return out, nil
}
func (m *mockStore) UpdateRule(_ context.Context, r *model.Rule) error {
	m.rules[r.ID] = r
	return nil
}
func (m *mockStore) DeleteRule(_ context.Context, id int64) error {
	delete(m.rules, id)
	return nil
}
func (m *mockStore) SetRuleActive(_ context.Context, id int64, active bool, xRuleID string) error {
	if r, ok := m.rules[id]; ok {
		r.Active = active
		r.XRuleID = xRuleID
	}
	return nil
}
func (m *mockStore) TweetRuleCount(_ context.Context, _ int64) (int, error) { return 0, nil }

func (m *mockStore) SaveTweetRule(_ context.Context, tr *model.TweetRule) error {
	m.tweetRules = append(m.tweetRules, tr)
	return nil
}

func (m *mockStore) SavePipeline(_ context.Context, _ *model.Pipeline) error         { return nil }
func (m *mockStore) GetPipeline(_ context.Context, _ int64) (*model.Pipeline, error) { return nil, nil }
func (m *mockStore) ListPipelines(_ context.Context, _ store.PipelineFilter) ([]*model.Pipeline, error) {
	return nil, nil
}
func (m *mockStore) UpdatePipeline(_ context.Context, _ *model.Pipeline) error       { return nil }
func (m *mockStore) DeletePipeline(_ context.Context, _ int64) error                 { return nil }
func (m *mockStore) SetPipelineActive(_ context.Context, _ int64, _ bool) error      { return nil }
func (m *mockStore) ListPipelineLabels(_ context.Context, _ int64) ([]string, error) { return nil, nil }
func (m *mockStore) AddPipelineLabel(_ context.Context, _ int64, _ string) error     { return nil }
func (m *mockStore) RemovePipelineLabel(_ context.Context, _ int64, _ string) error  { return nil }

func (m *mockStore) SaveChain(_ context.Context, _ *model.Chain) error { return nil }
func (m *mockStore) GetChain(_ context.Context, _ int64) (*model.Chain, error) {
	return nil, nil
}
func (m *mockStore) ListChains(_ context.Context) ([]*model.Chain, error)    { return nil, nil }
func (m *mockStore) UpdateChain(_ context.Context, _ *model.Chain) error     { return nil }
func (m *mockStore) DeleteChain(_ context.Context, _ int64) error            { return nil }
func (m *mockStore) SetChainActive(_ context.Context, _ int64, _ bool) error { return nil }
func (m *mockStore) GetChainGraph(_ context.Context, _ int64) (*model.ChainGraph, error) {
	return nil, nil
}
func (m *mockStore) SaveChainGraph(_ context.Context, _ int64, _ []*model.ChainNodeRequest, _ []*model.ChainEdgeRequest) (*model.ChainGraph, error) {
	return nil, nil
}
func (m *mockStore) SaveTriggerChainLink(_ context.Context, _ *model.TriggerChainLink) error {
	return nil
}
func (m *mockStore) ListTriggerChainLinks(_ context.Context) ([]*model.TriggerChainLink, error) {
	return nil, nil
}
func (m *mockStore) ListActiveTriggerChainLinks(_ context.Context, _ string) ([]*model.TriggerChainLink, error) { return nil, nil }
func (m *mockStore) DeleteTriggerChainLink(_ context.Context, _ int64) error            { return nil }
func (m *mockStore) SetTriggerChainLinkActive(_ context.Context, _ int64, _ bool) error { return nil }
func (m *mockStore) GetChainIDForNode(_ context.Context, _ int64) (int64, error)        { return 0, nil }

func (m *mockStore) SaveNotificationProvider(_ context.Context, _ *model.NotificationProvider) error {
	return nil
}
func (m *mockStore) GetNotificationProvider(_ context.Context, _ int64) (*model.NotificationProvider, error) {
	return nil, nil
}
func (m *mockStore) ListNotificationProviders(_ context.Context) ([]*model.NotificationProvider, error) {
	return nil, nil
}
func (m *mockStore) UpdateNotificationProvider(_ context.Context, _ *model.NotificationProvider) error {
	return nil
}
func (m *mockStore) DeleteNotificationProvider(_ context.Context, _ int64) error { return nil }
func (m *mockStore) SetNotificationProviderActive(_ context.Context, _ int64, _ bool) error {
	return nil
}
func (m *mockStore) SaveNotificationChannel(_ context.Context, _ *model.NotificationChannel) error {
	return nil
}
func (m *mockStore) GetNotificationChannel(_ context.Context, _ int64) (*model.NotificationChannel, error) {
	return nil, nil
}
func (m *mockStore) ListNotificationChannels(_ context.Context, _ *int64) ([]*model.NotificationChannel, error) {
	return nil, nil
}
func (m *mockStore) UpdateNotificationChannel(_ context.Context, _ *model.NotificationChannel) error {
	return nil
}
func (m *mockStore) DeleteNotificationChannel(_ context.Context, _ int64) error { return nil }
func (m *mockStore) SetNotificationChannelActive(_ context.Context, _ int64, _ bool) error {
	return nil
}

func (m *mockStore) CountTweets(_ context.Context, _ store.TweetFilter) (int, error) {
	return len(m.tweets), nil
}
func (m *mockStore) DeleteTweets(_ context.Context, _ []string) (int64, error) { return 0, nil }
func (m *mockStore) DeleteNoiseTweets(_ context.Context) (int64, error)        { return 0, nil }
func (m *mockStore) UpdateSource(_ context.Context, _ *model.Source) error     { return nil }
func (m *mockStore) ListRulesDependingOn(_ context.Context, _ int64) ([]*model.Rule, error) {
	return nil, nil
}
func (m *mockStore) ListTweetRuleIDs(_ context.Context, _ string) ([]int64, error)     { return nil, nil }
func (m *mockStore) SaveTrackedEntity(_ context.Context, _ *model.TrackedEntity) error { return nil }
func (m *mockStore) GetTrackedEntity(_ context.Context, _ int64) (*model.TrackedEntity, error) {
	return nil, fmt.Errorf("not found")
}
func (m *mockStore) ListTrackedEntities(_ context.Context, _ store.TrackedEntityFilter) ([]*model.TrackedEntity, error) {
	return nil, nil
}
func (m *mockStore) UpdateTrackedEntity(_ context.Context, _ *model.TrackedEntity) error { return nil }
func (m *mockStore) DeleteTrackedEntity(_ context.Context, _ int64) error                { return nil }
func (m *mockStore) SetTrackedEntityActive(_ context.Context, _ int64, _ bool) error     { return nil }
func (m *mockStore) ListActiveTrackedEntities(_ context.Context) ([]*model.TrackedEntity, error) {
	return nil, nil
}
func (m *mockStore) SaveEntityTrack(_ context.Context, _ *model.EntityTrack) error { return nil }
func (m *mockStore) ListEntityTracks(_ context.Context, _ store.EntityTrackFilter) ([]*model.EntityTrack, error) {
	return nil, nil
}
func (m *mockStore) CountEntityTracks(_ context.Context, _ store.EntityTrackFilter) (int, error) {
	return 0, nil
}
func (m *mockStore) ListSummaries(_ context.Context, _ store.SummaryFilter) ([]*model.SummaryItem, error) {
	return []*model.SummaryItem{}, nil
}
func (m *mockStore) CountSummaries(_ context.Context, _ store.SummaryFilter) (int, error) {
	return 0, nil
}

func (m *mockStore) SaveDigestJob(_ context.Context, _ *model.DigestJob) error { return nil }
func (m *mockStore) GetDigestJob(_ context.Context, _ string) (*model.DigestJob, error) {
	return nil, fmt.Errorf("not found")
}
func (m *mockStore) UpdateDigestJob(_ context.Context, _ *model.DigestJob) error { return nil }
func (m *mockStore) ListDigestJobs(_ context.Context, _ int) ([]*model.DigestJob, error) {
	return []*model.DigestJob{}, nil
}
func (m *mockStore) DeleteDigestJob(_ context.Context, _ string) error { return nil }

func (m *mockStore) SaveSearchJob(_ context.Context, _ *model.SearchJob) error { return nil }
func (m *mockStore) GetSearchJob(_ context.Context, _ string) (*model.SearchJob, error) {
	return nil, fmt.Errorf("not found")
}
func (m *mockStore) UpdateSearchJob(_ context.Context, _ *model.SearchJob) error { return nil }
func (m *mockStore) ListSearchJobs(_ context.Context, _ int) ([]*model.SearchJob, error) {
	return []*model.SearchJob{}, nil
}
func (m *mockStore) DeleteSearchJob(_ context.Context, _ string) error { return nil }

func (m *mockStore) ListNERSuggestionsForEntity(_ context.Context, _ int64) ([]string, error) {
	return nil, nil
}
func (m *mockStore) SearchNERSuggestions(_ context.Context, _ string, _ string) ([]string, error) {
	return nil, nil
}

func (m *mockStore) SaveUser(_ context.Context, _ *model.User) error         { return nil }
func (m *mockStore) GetUser(_ context.Context, _ int64) (*model.User, error) { return nil, nil }
func (m *mockStore) GetUserByUsername(_ context.Context, _ string) (*model.User, error) {
	return nil, nil
}
func (m *mockStore) ListUsers(_ context.Context) ([]*model.User, error) { return nil, nil }
func (m *mockStore) UpdateUser(_ context.Context, _ *model.User) error  { return nil }
func (m *mockStore) DeleteUser(_ context.Context, _ int64) error        { return nil }
func (m *mockStore) CountAdmins(_ context.Context) (int, error)         { return 1, nil }

func (m *mockStore) SaveAction(_ context.Context, a *model.Action) error {
	m.nextID++
	a.ID = m.nextID
	return nil
}
func (m *mockStore) GetAction(_ context.Context, _ int64) (*model.Action, error) {
	return nil, fmt.Errorf("not found")
}
func (m *mockStore) ListActions(_ context.Context, _ store.ActionFilter) ([]*model.Action, error) {
	return nil, nil
}
func (m *mockStore) UpdateAction(_ context.Context, _ *model.Action) error    { return nil }
func (m *mockStore) DeleteAction(_ context.Context, _ int64) error            { return nil }
func (m *mockStore) SetActionActive(_ context.Context, _ int64, _ bool) error { return nil }
func (m *mockStore) SaveActionExecution(_ context.Context, e *model.ActionExecution) error {
	m.nextID++
	e.ID = m.nextID
	return nil
}
func (m *mockStore) ListActionExecutions(_ context.Context, _ store.ExecutionFilter) ([]*model.ActionExecution, error) {
	return nil, nil
}
func (m *mockStore) ListActionExecutionMetadata(_ context.Context, _ int64) (map[string]string, error) {
	return nil, nil
}
func (m *mockStore) MergeActionExecutionMetadata(_ context.Context, _ int64, _ string) (map[string]string, error) {
	return nil, nil
}
func (m *mockStore) SaveActionExecutionMetadata(_ context.Context, _ int64, _, _ string) error {
	return nil
}
func (m *mockStore) DeleteActionExecutions(_ context.Context, _ []string, _ []int64) (int64, error) {
	return 0, nil
}
func (m *mockStore) MergeAllTweetExecutionMetadata(_ context.Context, _ string) (map[string]string, error) {
	return nil, nil
}
func (m *mockStore) ListTweetActionSummaries(_ context.Context, _ string) ([]store.TweetActionSummary, error) {
	return nil, nil
}
func (m *mockStore) SaveTwitterCredentials(_ context.Context, _ *model.TwitterCredentials) error {
	return nil
}
func (m *mockStore) GetTwitterCredentials(_ context.Context) (*model.TwitterCredentials, error) {
	return nil, fmt.Errorf("not found")
}
func (m *mockStore) DeleteTwitterCredentials(_ context.Context) error { return nil }

func (m *mockStore) ListMapPins(_ context.Context) ([]*store.MapPin, error) { return nil, nil }

func (m *mockStore) ListOllamaBackends(_ context.Context) ([]model.OllamaBackend, error) {
	return nil, nil
}
func (m *mockStore) GetOllamaBackend(_ context.Context, _ int64) (model.OllamaBackend, error) {
	return model.OllamaBackend{}, nil
}
func (m *mockStore) CreateOllamaBackend(_ context.Context, _, _ string) (model.OllamaBackend, error) {
	return model.OllamaBackend{}, nil
}
func (m *mockStore) UpdateOllamaBackend(_ context.Context, _ int64, _, _ string) error { return nil }
func (m *mockStore) DeleteOllamaBackend(_ context.Context, _ int64) error              { return nil }
func (m *mockStore) SetOllamaBackendEnabled(_ context.Context, _ int64, _ bool) error  { return nil }
func (m *mockStore) ListPipelineBackends(_ context.Context, _ int64) ([]model.PipelineBackend, error) {
	return nil, nil
}
func (m *mockStore) SetPipelineBackends(_ context.Context, _ int64, _ []model.PipelineBackend) error {
	return nil
}
func (m *mockStore) RemovePipelineBackend(_ context.Context, _, _ int64) error { return nil }
func (m *mockStore) ListAllPipelineBackendIDs(_ context.Context) (map[int64][]int64, error) {
	return nil, nil
}

func (m *mockStore) Close() error { return nil }

// ---------------------------------------------------------------------------
// mockProducer
// ---------------------------------------------------------------------------

type mockProducer struct {
	pushed []*queue.Task
}

func (m *mockProducer) PushTask(_ context.Context, t *queue.Task) error {
	m.pushed = append(m.pushed, t)
	return nil
}
func (m *mockProducer) PushFailed(_ context.Context, _ *queue.FailedTweet) error { return nil }

// ---------------------------------------------------------------------------
// mockRuleManager
// ---------------------------------------------------------------------------

type mockRuleManager struct {
	addRuleCalled    bool
	deleteRuleCalled bool
}

func (m *mockRuleManager) AddRule(_ context.Context, _, _ string) (string, error) {
	m.addRuleCalled = true
	return "rule_123", nil
}
func (m *mockRuleManager) DeleteRule(_ context.Context, _ string) error {
	m.deleteRuleCalled = true
	return nil
}
func (m *mockRuleManager) ListRules(_ context.Context) ([]stream.XRule, error) { return nil, nil }

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func newTestServer(t *testing.T) (*Server, *mockStore, *mockProducer, *mockRuleManager) {
	t.Helper()
	ms := newMockStore()
	mp := &mockProducer{}
	mr := &mockRuleManager{}
	logger := slog.Default()
	srv := NewServer("127.0.0.1:0", ms, mp, nil, nil, nil, nil, mr, nil, "basic", nil, 0, "", logger, nil, nil, nil, nil, nil, "")
	return srv, ms, mp, mr
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestCreateSource(t *testing.T) {
	srv, ms, _, _ := newTestServer(t)

	body := `{"type":"account","value":"testuser"}`
	req := httptest.NewRequest(http.MethodPost, "/api/sources", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var result model.Source
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result.ID == 0 {
		t.Error("expected non-zero ID in response")
	}
	if result.Operator != "from:" {
		t.Errorf("expected Operator=from:, got %q", result.Operator)
	}
	if result.Class != model.SourceClassAtom {
		t.Errorf("expected Class=atom, got %q", result.Class)
	}
	if len(ms.sources) != 1 {
		t.Errorf("expected 1 source in store, got %d", len(ms.sources))
	}
}

func TestDeleteSource(t *testing.T) {
	srv, ms, _, _ := newTestServer(t)

	src := &model.Source{Type: model.SourceTypeKeyword, Value: "iran"}
	_ = ms.SaveSource(context.Background(), src)

	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/sources/%d", src.ID), nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
	if len(ms.sources) != 0 {
		t.Errorf("expected 0 sources in store, got %d", len(ms.sources))
	}
}

func TestListTweets_Pagination(t *testing.T) {
	srv, ms, _, _ := newTestServer(t)

	for i := 0; i < 3; i++ {
		tw := &model.Tweet{
			ID:        fmt.Sprintf("tweet_%d", i),
			Text:      "test",
			FetchedAt: time.Now(),
		}
		_ = ms.SaveTweet(context.Background(), tw)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/tweets?page=2&per_page=10", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result []*model.Tweet
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

func TestGetTweet_ReturnsDetail(t *testing.T) {
	srv, ms, _, _ := newTestServer(t)

	tw := &model.Tweet{
		ID:        "t1",
		Text:      "test tweet",
		FetchedAt: time.Now(),
	}
	_ = ms.SaveTweet(context.Background(), tw)

	req := httptest.NewRequest(http.MethodGet, "/api/tweets/t1", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result struct {
		Tweet *model.Tweet `json:"tweet"`
	}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result.Tweet == nil {
		t.Fatal("expected tweet in response")
	}
	if result.Tweet.ID != "t1" {
		t.Errorf("expected tweet ID t1, got %s", result.Tweet.ID)
	}
}

func TestReprocessTweets_EnqueuesTweets(t *testing.T) {
	srv, ms, mp, _ := newTestServer(t)

	tw1 := &model.Tweet{ID: "t1", Text: "iran news", FetchedAt: time.Now()}
	tw2 := &model.Tweet{ID: "t2", Text: "more news", FetchedAt: time.Now()}
	_ = ms.SaveTweet(context.Background(), tw1)
	_ = ms.SaveTweet(context.Background(), tw2)

	body := `{"tweet_ids":["t1","t2"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/tweets/reprocess", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result struct {
		Requeued int `json:"requeued"`
	}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result.Requeued != 2 {
		t.Errorf("expected requeued=2, got %d", result.Requeued)
	}
	if len(mp.pushed) != 2 {
		t.Errorf("expected 2 pushed tweets, got %d", len(mp.pushed))
	}
}

func TestActivateModel_ReturnsGone(t *testing.T) {
	// The models API is deprecated and returns 410 Gone.
	srv, _, _, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/models/1/activate", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusGone {
		t.Fatalf("expected 410 Gone, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDownloadModel_InvalidRepoID(t *testing.T) {
	srv, _, _, _ := newTestServer(t)

	body := `{"repo_id":"../../etc/passwd"}`
	req := httptest.NewRequest(http.MethodPost, "/api/models/download", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetHealth(t *testing.T) {
	srv, _, _, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result struct {
		Status string   `json:"status"`
		Models []string `json:"models"`
	}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result.Status != "ok" {
		t.Errorf("expected status=ok, got %q", result.Status)
	}
	if result.Models == nil {
		t.Error("expected models field in health response")
	}
}

// Ensure mockStore and mockProducer satisfy their interfaces at compile time.
var _ store.Store = (*mockStore)(nil)
var _ queue.Producer = (*mockProducer)(nil)
