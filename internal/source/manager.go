package source

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/amir-the-h/tidewatch/internal/hub"
	"github.com/amir-the-h/tidewatch/internal/model"
	"github.com/amir-the-h/tidewatch/internal/store"
)

// State is a source's runtime state as reported by the manager.
type State string

const (
	StateRunning State = "running" // stream connected, or a poll in flight
	StateIdle    State = "idle"    // poll source waiting for its next poll
	StateBackoff State = "backoff" // stream waiting to reconnect after a failure
	StateError   State = "error"   // last poll failed, or the connector is unavailable
	StateStopped State = "stopped" // inactive
)

// Status is the in-memory status of one source, served at
// GET /api/sources/status and pushed as the source_status WebSocket event.
type Status struct {
	SourceID      int64      `json:"source_id"`
	Name          string     `json:"name"`
	Type          string     `json:"type"`
	Active        bool       `json:"active"`
	State         State      `json:"state"`
	LastRunAt     *time.Time `json:"last_run_at"`
	LastSuccessAt *time.Time `json:"last_success_at"`
	LastError     string     `json:"last_error"`
	NextRunAt     *time.Time `json:"next_run_at"`
	Fetched       int64      `json:"fetched"` // signals emitted since the server started
	New           int64      `json:"new"`     // of which new
}

// ErrNotFound is returned for an unknown source ID.
var ErrNotFound = errors.New("source: not found")

// Clock abstracts time so tests can drive the manager.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// Options tune the manager; zero values take the defaults.
type Options struct {
	MaxConcurrentPolls int           // default 8
	BackoffMin         time.Duration // stream restart backoff, default 5 s
	BackoffMax         time.Duration // default 5 min
	HealthyReset       time.Duration // a stream up this long resets the backoff, default 10 min
	PollTimeout        time.Duration // per poll / backfill, default 5 min (backfill: 1 h)
	Clock              Clock
	Rand               func() float64 // [0,1), for poll jitter
	HTTPClient         *http.Client
}

// Manager runs one supervised goroutine per active source.
type Manager struct {
	store    store.Store
	reg      *Registry
	ingester *Ingester
	bc       hub.Broadcaster
	logger   *slog.Logger
	opts     Options
	pollSem  chan struct{}

	reloadMu sync.Mutex // serialises Reload / ReloadAll

	mu        sync.Mutex
	ctx       context.Context
	workers   map[int64]*sourceWorker
	status    map[int64]*Status
	pollLocks map[int64]*sync.Mutex
	wg        sync.WaitGroup
}

type sourceWorker struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// NewManager builds a manager. bc and logger may be nil.
func NewManager(st store.Store, reg *Registry, in *Ingester, bc hub.Broadcaster, logger *slog.Logger, opts Options) *Manager {
	if opts.MaxConcurrentPolls <= 0 {
		opts.MaxConcurrentPolls = 8
	}
	if opts.BackoffMin <= 0 {
		opts.BackoffMin = 5 * time.Second
	}
	if opts.BackoffMax <= 0 {
		opts.BackoffMax = 5 * time.Minute
	}
	if opts.HealthyReset <= 0 {
		opts.HealthyReset = 10 * time.Minute
	}
	if opts.PollTimeout <= 0 {
		opts.PollTimeout = 5 * time.Minute
	}
	if opts.Clock == nil {
		opts.Clock = realClock{}
	}
	if opts.Rand == nil {
		opts.Rand = rand.Float64
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = NewHTTPClient()
	}
	if bc == nil {
		bc = hub.NopBroadcaster
	}
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Manager{
		store:     st,
		reg:       reg,
		ingester:  in,
		bc:        bc,
		logger:    logger.With("component", "source"),
		opts:      opts,
		pollSem:   make(chan struct{}, opts.MaxConcurrentPolls),
		workers:   map[int64]*sourceWorker{},
		status:    map[int64]*Status{},
		pollLocks: map[int64]*sync.Mutex{},
	}
}

// Registry returns the connector registry the manager validates against.
func (m *Manager) Registry() *Registry { return m.reg }

// Start marks runs interrupted by a previous process as failed, loads every
// source's last-known state and starts the active ones. It does not block;
// the goroutines stop when ctx is cancelled (or on Stop).
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	m.ctx = ctx
	m.mu.Unlock()

	if n, err := m.store.FailUnfinishedSourceRuns(ctx, "interrupted by restart"); err != nil {
		return fmt.Errorf("source: start: %w", err)
	} else if n > 0 {
		m.logger.Info("source: marked interrupted runs as failed", "runs", n)
	}
	return m.ReloadAll(ctx)
}

