package worker

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/action"
	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/queue"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

const (
	// pipelineChanBuf is the per-action in-memory task buffer.
	// The Redis queue is the authoritative backlog; this is just an in-flight window.
	pipelineChanBuf = 512
)

// Manager distributes N workers fairly across active actions. Each action
// gets its own in-memory task channel; each worker independently round-robins
// over those channels (staggered by worker id) so that fast actions cannot
// starve slow ones regardless of per-action processing speed differences.
type Manager struct {
	totalWorkers int
	consumer     queue.Consumer
	store        store.Store
	registry     *action.Registry
	logger       *slog.Logger
	active       atomic.Int32

	mu              sync.RWMutex
	actionChs       map[string]chan *ActionTask
	schedOrder      []chan *ActionTask // stable, sorted slice used by workers for round-robin
	schedNames      []string          // action names parallel to schedOrder
	actionLimits    map[string]int    // max concurrent workers per action; 0 = unlimited
	activeActions   sync.Map          // map[string]*atomic.Int32 — workers executing each action
	actionTrackers  sync.Map          // map[string]*rateTracker  — per-action throughput trackers
}

// NewManager creates a Manager with the given dependencies.
func NewManager(
	total int,
	consumer queue.Consumer,
	s store.Store,
	reg *action.Registry,
	logger *slog.Logger,
) *Manager {
	return &Manager{
		totalWorkers: total,
		consumer:     consumer,
		store:        s,
		registry:     reg,
		logger:       logger,
		actionChs:    make(map[string]chan *ActionTask),
		actionLimits: make(map[string]int),
	}
}

