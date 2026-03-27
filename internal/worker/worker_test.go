package worker

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/queue"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

// --- mockConsumer ---

type mockConsumer struct {
	tweets []*model.StreamTweet
	idx    int
}

func (m *mockConsumer) Pop(_ context.Context, _ string, _ time.Duration) (*model.StreamTweet, error) {
	if m.idx >= len(m.tweets) {
		return nil, nil
	}
	t := m.tweets[m.idx]
	m.idx++
	return t, nil
}

func (m *mockConsumer) PopFailed(_ context.Context, _ time.Duration) (*queue.FailedTweet, error) {
	return nil, nil
}

// --- mockProducer ---

type mockProducer struct {
	pushFailedCalled bool
	lastFailed       *queue.FailedTweet
}

func (m *mockProducer) Push(_ context.Context, _ string, _ *model.StreamTweet) error {
	return nil
}

func (m *mockProducer) PushFailed(_ context.Context, ft *queue.FailedTweet) error {
	m.pushFailedCalled = true
	m.lastFailed = ft
	return nil
}

// --- mockStore ---

type mockStore struct {
	categorizations []*model.Categorization
}

func (m *mockStore) SaveCategorization(_ context.Context, c *model.Categorization) error {
	m.categorizations = append(m.categorizations, c)
	return nil
}

func (m *mockStore) ListModels(_ context.Context) ([]*model.Model, error) { return nil, nil }

func (m *mockStore) SaveTweet(_ context.Context, _ *model.Tweet) error            { return nil }
func (m *mockStore) GetTweet(_ context.Context, _ string) (*model.Tweet, error)   { return nil, nil }
func (m *mockStore) ListTweets(_ context.Context, _ store.TweetFilter) ([]*model.Tweet, error) {
	return nil, nil
}
func (m *mockStore) SaveSource(_ context.Context, _ *model.Source) error          { return nil }
func (m *mockStore) GetSource(_ context.Context, _ int64) (*model.Source, error)  { return nil, nil }
func (m *mockStore) ListSources(_ context.Context, _ store.SourceFilter) ([]*model.Source, error) {
	return nil, nil
}
func (m *mockStore) DeleteSource(_ context.Context, _ int64) error                        { return nil }
func (m *mockStore) SetSourceActive(_ context.Context, _ int64, _ bool) error             { return nil }
func (m *mockStore) ActiveRuleRefCount(_ context.Context, _ int64) (int, error)           { return 0, nil }
func (m *mockStore) RuleRefCount(_ context.Context, _ int64) (int, error)                 { return 0, nil }
func (m *mockStore) SaveRule(_ context.Context, _ *model.Rule) error                      { return nil }
func (m *mockStore) GetRule(_ context.Context, _ int64) (*model.Rule, error)              { return nil, nil }
func (m *mockStore) GetRuleByXRuleID(_ context.Context, _ string) (*model.Rule, error)    { return nil, nil }
func (m *mockStore) ListRules(_ context.Context, _ store.RuleFilter) ([]*model.Rule, error) {
	return nil, nil
}
func (m *mockStore) UpdateRule(_ context.Context, _ *model.Rule) error                    { return nil }
func (m *mockStore) DeleteRule(_ context.Context, _ int64) error                          { return nil }
func (m *mockStore) SetRuleActive(_ context.Context, _ int64, _ bool, _ string) error     { return nil }
func (m *mockStore) TweetRuleCount(_ context.Context, _ int64) (int, error)               { return 0, nil }
func (m *mockStore) SaveTweetRule(_ context.Context, _ *model.TweetRule) error             { return nil }
func (m *mockStore) SaveModel(_ context.Context, _ *model.Model) error                    { return nil }
func (m *mockStore) GetModel(_ context.Context, _ int64) (*model.Model, error)            { return nil, nil }
func (m *mockStore) DeleteModel(_ context.Context, _ int64) error                         { return nil }
func (m *mockStore) SetModelActive(_ context.Context, _ int64, _ bool) error              { return nil }
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
func (m *mockStore) CountTweets(_ context.Context, _ store.TweetFilter) (int, error)   { return 0, nil }
func (m *mockStore) DeleteTweets(_ context.Context, _ []string) (int64, error)         { return 0, nil }
func (m *mockStore) DeleteNoiseTweets(_ context.Context) (int64, error)                { return 0, nil }
func (m *mockStore) UpdateSource(_ context.Context, _ *model.Source) error             { return nil }
func (m *mockStore) ListRulesDependingOn(_ context.Context, _ int64) ([]*model.Rule, error) {
	return nil, nil
}
func (m *mockStore) ListModelLabels(_ context.Context, _ string) ([]string, error) { return nil, nil }
func (m *mockStore) AddModelLabel(_ context.Context, _, _ string) error            { return nil }
func (m *mockStore) RemoveModelLabel(_ context.Context, _, _ string) error         { return nil }
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
func (m *mockStore) DeleteTrackedEntity(_ context.Context, _ int64) error              { return nil }
func (m *mockStore) SetTrackedEntityActive(_ context.Context, _ int64, _ bool) error   { return nil }
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
	return nil, nil
}
func (m *mockStore) CountSummaries(_ context.Context, _ store.SummaryFilter) (int, error) {
	return 0, nil
}
func (m *mockStore) SaveDigestJob(_ context.Context, _ *model.DigestJob) error    { return nil }
func (m *mockStore) GetDigestJob(_ context.Context, _ string) (*model.DigestJob, error) {
	return nil, nil
}
func (m *mockStore) UpdateDigestJob(_ context.Context, _ *model.DigestJob) error  { return nil }
func (m *mockStore) ListDigestJobs(_ context.Context, _ int) ([]*model.DigestJob, error) {
	return nil, nil
}
func (m *mockStore) DeleteDigestJob(_ context.Context, _ string) error            { return nil }
func (m *mockStore) SaveSearchJob(_ context.Context, _ *model.SearchJob) error    { return nil }
func (m *mockStore) GetSearchJob(_ context.Context, _ string) (*model.SearchJob, error) {
	return nil, nil
}
func (m *mockStore) UpdateSearchJob(_ context.Context, _ *model.SearchJob) error  { return nil }
func (m *mockStore) ListSearchJobs(_ context.Context, _ int) ([]*model.SearchJob, error) {
	return nil, nil
}
func (m *mockStore) DeleteSearchJob(_ context.Context, _ string) error            { return nil }
func (m *mockStore) ListNERSuggestionsForEntity(_ context.Context, _ int64) ([]string, error) {
	return nil, nil
}
func (m *mockStore) SearchNERSuggestions(_ context.Context, _, _ string) ([]string, error) {
	return nil, nil
}
func (m *mockStore) SaveUser(_ context.Context, _ *model.User) error              { return nil }
func (m *mockStore) GetUser(_ context.Context, _ int64) (*model.User, error)      { return nil, nil }
func (m *mockStore) GetUserByUsername(_ context.Context, _ string) (*model.User, error) {
	return nil, nil
}
func (m *mockStore) ListUsers(_ context.Context) ([]*model.User, error)           { return nil, nil }
func (m *mockStore) UpdateUser(_ context.Context, _ *model.User) error            { return nil }
func (m *mockStore) DeleteUser(_ context.Context, _ int64) error                  { return nil }
func (m *mockStore) CountAdmins(_ context.Context) (int, error)                   { return 1, nil }
func (m *mockStore) Close() error                                                  { return nil }