// Stop stops every source goroutine and run and waits for them.
func (m *Manager) Stop() {
	m.mu.Lock()
	workers := m.workers
	m.workers = map[int64]*sourceWorker{}
	m.mu.Unlock()
	for _, w := range workers {
		w.cancel()
	}
	m.wg.Wait()
}

// Reload restarts one source after it was created, updated, activated,
// deactivated or deleted: its goroutine is stopped and, if the source still
// exists and is active, started again with the stored definition.
func (m *Manager) Reload(ctx context.Context, id int64) error {
	m.reloadMu.Lock()
	defer m.reloadMu.Unlock()

	m.stopWorker(id)
	src, err := m.store.GetSource(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		m.mu.Lock()
		delete(m.status, id)
		delete(m.pollLocks, id)
		m.mu.Unlock()
		return nil
	}
	if err != nil {
		return fmt.Errorf("source: reload %d: %w", id, err)
	}
	m.seedStatus(ctx, src)
	if src.Active {
		m.startWorker(src)
	}
	return nil
}

// ReloadAll reconciles running goroutines with the stored sources: sources
// that are gone or inactive stop, active sources that aren't running start.
// Running sources are left alone (use Reload to restart one).
func (m *Manager) ReloadAll(ctx context.Context) error {
	m.reloadMu.Lock()
	defer m.reloadMu.Unlock()

	sources, err := m.store.ListSources(ctx, store.SourceFilter{})
	if err != nil {
		return fmt.Errorf("source: reload all: %w", err)
	}
	keep := make(map[int64]bool, len(sources))
	for _, src := range sources {
		keep[src.ID] = true
	}

	m.mu.Lock()
	var gone []int64
	for id := range m.status {
		if !keep[id] {
			gone = append(gone, id)
		}
	}
	m.mu.Unlock()
	for _, id := range gone {
		m.stopWorker(id)
		m.mu.Lock()
		delete(m.status, id)
		delete(m.pollLocks, id)
		m.mu.Unlock()
	}

	for _, src := range sources {
		m.mu.Lock()
		_, running := m.workers[src.ID]
		m.mu.Unlock()
		switch {
		case !src.Active && running:
			m.stopWorker(src.ID)
			m.seedStatus(ctx, src)
		case src.Active && !running:
			m.seedStatus(ctx, src)
			m.startWorker(src)
		case !running:
			m.seedStatus(ctx, src)
		}
	}
	return nil
}

// Statuses returns the status of every known source, ordered by name.
func (m *Manager) Statuses(ctx context.Context) ([]Status, error) {
	sources, err := m.store.ListSources(ctx, store.SourceFilter{})
	if err != nil {
		return nil, fmt.Errorf("source: statuses: %w", err)
	}
	out := make([]Status, 0, len(sources))
	for _, src := range sources {
		m.mu.Lock()
		st, ok := m.status[src.ID]
		var cp Status
		if ok {
			cp = *st
		}
		m.mu.Unlock()
		if !ok {
			m.seedStatus(ctx, src)
			m.mu.Lock()
			cp = *m.status[src.ID]
			m.mu.Unlock()
		}
		out = append(out, cp)
	}
	return out, nil
}

// Status returns one source's status.
func (m *Manager) Status(id int64) (Status, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.status[id]
	if !ok {
		return Status{}, false
	}
	return *st, true
}

// ── goroutines ────────────────────────────────────────────────────────────────

func (m *Manager) startWorker(src *model.Source) {
	conn, ok := m.reg.Get(src.Type)
	if !ok {
		m.logger.Warn("source: connector type not available", "source_id", src.ID, "type", src.Type)
		m.setStatus(src.ID, func(s *Status) {
			s.State = StateError
			s.LastError = fmt.Sprintf("connector type %q is not available", src.Type)
		})
		return
	}

	m.mu.Lock()
	root := m.ctx
	if root == nil {
		m.mu.Unlock()
		return // not started
	}
	ctx, cancel := context.WithCancel(root)
	w := &sourceWorker{cancel: cancel, done: make(chan struct{})}
	m.workers[src.ID] = w
	m.wg.Add(1)
	m.mu.Unlock()

	logger := m.logger.With("source_id", src.ID, "source", src.Name, "type", src.Type)
	go func() {
		defer m.wg.Done()
		defer close(w.done)
		switch c := conn.(type) {
		case StreamConnector:
			m.runStream(ctx, src, c, logger)
		case PollConnector:
			m.runPoll(ctx, src, c, logger)
		}
	}()
	logger.Info("source: started")
}

