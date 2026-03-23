package api

import (
	"bytes"
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
	sources         map[int64]*model.Source
	tweets          map[string]*model.Tweet
	models          map[int64]*model.Model
	categorizations []*model.Categorization
	nextID          int64
}

func newMockStore() *mockStore {
	return &mockStore{
		sources: make(map[int64]*model.Source),
		tweets:  make(map[string]*model.Tweet),
		models:  make(map[int64]*model.Model),
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
func (m *mockStore) UpdateSourceXRuleID(_ context.Context, id int64, xRuleID string) error {
	if s, ok := m.sources[id]; ok {
		s.XRuleID = xRuleID
	}
	return nil
}
func (m *mockStore) SaveTweetSource(_ context.Context, _ *model.TweetSource) error { return nil }

func (m *mockStore) SaveModel(_ context.Context, mdl *model.Model) error {
	m.nextID++
	mdl.ID = m.nextID
	m.models[mdl.ID] = mdl
	return nil
}
func (m *mockStore) GetModel(_ context.Context, id int64) (*model.Model, error) {
	mdl, ok := m.models[id]
	if !ok {
		return nil, fmt.Errorf("model not found: %d", id)
	}
	return mdl, nil
}
func (m *mockStore) ListModels(_ context.Context) ([]*model.Model, error) {
	out := make([]*model.Model, 0, len(m.models))
	for _, mdl := range m.models {
		out = append(out, mdl)
	}
	return out, nil
}
func (m *mockStore) DeleteModel(_ context.Context, id int64) error {
	delete(m.models, id)
	return nil
}
func (m *mockStore) SetModelActive(_ context.Context, id int64, active bool) error {
	if mdl, ok := m.models[id]; ok {
		mdl.Active = active
	}
	return nil
}

func (m *mockStore) SaveCategorization(_ context.Context, c *model.Categorization) error {
	m.categorizations = append(m.categorizations, c)
	return nil
}
func (m *mockStore) ListCategorizations(_ context.Context, f store.CategorizationFilter) ([]*model.Categorization, error) {
	if f.TweetID == nil {
		return m.categorizations, nil
	}
	out := make([]*model.Categorization, 0)
	for _, c := range m.categorizations {
		if c.TweetID == *f.TweetID {
			out = append(out, c)
		}
	}
	return out, nil
}
func (m *mockStore) ListUncategorizedTweets(_ context.Context, _ []string) ([]*model.Tweet, error) {
	out := make([]*model.Tweet, 0, len(m.tweets))
	for _, t := range m.tweets {
		out = append(out, t)
	}
	return out, nil
}
func (m *mockStore) Close() error { return nil }

// ---------------------------------------------------------------------------
// mockProducer
// ---------------------------------------------------------------------------

type mockProducer struct {
	pushed []*model.StreamTweet
}

func (m *mockProducer) Push(_ context.Context, _ string, t *model.StreamTweet) error {
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
	srv := NewServer("127.0.0.1:0", ms, mp, nil, mr, logger)
	return srv, ms, mp, mr
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestCreateSource(t *testing.T) {
	srv, ms, _, mr := newTestServer(t)

	body := `{"type":"account","value":"@testuser"}`
	req := httptest.NewRequest(http.MethodPost, "/api/sources", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if !mr.addRuleCalled {
		t.Error("expected AddRule to be called")
	}

	var result model.Source
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result.ID == 0 {
		t.Error("expected non-zero ID in response")
	}
	if result.XRuleID == "" {
		t.Error("expected XRuleID to be set")
	}
	if len(ms.sources) != 1 {
		t.Errorf("expected 1 source in store, got %d", len(ms.sources))
	}
}

func TestDeleteSource(t *testing.T) {
	srv, ms, _, mr := newTestServer(t)

	// seed a source
	src := &model.Source{Type: model.SourceTypeKeyword, Value: "iran", XRuleID: "rule_x"}
	_ = ms.SaveSource(context.Background(), src)

	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/sources/%d", src.ID), nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
	if !mr.deleteRuleCalled {
		t.Error("expected DeleteRule to be called")
	}
	if len(ms.sources) != 0 {
		t.Errorf("expected 0 sources in store, got %d", len(ms.sources))
	}
}

func TestListTweets_Pagination(t *testing.T) {
	srv, ms, _, _ := newTestServer(t)

	// seed some tweets
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

func TestGetTweet_WithCategorizations(t *testing.T) {
	srv, ms, _, _ := newTestServer(t)

	tw := &model.Tweet{
		ID:        "t1",
		Text:      "test tweet",
		FetchedAt: time.Now(),
	}
	_ = ms.SaveTweet(context.Background(), tw)

	cat := &model.Categorization{
		TweetID:   "t1",
		ModelName: "classifier",
		Results:   json.RawMessage(`{"label":"politics","score":0.9}`),
	}
	_ = ms.SaveCategorization(context.Background(), cat)

	req := httptest.NewRequest(http.MethodGet, "/api/tweets/t1", nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result struct {
		Tweet           *model.Tweet              `json:"tweet"`
		Categorizations []*model.Categorization   `json:"categorizations"`
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
	if len(result.Categorizations) != 1 {
		t.Errorf("expected 1 categorization, got %d", len(result.Categorizations))
	}
}

func TestReprocessTweets_EnqueuesTweets(t *testing.T) {
	srv, ms, mp, _ := newTestServer(t)

	// seed tweets
	tw1 := &model.Tweet{ID: "t1", Text: "iran news", FetchedAt: time.Now()}
	tw2 := &model.Tweet{ID: "t2", Text: "more news", FetchedAt: time.Now()}
	_ = ms.SaveTweet(context.Background(), tw1)
	_ = ms.SaveTweet(context.Background(), tw2)

	body := `{"model_names":["classifier"]}`
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

func TestActivateModel_ReloadsRegistry(t *testing.T) {
	srv, ms, _, _ := newTestServer(t)

	mdl := &model.Model{
		Name:    "test-model",
		Version: "1.0",
		Path:    "./models/test",
		Type:    model.ModelTypeClassification,
		Active:  false,
	}
	_ = ms.SaveModel(context.Background(), mdl)

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/models/%d/activate", mdl.ID), nil)
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify model is now marked active in the store
	if !ms.models[mdl.ID].Active {
		t.Error("expected model to be active after activate call")
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

// Ensure bytes import is used (for potential future use)
var _ = bytes.NewReader
