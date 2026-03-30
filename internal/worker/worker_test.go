package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/queue"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

// --- mockConsumer ---

type mockConsumer struct {
	tasks []*queue.Task
	idx   int
}

func (m *mockConsumer) PopTask(_ context.Context, _ time.Duration, _ ...string) (*queue.Task, error) {
	if m.idx >= len(m.tasks) {
		return nil, nil
	}
	t := m.tasks[m.idx]
	m.idx++
	return t, nil
}

func (m *mockConsumer) PopFailed(_ context.Context, _ time.Duration) (*queue.FailedTweet, error) {
	return nil, nil
}

// --- mockProducer ---

type mockProducer struct {
	pushed []*queue.Task
}

func (m *mockProducer) PushTask(_ context.Context, t *queue.Task) error {
	m.pushed = append(m.pushed, t)
	return nil
}

func (m *mockProducer) PushFailed(_ context.Context, _ *queue.FailedTweet) error {
	return nil
}

// --- mockStore ---

type mockStore struct {
	executions []*model.ActionExecution
}

func (m *mockStore) SaveActionExecution(_ context.Context, e *model.ActionExecution) error {
	e.ID = int64(len(m.executions) + 1)
	m.executions = append(m.executions, e)
	return nil
}
func (m *mockStore) SaveActionExecutionMetadata(_ context.Context, _ int64, _, _ string) error {
	return nil
}
// Tweets
func (m *mockStore) SaveTweet(_ context.Context, _ *model.Tweet) error { return nil }
func (m *mockStore) GetTweet(_ context.Context, _ string) (*model.Tweet, error) {
	return nil, nil
}
func (m *mockStore) ListTweets(_ context.Context, _ store.TweetFilter) ([]*model.Tweet, error) {
	return nil, nil
}
func (m *mockStore) CountTweets(_ context.Context, _ store.TweetFilter) (int, error) { return 0, nil }
func (m *mockStore) DeleteTweets(_ context.Context, _ []string) (int64, error)       { return 0, nil }
func (m *mockStore) DeleteNoiseTweets(_ context.Context) (int64, error)              { return 0, nil }

// Sources
func (m *mockStore) SaveSource(_ context.Context, _ *model.Source) error         { return nil }
func (m *mockStore) UpdateSource(_ context.Context, _ *model.Source) error       { return nil }
func (m *mockStore) GetSource(_ context.Context, _ int64) (*model.Source, error) { return nil, nil }
func (m *mockStore) ListSources(_ context.Context, _ store.SourceFilter) ([]*model.Source, error) {
	return nil, nil
}
func (m *mockStore) DeleteSource(_ context.Context, _ int64) error              { return nil }
func (m *mockStore) SetSourceActive(_ context.Context, _ int64, _ bool) error   { return nil }
func (m *mockStore) ActiveRuleRefCount(_ context.Context, _ int64) (int, error) { return 0, nil }
func (m *mockStore) RuleRefCount(_ context.Context, _ int64) (int, error)       { return 0, nil }

// Rules
func (m *mockStore) SaveRule(_ context.Context, _ *model.Rule) error         { return nil }
func (m *mockStore) GetRule(_ context.Context, _ int64) (*model.Rule, error) { return nil, nil }
func (m *mockStore) GetRuleByXRuleID(_ context.Context, _ string) (*model.Rule, error) {
	return nil, nil
}
func (m *mockStore) ListRules(_ context.Context, _ store.RuleFilter) ([]*model.Rule, error) {
	return nil, nil
}
func (m *mockStore) ListRulesDependingOn(_ context.Context, _ int64) ([]*model.Rule, error) {
	return nil, nil
}
func (m *mockStore) UpdateRule(_ context.Context, _ *model.Rule) error                { return nil }
func (m *mockStore) DeleteRule(_ context.Context, _ int64) error                      { return nil }
func (m *mockStore) SetRuleActive(_ context.Context, _ int64, _ bool, _ string) error { return nil }
func (m *mockStore) TweetRuleCount(_ context.Context, _ int64) (int, error)           { return 0, nil }
func (m *mockStore) SaveTweetRule(_ context.Context, _ *model.TweetRule) error        { return nil }
func (m *mockStore) ListTweetRuleIDs(_ context.Context, _ string) ([]int64, error)    { return nil, nil }