func (m *Manager) stopWorker(id int64) {
	m.mu.Lock()
	w, ok := m.workers[id]
	delete(m.workers, id)
	m.mu.Unlock()
	if !ok {
		return
	}
	w.cancel()
	<-w.done
	m.setStatus(id, func(s *Status) {
		s.State = StateStopped
		s.NextRunAt = nil
	})
}

// runStream calls Run until ctx is cancelled, restarting it with exponential
// backoff (BackoffMin doubling to BackoffMax). A run that stayed up for
// HealthyReset resets the backoff.
func (m *Manager) runStream(ctx context.Context, src *model.Source, conn StreamConnector, logger *slog.Logger) {
	backoff := m.opts.BackoffMin
	for {
		state, err := m.store.GetSourceState(ctx, src.ID)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			logger.Warn("source: load state failed", "error", err)
			state = &model.SourceState{SourceID: src.ID, Cursor: json.RawMessage("{}")}
		}
		started := m.opts.Clock.Now()
		m.setStatus(src.ID, func(s *Status) {
			s.State = StateRunning
			s.LastRunAt = &started
			s.NextRunAt = nil
		})

		rt := m.newRuntime(src, state.Cursor, logger)
		runErr := conn.Run(ctx, src, rt)
		if ctx.Err() != nil {
			m.saveState(context.WithoutCancel(ctx), src.ID, state, rt, started, nil)
			return
		}
		if runErr == nil {
			runErr = errors.New("stream ended")
		}
		m.saveState(ctx, src.ID, state, rt, started, runErr)

		now := m.opts.Clock.Now()
		if now.Sub(started) >= m.opts.HealthyReset {
			backoff = m.opts.BackoffMin
		}
		next := now.Add(backoff)
		logger.Warn("source: stream stopped, restarting", "error", runErr, "backoff", backoff)
		m.setStatus(src.ID, func(s *Status) {
			s.State = StateBackoff
			s.LastError = runErr.Error()
			s.NextRunAt = &next
		})
		select {
		case <-ctx.Done():
			return
		case <-m.opts.Clock.After(backoff):
		}
		backoff = min(backoff*2, m.opts.BackoffMax)
	}
}

// runPoll polls every interval ±10%. The first poll waits until a full
// interval has passed since the last stored run, so restarts don't re-poll
// everything at once.
func (m *Manager) runPoll(ctx context.Context, src *model.Source, conn PollConnector, logger *slog.Logger) {
	iv := interval(src, conn.Descriptor())

	var wait time.Duration
	if state, err := m.store.GetSourceState(ctx, src.ID); err == nil && state.LastRunAt != nil {
		wait = min(max(state.LastRunAt.Add(iv).Sub(m.opts.Clock.Now()), 0), iv)
	}
	for {
		if wait > 0 {
			next := m.opts.Clock.Now().Add(wait)
			m.setStatus(src.ID, func(s *Status) { s.NextRunAt = &next })
			select {
			case <-ctx.Done():
				return
			case <-m.opts.Clock.After(wait):
			}
		}
		if _, err := m.pollOnce(ctx, src, conn, logger); err != nil && ctx.Err() == nil {
			logger.Warn("source: poll failed", "error", err)
		}
		if ctx.Err() != nil {
			return
		}
		wait = m.jitter(iv)
	}
}

func (m *Manager) jitter(d time.Duration) time.Duration {
	return time.Duration(float64(d) * (0.9 + 0.2*m.opts.Rand()))
}