// --- stubPipeline ---

type stubPipeline struct {
	name    string
	failErr error
}

func (s *stubPipeline) Name() string     { return s.name }
func (s *stubPipeline) Load(_ string) error { return nil }
func (s *stubPipeline) Run(_ []byte) (json.RawMessage, error) {
	if s.failErr != nil {
		return nil, s.failErr
	}
	return json.RawMessage(`{"label":"test","score":1.0}`), nil
}
func (s *stubPipeline) RunBatch(_ [][]byte) ([]json.RawMessage, error) { return nil, nil }
func (s *stubPipeline) Close() error                                    { return nil }

// --- helpers ---

func makeTweet(id string) *model.StreamTweet {
	return &model.StreamTweet{
		Tweet: &model.Tweet{
			ID:   id,
			Text: "test tweet text",
		},
	}
}

func setupRegistry(t *testing.T, pipelines ...inference.Pipeline) *inference.Registry {
	t.Helper()
	reg := inference.NewRegistry()
	err := reg.Load(context.Background(), nil, func(m *model.Model) (inference.Pipeline, error) {
		return nil, nil
	})
	if err != nil {
		t.Fatalf("registry Load: %v", err)
	}
	models := make([]*model.Model, len(pipelines))
	for i, p := range pipelines {
		models[i] = &model.Model{Name: p.Name()}
	}
	pipelineMap := make(map[string]inference.Pipeline)
	for _, p := range pipelines {
		pipelineMap[p.Name()] = p
	}
	err = reg.Load(context.Background(), models, func(m *model.Model) (inference.Pipeline, error) {
		return pipelineMap[m.Name], nil
	})
	if err != nil {
		t.Fatalf("registry Load: %v", err)
	}
	return reg
}