// Pipelines
func (m *mockStore) SavePipeline(_ context.Context, _ *model.Pipeline) error { return nil }
func (m *mockStore) GetPipeline(_ context.Context, _ int64) (*model.Pipeline, error) {
	return nil, fmt.Errorf("not found")
}
func (m *mockStore) ListPipelines(_ context.Context, _ store.PipelineFilter) ([]*model.Pipeline, error) {
	return nil, nil
}
func (m *mockStore) UpdatePipeline(_ context.Context, _ *model.Pipeline) error  { return nil }
func (m *mockStore) DeletePipeline(_ context.Context, _ int64) error            { return nil }
func (m *mockStore) SetPipelineActive(_ context.Context, _ int64, _ bool) error { return nil }
func (m *mockStore) ListPipelineLabels(_ context.Context, _ int64) ([]string, error) {
	return nil, nil
}
func (m *mockStore) AddPipelineLabel(_ context.Context, _ int64, _ string) error    { return nil }
func (m *mockStore) RemovePipelineLabel(_ context.Context, _ int64, _ string) error { return nil }

// Chains
func (m *mockStore) SaveChain(_ context.Context, _ *model.Chain) error { return nil }
func (m *mockStore) GetChain(_ context.Context, _ int64) (*model.Chain, error) {
	return nil, nil
}
func (m *mockStore) ListChains(_ context.Context) ([]*model.Chain, error)     { return nil, nil }
func (m *mockStore) UpdateChain(_ context.Context, _ *model.Chain) error      { return nil }
func (m *mockStore) DeleteChain(_ context.Context, _ int64) error             { return nil }
func (m *mockStore) SetChainActive(_ context.Context, _ int64, _ bool) error  { return nil }
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
func (m *mockStore) DeleteTriggerChainLink(_ context.Context, _ int64) error             { return nil }
func (m *mockStore) SetTriggerChainLinkActive(_ context.Context, _ int64, _ bool) error  { return nil }
func (m *mockStore) GetChainIDForNode(_ context.Context, _ int64) (int64, error)         { return 0, nil }

// NotificationProviders
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

// NotificationChannels
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

// TrackedEntities
func (m *mockStore) SaveTrackedEntity(_ context.Context, _ *model.TrackedEntity) error {
	return nil
}
func (m *mockStore) GetTrackedEntity(_ context.Context, _ int64) (*model.TrackedEntity, error) {
	return nil, nil
}
func (m *mockStore) ListTrackedEntities(_ context.Context, _ store.TrackedEntityFilter) ([]*model.TrackedEntity, error) {
	return nil, nil
}
func (m *mockStore) UpdateTrackedEntity(_ context.Context, _ *model.TrackedEntity) error {
	return nil
}
func (m *mockStore) DeleteTrackedEntity(_ context.Context, _ int64) error            { return nil }
func (m *mockStore) SetTrackedEntityActive(_ context.Context, _ int64, _ bool) error { return nil }
func (m *mockStore) ListActiveTrackedEntities(_ context.Context) ([]*model.TrackedEntity, error) {
	return nil, nil
}

// EntityTracks
func (m *mockStore) SaveEntityTrack(_ context.Context, _ *model.EntityTrack) error { return nil }
func (m *mockStore) ListEntityTracks(_ context.Context, _ store.EntityTrackFilter) ([]*model.EntityTrack, error) {
	return nil, nil
}
func (m *mockStore) CountEntityTracks(_ context.Context, _ store.EntityTrackFilter) (int, error) {
	return 0, nil
}
func (m *mockStore) ListNERSuggestionsForEntity(_ context.Context, _ int64) ([]string, error) {
	return nil, nil
}
func (m *mockStore) SearchNERSuggestions(_ context.Context, _, _ string) ([]string, error) {
	return nil, nil
}

// Summaries
func (m *mockStore) ListSummaries(_ context.Context, _ store.SummaryFilter) ([]*model.SummaryItem, error) {
	return nil, nil
}
func (m *mockStore) CountSummaries(_ context.Context, _ store.SummaryFilter) (int, error) {
	return 0, nil
}

