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

func (m *mockConsumer) Pop(ctx context.Context, q string, timeout time.Duration) (*model.StreamTweet, error) {
	if m.idx >= len(m.tweets) {
		return nil, nil // timeout
	}
	t := m.tweets[m.idx]
	m.idx++
	return t, nil
}

func (m *mockConsumer) PopFailed(ctx context.Context, timeout time.Duration) (*queue.FailedTweet, error) {
	return nil, nil
}

// --- mockProducer ---

type mockProducer struct {
	pushFailedCalled bool
	lastFailed       *queue.FailedTweet
}

func (m *mockProducer) Push(ctx context.Context, q string, tweet *model.StreamTweet) error {
	return nil
}

func (m *mockProducer) PushFailed(ctx context.Context, ft *queue.FailedTweet) error {
	m.pushFailedCalled = true
	m.lastFailed = ft
	return nil
}

// --- mockStore ---

type mockStore struct {
	categorizations []*model.Categorization
}

func (m *mockStore) SaveCategorization(ctx context.Context, c *model.Categorization) error {
	m.categorizations = append(m.categorizations, c)
	return nil
}

func (m *mockStore) ListModels(ctx context.Context) ([]*model.Model, error) { return nil, nil }

func (m *mockStore) SaveTweet(ctx context.Context, tweet *model.Tweet) error { return nil }
func (m *mockStore) GetTweet(ctx context.Context, id string) (*model.Tweet, error) {
	return nil, nil
}
func (m *mockStore) ListTweets(ctx context.Context, filter store.TweetFilter) ([]*model.Tweet, error) {
	return nil, nil
}
func (m *mockStore) SaveSource(ctx context.Context, source *model.Source) error { return nil }
func (m *mockStore) GetSource(ctx context.Context, id int64) (*model.Source, error) {
	return nil, nil
}
func (m *mockStore) ListSources(ctx context.Context, filter store.SourceFilter) ([]*model.Source, error) {
	return nil, nil
}
func (m *mockStore) DeleteSource(ctx context.Context, id int64) error { return nil }
func (m *mockStore) UpdateSourceXRuleID(ctx context.Context, id int64, xRuleID string) error {
	return nil
}
func (m *mockStore) SaveTweetSource(ctx context.Context, ts *model.TweetSource) error { return nil }
func (m *mockStore) SaveModel(ctx context.Context, mod *model.Model) error             { return nil }
func (m *mockStore) GetModel(ctx context.Context, id int64) (*model.Model, error) {
	return nil, nil
}
func (m *mockStore) DeleteModel(ctx context.Context, id int64) error { return nil }
func (m *mockStore) SetModelActive(ctx context.Context, id int64, active bool) error { return nil }
func (m *mockStore) ListCategorizations(ctx context.Context, filter store.CategorizationFilter) ([]*model.Categorization, error) {
	return nil, nil
}
func (m *mockStore) ListUncategorizedTweets(ctx context.Context, modelNames []string) ([]*model.Tweet, error) {
	return nil, nil
}
func (m *mockStore) Close() error { return nil }

// --- stubPipeline ---

type stubPipeline struct {
	name    string
	failErr error
}

func (s *stubPipeline) Name() string { return s.name }
func (s *stubPipeline) Load(_ string) error { return nil }
func (s *stubPipeline) Run(_ []byte) (json.RawMessage, error) {
	if s.failErr != nil {
		return nil, s.failErr
	}
	return json.RawMessage(`{"label":"test","score":1.0}`), nil
}
func (s *stubPipeline) RunBatch(texts [][]byte) ([]json.RawMessage, error) { return nil, nil }
func (s *stubPipeline) Close() error                                        { return nil }

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
	// Directly replace active pipelines by reloading with real stubs via a factory approach
	// We load models by calling Load with models that match pipeline names via a closure.
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

// runWorkerUntilDrained runs one worker, cancels context once the consumer is drained,
// and waits for the worker to return. Uses a slightly delayed cancel to let worker loop.
func runWorkerUntilDrained(t *testing.T, w *worker, consumer *mockConsumer) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.run(ctx)
	}()
	// Poll until consumer is drained then cancel.
	for {
		time.Sleep(5 * time.Millisecond)
		if consumer.idx >= len(consumer.tweets) {
			break
		}
	}
	// Give a little time for the last tweet to be processed.
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not stop within timeout")
	}
}

// --- Tests ---

// TestWorker_ProcessesTweet: one pipeline, one tweet; SaveCategorization called with correct
// tweet ID and model name; PushFailed NOT called.
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

// TestWorker_TwoPipelines_OneFails: two pipelines (one succeeds, one fails);
// SaveCategorization called once (for the success); PushFailed called with annotation for failed model.
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

// TestWorker_TwoPipelines_BothFail: both fail; SaveCategorization NOT called;
// PushFailed called with both models in FailedModels.
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

// TestWorker_TwoPipelines_BothSucceed: both succeed; SaveCategorization called twice;
// PushFailed NOT called.
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

// TestPool_Start_LaunchesWorkers: Pool with count=2; verify it starts and returns
// cleanly when ctx is cancelled.
func TestPool_Start_LaunchesWorkers(t *testing.T) {
	consumer := &mockConsumer{} // empty, always returns nil,nil
	producer := &mockProducer{}
	storeImpl := &mockStore{}
	reg := inference.NewRegistry()

	pool := NewPool(2, consumer, producer, storeImpl, reg, nil)

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

// TestPool_Start_RespectsCtxCancel: cancel ctx immediately, verify Start returns nil within 1 second.
func TestPool_Start_RespectsCtxCancel(t *testing.T) {
	consumer := &mockConsumer{}
	producer := &mockProducer{}
	storeImpl := &mockStore{}
	reg := inference.NewRegistry()

	pool := NewPool(2, consumer, producer, storeImpl, reg, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

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