func runWorkerUntilDrained(t *testing.T, w *worker, consumer *mockConsumer) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.run(ctx)
	}()
	for {
		time.Sleep(5 * time.Millisecond)
		if consumer.idx >= len(consumer.tweets) {
			break
		}
	}
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not stop within timeout")
	}
}

// --- Tests ---

func TestWorker_ProcessesTweet(t *testing.T) {
	consumer := &mockConsumer{tweets: []*model.StreamTweet{makeTweet("tweet-1")}}
	producer := &mockProducer{}
	storeImpl := &mockStore{}
	reg := setupRegistry(t, &stubPipeline{name: "m1"})

	w := &worker{
		id:       1,
		consumer: consumer,
		producer: producer,
		store:    storeImpl,
		registry: reg,
		logger:   nil,
	}

	runWorkerUntilDrained(t, w, consumer)

	if len(storeImpl.categorizations) != 1 {
		t.Fatalf("expected 1 categorization, got %d", len(storeImpl.categorizations))
	}
	c := storeImpl.categorizations[0]
	if c.TweetID != "tweet-1" {
		t.Errorf("expected TweetID=tweet-1, got %s", c.TweetID)
	}
	if c.ModelName != "m1" {
		t.Errorf("expected ModelName=m1, got %s", c.ModelName)
	}
	if producer.pushFailedCalled {
		t.Error("expected PushFailed NOT called, but it was")
	}
}

func TestWorker_TwoPipelines_OneFails(t *testing.T) {
	consumer := &mockConsumer{tweets: []*model.StreamTweet{makeTweet("tweet-2")}}
	producer := &mockProducer{}
	storeImpl := &mockStore{}
	reg := setupRegistry(t,
		&stubPipeline{name: "m1"},
		&stubPipeline{name: "m2", failErr: errors.New("model error")},
	)

	w := &worker{
		id:       1,
		consumer: consumer,
		producer: producer,
		store:    storeImpl,
		registry: reg,
	}

	runWorkerUntilDrained(t, w, consumer)

	if len(storeImpl.categorizations) != 1 {
		t.Fatalf("expected 1 categorization (success only), got %d", len(storeImpl.categorizations))
	}
	if storeImpl.categorizations[0].ModelName != "m1" {
		t.Errorf("expected successful model=m1, got %s", storeImpl.categorizations[0].ModelName)
	}
	if !producer.pushFailedCalled {
		t.Fatal("expected PushFailed called, but it was not")
	}
	if len(producer.lastFailed.FailedModels) != 1 {
		t.Fatalf("expected 1 failed model, got %d", len(producer.lastFailed.FailedModels))
	}
	if producer.lastFailed.FailedModels[0].Name != "m2" {
		t.Errorf("expected failed model=m2, got %s", producer.lastFailed.FailedModels[0].Name)
	}
}

func TestWorker_TwoPipelines_BothFail(t *testing.T) {
	consumer := &mockConsumer{tweets: []*model.StreamTweet{makeTweet("tweet-3")}}
	producer := &mockProducer{}
	storeImpl := &mockStore{}
	reg := setupRegistry(t,
		&stubPipeline{name: "m1", failErr: errors.New("error1")},
		&stubPipeline{name: "m2", failErr: errors.New("error2")},
	)

	w := &worker{
		id:       1,
		consumer: consumer,
		producer: producer,
		store:    storeImpl,
		registry: reg,
	}

	runWorkerUntilDrained(t, w, consumer)

	if len(storeImpl.categorizations) != 0 {
		t.Fatalf("expected 0 categorizations, got %d", len(storeImpl.categorizations))
	}
	if !producer.pushFailedCalled {
		t.Fatal("expected PushFailed called, but it was not")
	}
	if len(producer.lastFailed.FailedModels) != 2 {
		t.Fatalf("expected 2 failed models, got %d", len(producer.lastFailed.FailedModels))
	}
}

