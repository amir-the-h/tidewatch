package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/notifier"
	"github.com/amir-the-h/iran-watcher/internal/queue"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

const (
	// pipelineChanBuf is the per-pipeline in-memory task buffer.
	// The Redis queue is the authoritative backlog; this is just an in-flight window.
	pipelineChanBuf = 512
)

// Manager distributes N workers fairly across active pipelines. Each pipeline
// gets its own in-memory task channel; each worker independently round-robins
// over those channels (staggered by worker id) so that fast pipelines cannot
// starve slow ones regardless of per-pipeline processing speed differences.
type Manager struct {
	totalWorkers int
	consumer     queue.Consumer
	producer     queue.Producer
	store        store.Store
	registry     *inference.Registry
	logger       *slog.Logger
	emitter      notifier.EventEmitter
	active       atomic.Int32

	mu               sync.RWMutex
	pipeChs          map[string]chan *PipelineTask
	schedOrder       []chan *PipelineTask // stable, sorted slice used by workers for round-robin
	schedNames       []string            // pipeline names parallel to schedOrder
	pipelineLimits   map[string]int      // max concurrent workers per pipeline; 0 = unlimited
	activePipelines  sync.Map            // map[string]*atomic.Int32 — workers executing each pipeline
	pipelineTrackers sync.Map            // map[string]*rateTracker  — per-pipeline throughput trackers
}

// NewManager creates a Manager with the given dependencies.
func NewManager(
	total int,
	consumer queue.Consumer,
	producer queue.Producer,
	s store.Store,
	reg *inference.Registry,
	emitter notifier.EventEmitter,
	logger *slog.Logger,
) *Manager {
	return &Manager{
		totalWorkers:   total,
		consumer:       consumer,
		producer:       producer,
		store:          s,
		registry:       reg,
		emitter:        emitter,
		logger:         logger,
		pipeChs:        make(map[string]chan *PipelineTask),
		pipelineLimits: make(map[string]int),
	}
}

// Stats satisfies the workerPoolStatsProvider interface consumed by the API.
// Pipelines and Queued reflect the current in-memory task buffer depths.
func (m *Manager) Stats() PoolStats {
	active := int(m.active.Load())
	idle := m.totalWorkers - active
	if idle < 0 {
		idle = 0
	}

	m.mu.RLock()
	pipelines := make(map[string]int, len(m.pipeChs))
	queued := 0
	for name, ch := range m.pipeChs {
		n := len(ch)
		pipelines[name] = n
		queued += n
	}
	m.mu.RUnlock()

	pipelineActive := make(map[string]int)
	m.activePipelines.Range(func(key, value any) bool {
		pipelineActive[key.(string)] = int(value.(*atomic.Int32).Load())
		return true
	})

	pipelineRate := make(map[string]float64)
	m.pipelineTrackers.Range(func(key, value any) bool {
		pipelineRate[key.(string)] = value.(*rateTracker).rate()
		return true
	})

	return PoolStats{
		Total:          m.totalWorkers,
		Active:         active,
		Idle:           idle,
		Queued:         queued,
		Pipelines:      pipelines,
		PipelineActive: pipelineActive,
		PipelineRate:   pipelineRate,
	}
}

// Start launches the dispatcher and N worker goroutines.
// Each worker independently round-robins over pipeline channels so that fast
// pipelines cannot starve slow ones regardless of processing time differences.
// Blocks until ctx is cancelled; returns after all goroutines exit.
func (m *Manager) Start(ctx context.Context) error {
	m.syncPipelineChans()

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		m.dispatcher(ctx)
	}()

	for i := range m.totalWorkers {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			m.runWorker(ctx, id+1)
		}(i)
	}

	wg.Wait()
	return nil
}

