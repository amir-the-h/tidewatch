package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amir-the-h/tidewatch/internal/hub"
	"github.com/amir-the-h/tidewatch/internal/model"
	"github.com/amir-the-h/tidewatch/internal/queue"
	"github.com/amir-the-h/tidewatch/internal/store"
	"github.com/amir-the-h/tidewatch/migrations"
)

// ── fakes ─────────────────────────────────────────────────────────────────────

// fakeClock only moves when Advance is called. Every After call is reported
// on afters so tests can wait for the manager to start waiting.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	waiters []fakeWaiter
	afters  chan time.Duration
}

type fakeWaiter struct {
	at time.Time
	ch chan time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC), afters: make(chan time.Duration, 100)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	ch := make(chan time.Time, 1)
	c.waiters = append(c.waiters, fakeWaiter{at: c.now.Add(d), ch: ch})
	c.mu.Unlock()
	c.afters <- d
	return ch
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	kept := c.waiters[:0]
	for _, w := range c.waiters {
		if !w.at.After(c.now) {
			w.ch <- c.now
		} else {
			kept = append(kept, w)
		}
	}
	c.waiters = kept
}

// waitAfter returns the duration of the next After call.
func (c *fakeClock) waitAfter(t *testing.T) time.Duration {
	t.Helper()
	select {
	case d := <-c.afters:
		return d
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the manager to wait")
		return 0
	}
}

// pollConn emits one new signal per poll and counts polls into its cursor.
// When block is set, each poll waits for a value on it.
type pollConn struct {
	typ      string
	maxInst  int
	backfill bool
	block    chan struct{}
	polls    atomic.Int32
	inFlight atomic.Int32
	maxSeen  atomic.Int32
	polled   chan int
	fail     atomic.Bool
}

func newPollConn(typ string) *pollConn {
	return &pollConn{typ: typ, polled: make(chan int, 100)}
}

func (c *pollConn) Descriptor() Descriptor {
	return Descriptor{
		Type: c.typ, Label: c.typ, Mode: ModePoll, ExtNS: "test", Kinds: []model.SignalKind{model.SignalKindArticle},
		Fields: []Field{
			{Key: "url", Type: FieldURL, Required: true},
			{Key: "token", Type: FieldString, Secret: true},
			{Key: "format", Type: FieldSelect, Options: []string{"rss", "atom"}},
		},
		MaxInstances: c.maxInst, DefaultInterval: 10 * time.Minute, MinInterval: 2 * time.Minute, Backfill: c.backfill,
	}
}

func (c *pollConn) Validate(cfg json.RawMessage) error {
	var m map[string]any
	_ = json.Unmarshal(cfg, &m)
	if m["url"] == "bad" {
		return errors.New("bad url")
	}
	return nil
}

