package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amir-the-h/tidewatch/internal/auth"
	"github.com/amir-the-h/tidewatch/internal/model"
	"github.com/amir-the-h/tidewatch/internal/queue"
	"github.com/amir-the-h/tidewatch/internal/store"
	"github.com/amir-the-h/tidewatch/internal/stream"
)

// ---------------------------------------------------------------------------
// mockStore
// ---------------------------------------------------------------------------

type mockStore struct {
	sources             map[int64]*model.Source
	rules               map[int64]*model.Rule
	tweets              map[string]*model.Tweet
	tweetRules          []*model.TweetRule
	pipelines           map[int64]*model.Pipeline
	pipelineGraphs      map[int64]*model.PipelineGraph
	triggerLinks        []*model.TriggerPipelineLink
	executions          []*model.ActionExecution
	entityTrackedTweets map[string]bool
	nextID              int64
}

func newMockStore() *mockStore {
	return &mockStore{
		sources:             make(map[int64]*model.Source),
		rules:               make(map[int64]*model.Rule),
		tweets:              make(map[string]*model.Tweet),
		pipelines:           make(map[int64]*model.Pipeline),
		pipelineGraphs:      make(map[int64]*model.PipelineGraph),
		entityTrackedTweets: make(map[string]bool),
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
func (m *mockStore) ListTweets(_ context.Context, f store.TweetFilter) ([]*model.Tweet, error) {
	out := make([]*model.Tweet, 0, len(m.tweets))
	for _, t := range m.tweets {
		if f.HasEntityTrack && !m.entityTrackedTweets[t.ID] {
			continue
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		if f.OrderAsc {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
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

// Pipelines (DAG workflows — replaces Chains)
func (m *mockStore) SavePipeline(_ context.Context, p *model.Pipeline) error {
	m.nextID++
	p.ID = m.nextID
	m.pipelines[p.ID] = p
	return nil
}
func (m *mockStore) GetPipeline(_ context.Context, id int64) (*model.Pipeline, error) {
	if p, ok := m.pipelines[id]; ok {
		return p, nil
	}
	return nil, fmt.Errorf("pipeline not found: %d", id)
}
func (m *mockStore) ListPipelines(_ context.Context) ([]*model.Pipeline, error) {
	out := make([]*model.Pipeline, 0, len(m.pipelines))
	for _, p := range m.pipelines {
		out = append(out, p)
	}
	return out, nil
}
func (m *mockStore) UpdatePipeline(_ context.Context, p *model.Pipeline) error {
	m.pipelines[p.ID] = p
	return nil
}
func (m *mockStore) DeletePipeline(_ context.Context, id int64) error {
	delete(m.pipelines, id)
	return nil
}
func (m *mockStore) SetPipelineActive(_ context.Context, id int64, active bool) error {
	if p, ok := m.pipelines[id]; ok {
		p.Active = active
	}
	return nil
}
func (m *mockStore) GetPipelineGraph(_ context.Context, id int64) (*model.PipelineGraph, error) {
	if g, ok := m.pipelineGraphs[id]; ok {
		return g, nil
	}
	return &model.PipelineGraph{}, nil
}
func (m *mockStore) SavePipelineGraph(_ context.Context, _ int64, _ []*model.PipelineNodeRequest, _ []*model.PipelineEdgeRequest) (*model.PipelineGraph, error) {
	return nil, nil
}
func (m *mockStore) GetPipelineIDForNode(_ context.Context, _ int64) (int64, error) { return 0, nil }

// TriggerPipelineLinks (replaces TriggerChainLinks)
func (m *mockStore) SaveTriggerPipelineLink(_ context.Context, _ *model.TriggerPipelineLink) error {
	return nil
}
func (m *mockStore) ListTriggerPipelineLinks(_ context.Context) ([]*model.TriggerPipelineLink, error) {
	return m.triggerLinks, nil
}
func (m *mockStore) ListActiveTriggerPipelineLinks(_ context.Context, triggerType string, _ int64) ([]*model.TriggerPipelineLink, error) {
	var out []*model.TriggerPipelineLink
	for _, l := range m.triggerLinks {
		if l.Active && l.TriggerType == triggerType {
			out = append(out, l)
		}
	}
	return out, nil
}
func (m *mockStore) DeleteTriggerPipelineLink(_ context.Context, _ int64) error { return nil }
func (m *mockStore) SetTriggerPipelineLinkActive(_ context.Context, _ int64, _ bool) error {
	return nil
}

// Pipeline node execution tracking (sub-pipeline fan-in)
func (m *mockStore) InsertPipelineNodeExecution(_ context.Context, _ int64, _ string, _ int64) error {
	return nil
}
func (m *mockStore) MarkPipelineNodeLeafComplete(_ context.Context, _ int64, _ string, _ int64) (bool, error) {
	return false, nil
}

// ActionLabels
func (m *mockStore) ListActionLabels(_ context.Context, _ int64) ([]string, error) { return nil, nil }
func (m *mockStore) AddActionLabel(_ context.Context, _ int64, _ string) error     { return nil }
func (m *mockStore) RemoveActionLabel(_ context.Context, _ int64, _ string) error  { return nil }

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
func (m *mockStore) ListActionExecutions(_ context.Context, f store.ExecutionFilter) ([]*model.ActionExecution, error) {
	var out []*model.ActionExecution
	for _, e := range m.executions {
		if f.ActionID != nil && e.ActionID != *f.ActionID {
			continue
		}
		out = append(out, e)
	}
	return out, nil
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

func (m *mockStore) ListMapPins(_ context.Context, _, _ int) ([]*store.MapPin, error) {
	return nil, nil
}

func (m *mockStore) ListOllamaBackends(_ context.Context) ([]model.OllamaBackend, error) {
	return nil, nil
}
func (m *mockStore) GetOllamaBackend(_ context.Context, _ int64) (model.OllamaBackend, error) {
	return model.OllamaBackend{}, nil
}
func (m *mockStore) CreateOllamaBackend(_ context.Context, _, _ string, _ int) (model.OllamaBackend, error) {
	return model.OllamaBackend{}, nil
}
func (m *mockStore) UpdateOllamaBackend(_ context.Context, _ int64, _, _ string, _ int) error {
	return nil
}
func (m *mockStore) DeleteOllamaBackend(_ context.Context, _ int64) error             { return nil }
func (m *mockStore) SetOllamaBackendEnabled(_ context.Context, _ int64, _ bool) error { return nil }
func (m *mockStore) ListActionBackends(_ context.Context, _ int64) ([]model.ActionBackend, error) {
	return nil, nil
}
func (m *mockStore) SetActionBackends(_ context.Context, _ int64, _ []model.ActionBackend) error {
	return nil
}
func (m *mockStore) RemoveActionBackend(_ context.Context, _, _ int64) error { return nil }
func (m *mockStore) ListAllActionBackendIDs(_ context.Context) (map[int64][]int64, error) {
	return nil, nil
}

func (m *mockStore) UpsertEntityCandidate(_ context.Context, _ *model.EntityCandidate) error {
	return nil
}
func (m *mockStore) GetEntityCandidate(_ context.Context, _ int64) (*model.EntityCandidate, error) {
	return nil, nil
}
func (m *mockStore) ListEntityCandidates(_ context.Context, _ store.EntityCandidateFilter) ([]*model.EntityCandidate, error) {
	return nil, nil
}
func (m *mockStore) CountEntityCandidates(_ context.Context, _ store.EntityCandidateFilter) (int, error) {
	return 0, nil
}
func (m *mockStore) UpdateEntityCandidateStatus(_ context.Context, _ int64, _ string, _ *int64, _ bool) error {
	return nil
}
func (m *mockStore) DeleteEntityCandidate(_ context.Context, _ int64) error {
	return nil
}

func (m *mockStore) UpsertEntityConnection(_ context.Context, _, _ int64) error {
	return nil
}
func (m *mockStore) ListEntityConnections(_ context.Context) ([]*model.EntityConnection, error) {
	return []*model.EntityConnection{}, nil
}
func (m *mockStore) DeleteAllEntityConnections(_ context.Context) error {
	return nil
}
func (m *mockStore) GetEntityConnectionSubgraph(_ context.Context, _ int64, _, _ int) ([]*model.EntityConnection, error) {
	return []*model.EntityConnection{}, nil
}

func (m *mockStore) GetGeocodeCache(_ context.Context, _, _ float64) (string, bool, error) {
	return "", false, nil
}
func (m *mockStore) SetGeocodeCache(_ context.Context, _, _ float64, _ string) error {
	return nil
}

func (m *mockStore) Close() error { return nil }

func (m *mockStore) BulkDeleteRules(_ context.Context, _ store.BulkDeleteRulesFilter) (int64, error) {
	return 0, nil
}
func (m *mockStore) BulkDeleteSources(_ context.Context, _ store.BulkDeleteSourcesFilter) (int64, error) {
	return 0, nil
}
func (m *mockStore) BulkDeletePipelines(_ context.Context, _ store.BulkDeletePipelinesFilter) (int64, error) {
	return 0, nil
}
func (m *mockStore) BulkDeleteTriggerPipelineLinks(_ context.Context, _ store.BulkDeleteTriggerPipelineLinksFilter) (int64, error) {
	return 0, nil
}
func (m *mockStore) BulkDeleteTrackedEntities(_ context.Context, _ store.BulkDeleteTrackedEntitiesFilter) (int64, error) {
	return 0, nil
}
func (m *mockStore) BulkDeleteEntityCandidates(_ context.Context, _ store.BulkDeleteEntityCandidatesFilter) (int64, error) {
	return 0, nil
}
func (m *mockStore) BulkDeleteActions(_ context.Context, _ store.BulkDeleteActionsFilter) (int64, error) {
	return 0, nil
}
func (m *mockStore) BulkDeleteOllamaBackends(_ context.Context, _ store.BulkDeleteOllamaBackendsFilter) (int64, error) {
	return 0, nil
}
func (m *mockStore) BulkDeleteNotificationProviders(_ context.Context, _ store.BulkDeleteNotificationProvidersFilter) (int64, error) {
	return 0, nil
}
func (m *mockStore) BulkDeleteNotificationChannels(_ context.Context, _ store.BulkDeleteNotificationChannelsFilter) (int64, error) {
	return 0, nil
}
func (m *mockStore) BulkDeleteSearchJobs(_ context.Context, _ store.BulkDeleteSearchJobsFilter) (int64, error) {
	return 0, nil
}
func (m *mockStore) BulkDeleteDigestJobs(_ context.Context, _ store.BulkDeleteDigestJobsFilter) (int64, error) {
	return 0, nil
}
func (m *mockStore) InitPipelineRunTracking(_ context.Context, _ int64, _ string, _ int, _ []byte) error {
	return nil
}
func (m *mockStore) IncrementPipelineRunTracking(_ context.Context, _ int64, _ string) error {
	return nil
}
func (m *mockStore) DecrementPipelineRunTracking(_ context.Context, _ int64, _ string) (bool, error) {
	return false, nil
}
func (m *mockStore) GetPipelineRunEventPayload(_ context.Context, _ int64, _ string) ([]byte, error) {
	return nil, nil
}

// ---------------------------------------------------------------------------
// mockProducer
// ---------------------------------------------------------------------------

type mockProducer struct {
	mu     sync.Mutex
	pushed []*queue.Task
}

func (m *mockProducer) PushTask(_ context.Context, t *queue.Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pushed = append(m.pushed, t)
	return nil
}
func (m *mockProducer) PushFailed(_ context.Context, _ *queue.FailedTweet) error { return nil }

// waitForPushes polls until mp.pushed has at least n entries or deadline is exceeded.
func (m *mockProducer) waitForPushes(t *testing.T, n int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		got := len(m.pushed)
		m.mu.Unlock()
		if got >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// pushedSnapshot returns a snapshot of pushed tasks under the lock.
func (m *mockProducer) pushedSnapshot() []*queue.Task {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*queue.Task, len(m.pushed))
	copy(out, m.pushed)
	return out
}

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
	srv := NewServer("127.0.0.1:0", ms, mp, nil, nil, nil, nil, mr, nil, "basic", nil, 0, "", logger, nil, nil, nil, nil, nil, "", nil)
	return srv, ms, mp, mr
}

// adminToken returns a valid admin JWT for the test server (jwtSecret="").
func adminToken(t *testing.T) string {
	t.Helper()
	tok, err := auth.IssueToken("", &model.User{ID: 1, Username: "admin", Role: model.RoleAdmin})
	if err != nil {
		t.Fatalf("adminToken: %v", err)
	}
	return tok
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestCreateSource(t *testing.T) {
	srv, ms, _, _ := newTestServer(t)

	body := `{"type":"account","value":"testuser"}`
	req := httptest.NewRequest(http.MethodPost, "/api/sources", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken(t))
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
	req.Header.Set("Authorization", "Bearer "+adminToken(t))
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
	req.Header.Set("Authorization", "Bearer "+adminToken(t))
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result struct {
		Tweets []*model.Tweet `json:"tweets"`
		Total  int64          `json:"total"`
	}
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
	req.Header.Set("Authorization", "Bearer "+adminToken(t))
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

	// Use targeted action_ids dispatch to bypass the pipeline/trigger system.
	body := `{"tweet_ids":["t1","t2"],"action_ids":[42]}`
	req := httptest.NewRequest(http.MethodPost, "/api/tweets/reprocess", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken(t))
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
	if len(mp.pushedSnapshot()) != 2 {
		t.Errorf("expected 2 pushed tweets, got %d", len(mp.pushedSnapshot()))
	}
}

func TestListModels_ReturnsEmpty(t *testing.T) {
	// GET /api/models with no modelsDir set returns empty list.
	srv, _, _, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/models", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken(t))
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetHealth(t *testing.T) {
	srv, _, _, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken(t))
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

func TestRunPipeline_NotFound(t *testing.T) {
	srv, _, _, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/pipelines/99/run", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken(t))
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRunPipeline_Inactive(t *testing.T) {
	srv, ms, _, _ := newTestServer(t)
	ms.pipelines[1] = &model.Pipeline{ID: 1, Name: "p1", Active: false}

	req := httptest.NewRequest(http.MethodPost, "/api/pipelines/1/run", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken(t))
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRunPipeline_QueuesTweets(t *testing.T) {
	srv, ms, mp, _ := newTestServer(t)

	// active pipeline
	ms.pipelines[1] = &model.Pipeline{ID: 1, Name: "p1", Active: true}

	// trigger link for this pipeline
	ms.triggerLinks = []*model.TriggerPipelineLink{
		{ID: 1, PipelineID: 1, TriggerType: "new_tweet", Active: true},
	}

	// pipeline graph: start → action node (action_id=5, node_id=10)
	actionID := int64(5)
	ms.pipelineGraphs[1] = &model.PipelineGraph{
		Nodes: []*model.PipelineNode{
			{ID: 9, PipelineID: 1, Type: model.PipelineNodeTypeStart},
			{ID: 10, PipelineID: 1, Type: model.PipelineNodeTypeAction, ActionID: &actionID},
		},
		Edges: []*model.PipelineEdge{
			{ID: 1, PipelineID: 1, SourceNodeID: 9, TargetNodeID: 10},
		},
	}

	// 2 tweets
	ms.tweets["t1"] = &model.Tweet{ID: "t1", Text: "tweet 1", AuthorHandle: "h1"}
	ms.tweets["t2"] = &model.Tweet{ID: "t2", Text: "tweet 2", AuthorHandle: "h2"}

	req := httptest.NewRequest(http.MethodPost, "/api/pipelines/1/run", strings.NewReader(`{"new_only":false}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken(t))
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}

	// Dispatch is async — response always has queued=0; wait for background goroutine.
	mp.waitForPushes(t, 2, 2*time.Second)
	pushed := mp.pushedSnapshot()
	if len(pushed) != 2 {
		t.Fatalf("expected 2 pushed tasks, got %d", len(pushed))
	}
	for _, task := range pushed {
		if task.PipelineID != 1 {
			t.Errorf("expected PipelineID=1, got %d", task.PipelineID)
		}
		if task.PipelineNodeID != 10 {
			t.Errorf("expected PipelineNodeID=10, got %d", task.PipelineNodeID)
		}
		if task.ActionID != 5 {
			t.Errorf("expected ActionID=5, got %d", task.ActionID)
		}
	}
}

func TestRunPipeline_NewOnly(t *testing.T) {
	srv, ms, mp, _ := newTestServer(t)

	ms.pipelines[1] = &model.Pipeline{ID: 1, Name: "p1", Active: true}
	ms.triggerLinks = []*model.TriggerPipelineLink{
		{ID: 1, PipelineID: 1, TriggerType: "new_tweet", Active: true},
	}
	actionID := int64(5)
	ms.pipelineGraphs[1] = &model.PipelineGraph{
		Nodes: []*model.PipelineNode{
			{ID: 9, PipelineID: 1, Type: model.PipelineNodeTypeStart},
			{ID: 10, PipelineID: 1, Type: model.PipelineNodeTypeAction, ActionID: &actionID},
		},
		Edges: []*model.PipelineEdge{
			{ID: 1, PipelineID: 1, SourceNodeID: 9, TargetNodeID: 10},
		},
	}
	ms.tweets["t1"] = &model.Tweet{ID: "t1", Text: "tweet 1", AuthorHandle: "h1"}
	ms.tweets["t2"] = &model.Tweet{ID: "t2", Text: "tweet 2", AuthorHandle: "h2"}

	// t1 already executed
	ms.executions = []*model.ActionExecution{
		{ID: 1, ActionID: 5, TweetID: "t1", Status: "success"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/pipelines/1/run", strings.NewReader(`{"new_only":true}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken(t))
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}

	// Dispatch is async — wait for the background goroutine to push the task.
	mp.waitForPushes(t, 1, 2*time.Second)
	pushed := mp.pushedSnapshot()
	if len(pushed) != 1 {
		t.Fatalf("expected 1 pushed task, got %d", len(pushed))
	}
	if pushed[0].ActionID != 5 {
		t.Errorf("expected ActionID=5, got %d", pushed[0].ActionID)
	}
}

func TestRunPipeline_EntityMatched(t *testing.T) {
	srv, ms, mp, _ := newTestServer(t)

	ms.pipelines[1] = &model.Pipeline{ID: 1, Name: "p1", Active: true}
	ms.triggerLinks = []*model.TriggerPipelineLink{
		{ID: 1, PipelineID: 1, TriggerType: "entity_matched", Active: true},
	}
	actionID := int64(5)
	ms.pipelineGraphs[1] = &model.PipelineGraph{
		Nodes: []*model.PipelineNode{
			{ID: 9, PipelineID: 1, Type: model.PipelineNodeTypeStart},
			{ID: 10, PipelineID: 1, Type: model.PipelineNodeTypeAction, ActionID: &actionID},
		},
		Edges: []*model.PipelineEdge{
			{ID: 1, PipelineID: 1, SourceNodeID: 9, TargetNodeID: 10},
		},
	}
	ms.tweets["t1"] = &model.Tweet{ID: "t1", Text: "tweet 1", AuthorHandle: "h1"}
	ms.tweets["t2"] = &model.Tweet{ID: "t2", Text: "tweet 2", AuthorHandle: "h2"}
	ms.tweets["t3"] = &model.Tweet{ID: "t3", Text: "tweet 3", AuthorHandle: "h3"}
	ms.entityTrackedTweets = map[string]bool{"t1": true, "t2": true}

	req := httptest.NewRequest(http.MethodPost, "/api/pipelines/1/run", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken(t))
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}

	// Dispatch is async — wait for the background goroutine to push tasks.
	mp.waitForPushes(t, 2, 2*time.Second)
	pushed := mp.pushedSnapshot()
	if len(pushed) != 2 {
		t.Fatalf("expected 2 pushed tasks, got %d", len(pushed))
	}
}

func TestRunPipeline_InvalidStartNodeType(t *testing.T) {
	srv, ms, _, _ := newTestServer(t)
	ms.pipelines[1] = &model.Pipeline{ID: 1, Name: "p1", Active: true}
	ms.triggerLinks = []*model.TriggerPipelineLink{
		{ID: 1, PipelineID: 1, TriggerType: "new_tweet", Active: true},
	}
	ms.pipelineGraphs[1] = &model.PipelineGraph{
		Nodes: []*model.PipelineNode{
			{ID: 9, PipelineID: 1, Type: model.PipelineNodeTypeStart},
		},
		Edges: []*model.PipelineEdge{},
	}

	body := `{"start_node_id": 9}` // start-type node — should be rejected
	req := httptest.NewRequest(http.MethodPost, "/api/pipelines/1/run", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken(t))
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRunPipeline_StartNodeIdDispatchesFromNode(t *testing.T) {
	srv, ms, mp, _ := newTestServer(t)
	ms.pipelines[1] = &model.Pipeline{ID: 1, Name: "p1", Active: true}
	ms.triggerLinks = []*model.TriggerPipelineLink{
		{ID: 1, PipelineID: 1, TriggerType: "new_tweet", Active: true},
	}
	actionID1 := int64(5)
	actionID2 := int64(6)
	ms.pipelineGraphs[1] = &model.PipelineGraph{
		Nodes: []*model.PipelineNode{
			{ID: 9, PipelineID: 1, Type: model.PipelineNodeTypeStart},
			{ID: 10, PipelineID: 1, Type: model.PipelineNodeTypeAction, ActionID: &actionID1},
			{ID: 11, PipelineID: 1, Type: model.PipelineNodeTypeAction, ActionID: &actionID2},
		},
		Edges: []*model.PipelineEdge{
			{ID: 1, PipelineID: 1, SourceNodeID: 9, TargetNodeID: 10},
			{ID: 2, PipelineID: 1, SourceNodeID: 10, TargetNodeID: 11},
		},
	}
	ms.tweets["t1"] = &model.Tweet{ID: "t1", Text: "tweet 1", AuthorHandle: "h1"}

	body := `{"start_node_id": 11}` // start from node 11 (action 6), skipping node 10
	req := httptest.NewRequest(http.MethodPost, "/api/pipelines/1/run", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken(t))
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}

	// Handler dispatches in a background goroutine; wait for it.
	mp.waitForPushes(t, 1, 2*time.Second)
	pushed := mp.pushedSnapshot()

	if len(pushed) != 1 {
		t.Fatalf("expected 1 pushed task (only node 11), got %d", len(pushed))
	}
	if pushed[0].PipelineNodeID != 11 {
		t.Errorf("expected PipelineNodeID=11, got %d", pushed[0].PipelineNodeID)
	}
	if pushed[0].ActionID != 6 {
		t.Errorf("expected ActionID=6, got %d", pushed[0].ActionID)
	}
}

func TestRunPipeline_StartNodeIdNewOnlyScoped(t *testing.T) {
	srv, ms, mp, _ := newTestServer(t)
	ms.pipelines[1] = &model.Pipeline{ID: 1, Name: "p1", Active: true}
	ms.triggerLinks = []*model.TriggerPipelineLink{
		{ID: 1, PipelineID: 1, TriggerType: "new_tweet", Active: true},
	}
	actionID1 := int64(5)
	actionID2 := int64(6)
	ms.pipelineGraphs[1] = &model.PipelineGraph{
		Nodes: []*model.PipelineNode{
			{ID: 9, PipelineID: 1, Type: model.PipelineNodeTypeStart},
			{ID: 10, PipelineID: 1, Type: model.PipelineNodeTypeAction, ActionID: &actionID1},
			{ID: 11, PipelineID: 1, Type: model.PipelineNodeTypeAction, ActionID: &actionID2},
		},
		Edges: []*model.PipelineEdge{
			{ID: 1, PipelineID: 1, SourceNodeID: 9, TargetNodeID: 10},
			{ID: 2, PipelineID: 1, SourceNodeID: 10, TargetNodeID: 11},
		},
	}
	ms.tweets["t1"] = &model.Tweet{ID: "t1", Text: "tweet 1", AuthorHandle: "h1"}
	ms.tweets["t2"] = &model.Tweet{ID: "t2", Text: "tweet 2", AuthorHandle: "h2"}

	// t1 already ran action 6 (node 11's action) — should be excluded.
	// t1 also ran action 5 (node 10) — irrelevant when start_node_id=11.
	ms.executions = []*model.ActionExecution{
		{ID: 1, ActionID: 6, TweetID: "t1", Status: "success"},
		{ID: 2, ActionID: 5, TweetID: "t2", Status: "success"}, // only ran upstream action, NOT action 6
	}

	body := `{"new_only": true, "start_node_id": 11}`
	req := httptest.NewRequest(http.MethodPost, "/api/pipelines/1/run", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken(t))
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}

	// Handler dispatches in a background goroutine; wait for it.
	mp.waitForPushes(t, 1, 2*time.Second)
	pushed := mp.pushedSnapshot()

	// Only t2 should be queued: t1 already ran action 6, t2 only ran action 5.
	if len(pushed) != 1 {
		t.Fatalf("expected 1 pushed task, got %d", len(pushed))
	}
	tweetIDs := make(map[string]bool)
	for _, task := range pushed {
		var ev struct {
			TweetID string `json:"tweet_id"`
		}
		_ = json.Unmarshal(task.Event, &ev)
		tweetIDs[ev.TweetID] = true
	}
	if !tweetIDs["t2"] {
		t.Errorf("expected t2 to be queued, got tasks for: %v", tweetIDs)
	}
}

func TestRunPipeline_AscOrder(t *testing.T) {
	srv, ms, mp, _ := newTestServer(t)
	ms.pipelines[1] = &model.Pipeline{ID: 1, Name: "p1", Active: true}
	ms.triggerLinks = []*model.TriggerPipelineLink{
		{ID: 1, PipelineID: 1, TriggerType: "new_tweet", Active: true},
	}
	actionID := int64(5)
	ms.pipelineGraphs[1] = &model.PipelineGraph{
		Nodes: []*model.PipelineNode{
			{ID: 1, PipelineID: 1, Type: model.PipelineNodeTypeStart},
			{ID: 2, PipelineID: 1, Type: model.PipelineNodeTypeAction, ActionID: &actionID},
		},
		Edges: []*model.PipelineEdge{
			{ID: 1, PipelineID: 1, SourceNodeID: 1, TargetNodeID: 2},
		},
	}
	ms.tweets["t_old"] = &model.Tweet{
		ID: "t_old", Text: "old tweet", AuthorHandle: "h1",
		CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	ms.tweets["t_new"] = &model.Tweet{
		ID: "t_new", Text: "new tweet", AuthorHandle: "h2",
		CreatedAt: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
	}

	body := `{"order": "asc"}`
	req := httptest.NewRequest(http.MethodPost, "/api/pipelines/1/run", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken(t))
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}

	// Handler dispatches in a background goroutine; wait for it.
	mp.waitForPushes(t, 2, 2*time.Second)
	pushed := mp.pushedSnapshot()

	if len(pushed) != 2 {
		t.Fatalf("expected 2 pushed tasks, got %d", len(pushed))
	}
	var ev0 struct {
		TweetID string `json:"tweet_id"`
	}
	_ = json.Unmarshal(pushed[0].Event, &ev0)
	if ev0.TweetID != "t_old" {
		t.Errorf("expected first task for t_old (oldest), got %s", ev0.TweetID)
	}
}

// Ensure mockStore and mockProducer satisfy their interfaces at compile time.
var _ store.Store = (*mockStore)(nil)
var _ queue.Producer = (*mockProducer)(nil)