// pollOnce runs one Poll: at most one per source at a time (scheduled and
// manual fetches share the lock) and at most MaxConcurrentPolls overall.
func (m *Manager) pollOnce(ctx context.Context, src *model.Source, conn PollConnector, logger *slog.Logger) (*runtime, error) {
	lock := m.pollLock(src.ID)
	lock.Lock()
	defer lock.Unlock()

	select {
	case m.pollSem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-m.pollSem }()

	state, err := m.store.GetSourceState(ctx, src.ID)
	if err != nil {
		return nil, err
	}
	started := m.opts.Clock.Now()
	m.setStatus(src.ID, func(s *Status) {
		s.State = StateRunning
		s.LastRunAt = &started
		s.NextRunAt = nil
	})

	pctx, cancel := context.WithTimeout(ctx, m.opts.PollTimeout)
	defer cancel()
	rt := m.newRuntime(src, state.Cursor, logger)
	pollErr := conn.Poll(pctx, src, rt)
	if ctx.Err() != nil {
		return rt, ctx.Err() // stopped mid-poll: not the source's fault
	}
	m.saveState(ctx, src.ID, state, rt, started, pollErr)

	m.setStatus(src.ID, func(s *Status) {
		if pollErr != nil {
			s.State = StateError
		} else {
			s.State = StateIdle
		}
	})
	return rt, pollErr
}

// saveState writes source_state after a run: last_run_at always; on success
// the new cursor, last_success_at and a cleared error; on failure the error.
func (m *Manager) saveState(ctx context.Context, id int64, state *model.SourceState, rt *runtime, started time.Time, runErr error) {
	state.LastRunAt = &started
	if runErr == nil {
		now := m.opts.Clock.Now()
		state.LastSuccessAt = &now
		state.LastError = ""
		state.Cursor = rt.Cursor()
	} else {
		state.LastError = runErr.Error()
	}
	if err := m.store.UpsertSourceState(ctx, state); err != nil {
		m.logger.Warn("source: save state failed", "source_id", id, "error", err)
	}
	m.setStatus(id, func(s *Status) {
		s.LastRunAt = state.LastRunAt
		s.LastSuccessAt = state.LastSuccessAt
		s.LastError = state.LastError
	})
}

func (m *Manager) pollLock(id int64) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.pollLocks[id]
	if !ok {
		l = &sync.Mutex{}
		m.pollLocks[id] = l
	}
	return l
}

// ── runs ──────────────────────────────────────────────────────────────────────

// Fetch polls a poll source once now, active or not, as a source run. It
// returns the pending run; the poll happens in the background.
func (m *Manager) Fetch(ctx context.Context, id int64) (*model.SourceRun, error) {
	src, conn, err := m.lookup(ctx, id)
	if err != nil {
		return nil, err
	}
	pc, ok := conn.(PollConnector)
	if !ok {
		return nil, invalid("%s sources can't be fetched on demand", src.Type)
	}
	return m.startRun(ctx, src, model.SourceRunFetch, nil, func(rctx context.Context, logger *slog.Logger) (*runtime, error) {
		return m.pollOnce(rctx, src, pc, logger)
	})
}

// Backfill runs a historical fetch with connector-defined params as a source
// run. The source's cursor is not changed.
func (m *Manager) Backfill(ctx context.Context, id int64, params json.RawMessage) (*model.SourceRun, error) {
	src, conn, err := m.lookup(ctx, id)
	if err != nil {
		return nil, err
	}
	bf, ok := conn.(Backfiller)
	if !ok || !conn.Descriptor().Backfill {
		return nil, invalid("%s sources don't support backfill", src.Type)
	}
	return m.startRun(ctx, src, model.SourceRunBackfill, params, func(rctx context.Context, logger *slog.Logger) (*runtime, error) {
		select {
		case m.pollSem <- struct{}{}:
		case <-rctx.Done():
			return nil, rctx.Err()
		}
		defer func() { <-m.pollSem }()
		state, err := m.store.GetSourceState(rctx, src.ID)
		if err != nil {
			return nil, err
		}
		bctx, cancel := context.WithTimeout(rctx, max(m.opts.PollTimeout, time.Hour))
		defer cancel()
		rt := m.newRuntime(src, state.Cursor, logger)
		return rt, bf.Backfill(bctx, src, params, rt)
	})
}

