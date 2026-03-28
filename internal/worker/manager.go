package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/queue"
	"github.com/amir-the-h/iran-watcher/internal/store"
)

// Manager launches N worker goroutines that pop Tasks from Redis.
type Manager struct {
	totalWorkers  int
	consumer      queue.Consumer
	producer      queue.Producer
	store         store.Store
	cache         *ActionCache
	logger        *slog.Logger
	active        atomic.Int32
	workerActions sync.Map // key: int (worker ID), value: string (action name)
}

func NewManager(
	total int,
	consumer queue.Consumer,
	producer queue.Producer,
	st store.Store,
	cache *ActionCache,
	logger *slog.Logger,
) *Manager {
	return &Manager{
		totalWorkers: total,
		consumer:     consumer,
		producer:     producer,
		store:        st,
		cache:        cache,
		logger:       logger,
	}
}

// Stats returns a snapshot of worker pool activity.
func (m *Manager) Stats() PoolStats {
	active := int(m.active.Load())
	idle := m.totalWorkers - active
	if idle < 0 {
		idle = 0
	}

	var activeWorkers []WorkerInfo
	m.workerActions.Range(func(k, v any) bool {
		activeWorkers = append(activeWorkers, WorkerInfo{
			ID:     k.(int),
			Action: v.(string),
		})
		return true
	})

	return PoolStats{
		Total:         m.totalWorkers,
		Active:        active,
		Idle:          idle,
		ActiveWorkers: activeWorkers,
	}
}

// Start launches N worker goroutines. Blocks until ctx is cancelled.
func (m *Manager) Start(ctx context.Context) error {
	var wg sync.WaitGroup
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

func (m *Manager) runWorker(ctx context.Context, id int) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		task, err := m.consumer.PopTask(ctx, 3*time.Second)
		if err != nil {
			if m.logger != nil {
				m.logger.Error("worker: pop task", "worker_id", id, "error", err)
			}
			continue
		}
		if task == nil {
			continue
		}
		m.processTask(ctx, id, task)
	}
}

func (m *Manager) processTask(ctx context.Context, workerID int, task *queue.Task) {
	m.active.Add(1)
	defer m.active.Add(-1)

	entry := m.cache.Get(task.ActionID)
	if entry == nil {
		if m.logger != nil {
			m.logger.Warn("worker: unknown action_id in task",
				"action_id", task.ActionID, "trigger", task.TriggerType)
		}
		return
	}

	// Track which action this worker is executing.
	m.workerActions.Store(workerID, entry.Action.Name)
	defer m.workerActions.Delete(workerID)

	var ev queue.TweetEvent
	_ = json.Unmarshal(task.Event, &ev)

	mergedCtx := make(map[string]any, len(task.Context)+3)
	for k, v := range task.Context {
		mergedCtx[k] = v
	}
	mergedCtx["tweet_id"] = ev.TweetID
	mergedCtx["tweet_author"] = ev.AuthorHandle

	input := inference.PipelineInput{
		Text:    ev.Text,
		Context: mergedCtx,
	}

	startTime := time.Now()
	outputs, runErr := entry.Pipeline.Run(ctx, input)

	exec := &model.ActionExecution{
		ActionID:   task.ActionID,
		TweetID:    ev.TweetID,
		Status:     model.ExecutionStatusSuccess,
		ExecutedAt: startTime,
	}
	if runErr != nil {
		exec.Status = model.ExecutionStatusError
		exec.Error = runErr.Error()
		if m.logger != nil {
			m.logger.Warn("worker: pipeline run failed",
				"worker_id", workerID, "action_id", task.ActionID,
				"pipeline", entry.Pipeline.Name(), "error", runErr)
		}
	}
	if saveErr := m.store.SaveActionExecution(ctx, exec); saveErr != nil && m.logger != nil {
		m.logger.Warn("worker: save action execution", "error", saveErr)
	}

	if runErr != nil || outputs == nil {
		return
	}

	for k, v := range outputs {
		val := fmt.Sprintf("%v", v)
		if b, err := json.Marshal(v); err == nil {
			val = string(b)
		}
		_ = m.store.SaveActionExecutionMetadata(ctx, exec.ID, k, val)
	}

	for k, v := range outputs {
		mergedCtx[k] = v
	}

	m.evaluateChains(ctx, task.ActionID, task.TriggerType, task.Event, mergedCtx)
}

func (m *Manager) evaluateChains(ctx context.Context, sourceActionID int64, triggerType string, event json.RawMessage, mergedCtx map[string]any) {
	chains, err := m.store.ListActionChains(ctx, store.ActionChainFilter{SourceActionID: &sourceActionID})
	if err != nil {
		if m.logger != nil {
			m.logger.Warn("worker: list chains", "source_action_id", sourceActionID, "error", err)
		}
		return
	}
	for _, chain := range chains {
		if !evaluateConditions(chain.Conditions, mergedCtx) {
			continue
		}
		downstream := &queue.Task{
			TriggerType: triggerType,
			Event:       event,
			ActionID:    chain.TargetActionID,
			Context:     mergedCtx,
		}
		if pushErr := m.producer.PushTask(ctx, downstream); pushErr != nil && m.logger != nil {
			m.logger.Warn("worker: push chain task",
				"target_action_id", chain.TargetActionID, "error", pushErr)
		}
	}
}

func evaluateConditions(conds []model.ChainCondition, ctx map[string]any) bool {
	for _, c := range conds {
		val := fmt.Sprintf("%v", ctx[c.Key])
		if !evalCondition(c.Operator, val, c.Value) {
			return false
		}
	}
	return true
}

func evalCondition(op, actual, expected string) bool {
	switch op {
	case "==":
		return actual == expected
	case "!=":
		return actual != expected
	case "is_empty":
		return actual == "" || actual == "null" || actual == "[]"
	case "not_empty":
		return actual != "" && actual != "null" && actual != "[]"
	case "contains":
		return strings.Contains(actual, expected)
	case "not_contains":
		return !strings.Contains(actual, expected)
	default:
		return false
	}
}

// Flush and FlushPipeline are no-ops in the simplified model.
func (m *Manager) Flush() int                 { return 0 }
func (m *Manager) FlushPipeline(_ string) int { return 0 }