func (c *pollConn) Poll(ctx context.Context, src *model.Source, rt Runtime) error {
	n := c.inFlight.Add(1)
	defer c.inFlight.Add(-1)
	for {
		seen := c.maxSeen.Load()
		if n <= seen || c.maxSeen.CompareAndSwap(seen, n) {
			break
		}
	}
	if c.block != nil {
		select {
		case <-c.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	i := int(c.polls.Add(1))
	defer func() { c.polled <- i }()
	if c.fail.Load() {
		return errors.New("upstream down")
	}
	sig := &model.Signal{Kind: model.SignalKindArticle, ExtNS: "test", ExternalID: fmt.Sprintf("%s-%d", src.Name, i),
		Title: "item", PublishedAt: time.Now()}
	if _, err := rt.Emit(ctx, sig); err != nil {
		return err
	}
	rt.SetCursor(json.RawMessage(fmt.Sprintf(`{"n":%d}`, i)))
	return nil
}

func (c *pollConn) Backfill(ctx context.Context, src *model.Source, _ json.RawMessage, rt Runtime) error {
	for i := range 3 {
		sig := &model.Signal{Kind: model.SignalKindArticle, ExtNS: "test", ExternalID: fmt.Sprintf("bf-%d", i), PublishedAt: time.Now()}
		if _, err := rt.Emit(ctx, sig); err != nil {
			return err
		}
	}
	rt.SetCursor(json.RawMessage(`{"backfill":"must not persist"}`))
	return nil
}

// streamConn runs until ctx is cancelled or fail is set; each Run is reported
// on runs. onRun, when set, runs at the start of each Run.
type streamConn struct {
	runs  chan int
	n     atomic.Int32
	fail  atomic.Bool
	onRun func(n int)
}

func (c *streamConn) Descriptor() Descriptor {
	return Descriptor{Type: "fake_stream", Label: "Fake stream", Mode: ModeStream, ExtNS: "test", MaxInstances: 1}
}
func (c *streamConn) Validate(json.RawMessage) error { return nil }
func (c *streamConn) Run(ctx context.Context, _ *model.Source, _ Runtime) error {
	n := int(c.n.Add(1))
	if c.onRun != nil {
		c.onRun(n)
	}
	c.runs <- n
	if c.fail.Load() {
		return errors.New("disconnected")
	}
	<-ctx.Done()
	return ctx.Err()
}

// countingStore counts pipeline dispatches (one link lookup per new signal).
type countingStore struct {
	store.Store
	dispatches atomic.Int32
}

func (s *countingStore) ListActiveTriggerPipelineLinks(ctx context.Context, triggerType string, src int64) ([]*model.TriggerPipelineLink, error) {
	s.dispatches.Add(1)
	return s.Store.ListActiveTriggerPipelineLinks(ctx, triggerType, src)
}

type nopProducer struct{}

func (nopProducer) PushTask(context.Context, *queue.Task) error         { return nil }
func (nopProducer) PushFailed(context.Context, *queue.FailedTask) error { return nil }

type recordingBroadcaster struct {
	mu     sync.Mutex
	events []hub.Event
}

func (b *recordingBroadcaster) Broadcast(e hub.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, e)
}

func (b *recordingBroadcaster) count(typ string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, e := range b.events {
		if e.Type == typ {
			n++
		}
	}
	return n
}

type harness struct {
	st    *countingStore
	reg   *Registry
	clock *fakeClock
	bc    *recordingBroadcaster
	m     *Manager
	ctx   context.Context
}

func newHarness(t *testing.T, opts Options, conns ...Connector) *harness {
	t.Helper()
	base, err := store.NewSQLiteStore(":memory:", migrations.FS)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = base.Close() })
	h := &harness{st: &countingStore{Store: base}, reg: NewRegistry(), clock: newFakeClock(), bc: &recordingBroadcaster{}}
	for _, c := range conns {
		h.reg.Register(c)
	}
	opts.Clock = h.clock
	if opts.Rand == nil {
		opts.Rand = func() float64 { return 0.5 }
	}
	in := &Ingester{Store: h.st, Producer: nopProducer{}, Broadcaster: h.bc}
	h.m = NewManager(h.st, h.reg, in, h.bc, nil, opts)
	ctx, cancel := context.WithCancel(context.Background())
	h.ctx = ctx
	t.Cleanup(func() {
		cancel()
		h.m.Stop()
	})
	return h
}

func (h *harness) addSource(t *testing.T, name, typ string, active bool) *model.Source {
	t.Helper()
	src := &model.Source{Name: name, Type: typ, Config: json.RawMessage(`{"url":"https://example.com"}`), Active: active}
	if err := h.st.SaveSource(context.Background(), src); err != nil {
		t.Fatalf("SaveSource: %v", err)
	}
	return src
}

func waitPolled(t *testing.T, c *pollConn) int {
	t.Helper()
	select {
	case n := <-c.polled:
		return n
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a poll")
		return 0
	}
}