// Stats satisfies the workerPoolStatsProvider interface consumed by the API.
func (m *Manager) Stats() PoolStats {
	active := int(m.active.Load())
	idle := m.totalWorkers - active
	if idle < 0 {
		idle = 0
	}

	m.mu.RLock()
	pipelines := make(map[string]int, len(m.actionChs))
	queued := 0
	for name, ch := range m.actionChs {
		n := len(ch)
		pipelines[name] = n
		queued += n
	}
	m.mu.RUnlock()

	pipelineActive := make(map[string]int)
	m.activeActions.Range(func(key, value any) bool {
		pipelineActive[key.(string)] = int(value.(*atomic.Int32).Load())
		return true
	})

	pipelineRate := make(map[string]float64)
	m.actionTrackers.Range(func(key, value any) bool {
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
// Blocks until ctx is cancelled; returns after all goroutines exit.
func (m *Manager) Start(ctx context.Context) error {
	m.syncActionChans()

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

// syncActionChans reconciles actionChs with the registry's current active actions.
func (m *Manager) syncActionChans() {
	all := m.registry.All()
	names := make(map[string]struct{}, len(all))
	for _, a := range all {
		names[a.Name()] = struct{}{}
	}

	m.mu.Lock()
	for name := range names {
		if _, ok := m.actionChs[name]; !ok {
			m.actionChs[name] = make(chan *ActionTask, pipelineChanBuf)
		}
	}
	for name := range m.actionChs {
		if _, ok := names[name]; !ok {
			delete(m.actionChs, name)
		}
	}
	keys := make([]string, 0, len(m.actionChs))
	for name := range m.actionChs {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	m.schedOrder = make([]chan *ActionTask, len(keys))
	m.schedNames = make([]string, len(keys))
	for i, k := range keys {
		m.schedOrder[i] = m.actionChs[k]
		m.schedNames[i] = k
	}

	// Rebuild per-action concurrency limits from ConcurrencyLimiter implementations.
	limits := make(map[string]int, len(all))
	for _, a := range all {
		if cl, ok := a.(action.ConcurrencyLimiter); ok {
			limits[a.Name()] = cl.MaxConcurrency()
		}
	}
	m.actionLimits = limits

	m.mu.Unlock()

	for name := range names {
		m.activeActions.LoadOrStore(name, new(atomic.Int32))
		m.actionTrackers.LoadOrStore(name, new(rateTracker))
	}
	m.activeActions.Range(func(key, _ any) bool {
		if _, ok := names[key.(string)]; !ok {
			m.activeActions.Delete(key)
		}
		return true
	})
	m.actionTrackers.Range(func(key, _ any) bool {
		if _, ok := names[key.(string)]; !ok {
			m.actionTrackers.Delete(key)
		}
		return true
	})
}

// dispatcher pops StreamTweets from Redis and fans each tweet out to the
// per-action task channels of its applicable actions.
func (m *Manager) dispatcher(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.registry.NotifyChan():
			m.syncActionChans()
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
			continue
		}

		ruleIDs, ruleErr := m.store.ListTweetRuleIDs(ctx, st.Tweet.ID)
		if ruleErr != nil && m.logger != nil {
			m.logger.Warn("manager: dispatcher: list tweet rule ids",
				"tweet_id", st.Tweet.ID, "error", ruleErr)
		}

		actions := m.registry.AllForRules(ruleIDs)

		// If the queued item targets specific actions, restrict to those only.
		if len(st.TargetActions) > 0 {
			wanted := make(map[int64]struct{}, len(st.TargetActions))
			for _, id := range st.TargetActions {
				wanted[id] = struct{}{}
			}
			filtered := actions[:0]
			for _, a := range actions {
				if _, ok := wanted[a.ID()]; ok {
					filtered = append(filtered, a)
				}
			}
			actions = filtered
		}

		if len(actions) == 0 {
			continue
		}

		m.mu.RLock()
		for _, a := range actions {
			ch, ok := m.actionChs[a.Name()]
			if !ok {
				continue // race with reload
			}
			task := &ActionTask{Tweet: st, Action: a}
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

// runWorker picks tasks directly from action channels in a round-robin cycle,
// staggered by worker id so workers start at different action slots.
func (m *Manager) runWorker(ctx context.Context, id int) {
	round := id - 1

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		m.mu.RLock()
		chs    := m.schedOrder
		names  := m.schedNames
		limits := m.actionLimits
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

			if limit, ok := limits[name]; ok && limit > 0 {
				if v, loaded := m.activeActions.Load(name); loaded {
					if int(v.(*atomic.Int32).Load()) >= limit {
						continue
					}
				}
			}

			select {
			case task := <-chs[pos]:
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

// Flush drains all in-memory action channel buffers.
// Returns the total number of tasks discarded.
func (m *Manager) Flush() int {
	m.mu.RLock()
	chs := make([]chan *ActionTask, len(m.schedOrder))
	copy(chs, m.schedOrder)
	m.mu.RUnlock()

	total := 0
	for _, ch := range chs {
		total += drainChan(ch)
	}
	return total
}

// FlushPipeline drains the in-memory buffer for a single named action.
func (m *Manager) FlushPipeline(name string) int {
	m.mu.RLock()
	ch, ok := m.actionChs[name]
	m.mu.RUnlock()
	if !ok {
		return 0
	}
	return drainChan(ch)
}

func drainChan(ch chan *ActionTask) int {
	n := 0
	for {
		select {
		case <-ch:
			n++
		default:
			return n
		}
	}
}

// processTask executes one action for one tweet.
func (m *Manager) processTask(ctx context.Context, workerID int, task *ActionTask) {
	m.active.Add(1)
	defer m.active.Add(-1)

	if v, ok := m.activeActions.Load(task.Action.Name()); ok {
		counter := v.(*atomic.Int32)
		counter.Add(1)
		defer counter.Add(-1)
	}

	execErr := task.Action.Execute(ctx, *task.Tweet.Tweet)

	// Record execution in the database.
	exec := &model.ActionExecution{
		ActionID:   task.Action.ID(),
		TweetID:    task.Tweet.Tweet.ID,
		Status:     model.ExecutionStatusSuccess,
		ExecutedAt: time.Now(),
	}
	if execErr != nil {
		exec.Status = model.ExecutionStatusError
		exec.Error = execErr.Error()
		if m.logger != nil {
			m.logger.Warn("manager: action execute failed",
				"worker_id", workerID,
				"tweet_id", task.Tweet.Tweet.ID,
				"action", task.Action.Name(),
				"error", fmt.Errorf("manager: %w", execErr))
		}
	}
	if saveErr := m.store.SaveActionExecution(ctx, exec); saveErr != nil {
		if m.logger != nil {
			m.logger.Warn("manager: save action execution",
				"action_id", task.Action.ID(),
				"tweet_id", task.Tweet.Tweet.ID,
				"error", saveErr)
		}
	}

	if v, ok := m.actionTrackers.Load(task.Action.Name()); ok {
		v.(*rateTracker).record()
	}
}