// syncPipelineChans reconciles pipeChs and activePipelines with the registry's
// current active pipelines. New pipelines get a fresh channel and counter;
// pipelines removed from the registry are dropped.
func (m *Manager) syncPipelineChans() {
	active := m.registry.All()
	names := make(map[string]struct{}, len(active))
	for _, p := range active {
		names[p.Name()] = struct{}{}
	}

	m.mu.Lock()
	for name := range names {
		if _, ok := m.pipeChs[name]; !ok {
			m.pipeChs[name] = make(chan *PipelineTask, pipelineChanBuf)
		}
	}
	for name := range m.pipeChs {
		if _, ok := names[name]; !ok {
			delete(m.pipeChs, name)
		}
	}
	// Rebuild schedOrder/schedNames as stable, sorted slices so workers truly round-robin.
	keys := make([]string, 0, len(m.pipeChs))
	for name := range m.pipeChs {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	m.schedOrder = make([]chan *PipelineTask, len(keys))
	m.schedNames = make([]string, len(keys))
	for i, k := range keys {
		m.schedOrder[i] = m.pipeChs[k]
		m.schedNames[i] = k
	}

	// Rebuild per-pipeline concurrency limits from ConcurrencyLimiter implementations.
	limits := make(map[string]int, len(active))
	for _, p := range active {
		if cl, ok := p.(inference.ConcurrencyLimiter); ok {
			limits[p.Name()] = cl.MaxConcurrency()
		}
	}
	m.pipelineLimits = limits

	m.mu.Unlock()

	// Mirror additions/removals in activePipelines and pipelineTrackers.
	for name := range names {
		m.activePipelines.LoadOrStore(name, new(atomic.Int32))
		m.pipelineTrackers.LoadOrStore(name, new(rateTracker))
	}
	m.activePipelines.Range(func(key, _ any) bool {
		if _, ok := names[key.(string)]; !ok {
			m.activePipelines.Delete(key)
		}
		return true
	})
	m.pipelineTrackers.Range(func(key, _ any) bool {
		if _, ok := names[key.(string)]; !ok {
			m.pipelineTrackers.Delete(key)
		}
		return true
	})
}

// dispatcher pops StreamTweets from Redis and fans each tweet out to the per-pipeline
// task channels of its applicable pipelines.
func (m *Manager) dispatcher(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.registry.NotifyChan():
			m.syncPipelineChans()
			continue
		default:
		}

		st, err := m.consumer.Pop(ctx, queue.QueueRaw, 3*time.Second)
		if err != nil {
			if m.logger != nil {
				m.logger.Error("manager: dispatcher: pop",
					"error", fmt.Errorf("manager: dispatcher: %w", err))
			}
			continue
		}
		if st == nil {
			continue // timeout — no item available
		}

		ruleIDs, ruleErr := m.store.ListTweetRuleIDs(ctx, st.Tweet.ID)
		if ruleErr != nil && m.logger != nil {
			m.logger.Warn("manager: dispatcher: list tweet rule ids",
				"tweet_id", st.Tweet.ID, "error", ruleErr)
		}

		pipelines := m.registry.AllForRules(ruleIDs)

		// If the queued item targets specific models, restrict to those only.
		if len(st.TargetModels) > 0 {
			wanted := make(map[string]struct{}, len(st.TargetModels))
			for _, n := range st.TargetModels {
				wanted[n] = struct{}{}
			}
			filtered := pipelines[:0]
			for _, p := range pipelines {
				if _, ok := wanted[p.Name()]; ok {
					filtered = append(filtered, p)
				}
			}
			pipelines = filtered
		}

		if len(pipelines) == 0 {
			continue
		}

		tracker := newCompletionTracker(st, len(pipelines))

		m.mu.RLock()
		for _, p := range pipelines {
			ch, ok := m.pipeChs[p.Name()]
			if !ok {
				// Race with reload: channel removed. Mark this pipeline as failed.
				if tracker.record(nil, unavailableFailedModel(p.Name())) {
					// All pipelines accounted for (all unavailable); finalize.
					go m.finalizeTweet(ctx, tracker)
				}
				continue
			}
			task := &PipelineTask{Tweet: st, Pipeline: p, Tracker: tracker}
			select {
			case ch <- task:
			case <-ctx.Done():
				m.mu.RUnlock()
				return
			}
		}
		m.mu.RUnlock()
	}
}