func TestWorker_TwoPipelines_BothSucceed(t *testing.T) {
	consumer := &mockConsumer{tweets: []*model.StreamTweet{makeTweet("tweet-4")}}
	producer := &mockProducer{}
	storeImpl := &mockStore{}
	reg := setupRegistry(t,
		&stubPipeline{name: "m1"},
		&stubPipeline{name: "m2"},
	)

	w := &worker{
		id:       1,
		consumer: consumer,
		producer: producer,
		store:    storeImpl,
		registry: reg,
	}

	runWorkerUntilDrained(t, w, consumer)

	if len(storeImpl.categorizations) != 2 {
		t.Fatalf("expected 2 categorizations, got %d", len(storeImpl.categorizations))
	}
	if producer.pushFailedCalled {
		t.Error("expected PushFailed NOT called, but it was")
	}
}

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

// runManagerUntilDrained drives a Manager with a mock consumer and waits until
// all tweets have been popped and processed (or the timeout fires).
func runManagerUntilDrained(t *testing.T, mgr *Manager, consumer *mockConsumer, expectCats int) {
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
		// Wait until consumer is drained and all active tasks complete.
		if consumer.idx >= len(consumer.tweets) && int(mgr.active.Load()) == 0 {
			break
		}
	}
	// Give in-flight finalizations a moment to write categorizations.
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Manager.Start did not stop within timeout")
	}
}

func TestManager_ProcessesTweet(t *testing.T) {
	consumer := &mockConsumer{tweets: []*model.StreamTweet{makeTweet("tweet-m1")}}
	producer := &mockProducer{}
	storeImpl := &mockStore{}
	reg := setupRegistry(t, &stubPipeline{name: "pa"})

	mgr := NewManager(2, consumer, producer, storeImpl, reg, nil, nil)
	runManagerUntilDrained(t, mgr, consumer, 1)

	if len(storeImpl.categorizations) != 1 {
		t.Fatalf("expected 1 categorization, got %d", len(storeImpl.categorizations))
	}
	if storeImpl.categorizations[0].TweetID != "tweet-m1" {
		t.Errorf("wrong tweet id: %s", storeImpl.categorizations[0].TweetID)
	}
	if storeImpl.categorizations[0].ModelName != "pa" {
		t.Errorf("wrong model name: %s", storeImpl.categorizations[0].ModelName)
	}
	if producer.pushFailedCalled {
		t.Error("expected PushFailed NOT called, but it was")
	}
}

func TestManager_TwoPipelines_OneFails(t *testing.T) {
	consumer := &mockConsumer{tweets: []*model.StreamTweet{makeTweet("tweet-m2")}}
	producer := &mockProducer{}
	storeImpl := &mockStore{}
	reg := setupRegistry(t,
		&stubPipeline{name: "pa"},
		&stubPipeline{name: "pb", failErr: errors.New("boom")},
	)

	mgr := NewManager(2, consumer, producer, storeImpl, reg, nil, nil)
	runManagerUntilDrained(t, mgr, consumer, 1)

	if len(storeImpl.categorizations) != 1 {
		t.Fatalf("expected 1 categorization (success only), got %d", len(storeImpl.categorizations))
	}
	if storeImpl.categorizations[0].ModelName != "pa" {
		t.Errorf("expected model pa, got %s", storeImpl.categorizations[0].ModelName)
	}
	if !producer.pushFailedCalled {
		t.Fatal("expected PushFailed called, but it was not")
	}
	if len(producer.lastFailed.FailedModels) != 1 || producer.lastFailed.FailedModels[0].Name != "pb" {
		t.Errorf("unexpected failed models: %+v", producer.lastFailed.FailedModels)
	}
}

func TestManager_TwoPipelines_BothSucceed(t *testing.T) {
	consumer := &mockConsumer{tweets: []*model.StreamTweet{makeTweet("tweet-m3")}}
	producer := &mockProducer{}
	storeImpl := &mockStore{}
	reg := setupRegistry(t,
		&stubPipeline{name: "pa"},
		&stubPipeline{name: "pb"},
	)

	mgr := NewManager(2, consumer, producer, storeImpl, reg, nil, nil)
	runManagerUntilDrained(t, mgr, consumer, 2)

	if len(storeImpl.categorizations) != 2 {
		t.Fatalf("expected 2 categorizations, got %d", len(storeImpl.categorizations))
	}
	if producer.pushFailedCalled {
		t.Error("expected PushFailed NOT called, but it was")
	}
}

func TestManager_Start_RespectsCtxCancel(t *testing.T) {
	consumer := &mockConsumer{}
	producer := &mockProducer{}
	storeImpl := &mockStore{}
	reg := inference.NewRegistry()

	mgr := NewManager(2, consumer, producer, storeImpl, reg, nil, nil)

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