func waitRun(t *testing.T, st store.Store, id string) *model.SourceRun {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		run, err := st.GetSourceRun(context.Background(), id)
		if err == nil && (run.Status == model.SourceRunDone || run.Status == model.SourceRunError) {
			return run
		}
		select {
		case <-deadline:
			t.Fatalf("run %s did not finish: %+v %v", id, run, err)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// ── manager ───────────────────────────────────────────────────────────────────

func TestManager_PollIntervalCursorAndState(t *testing.T) {
	conn := newPollConn("fake_poll")
	h := newHarness(t, Options{}, conn)
	src := h.addSource(t, "feed", "fake_poll", true)
	if err := h.m.Start(h.ctx); err != nil {
		t.Fatal(err)
	}

	if n := waitPolled(t, conn); n != 1 {
		t.Fatalf("first poll: %d", n)
	}
	if d := h.clock.waitAfter(t); d != 10*time.Minute {
		t.Errorf("wait after poll: %v, want 10m (jitter midpoint)", d)
	}
	h.clock.Advance(10 * time.Minute)
	waitPolled(t, conn)
	h.clock.waitAfter(t)

	ctx := context.Background()
	state, _ := h.st.GetSourceState(ctx, src.ID)
	if string(state.Cursor) != `{"n":2}` || state.LastSuccessAt == nil || state.LastError != "" {
		t.Errorf("state: cursor=%s %+v", state.Cursor, state)
	}
	if n, _ := h.st.CountSignals(ctx, store.SignalFilter{SourceID: &src.ID}); n != 2 {
		t.Errorf("signals linked to source: %d", n)
	}
	st, _ := h.m.Status(src.ID)
	if st.State != StateIdle || st.Fetched != 2 || st.New != 2 || st.NextRunAt == nil {
		t.Errorf("status: %+v", st)
	}
	if h.bc.count("source_status") == 0 || h.bc.count("new_signal") != 2 {
		t.Errorf("broadcasts: status=%d new_signal=%d", h.bc.count("source_status"), h.bc.count("new_signal"))
	}

	// A failed poll keeps the cursor and records the error.
	conn.fail.Store(true)
	h.clock.Advance(10 * time.Minute)
	waitPolled(t, conn)
	h.clock.waitAfter(t)
	state, _ = h.st.GetSourceState(ctx, src.ID)
	if string(state.Cursor) != `{"n":2}` || state.LastError != "upstream down" {
		t.Errorf("state after failure: cursor=%s err=%q", state.Cursor, state.LastError)
	}
	if st, _ := h.m.Status(src.ID); st.State != StateError {
		t.Errorf("state after failure: %s", st.State)
	}
}

func TestManager_Jitter(t *testing.T) {
	for _, tc := range []struct {
		r    float64
		want time.Duration
	}{{0, 9 * time.Minute}, {0.5, 10 * time.Minute}, {0.999999999, 11 * time.Minute}} {
		m := &Manager{opts: Options{Rand: func() float64 { return tc.r }}}
		if got := m.jitter(10 * time.Minute).Round(time.Second); got != tc.want {
			t.Errorf("jitter(rand=%v) = %v, want %v", tc.r, got, tc.want)
		}
	}
}

// After a restart the first poll waits out the rest of the interval.
func TestManager_FirstPollWaitsForInterval(t *testing.T) {
	conn := newPollConn("fake_poll")
	h := newHarness(t, Options{}, conn)
	src := h.addSource(t, "feed", "fake_poll", true)
	last := h.clock.Now().Add(-4 * time.Minute)
	_ = h.st.UpsertSourceState(context.Background(), &model.SourceState{SourceID: src.ID, LastRunAt: &last})
	if err := h.m.Start(h.ctx); err != nil {
		t.Fatal(err)
	}
	if d := h.clock.waitAfter(t); d != 6*time.Minute {
		t.Errorf("first wait: %v, want 6m", d)
	}
	if n := conn.polls.Load(); n != 0 {
		t.Errorf("polled before the interval: %d", n)
	}
}

func TestManager_OnePollInFlightPerSource(t *testing.T) {
	conn := newPollConn("fake_poll")
	conn.block = make(chan struct{})
	h := newHarness(t, Options{}, conn)
	src := h.addSource(t, "feed", "fake_poll", true)
	if err := h.m.Start(h.ctx); err != nil {
		t.Fatal(err)
	}
	run, err := h.m.Fetch(context.Background(), src.ID)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	conn.block <- struct{}{}
	conn.block <- struct{}{}
	waitPolled(t, conn)
	waitPolled(t, conn)
	got := waitRun(t, h.st, run.ID)
	if got.Status != model.SourceRunDone || got.FetchedCount != 1 || got.NewCount != 1 {
		t.Errorf("run: %+v", got)
	}
	if conn.maxSeen.Load() != 1 {
		t.Errorf("concurrent polls of one source: %d", conn.maxSeen.Load())
	}
}

func TestManager_GlobalPollSemaphore(t *testing.T) {
	conn := newPollConn("fake_poll")
	conn.block = make(chan struct{})
	h := newHarness(t, Options{MaxConcurrentPolls: 1}, conn)
	h.addSource(t, "a", "fake_poll", true)
	h.addSource(t, "b", "fake_poll", true)
	if err := h.m.Start(h.ctx); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		conn.block <- struct{}{}
		waitPolled(t, conn)
	}
	if conn.maxSeen.Load() != 1 {
		t.Errorf("concurrent polls: %d, want 1", conn.maxSeen.Load())
	}
}

func TestManager_StreamBackoff(t *testing.T) {
	conn := &streamConn{runs: make(chan int, 100)}
	conn.fail.Store(true)
	h := newHarness(t, Options{}, conn)
	src := h.addSource(t, "stream", "fake_stream", true)
	conn.onRun = func(n int) {
		if n == 4 { // this run stays up past HealthyReset before failing
			h.clock.Advance(11 * time.Minute)
		}
	}
	if err := h.m.Start(h.ctx); err != nil {
		t.Fatal(err)
	}

	want := []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 5 * time.Second}
	for i, w := range want {
		<-conn.runs
		d := h.clock.waitAfter(t)
		if d != w {
			t.Errorf("backoff %d: %v, want %v", i+1, d, w)
		}
		if i == 0 {
			if st, _ := h.m.Status(src.ID); st.State != StateBackoff || st.LastError != "disconnected" {
				t.Errorf("status in backoff: %+v", st)
			}
		}
		h.clock.Advance(d)
	}
	<-conn.runs
	state, _ := h.st.GetSourceState(context.Background(), src.ID)
	if state.LastError != "disconnected" {
		t.Errorf("stored error: %q", state.LastError)
	}
}