// DigestJobs
func (m *mockStore) SaveDigestJob(_ context.Context, _ *model.DigestJob) error { return nil }
func (m *mockStore) GetDigestJob(_ context.Context, _ string) (*model.DigestJob, error) {
	return nil, nil
}
func (m *mockStore) UpdateDigestJob(_ context.Context, _ *model.DigestJob) error { return nil }
func (m *mockStore) ListDigestJobs(_ context.Context, _ int) ([]*model.DigestJob, error) {
	return nil, nil
}
func (m *mockStore) DeleteDigestJob(_ context.Context, _ string) error { return nil }

// SearchJobs
func (m *mockStore) SaveSearchJob(_ context.Context, _ *model.SearchJob) error { return nil }
func (m *mockStore) GetSearchJob(_ context.Context, _ string) (*model.SearchJob, error) {
	return nil, nil
}
func (m *mockStore) UpdateSearchJob(_ context.Context, _ *model.SearchJob) error { return nil }
func (m *mockStore) ListSearchJobs(_ context.Context, _ int) ([]*model.SearchJob, error) {
	return nil, nil
}
func (m *mockStore) DeleteSearchJob(_ context.Context, _ string) error { return nil }

// Users
func (m *mockStore) SaveUser(_ context.Context, _ *model.User) error         { return nil }
func (m *mockStore) GetUser(_ context.Context, _ int64) (*model.User, error) { return nil, nil }
func (m *mockStore) GetUserByUsername(_ context.Context, _ string) (*model.User, error) {
	return nil, nil
}
func (m *mockStore) ListUsers(_ context.Context) ([]*model.User, error) { return nil, nil }
func (m *mockStore) UpdateUser(_ context.Context, _ *model.User) error  { return nil }
func (m *mockStore) DeleteUser(_ context.Context, _ int64) error        { return nil }
func (m *mockStore) CountAdmins(_ context.Context) (int, error)         { return 1, nil }

// Actions
func (m *mockStore) SaveAction(_ context.Context, _ *model.Action) error { return nil }
func (m *mockStore) GetAction(_ context.Context, _ int64) (*model.Action, error) {
	return nil, fmt.Errorf("not found")
}
func (m *mockStore) ListActions(_ context.Context, _ store.ActionFilter) ([]*model.Action, error) {
	return nil, nil
}
func (m *mockStore) UpdateAction(_ context.Context, _ *model.Action) error    { return nil }
func (m *mockStore) DeleteAction(_ context.Context, _ int64) error            { return nil }
func (m *mockStore) SetActionActive(_ context.Context, _ int64, _ bool) error { return nil }

// ActionExecutions
func (m *mockStore) ListActionExecutions(_ context.Context, _ store.ExecutionFilter) ([]*model.ActionExecution, error) {
	return nil, nil
}
func (m *mockStore) ListActionExecutionMetadata(_ context.Context, _ int64) (map[string]string, error) {
	return nil, nil
}
func (m *mockStore) MergeActionExecutionMetadata(_ context.Context, _ int64, _ string) (map[string]string, error) {
	return nil, nil
}
func (m *mockStore) MergeAllTweetExecutionMetadata(_ context.Context, _ string) (map[string]string, error) {
	return nil, nil
}
func (m *mockStore) ListTweetActionSummaries(_ context.Context, _ string) ([]store.TweetActionSummary, error) {
	return nil, nil
}
func (m *mockStore) DeleteActionExecutions(_ context.Context, _ []string, _ []int64) (int64, error) {
	return 0, nil
}

// TwitterCredentials
func (m *mockStore) SaveTwitterCredentials(_ context.Context, _ *model.TwitterCredentials) error {
	return nil
}
func (m *mockStore) GetTwitterCredentials(_ context.Context) (*model.TwitterCredentials, error) {
	return nil, fmt.Errorf("not found")
}
func (m *mockStore) DeleteTwitterCredentials(_ context.Context) error { return nil }

func (m *mockStore) ListMapPins(_ context.Context, _, _ int) ([]*store.MapPin, error) { return nil, nil }

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

