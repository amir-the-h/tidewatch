package x

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amir-the-h/tidewatch/internal/model"
	"github.com/amir-the-h/tidewatch/internal/source"
	"github.com/amir-the-h/tidewatch/internal/store"
	"github.com/amir-the-h/tidewatch/migrations"
)

// ── harness ───────────────────────────────────────────────────────────────────

func newStore(t *testing.T) store.Store {
	t.Helper()
	st, err := store.NewSQLiteStore(":memory:", migrations.FS)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// fakeRuntime ingests through the real source.Ingester (no producer, so
// nothing is dispatched) and records the signals it was given.
type fakeRuntime struct {
	in     *source.Ingester
	src    *model.Source
	mu     sync.Mutex
	cursor json.RawMessage
	emits  []*model.Signal
}

func newRuntime(st store.Store, src *model.Source) *fakeRuntime {
	return &fakeRuntime{in: &source.Ingester{Store: st}, src: src, cursor: json.RawMessage("{}")}
}

func (rt *fakeRuntime) Emit(ctx context.Context, s *model.Signal, opts ...source.EmitOption) (bool, error) {
	isNew, err := rt.in.Ingest(ctx, rt.src, "", s, opts...)
	rt.mu.Lock()
	rt.emits = append(rt.emits, s)
	rt.mu.Unlock()
	return isNew, err
}
func (rt *fakeRuntime) Cursor() json.RawMessage     { return rt.cursor }
func (rt *fakeRuntime) SetCursor(c json.RawMessage) { rt.cursor = c }
func (rt *fakeRuntime) HTTPClient() *http.Client    { return source.NewHTTPClient() }
func (rt *fakeRuntime) Logger() *slog.Logger        { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
func (rt *fakeRuntime) emitted() []*model.Signal    { rt.mu.Lock(); defer rt.mu.Unlock(); return rt.emits }
func (rt *fakeRuntime) cursorJSON() map[string]any {
	var m map[string]any
	_ = json.Unmarshal(rt.cursor, &m)
	return m
}

func saveSource(t *testing.T, st store.Store, typ string, cfg string, active bool) *model.Source {
	t.Helper()
	src := &model.Source{Name: typ + "-src", Type: typ, Config: json.RawMessage(cfg), Active: active}
	if err := st.SaveSource(context.Background(), src); err != nil {
		t.Fatalf("save source: %v", err)
	}
	return src
}

// saveRule stores a non-local rule with a compiled value; active rules get
// xRuleID.
func saveRule(t *testing.T, st store.Store, name, value string, active bool, xRuleID string) *model.XRule {
	t.Helper()
	ctx := context.Background()
	r := &model.XRule{Name: name, Expression: json.RawMessage(`{}`), CompiledValue: value, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := st.SaveXRule(ctx, r); err != nil {
		t.Fatalf("save rule: %v", err)
	}
	if active {
		if err := st.SetXRuleActive(ctx, r.ID, true, xRuleID); err != nil {
			t.Fatalf("activate rule: %v", err)
		}
	}
	return r
}

// fakeX serves the stream rules API, the stream and search. Handlers are
// swapped per test.
type fakeX struct {
	mu      sync.Mutex
	rules   map[string]string // X rule ID → value
	nextID  int
	added   []string
	deleted []string
	stream  http.HandlerFunc
	search  http.HandlerFunc
	queries []url.Values
}

func newFakeX(t *testing.T) (*fakeX, *httptest.Server) {
	f := &fakeX{rules: map[string]string{}, nextID: 100}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, `{"title":"Unauthorized"}`, http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/2/tweets/search/stream/rules":
			f.handleRules(w, r)
		case r.URL.Path == "/2/tweets/search/stream":
			f.stream(w, r)
		case strings.HasPrefix(r.URL.Path, "/2/tweets/search/"):
			f.mu.Lock()
			f.queries = append(f.queries, r.URL.Query())
			f.mu.Unlock()
			f.search(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeX) handleRules(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Method == http.MethodGet {
		var data []XRule
		for id, v := range f.rules {
			data = append(data, XRule{ID: id, Value: v})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
		return
	}
	var body struct {
		Add []struct {
			Value string `json:"value"`
		} `json:"add"`
		Delete struct {
			IDs []string `json:"ids"`
		} `json:"delete"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	var out []map[string]string
	for _, a := range body.Add {
		if strings.Contains(a.Value, "bad") {
			http.Error(w, `{"title":"Invalid Rule"}`, http.StatusBadRequest)
			return
		}
		id := fmt.Sprint(f.nextID)
		f.nextID++
		f.rules[id] = a.Value
		f.added = append(f.added, a.Value)
		out = append(out, map[string]string{"id": id})
	}
	for _, id := range body.Delete.IDs {
		delete(f.rules, id)
		f.deleted = append(f.deleted, id)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"data": out})
}

func newTestStream(st store.Store, base string) *Stream {
	c := NewStream(st)
	c.apiBase = base
	return c
}

func tweetEvent(id string, xRuleIDs ...string) string {
	var mr []string
	for _, r := range xRuleIDs {
		mr = append(mr, fmt.Sprintf(`{"id":%q,"tag":"t"}`, r))
	}
	return fmt.Sprintf(`{"data":{"id":%q,"text":"hello","author_id":"42","created_at":"2026-10-22T12:00:00.000Z"},`+
		`"includes":{"users":[{"id":"42","username":"someone"}]},"matching_rules":[%s]}`, id, strings.Join(mr, ","))
}

// ── x_stream ──────────────────────────────────────────────────────────────────

func TestStream_RunSyncsRulesAndIngests(t *testing.T) {
	st := newStore(t)
	fx, srv := newFakeX(t)
	fx.rules["1"] = "iran"   // kept: rule a's x_rule_id and value
	fx.rules["5"] = "tehran" // adopted by rule b, which lost its x_rule_id
	fx.rules["9"] = "stale"  // claimed by no rule: deleted
	a := saveRule(t, st, "a", "iran", true, "1")
	b := saveRule(t, st, "b", "tehran", true, "")
	c := saveRule(t, st, "c", "hormuz", true, "77") // 77 isn't on X: added
	saveRule(t, st, "d", "inactive", false, "")
	bad := saveRule(t, st, "e", "bad rule", true, "")
	fx.stream = func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "\r\n"+tweetEvent("1001", "1", "100")+"\r\n")
	}

	src := saveSource(t, st, TypeStream, `{"bearer_token":"tok"}`, true)
	rt := newRuntime(st, src)
	err := newTestStream(st, srv.URL).Run(context.Background(), src, rt)
	if err == nil || !strings.Contains(err.Error(), "closed by X") {
		t.Fatalf("Run: got %v, want connection closed error", err)
	}

	if len(fx.added) != 1 || fx.added[0] != "hormuz" {
		t.Errorf("added on X: %v, want [hormuz]", fx.added)
	}
	if len(fx.deleted) != 1 || fx.deleted[0] != "9" {
		t.Errorf("deleted on X: %v, want [9]", fx.deleted)
	}
	ctx := context.Background()
	for _, want := range []struct {
		id  int64
		xid string
	}{{a.ID, "1"}, {b.ID, "5"}, {c.ID, "100"}, {bad.ID, ""}} {
		r, _ := st.GetXRule(ctx, want.id)
		if r.XRuleID != want.xid || !r.Active {
			t.Errorf("rule %d: x_rule_id %q active %v, want %q active", want.id, r.XRuleID, r.Active, want.xid)
		}
	}

	emits := rt.emitted()
	if len(emits) != 1 {
		t.Fatalf("emitted %d signals, want 1", len(emits))
	}
	sig := emits[0]
	if sig.ExtNS != ExtNS || sig.ExternalID != "1001" || sig.Author != "someone" {
		t.Errorf("signal: %+v", sig)
	}
	ids, _ := st.ListXRuleIDs(ctx, sig.ID)
	if len(ids) != 2 {
		t.Errorf("x_rule_matches: %v, want rules a and c", ids)
	}
	refs, _ := st.ListSignalSources(ctx, sig.ID)
	if len(refs) != 1 || refs[0].ID != src.ID {
		t.Errorf("signal_sources: %+v, want the x_stream source", refs)
	}
}

func TestStream_BadTokenFailsRun(t *testing.T) {
	st := newStore(t)
	_, srv := newFakeX(t)
	src := saveSource(t, st, TypeStream, `{"bearer_token":"wrong"}`, true)
	err := newTestStream(st, srv.URL).Run(context.Background(), src, newRuntime(st, src))
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("Run: got %v, want a 401 from the rules sync", err)
	}
}

func TestStream_IdleConnectionIsDropped(t *testing.T) {
	st := newStore(t)
	fx, srv := newFakeX(t)
	fx.stream = func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}
	src := saveSource(t, st, TypeStream, `{"bearer_token":"tok"}`, true)
	c := newTestStream(st, srv.URL)
	c.idleTimeout = 50 * time.Millisecond
	if err := c.Run(context.Background(), src, newRuntime(st, src)); err != errStreamIdle {
		t.Fatalf("Run: got %v, want errStreamIdle", err)
	}
}

func TestStream_CancelReturnsNil(t *testing.T) {
	st := newStore(t)
	fx, srv := newFakeX(t)
	connected := make(chan struct{})
	fx.stream = func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		close(connected)
		<-r.Context().Done()
	}
	src := saveSource(t, st, TypeStream, `{"bearer_token":"tok"}`, true)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- newTestStream(st, srv.URL).Run(ctx, src, newRuntime(st, src)) }()
	<-connected
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run after cancel: %v", err)
	}
}

func TestStream_WithRules(t *testing.T) {
	st := newStore(t)
	c := NewStream(st)
	ctx := context.Background()

	var gotAPI RuleAPI
	var gotTier string
	_ = c.WithRules(ctx, func(api RuleAPI, tier string) error { gotAPI, gotTier = api, tier; return nil })
	if gotAPI != nil || gotTier != "basic" {
		t.Errorf("no active source: api %v tier %q, want nil and basic", gotAPI, gotTier)
	}

	saveSource(t, st, TypeStream, `{"bearer_token":"tok","tier":"enterprise"}`, true)
	_ = c.WithRules(ctx, func(api RuleAPI, tier string) error { gotAPI, gotTier = api, tier; return nil })
	if gotAPI == nil || gotTier != "enterprise" {
		t.Errorf("active source: api %v tier %q, want a client and enterprise", gotAPI, gotTier)
	}
}

func TestValidate(t *testing.T) {
	s, q := NewStream(nil), NewSearch(nil)
	for _, tc := range []struct {
		conn source.Connector
		cfg  string
		ok   bool
	}{
		{s, `{"bearer_token":"t"}`, true},
		{s, `{"bearer_token":"t","tier":"gold"}`, false},
		{q, `{"bearer_token":"t","query":"iran"}`, true},
		{q, `{"bearer_token":"t","query":"iran","rule_ids":[1]}`, false},
		{q, `{"bearer_token":"t","max_results":5}`, false},
		{q, `{"bearer_token":"t","start_time":"yesterday"}`, false},
		{q, `{"bearer_token":"t","start_time":"2026-10-01T00:00:00Z","limit":50}`, true},
	} {
		if err := tc.conn.Validate(json.RawMessage(tc.cfg)); (err == nil) != tc.ok {
			t.Errorf("%s %s: err %v, want ok=%v", tc.conn.Descriptor().Type, tc.cfg, err, tc.ok)
		}
	}
}

// ── x_search ──────────────────────────────────────────────────────────────────

// searchPage answers with one page holding tweets newest first.
func searchPage(ids ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var data []string
		for _, id := range ids {
			data = append(data, fmt.Sprintf(`{"id":%q,"text":"t","author_id":"42","created_at":"2026-10-22T12:00:00Z"}`, id))
		}
		newest := ""
		if len(ids) > 0 {
			newest = ids[0]
		}
		_, _ = fmt.Fprintf(w, `{"data":[%s],"includes":{"users":[{"id":"42","username":"someone"}]},"meta":{"newest_id":%q}}`,
			strings.Join(data, ","), newest)
	}
}

func newTestSearch(st store.Store, base string) *Search {
	c := NewSearch(st)
	c.apiBase = base
	return c
}

func TestSearch_PollAdvancesSinceIDPerRule(t *testing.T) {
	st := newStore(t)
	fx, srv := newFakeX(t)
	fx.search = searchPage("20", "19")
	a := saveRule(t, st, "a", "iran", true, "1")
	saveRule(t, st, "b", "tehran", true, "2")
	saveRule(t, st, "off", "off", false, "")
	src := saveSource(t, st, TypeSearch, `{"bearer_token":"tok","start_time":"2026-10-01T00:00:00Z"}`, true)
	rt := newRuntime(st, src)
	c := newTestSearch(st, srv.URL)
	ctx := context.Background()

	if err := c.Poll(ctx, src, rt); err != nil {
		t.Fatalf("first poll: %v", err)
	}
	if len(fx.queries) != 2 {
		t.Fatalf("queries: %d, want one per active rule", len(fx.queries))
	}
	if q := fx.queries[0]; q.Get("start_time") != "2026-10-01T00:00:00Z" || q.Get("since_id") != "" {
		t.Errorf("first poll query: %v", q)
	}
	since, _ := rt.cursorJSON()["since_ids"].(map[string]any)
	if since[fmt.Sprintf("rule:%d", a.ID)] != "20" || len(since) != 2 {
		t.Errorf("cursor: %s", rt.cursor)
	}

	fx.queries = nil
	if err := c.Poll(ctx, src, rt); err != nil {
		t.Fatalf("second poll: %v", err)
	}
	if q := fx.queries[0]; q.Get("since_id") != "20" || q.Get("start_time") != "" {
		t.Errorf("second poll query: %v, want since_id 20 and no start_time", q)
	}

	// Both rules found the same two tweets: two signals, each matching both.
	emits := rt.emitted()
	sig := emits[0]
	ids, _ := st.ListXRuleIDs(ctx, sig.ID)
	if len(ids) != 2 {
		t.Errorf("x_rule_matches of %s: %v, want both rules", sig.ExternalID, ids)
	}
}

func TestSearch_QueryAndProTier(t *testing.T) {
	st := newStore(t)
	fx, srv := newFakeX(t)
	var path string
	fx.search = func(w http.ResponseWriter, r *http.Request) { path = r.URL.Path; searchPage("5")(w, r) }
	src := saveSource(t, st, TypeSearch, `{"bearer_token":"tok","tier":"pro","query":"from:someone"}`, true)
	rt := newRuntime(st, src)
	if err := newTestSearch(st, srv.URL).Poll(context.Background(), src, rt); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if path != searchAllPath || fx.queries[0].Get("query") != "from:someone" {
		t.Errorf("path %s query %v", path, fx.queries[0])
	}
	if rt.cursorJSON()["since_ids"].(map[string]any)["query"] != "5" {
		t.Errorf("cursor: %s", rt.cursor)
	}
	if ids, _ := rt.emitted()[0].Attrs["x_rule_ids"].([]int64); ids == nil || len(ids) != 0 {
		t.Errorf("x_rule_ids for a raw query: %v, want []", rt.emitted()[0].Attrs["x_rule_ids"])
	}
}

func TestSearch_PartialFailureKeepsCursor(t *testing.T) {
	st := newStore(t)
	fx, srv := newFakeX(t)
	good := saveRule(t, st, "good", "iran", true, "1")
	bad := saveRule(t, st, "bad", "broken", true, "2")
	fx.search = func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("query") == "broken" {
			http.Error(w, "nope", http.StatusBadRequest)
			return
		}
		searchPage("30")(w, r)
	}
	src := saveSource(t, st, TypeSearch, `{"bearer_token":"tok"}`, true)
	rt := newRuntime(st, src)
	rt.cursor = json.RawMessage(fmt.Sprintf(`{"since_ids":{"rule:%d":"7"}}`, bad.ID))
	c := newTestSearch(st, srv.URL)
	if err := c.Poll(context.Background(), src, rt); err != nil {
		t.Fatalf("poll with one failing rule: %v", err)
	}
	since := rt.cursorJSON()["since_ids"].(map[string]any)
	if since[fmt.Sprintf("rule:%d", good.ID)] != "30" || since[fmt.Sprintf("rule:%d", bad.ID)] != "7" {
		t.Errorf("cursor: %s", rt.cursor)
	}

	fx.search = func(w http.ResponseWriter, r *http.Request) { http.Error(w, "down", http.StatusServiceUnavailable) }
	if err := c.Poll(context.Background(), src, rt); err == nil {
		t.Error("poll with every rule failing: want an error")
	}
}

func TestSearch_BackfillLeavesCursor(t *testing.T) {
	st := newStore(t)
	fx, srv := newFakeX(t)
	fx.search = searchPage("12", "11", "10")
	saveRule(t, st, "a", "iran", true, "1")
	src := saveSource(t, st, TypeSearch, `{"bearer_token":"tok","start_time":"2026-09-01T00:00:00Z"}`, true)
	rt := newRuntime(st, src)
	params := json.RawMessage(`{"start_time":"2026-10-01T00:00:00Z","end_time":"2026-10-02T00:00:00Z","limit":2}`)
	if err := newTestSearch(st, srv.URL).Backfill(context.Background(), src, params, rt); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	q := fx.queries[0]
	if q.Get("start_time") != "2026-10-01T00:00:00Z" || q.Get("end_time") != "2026-10-02T00:00:00Z" || q.Get("max_results") != "10" {
		t.Errorf("backfill query: %v", q)
	}
	if n := len(rt.emitted()); n != 2 {
		t.Errorf("emitted %d, want the limit of 2", n)
	}
	if string(rt.cursor) != "{}" {
		t.Errorf("cursor changed: %s", rt.cursor)
	}
}

// A tweet found by the stream and then by search is one signal with two
// signal_sources rows.
func TestStreamAndSearchShareExtNS(t *testing.T) {
	st := newStore(t)
	fx, srv := newFakeX(t)
	if NewStream(nil).Descriptor().ExtNS != NewSearch(nil).Descriptor().ExtNS {
		t.Fatal("x_stream and x_search must share ext_ns")
	}
	fx.stream = func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, tweetEvent("555")+"\n") }
	fx.search = searchPage("555")
	ctx := context.Background()

	ss := saveSource(t, st, TypeStream, `{"bearer_token":"tok"}`, true)
	srt := newRuntime(st, ss)
	_ = newTestStream(st, srv.URL).Run(ctx, ss, srt)

	qs := saveSource(t, st, TypeSearch, `{"bearer_token":"tok","query":"x"}`, true)
	qrt := newRuntime(st, qs)
	if err := newTestSearch(st, srv.URL).Poll(ctx, qs, qrt); err != nil {
		t.Fatalf("poll: %v", err)
	}
	a, b := srt.emitted()[0], qrt.emitted()[0]
	if a.ID == 0 || a.ID != b.ID {
		t.Fatalf("signal IDs %d and %d, want the same signal", a.ID, b.ID)
	}
	refs, _ := st.ListSignalSources(ctx, a.ID)
	if len(refs) != 2 {
		t.Errorf("signal_sources: %d rows, want 2", len(refs))
	}
}