func TestManager_BackoffCapsAtMax(t *testing.T) {
	conn := &streamConn{runs: make(chan int, 100)}
	conn.fail.Store(true)
	h := newHarness(t, Options{BackoffMin: time.Minute, BackoffMax: 3 * time.Minute}, conn)
	h.addSource(t, "stream", "fake_stream", true)
	if err := h.m.Start(h.ctx); err != nil {
		t.Fatal(err)
	}
	var got []time.Duration
	for range 4 {
		<-conn.runs
		d := h.clock.waitAfter(t)
		got = append(got, d)
		h.clock.Advance(d)
	}
	want := []time.Duration{time.Minute, 2 * time.Minute, 3 * time.Minute, 3 * time.Minute}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("backoffs: %v, want %v", got, want)
			break
		}
	}
}

func TestManager_HotReload(t *testing.T) {
	conn := &streamConn{runs: make(chan int, 100)}
	h := newHarness(t, Options{}, conn)
	src := h.addSource(t, "stream", "fake_stream", true)
	ctx := context.Background()
	if err := h.m.Start(h.ctx); err != nil {
		t.Fatal(err)
	}
	<-conn.runs
	if st, _ := h.m.Status(src.ID); st.State != StateRunning {
		t.Errorf("running state: %s", st.State)
	}

	// Update → restart.
	if err := h.m.Reload(ctx, src.ID); err != nil {
		t.Fatal(err)
	}
	if n := <-conn.runs; n != 2 {
		t.Errorf("run after reload: %d", n)
	}

	// Deactivate → stopped, no new run.
	_ = h.st.SetSourceActive(ctx, src.ID, false)
	if err := h.m.Reload(ctx, src.ID); err != nil {
		t.Fatal(err)
	}
	if st, _ := h.m.Status(src.ID); st.State != StateStopped || st.Active {
		t.Errorf("after deactivate: %+v", st)
	}

	// Reactivate, then delete → ReloadAll stops it and forgets the status.
	_ = h.st.SetSourceActive(ctx, src.ID, true)
	_ = h.m.Reload(ctx, src.ID)
	<-conn.runs
	_ = h.st.DeleteSource(ctx, src.ID)
	if err := h.m.ReloadAll(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.m.Status(src.ID); ok {
		t.Error("status kept for a deleted source")
	}
	h.m.mu.Lock()
	running := len(h.m.workers)
	h.m.mu.Unlock()
	if running != 0 {
		t.Errorf("workers after delete: %d", running)
	}
	select {
	case n := <-conn.runs:
		t.Errorf("unexpected run %d", n)
	default:
	}
}

func TestManager_UnknownTypeIsError(t *testing.T) {
	h := newHarness(t, Options{})
	src := h.addSource(t, "orphan", "gone_type", true)
	if err := h.m.Start(h.ctx); err != nil {
		t.Fatal(err)
	}
	st, _ := h.m.Status(src.ID)
	if st.State != StateError || st.LastError == "" {
		t.Errorf("status: %+v", st)
	}
	var verr *ValidationError
	if _, err := h.m.Fetch(context.Background(), src.ID); !errors.As(err, &verr) {
		t.Errorf("Fetch unknown type: %v", err)
	}
}

func TestManager_FetchInactiveAndBackfill(t *testing.T) {
	conn := newPollConn("fake_poll")
	conn.backfill = true
	h := newHarness(t, Options{}, conn)
	src := h.addSource(t, "feed", "fake_poll", false)
	ctx := context.Background()
	_ = h.st.SaveSourceRun(ctx, &model.SourceRun{ID: "stale", SourceID: src.ID, Kind: model.SourceRunFetch, Status: model.SourceRunRunning})
	if err := h.m.Start(h.ctx); err != nil {
		t.Fatal(err)
	}
	if stale, _ := h.st.GetSourceRun(ctx, "stale"); stale.Status != model.SourceRunError {
		t.Errorf("stale run not failed at start: %+v", stale)
	}

	run, err := h.m.Fetch(ctx, src.ID)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if run.Status != model.SourceRunPending || run.Kind != model.SourceRunFetch {
		t.Errorf("returned run: %+v", run)
	}
	if got := waitRun(t, h.st, run.ID); got.Status != model.SourceRunDone || got.NewCount != 1 {
		t.Errorf("fetch run: %+v", got)
	}

	bf, err := h.m.Backfill(ctx, src.ID, json.RawMessage(`{"limit":3}`))
	if err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	got := waitRun(t, h.st, bf.ID)
	if got.Status != model.SourceRunDone || got.FetchedCount != 3 || got.NewCount != 3 || string(got.Params) != `{"limit":3}` {
		t.Errorf("backfill run: %+v params=%s", got, got.Params)
	}
	state, _ := h.st.GetSourceState(ctx, src.ID)
	if string(state.Cursor) != `{"n":1}` {
		t.Errorf("backfill changed the cursor: %s", state.Cursor)
	}

	if _, err := h.m.Fetch(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("Fetch unknown source: %v", err)
	}
}

func TestManager_BackfillUnsupported(t *testing.T) {
	conn := &streamConn{runs: make(chan int, 10)}
	h := newHarness(t, Options{}, conn)
	src := h.addSource(t, "stream", "fake_stream", false)
	if err := h.m.Start(h.ctx); err != nil {
		t.Fatal(err)
	}
	var verr *ValidationError
	if _, err := h.m.Backfill(context.Background(), src.ID, nil); !errors.As(err, &verr) {
		t.Errorf("Backfill on stream: %v", err)
	}
	if _, err := h.m.Fetch(context.Background(), src.ID); !errors.As(err, &verr) {
		t.Errorf("Fetch on stream: %v", err)
	}
}

// ── ingest ────────────────────────────────────────────────────────────────────

func TestIngest_DuplicateDoesNotDispatch(t *testing.T) {
	h := newHarness(t, Options{})
	ctx := context.Background()
	a := h.addSource(t, "a", "rss", true)
	b := h.addSource(t, "b", "rss", true)
	in := &Ingester{Store: h.st, Producer: nopProducer{}, Broadcaster: h.bc}

	newSig := func() *model.Signal {
		return &model.Signal{Kind: model.SignalKindPost, ExtNS: "x", ExternalID: "42", Text: "t", PublishedAt: time.Now()}
	}
	isNew, err := in.Ingest(ctx, a, "", newSig(), WithXRuleMatches(nil))
	if err != nil || !isNew {
		t.Fatalf("first: %v %v", isNew, err)
	}
	sig := newSig()
	isNew, err = in.Ingest(ctx, b, "", sig)
	if err != nil || isNew {
		t.Fatalf("duplicate: %v %v", isNew, err)
	}
	if n := h.st.dispatches.Load(); n != 1 {
		t.Errorf("dispatches: %d, want 1", n)
	}
	refs, _ := h.st.ListSignalSources(ctx, sig.ID)
	if len(refs) != 2 {
		t.Errorf("signal_sources rows: %d, want 2", len(refs))
	}
	stored, _ := h.st.GetSignal(ctx, sig.ID)
	if ids, ok := stored.Attrs["x_rule_ids"].([]any); !ok || len(ids) != 0 {
		t.Errorf("x_rule_ids attr: %#v", stored.Attrs["x_rule_ids"])
	}

	// No source row (the X shim): no signal_sources row, still dispatched.
	isNew, err = in.Ingest(ctx, nil, "x_stream", &model.Signal{Kind: model.SignalKindPost, ExtNS: "x", ExternalID: "43", PublishedAt: time.Now()})
	if err != nil || !isNew {
		t.Fatalf("sourceless: %v %v", isNew, err)
	}
	if n := h.st.dispatches.Load(); n != 2 {
		t.Errorf("dispatches: %d, want 2", n)
	}
}

// ── validation and masking ────────────────────────────────────────────────────

func TestRegistry_Validate(t *testing.T) {
	reg := NewRegistry()
	conn := newPollConn("fake_poll")
	conn.maxInst = 1
	reg.Register(conn)
	iv := func(n int) *int { return &n }

	for _, tc := range []struct {
		name string
		src  model.Source
		ok   bool
	}{
		{"valid", model.Source{Name: "a", Type: "fake_poll", Config: json.RawMessage(`{"url":"u"}`)}, true},
		{"no name", model.Source{Type: "fake_poll", Config: json.RawMessage(`{"url":"u"}`)}, false},
		{"unknown type", model.Source{Name: "a", Type: "nope"}, false},
		{"missing required", model.Source{Name: "a", Type: "fake_poll", Config: json.RawMessage(`{}`)}, false},
		{"not an object", model.Source{Name: "a", Type: "fake_poll", Config: json.RawMessage(`[1]`)}, false},
		{"bad select", model.Source{Name: "a", Type: "fake_poll", Config: json.RawMessage(`{"url":"u","format":"csv"}`)}, false},
		{"connector rejects", model.Source{Name: "a", Type: "fake_poll", Config: json.RawMessage(`{"url":"bad"}`)}, false},
		{"interval below min", model.Source{Name: "a", Type: "fake_poll", Config: json.RawMessage(`{"url":"u"}`), PollIntervalS: iv(60)}, false},
		{"interval ok", model.Source{Name: "a", Type: "fake_poll", Config: json.RawMessage(`{"url":"u"}`), PollIntervalS: iv(120)}, true},
		{"tier out of range", model.Source{Name: "a", Type: "fake_poll", Config: json.RawMessage(`{"url":"u"}`), Tier: iv(5)}, false},
	} {
		err := reg.Validate(&tc.src)
		if (err == nil) != tc.ok {
			t.Errorf("%s: err=%v", tc.name, err)
		}
		var verr *ValidationError
		if err != nil && !errors.As(err, &verr) {
			t.Errorf("%s: not a ValidationError: %v", tc.name, err)
		}
	}

	active := &model.Source{ID: 1, Name: "a", Type: "fake_poll", Active: true}
	second := &model.Source{ID: 2, Name: "b", Type: "fake_poll", Active: true}
	if err := reg.CheckInstances(second, []*model.Source{active}); err == nil {
		t.Error("MaxInstances: second active source allowed")
	}
	if err := reg.CheckInstances(active, []*model.Source{active}); err != nil {
		t.Errorf("MaxInstances counts the source itself: %v", err)
	}
}

func TestMaskAndMergeSecrets(t *testing.T) {
	keys := []string{"token", "unset"}
	stored := json.RawMessage(`{"url":"u","token":"s3cret","unset":""}`)

	masked := MaskConfig(keys, stored)
	var m map[string]any
	_ = json.Unmarshal(masked, &m)
	if m["token"] != Mask || m["url"] != "u" || m["unset"] != "" {
		t.Errorf("masked: %s", masked)
	}

	merged, err := MergeSecrets(keys, json.RawMessage(`{"url":"v","token":"••••"}`), stored)
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(merged, &m)
	if m["token"] != "s3cret" || m["url"] != "v" {
		t.Errorf("merged: %s", merged)
	}

	replaced, _ := MergeSecrets(keys, json.RawMessage(`{"token":"new"}`), stored)
	if string(replaced) != `{"token":"new"}` {
		t.Errorf("replaced: %s", replaced)
	}
	if _, err := MergeSecrets(keys, json.RawMessage(`"x"`), stored); err == nil {
		t.Error("non-object config: want error")
	}
}

func TestDescriptor_JSON(t *testing.T) {
	data, err := json.Marshal(newPollConn("fake_poll").Descriptor())
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(data, &m)
	if m["default_interval_s"] != float64(600) || m["min_interval_s"] != float64(120) || m["fetch"] != true || m["mode"] != "poll" {
		t.Errorf("descriptor JSON: %s", data)
	}
}

func TestRegistry_RejectsMismatchedMode(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("want panic for a backfill descriptor without Backfill")
		}
	}()
	NewRegistry().Register(&streamWithBackfillFlag{})
}

type streamWithBackfillFlag struct{ streamConn }

func (s *streamWithBackfillFlag) Descriptor() Descriptor {
	d := s.streamConn.Descriptor()
	d.Backfill = true
	return d
}