func (m *mockStore) ListEntityCandidates(_ context.Context, _ store.EntityCandidateFilter) ([]*model.EntityCandidate, error) { return nil, nil }
func (m *mockStore) GetEntityCandidate(_ context.Context, _ int64) (*model.EntityCandidate, error) { return nil, nil }
func (m *mockStore) UpsertEntityCandidate(_ context.Context, _ *model.EntityCandidate) error { return nil }
func (m *mockStore) DeleteEntityCandidate(_ context.Context, _ int64) error { return nil }
func (m *mockStore) UpdateEntityCandidateStatus(_ context.Context, _ int64, _ string, _ *int64, _ bool) error { return nil }
func (m *mockStore) CountEntityCandidates(_ context.Context, _ store.EntityCandidateFilter) (int, error) { return 0, nil }
func (m *mockStore) UpsertEntityConnection(_ context.Context, _, _ int64) error { return nil }
func (m *mockStore) ListEntityConnections(_ context.Context) ([]*model.EntityConnection, error) { return nil, nil }
func (m *mockStore) DeleteAllEntityConnections(_ context.Context) error { return nil }
func (m *mockStore) Close() error { return nil }

// --- stubPipeline ---

type stubPipeline struct {
	name  string
	pType inference.PipelineType
}

func (s *stubPipeline) Name() string                 { return s.name }
func (s *stubPipeline) Type() inference.PipelineType { return s.pType }
func (s *stubPipeline) Load(_ string) error          { return nil }
func (s *stubPipeline) Run(_ context.Context, _ inference.PipelineInput) (map[string]any, error) {
	return map[string]any{"label": "test", "score": 1.0}, nil
}
func (s *stubPipeline) Close() error { return nil }

// --- helpers ---

func makeTask(actionID int64, tweetID string) *queue.Task {
	ev, _ := json.Marshal(queue.TweetEvent{TweetID: tweetID, Text: "test tweet text"})
	return &queue.Task{
		TriggerType: "new_tweet",
		Event:       ev,
		ActionID:    actionID,
		Context:     map[string]any{},
	}
}

// buildCacheWithEntries creates an ActionCache populated with the given entries
// without going through the DB by directly setting the internal map.
func buildCacheWithEntries(entries map[int64]*CacheEntry) *ActionCache {
	return &ActionCache{entries: entries}
}

// --- Pool tests ---

func TestPool_Start_LaunchesWorkers(t *testing.T) {
	consumer := &mockConsumer{}
	producer := &mockProducer{}
	storeImpl := &mockStore{}
	reg := inference.NewRegistry()

	pool := NewPool(2, consumer, producer, storeImpl, reg, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- pool.Start(ctx)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Pool.Start did not return within timeout after ctx cancelled")
	}
}

func TestPool_Start_RespectsCtxCancel(t *testing.T) {
	consumer := &mockConsumer{}
	producer := &mockProducer{}
	storeImpl := &mockStore{}
	reg := inference.NewRegistry()

	pool := NewPool(2, consumer, producer, storeImpl, reg, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan error, 1)
	go func() {
		done <- pool.Start(ctx)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Pool.Start did not return within 1 second after immediate ctx cancel")
	}
}

// --- Manager tests ---

func runManagerUntilDrained(t *testing.T, mgr *Manager, consumer *mockConsumer) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = mgr.Start(ctx)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		if consumer.idx >= len(consumer.tasks) && int(mgr.active.Load()) == 0 {
			break
		}
	}
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Manager.Start did not stop within timeout")
	}
}

func TestManager_ProcessesTweet(t *testing.T) {
	pipe := &stubPipeline{name: "pa", pType: inference.PipelineTypeONNX}
	action := &model.Action{ID: 1, Name: "pa", Active: true}
	cache := buildCacheWithEntries(map[int64]*CacheEntry{
		1: {Action: action, Pipeline: pipe},
	})

	consumer := &mockConsumer{tasks: []*queue.Task{makeTask(1, "tweet-m1")}}
	producer := &mockProducer{}
	storeImpl := &mockStore{}

	mgr := NewManager(2, consumer, producer, storeImpl, cache, nil)
	runManagerUntilDrained(t, mgr, consumer)

	if len(storeImpl.executions) != 1 {
		t.Fatalf("expected 1 execution, got %d", len(storeImpl.executions))
	}
	if storeImpl.executions[0].TweetID != "tweet-m1" {
		t.Errorf("wrong tweet id: %s", storeImpl.executions[0].TweetID)
	}
}