func (m *Manager) lookup(ctx context.Context, id int64) (*model.Source, Connector, error) {
	src, err := m.store.GetSource(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	conn, ok := m.reg.Get(src.Type)
	if !ok {
		return nil, nil, invalid("connector type %q is not available", src.Type)
	}
	return src, conn, nil
}

func (m *Manager) startRun(ctx context.Context, src *model.Source, kind model.SourceRunKind, params json.RawMessage,
	do func(context.Context, *slog.Logger) (*runtime, error)) (*model.SourceRun, error) {
	m.mu.Lock()
	root := m.ctx
	m.mu.Unlock()
	if root == nil {
		return nil, errors.New("source: manager not started")
	}
	if len(params) == 0 {
		params = json.RawMessage("{}")
	}
	run := &model.SourceRun{
		ID:       uuid.New().String(),
		SourceID: src.ID,
		Kind:     kind,
		Params:   params,
		Status:   model.SourceRunPending,
	}
	if err := m.store.SaveSourceRun(ctx, run); err != nil {
		return nil, err
	}
	created := *run

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		logger := m.logger.With("source_id", src.ID, "source", src.Name, "run_id", run.ID, "kind", kind)
		run.Status = model.SourceRunRunning
		if err := m.store.UpdateSourceRun(root, run); err != nil {
			logger.Warn("source: update run failed", "error", err)
		}
		rt, err := do(root, logger)
		if rt != nil {
			run.FetchedCount = int(rt.fetched.Load())
			run.NewCount = int(rt.new.Load())
		}
		now := time.Now().UTC()
		run.CompletedAt = &now
		if err != nil {
			run.Status = model.SourceRunError
			run.Error = err.Error()
		} else {
			run.Status = model.SourceRunDone
		}
		// The process may be shutting down; still record the outcome.
		if uerr := m.store.UpdateSourceRun(context.WithoutCancel(root), run); uerr != nil {
			logger.Warn("source: update run failed", "error", uerr)
		}
		logger.Info("source: run finished", "status", run.Status, "fetched", run.FetchedCount, "new", run.NewCount)
		m.bc.Broadcast(hub.Event{Type: "source_run", Payload: run})
	}()
	return &created, nil
}

// ── status ────────────────────────────────────────────────────────────────────

// seedStatus creates or refreshes a source's status entry from its row and
// stored state, keeping the in-memory counters.
func (m *Manager) seedStatus(ctx context.Context, src *model.Source) {
	state, err := m.store.GetSourceState(ctx, src.ID)
	if err != nil {
		state = &model.SourceState{}
	}
	m.setStatus(src.ID, func(s *Status) {
		s.Name = src.Name
		s.Type = src.Type
		s.Active = src.Active
		if s.LastRunAt == nil {
			s.LastRunAt = state.LastRunAt
		}
		if s.LastSuccessAt == nil {
			s.LastSuccessAt = state.LastSuccessAt
		}
		if s.LastError == "" {
			s.LastError = state.LastError
		}
		if !src.Active {
			s.State = StateStopped
			s.NextRunAt = nil
		} else if s.State == "" || s.State == StateStopped {
			s.State = StateIdle
		}
	})
}

// setStatus applies fn to a source's status (creating it) and broadcasts the
// result as a source_status event.
func (m *Manager) setStatus(id int64, fn func(*Status)) {
	m.mu.Lock()
	st, ok := m.status[id]
	if !ok {
		st = &Status{SourceID: id, State: StateStopped}
		m.status[id] = st
	}
	fn(st)
	cp := *st
	m.mu.Unlock()
	m.bc.Broadcast(hub.Event{Type: "source_status", Payload: cp})
}

func (m *Manager) countEmit(id int64, isNew bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if st, ok := m.status[id]; ok {
		st.Fetched++
		if isNew {
			st.New++
		}
	}
}

// ── runtime ───────────────────────────────────────────────────────────────────

type runtime struct {
	m       *Manager
	src     *model.Source
	logger  *slog.Logger
	mu      sync.Mutex
	cursor  json.RawMessage
	fetched atomic.Int64
	new     atomic.Int64
}

func (m *Manager) newRuntime(src *model.Source, cursor json.RawMessage, logger *slog.Logger) *runtime {
	if len(cursor) == 0 {
		cursor = json.RawMessage("{}")
	}
	return &runtime{m: m, src: src, logger: logger, cursor: cursor}
}

func (rt *runtime) Emit(ctx context.Context, s *model.Signal, opts ...EmitOption) (bool, error) {
	isNew, err := rt.m.ingester.Ingest(ctx, rt.src, "", s, opts...)
	if err != nil {
		return false, err
	}
	rt.fetched.Add(1)
	if isNew {
		rt.new.Add(1)
	}
	rt.m.countEmit(rt.src.ID, isNew)
	return isNew, nil
}

func (rt *runtime) Cursor() json.RawMessage {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.cursor
}

func (rt *runtime) SetCursor(c json.RawMessage) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.cursor = c
}

func (rt *runtime) HTTPClient() *http.Client { return rt.m.opts.HTTPClient }
func (rt *runtime) Logger() *slog.Logger     { return rt.logger }
