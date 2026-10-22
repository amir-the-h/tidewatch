package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/amir-the-h/tidewatch/internal/model"
	"github.com/amir-the-h/tidewatch/internal/source"
	"github.com/amir-the-h/tidewatch/internal/store"
	"github.com/amir-the-h/tidewatch/migrations"
)

// feedConn is a minimal poll connector for the sources API tests.
type feedConn struct{ maxInstances int }

func (c feedConn) Descriptor() source.Descriptor {
	return source.Descriptor{
		Type: "feed", Label: "Feed", Mode: source.ModePoll, ExtNS: "feed",
		Kinds: []model.SignalKind{model.SignalKindArticle},
		Fields: []source.Field{
			{Key: "url", Label: "URL", Type: source.FieldURL, Required: true},
			{Key: "api_key", Label: "API key", Type: source.FieldString, Secret: true},
		},
		MaxInstances: c.maxInstances, DefaultInterval: 10 * time.Minute, MinInterval: 2 * time.Minute,
	}
}
func (feedConn) Validate(json.RawMessage) error { return nil }
func (feedConn) Poll(ctx context.Context, src *model.Source, rt source.Runtime) error {
	_, err := rt.Emit(ctx, &model.Signal{Kind: model.SignalKindArticle, ExtNS: "feed", ExternalID: "item-1", Title: "t", PublishedAt: time.Now()})
	return err
}

// newSourcesTestServer returns a server backed by an in-memory SQLite store
// and a started source manager with the feed connector registered.
func newSourcesTestServer(t *testing.T, maxInstances int) (*Server, store.Store) {
	t.Helper()
	st, err := store.NewSQLiteStore(":memory:", migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	reg := source.NewRegistry()
	reg.Register(feedConn{maxInstances: maxInstances})
	logger := slog.New(slog.DiscardHandler)
	mgr := source.NewManager(st, reg, &source.Ingester{Store: st}, nil, logger, source.Options{})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		mgr.Stop()
	})
	if err := mgr.Start(ctx); err != nil {
		t.Fatal(err)
	}
	srv := NewServer("127.0.0.1:0", st, nil, nil, nil, nil, nil, &mockRuleManager{}, nil, "basic", nil, 0, "", logger, nil, nil, nil, nil, nil, "", nil)
	srv.WithSources(mgr)
	return srv, st
}

func doJSON(t *testing.T, srv *Server, method, path, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken(t))
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w, out
}