func TestManager_UnknownActionID_Skipped(t *testing.T) {
	cache := buildCacheWithEntries(map[int64]*CacheEntry{})

	consumer := &mockConsumer{tasks: []*queue.Task{makeTask(999, "tweet-unknown")}}
	producer := &mockProducer{}
	storeImpl := &mockStore{}

	mgr := NewManager(2, consumer, producer, storeImpl, cache, nil)
	runManagerUntilDrained(t, mgr, consumer)

	if len(storeImpl.executions) != 0 {
		t.Fatalf("expected 0 executions, got %d", len(storeImpl.executions))
	}
}

func TestManager_Start_RespectsCtxCancel(t *testing.T) {
	cache := buildCacheWithEntries(map[int64]*CacheEntry{})
	consumer := &mockConsumer{}
	storeImpl := &mockStore{}
	producer := &mockProducer{}

	mgr := NewManager(2, consumer, producer, storeImpl, cache, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan error, 1)
	go func() {
		done <- mgr.Start(ctx)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Manager.Start did not return within 1 second after immediate ctx cancel")
	}
}

func TestManager_Stats_ActiveWorkers(t *testing.T) {
	unblock := make(chan struct{})
	started := make(chan struct{}, 1)
	done := make(chan struct{})

	stub := &stubPipeline{name: "slow-action", pType: inference.PipelineTypeONNX}
	pipe := &blockPipeline{stub: stub, started: started, unblock: unblock, done: done}

	action := &model.Action{ID: 1, Name: "slow-action", Active: true}
	cache := buildCacheWithEntries(map[int64]*CacheEntry{
		1: {Action: action, Pipeline: pipe},
	})

	consumer := &mockConsumer{tasks: []*queue.Task{makeTask(1, "tweet-stats")}}
	producer := &mockProducer{}
	storeImpl := &mockStore{}

	mgr := NewManager(2, consumer, producer, storeImpl, cache, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = mgr.Start(ctx) }()

	// Wait until the pipeline has started executing.
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("pipeline did not start within 2s")
	}

	stats := mgr.Stats()
	if stats.Active != 1 {
		t.Errorf("expected 1 active worker, got %d", stats.Active)
	}
	if len(stats.ActiveWorkers) != 1 {
		t.Fatalf("expected 1 entry in ActiveWorkers, got %d", len(stats.ActiveWorkers))
	}
	if stats.ActiveWorkers[0].Action != "slow-action" {
		t.Errorf("expected action 'slow-action', got %q", stats.ActiveWorkers[0].Action)
	}
	if stats.ActiveWorkers[0].Pipeline != "slow-action" {
		t.Errorf("expected pipeline 'slow-action', got %q", stats.ActiveWorkers[0].Pipeline)
	}

	// Unblock and verify workers clear.
	close(unblock)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pipeline did not complete within 2s")
	}
	// Give the defer in processTask a moment to clean up workerActions.
	time.Sleep(10 * time.Millisecond)

	stats = mgr.Stats()
	if len(stats.ActiveWorkers) != 0 {
		t.Errorf("expected 0 active workers after completion, got %d", len(stats.ActiveWorkers))
	}
}

func TestManager_Throughput_PopulatedAfterCompletions(t *testing.T) {
	pipe := &stubPipeline{name: "tp", pType: inference.PipelineTypeONNX}
	action := &model.Action{ID: 7, Name: "tp", Active: true}
	cache := buildCacheWithEntries(map[int64]*CacheEntry{
		7: {Action: action, Pipeline: pipe},
	})

	tasks := []*queue.Task{
		makeTask(7, "tw-1"),
		makeTask(7, "tw-2"),
		makeTask(7, "tw-3"),
	}
	consumer := &mockConsumer{tasks: tasks}
	producer := &mockProducer{}
	storeImpl := &mockStore{}

	mgr := NewManager(2, consumer, producer, storeImpl, cache, nil)
	runManagerUntilDrained(t, mgr, consumer)

	throughput := mgr.throughputPerAction()
	if throughput[7] <= 0 {
		t.Errorf("expected throughput > 0 for action 7 after 3 completions, got %f", throughput[7])
	}
}