// runWorker picks tasks directly from pipeline channels in a round-robin cycle,
// staggered by worker id so workers start at different pipeline slots.
//
// After processing a task at position pos, the worker's next search starts at
// pos+1 — so a fast pipeline cannot be picked twice in a row. This guarantees
// fair distribution regardless of per-pipeline processing speed: a worker that
// finishes a fast geo task must cycle through all other pipeline slots before it
// can pick another geo task.
//
// If a pipeline declares a MaxConcurrency limit (e.g. 1 for Ollama), this worker
// skips that slot when the active count already equals the limit, preventing idle
// workers from piling up behind a serialised backend.
func (m *Manager) runWorker(ctx context.Context, id int) {
	// round is the index into schedOrder where this worker will look next.
	// Start staggered so workers spread across different pipelines at launch.
	round := id - 1 // id is 1-indexed; offset is 0-indexed

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		m.mu.RLock()
		chs    := m.schedOrder
		names  := m.schedNames
		limits := m.pipelineLimits
		n      := len(chs)
		m.mu.RUnlock()

		if n == 0 {
			time.Sleep(50 * time.Millisecond)
			continue
		}

		found := false
		for i := 0; i < n; i++ {
			pos  := (round + i) % n
			name := names[pos]

			// Enforce per-pipeline concurrency cap (e.g. 1 for Ollama pipelines).
			// This is an approximate check — a slight overshoot is harmless because
			// Ollama's own semaphore enforces the true limit inside the pipeline.
			if limit, ok := limits[name]; ok && limit > 0 {
				if v, loaded := m.activePipelines.Load(name); loaded {
					if int(v.(*atomic.Int32).Load()) >= limit {
						continue // at capacity; try next pipeline
					}
				}
			}

			select {
			case task := <-chs[pos]:
				// Advance round to the slot after where we found a task, so next
				// search starts at the following pipeline — enforcing true round-robin.
				round = pos + 1
				m.processTask(ctx, id, task)
				found = true
			default:
			}
			if found {
				break
			}
		}

		if !found {
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// Flush drains all in-memory pipeline channel buffers, discarding the queued
// tasks and signalling their completion trackers so that any in-flight tasks
// for the same tweets can still finalise correctly. Returns the total number
// of tasks discarded.
func (m *Manager) Flush() int {
	m.mu.RLock()
	chs := make([]chan *PipelineTask, len(m.schedOrder))
	copy(chs, m.schedOrder)
	m.mu.RUnlock()

	total := 0
	for _, ch := range chs {
		total += drainChan(ch, m.finalizeTweet)
	}
	return total
}

// FlushPipeline drains the in-memory buffer for a single named pipeline.
// Returns the number of tasks discarded, or 0 if the pipeline is not found.
func (m *Manager) FlushPipeline(name string) int {
	m.mu.RLock()
	ch, ok := m.pipeChs[name]
	m.mu.RUnlock()
	if !ok {
		return 0
	}
	return drainChan(ch, m.finalizeTweet)
}

// drainChan empties ch non-blockingly, signalling each task's tracker.
func drainChan(ch chan *PipelineTask, finalize func(context.Context, *completionTracker)) int {
	n := 0
	for {
		select {
		case task := <-ch:
			n++
			if task.Tracker.record(nil, nil) {
				go finalize(context.Background(), task.Tracker)
			}
		default:
			return n
		}
	}
}

// processTask runs one pipeline for one tweet, saves the result, and signals the
// completion tracker. When the tracker reaches zero (last pipeline for this tweet),
// finalizeTweet is called inline.
func (m *Manager) processTask(ctx context.Context, workerID int, task *PipelineTask) {
	m.active.Add(1)
	defer m.active.Add(-1)

	if v, ok := m.activePipelines.Load(task.Pipeline.Name()); ok {
		counter := v.(*atomic.Int32)
		counter.Add(1)
		defer counter.Add(-1)
	}

	var nerEnts []model.NEREntity
	var failed *queue.FailedModel

	result, runErr := task.Pipeline.Run([]byte(task.Tweet.Tweet.Text))
	if runErr != nil {
		if m.logger != nil {
			m.logger.Warn("manager: pipeline failed",
				"worker_id", workerID,
				"tweet_id", task.Tweet.Tweet.ID,
				"pipeline", task.Pipeline.Name(),
				"error", fmt.Errorf("manager: %w", runErr))
		}
		failed = &queue.FailedModel{
			Name:      task.Pipeline.Name(),
			Error:     runErr.Error(),
			Timestamp: time.Now(),
		}
		if m.emitter != nil {
			m.emitter.Emit(notifier.NotificationEvent{
				Type:      notifier.EventCategorizationFailed,
				Tweet:     task.Tweet.Tweet,
				ModelName: task.Pipeline.Name(),
				Error:     runErr.Error(),
				Timestamp: time.Now(),
			})
		}
	} else {
		var nerOut struct {
			Entities []model.NEREntity `json:"entities"`
		}
		if json.Unmarshal(result, &nerOut) == nil {
			nerEnts = nerOut.Entities
		}

		c := &model.Categorization{
			TweetID:   task.Tweet.Tweet.ID,
			ModelName: task.Pipeline.Name(),
			Results:   result,
			CreatedAt: time.Now(),
		}
		if saveErr := m.store.SaveCategorization(ctx, c); saveErr != nil {
			if m.logger != nil {
				m.logger.Warn("manager: save categorization",
					"worker_id", workerID,
					"tweet_id", task.Tweet.Tweet.ID,
					"pipeline", task.Pipeline.Name(),
					"error", fmt.Errorf("manager: %w", saveErr))
			}
			failed = &queue.FailedModel{
				Name:      task.Pipeline.Name(),
				Error:     saveErr.Error(),
				Timestamp: time.Now(),
			}
		}
		if m.emitter != nil && failed == nil {
			m.emitter.Emit(notifier.NotificationEvent{
				Type:      notifier.EventTweetCategorized,
				Tweet:     task.Tweet.Tweet,
				ModelName: task.Pipeline.Name(),
				Results:   result,
				Timestamp: time.Now(),
			})
		}
	}

	// Record throughput for ETA estimation regardless of success/failure.
	if v, ok := m.pipelineTrackers.Load(task.Pipeline.Name()); ok {
		v.(*rateTracker).record()
	}

	if task.Tracker.record(nerEnts, failed) {
		m.finalizeTweet(ctx, task.Tracker)
	}
}

// finalizeTweet runs entity matching and flushes any pipeline failures to the
// dead-letter queue. Called exactly once per tweet by the goroutine that completes
// the last pipeline task.
func (m *Manager) finalizeTweet(ctx context.Context, t *completionTracker) {
	t.mu.Lock()
	nerEntities := make([]model.NEREntity, len(t.nerEntities))
	copy(nerEntities, t.nerEntities)
	failedModels := make([]queue.FailedModel, len(t.failedModels))
	copy(failedModels, t.failedModels)
	t.mu.Unlock()

	doMatchEntities(ctx, t.tweet.Tweet, nerEntities, m.store, m.registry, m.logger)

	if len(failedModels) > 0 {
		ft := &queue.FailedTweet{Tweet: t.tweet, FailedModels: failedModels}
		if err := m.producer.PushFailed(ctx, ft); err != nil && m.logger != nil {
			m.logger.Error("manager: push failed tweet",
				"tweet_id", t.tweet.Tweet.ID,
				"error", fmt.Errorf("manager: %w", err))
		}
	}
}