func TestSourcesAPI_CreateMaskUpdate(t *testing.T) {
	srv, st := newSourcesTestServer(t, 0)

	w, created := doJSON(t, srv, http.MethodPost, "/api/sources",
		`{"name":"Wire","type":"feed","config":{"url":"https://example.com/rss","api_key":"s3cret"},"tier":1,"region":"global"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	cfg, _ := created["config"].(map[string]any)
	if cfg["api_key"] != source.Mask || cfg["url"] != "https://example.com/rss" {
		t.Errorf("create response config: %v", cfg)
	}
	id := int64(created["id"].(float64))

	// Sending the masked config back keeps the stored secret.
	w, _ = doJSON(t, srv, http.MethodPut, "/api/sources/1",
		`{"config":{"url":"https://example.com/atom","api_key":"••••"},"poll_interval_s":300}`)
	if w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body)
	}
	stored, _ := st.GetSource(context.Background(), id)
	var raw map[string]any
	_ = json.Unmarshal(stored.Config, &raw)
	if raw["api_key"] != "s3cret" || raw["url"] != "https://example.com/atom" || stored.PollIntervalS == nil || *stored.PollIntervalS != 300 {
		t.Errorf("stored after update: %s interval=%v", stored.Config, stored.PollIntervalS)
	}
	if stored.Tier == nil || *stored.Tier != 1 || stored.Region != "global" {
		t.Errorf("fields not sent on PUT changed: %+v", stored)
	}

	w, got := doJSON(t, srv, http.MethodGet, "/api/sources/1", "")
	if cfg, _ := got["config"].(map[string]any); w.Code != http.StatusOK || cfg["api_key"] != source.Mask {
		t.Errorf("get: %d %v", w.Code, got)
	}

	for _, tc := range []struct{ name, method, path, body string }{
		{"unknown type", http.MethodPost, "/api/sources", `{"name":"x","type":"nope"}`},
		{"missing required", http.MethodPost, "/api/sources", `{"name":"x","type":"feed","config":{}}`},
		{"interval below min", http.MethodPut, "/api/sources/1", `{"poll_interval_s":30}`},
		{"type change", http.MethodPut, "/api/sources/1", `{"type":"other"}`},
	} {
		if w, _ := doJSON(t, srv, tc.method, tc.path, tc.body); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", tc.name, w.Code, w.Body)
		}
	}
	if w, _ := doJSON(t, srv, http.MethodPost, "/api/sources", `{"name":"Wire","type":"feed","config":{"url":"u"}}`); w.Code != http.StatusConflict {
		t.Errorf("duplicate name: %d", w.Code)
	}
	if w, _ := doJSON(t, srv, http.MethodGet, "/api/sources/99", ""); w.Code != http.StatusNotFound {
		t.Errorf("missing source: %d", w.Code)
	}
}

func TestSourcesAPI_MaxInstancesAndActivate(t *testing.T) {
	srv, _ := newSourcesTestServer(t, 1)
	if w, _ := doJSON(t, srv, http.MethodPost, "/api/sources", `{"name":"a","type":"feed","config":{"url":"u"},"active":true}`); w.Code != http.StatusCreated {
		t.Fatalf("create a: %d %s", w.Code, w.Body)
	}
	if w, _ := doJSON(t, srv, http.MethodPost, "/api/sources", `{"name":"b","type":"feed","config":{"url":"u"},"active":true}`); w.Code != http.StatusBadRequest {
		t.Errorf("second active: %d", w.Code)
	}
	if w, _ := doJSON(t, srv, http.MethodPost, "/api/sources", `{"name":"b","type":"feed","config":{"url":"u"}}`); w.Code != http.StatusCreated {
		t.Fatalf("inactive b: %d", w.Code)
	}
	if w, _ := doJSON(t, srv, http.MethodPost, "/api/sources/2/activate", ""); w.Code != http.StatusBadRequest {
		t.Errorf("activate b: %d", w.Code)
	}
	if w, _ := doJSON(t, srv, http.MethodPost, "/api/sources/1/deactivate", ""); w.Code != http.StatusOK {
		t.Errorf("deactivate a: %d", w.Code)
	}
	if w, body := doJSON(t, srv, http.MethodPost, "/api/sources/2/activate", ""); w.Code != http.StatusOK || body["active"] != true {
		t.Errorf("activate b after a stopped: %d %v", w.Code, body)
	}
}

func TestSourcesAPI_TypesStatusFetchRuns(t *testing.T) {
	srv, st := newSourcesTestServer(t, 0)
	doJSON(t, srv, http.MethodPost, "/api/sources", `{"name":"a","type":"feed","config":{"url":"u"}}`)

	req := httptest.NewRequest(http.MethodGet, "/api/sources/types", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken(t))
	w := httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	var types []map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &types)
	if w.Code != http.StatusOK || len(types) != 1 || types[0]["type"] != "feed" || types[0]["min_interval_s"] != float64(120) {
		t.Errorf("types: %d %s", w.Code, w.Body)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/sources/status", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken(t))
	w = httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	var statuses []map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &statuses)
	if w.Code != http.StatusOK || len(statuses) != 1 || statuses[0]["state"] != "stopped" {
		t.Errorf("status: %d %s", w.Code, w.Body)
	}

	w, run := doJSON(t, srv, http.MethodPost, "/api/sources/1/fetch", "")
	if w.Code != http.StatusAccepted || run["kind"] != "fetch" {
		t.Fatalf("fetch: %d %s", w.Code, w.Body)
	}
	runID := run["id"].(string)
	deadline := time.Now().Add(5 * time.Second)
	for {
		r, err := st.GetSourceRun(context.Background(), runID)
		if err == nil && r.Status == model.SourceRunDone {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("run not done: %+v %v", r, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if w, got := doJSON(t, srv, http.MethodGet, "/api/source-runs/"+runID, ""); w.Code != http.StatusOK || got["new_count"] != float64(1) {
		t.Errorf("run: %d %v", w.Code, got)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/sources/1/runs", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken(t))
	w = httptest.NewRecorder()
	srv.router.ServeHTTP(w, req)
	var runs []map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &runs)
	if len(runs) != 1 {
		t.Errorf("runs: %s", w.Body)
	}
	if w, _ := doJSON(t, srv, http.MethodPost, "/api/sources/1/backfill", `{}`); w.Code != http.StatusBadRequest {
		t.Errorf("backfill unsupported: %d", w.Code)
	}

	// The fetched signal is linked to the source and filterable by it.
	if w, got := doJSON(t, srv, http.MethodGet, "/api/signals?source_type=feed", ""); w.Code != http.StatusOK || got["total"] != float64(1) {
		t.Errorf("signals by source_type: %d %s", w.Code, w.Body)
	}

	if w, got := doJSON(t, srv, http.MethodDelete, "/api/sources", ""); w.Code != http.StatusOK || got["deleted"] != float64(1) {
		t.Errorf("bulk delete: %d %s", w.Code, w.Body)
	}
}

func TestNotificationProviders_SecretsMasked(t *testing.T) {
	srv, st := newSourcesTestServer(t, 0)
	w, created := doJSON(t, srv, http.MethodPost, "/api/notification-providers",
		`{"name":"tg","type":"telegram","config":{"bot_token":"123:abc"}}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	if cfg, _ := created["config"].(map[string]any); cfg["bot_token"] != source.Mask {
		t.Errorf("create response: %v", created)
	}
	id := int64(created["id"].(float64))

	w, _ = doJSON(t, srv, http.MethodPut, "/api/notification-providers/"+strconv.FormatInt(id, 10),
		`{"name":"tg renamed","type":"telegram","config":{"bot_token":"••••"}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body)
	}
	p, _ := st.GetNotificationProvider(context.Background(), id)
	if string(p.Config) != `{"bot_token":"123:abc"}` || p.Name != "tg renamed" {
		t.Errorf("stored: %s %s", p.Name, p.Config)
	}
}