// blockPipeline is a Pipeline that signals started and blocks until unblock is closed.
type blockPipeline struct {
	stub    *stubPipeline
	started chan struct{}
	unblock chan struct{}
	done    chan struct{}
}

func (b *blockPipeline) Name() string                 { return b.stub.Name() }
func (b *blockPipeline) Type() inference.PipelineType { return b.stub.Type() }
func (b *blockPipeline) Load(s string) error          { return b.stub.Load(s) }
func (b *blockPipeline) Close() error                 { return b.stub.Close() }
func (b *blockPipeline) Run(_ context.Context, _ inference.PipelineInput) (map[string]any, error) {
	select {
	case b.started <- struct{}{}:
	default:
	}
	<-b.unblock
	if b.done != nil {
		close(b.done)
	}
	return map[string]any{"label": "test"}, nil
}

// ── enrichContext tests ───────────────────────────────────────────────────────

func TestEnrichContext_ZeroShot_TopLabel(t *testing.T) {
	ctx := map[string]any{
		"labels": []any{"political", "news"},
		"scores": []any{float64(0.8), float64(0.2)},
	}
	enrichContext(ctx)
	if ctx["top_label"] != "political" {
		t.Errorf("top_label: got %v, want political", ctx["top_label"])
	}
	if ctx["top_score"] != float64(0.8) {
		t.Errorf("top_score: got %v, want 0.8", ctx["top_score"])
	}
}

func TestEnrichContext_ZeroShot_DoesNotOverwrite(t *testing.T) {
	ctx := map[string]any{
		"labels":    []any{"political"},
		"top_label": "existing",
	}
	enrichContext(ctx)
	if ctx["top_label"] != "existing" {
		t.Errorf("top_label should not be overwritten, got %v", ctx["top_label"])
	}
}

func TestEnrichContext_NER_EntityList(t *testing.T) {
	ctx := map[string]any{
		"entities": []any{
			map[string]any{"word": "Iran", "label": "GPE", "score": float64(0.99)},
			map[string]any{"word": "Khamenei", "label": "PER", "score": float64(0.95)},
		},
	}
	enrichContext(ctx)
	got, ok := ctx["entity_list"].(string)
	if !ok {
		t.Fatalf("entity_list missing or wrong type: %v", ctx["entity_list"])
	}
	if got != "Iran (GPE), Khamenei (PER)" {
		t.Errorf("entity_list: got %q", got)
	}
}

func TestEnrichContext_NER_DoesNotOverwrite(t *testing.T) {
	ctx := map[string]any{
		"entities":    []any{map[string]any{"word": "Iran", "label": "GPE", "score": float64(0.99)}},
		"entity_list": "custom",
	}
	enrichContext(ctx)
	if ctx["entity_list"] != "custom" {
		t.Errorf("entity_list should not be overwritten, got %v", ctx["entity_list"])
	}
}

// ── decodeMetaValue tests ─────────────────────────────────────────────────────

func TestDecodeMetaValue_String(t *testing.T) {
	got := decodeMetaValue(`"political"`)
	if got != "political" {
		t.Errorf("got %v (%T), want political (string)", got, got)
	}
}

func TestDecodeMetaValue_Number(t *testing.T) {
	got := decodeMetaValue("0.95")
	if got != float64(0.95) {
		t.Errorf("got %v (%T), want 0.95 (float64)", got, got)
	}
}

func TestDecodeMetaValue_Array(t *testing.T) {
	got := decodeMetaValue(`["a","b"]`)
	arr, ok := got.([]any)
	if !ok || len(arr) != 2 {
		t.Errorf("expected []any of length 2, got %v (%T)", got, got)
	}
}

func TestDecodeMetaValue_InvalidJSON(t *testing.T) {
	got := decodeMetaValue("not json")
	if got != "not json" {
		t.Errorf("should fall back to raw string, got %v", got)
	}
}
