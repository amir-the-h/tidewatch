package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/amir-the-h/tidewatch/internal/model"
)

func saveTestSource(t *testing.T, s Store, name, typ string) *model.Source {
	t.Helper()
	tier := 2
	src := &model.Source{Name: name, Type: typ, Config: json.RawMessage(`{"url":"https://example.com/feed"}`), Tier: &tier, Active: true}
	if err := s.SaveSource(context.Background(), src); err != nil {
		t.Fatalf("SaveSource: %v", err)
	}
	return src
}

func TestMigration002_ForeignKeyTargets(t *testing.T) {
	s := newTestStore(t).(*sqliteStore)
	for table, want := range map[string]map[string]string{
		"x_rule_matches": {"signal_id": "signals", "rule_id": "x_rules"},
		"signal_sources": {"signal_id": "signals", "source_id": "sources"},
		"source_state":   {"source_id": "sources"},
		"source_runs":    {"source_id": "sources"},
	} {
		rows, err := s.db.Query(`SELECT "from", "table" FROM pragma_foreign_key_list(?)`, table)
		if err != nil {
			t.Fatalf("%s: foreign_key_list: %v", table, err)
		}
		got := map[string]string{}
		for rows.Next() {
			var from, target string
			if err := rows.Scan(&from, &target); err != nil {
				t.Fatal(err)
			}
			got[from] = target
		}
		rows.Close()
		for col, target := range want {
			if got[col] != target {
				t.Errorf("%s.%s references %q, want %q", table, col, got[col], target)
			}
		}
	}
	for _, gone := range []string{"rules"} {
		var n int
		_ = s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, gone).Scan(&n)
		if n != 0 {
			t.Errorf("table %s still exists", gone)
		}
	}
}

func TestSources_CRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	src := saveTestSource(t, s, "BBC World", "rss")
	got, err := s.GetSource(ctx, src.ID)
	if err != nil {
		t.Fatalf("GetSource: %v", err)
	}
	if got.Name != "BBC World" || got.Type != "rss" || got.Tier == nil || *got.Tier != 2 || !got.Active || got.PollIntervalS != nil {
		t.Errorf("GetSource: got %+v", got)
	}
	if string(got.Config) != `{"url":"https://example.com/feed"}` {
		t.Errorf("config: got %s", got.Config)
	}

	iv := 300
	got.PollIntervalS = &iv
	got.Tier = nil
	got.Region = "global"
	if err := s.UpdateSource(ctx, got); err != nil {
		t.Fatalf("UpdateSource: %v", err)
	}
	got, _ = s.GetSource(ctx, src.ID)
	if got.PollIntervalS == nil || *got.PollIntervalS != 300 || got.Tier != nil || got.Region != "global" {
		t.Errorf("after update: %+v", got)
	}

	if err := s.SetSourceActive(ctx, src.ID, false); err != nil {
		t.Fatalf("SetSourceActive: %v", err)
	}
	active := false
	list, err := s.ListSources(ctx, SourceFilter{Active: &active, Type: "rss"})
	if err != nil || len(list) != 1 {
		t.Fatalf("ListSources inactive rss: %v, %d", err, len(list))
	}

	if err := s.SaveSource(ctx, &model.Source{Name: "BBC World", Type: "rss"}); err == nil {
		t.Error("duplicate name: want error")
	}

	saveTestSource(t, s, "Other", "x_search")
	n, err := s.BulkDeleteSources(ctx, BulkDeleteSourcesFilter{Type: "x_search"})
	if err != nil || n != 1 {
		t.Errorf("BulkDeleteSources: %d, %v", n, err)
	}
	if err := s.DeleteSource(ctx, src.ID); err != nil {
		t.Fatalf("DeleteSource: %v", err)
	}
	if _, err := s.GetSource(ctx, src.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("GetSource after delete: %v", err)
	}
}

func TestSourceState_RoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	src := saveTestSource(t, s, "feed", "rss")

	st, err := s.GetSourceState(ctx, src.ID)
	if err != nil || string(st.Cursor) != "{}" || st.LastRunAt != nil {
		t.Fatalf("empty state: %+v, %v", st, err)
	}

	run := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	st.Cursor = json.RawMessage(`{"etag":"abc"}`)
	st.LastRunAt = &run
	st.LastSuccessAt = &run
	if err := s.UpsertSourceState(ctx, st); err != nil {
		t.Fatalf("UpsertSourceState: %v", err)
	}
	st.LastError = "boom"
	if err := s.UpsertSourceState(ctx, st); err != nil {
		t.Fatalf("UpsertSourceState again: %v", err)
	}
	got, _ := s.GetSourceState(ctx, src.ID)
	if string(got.Cursor) != `{"etag":"abc"}` || got.LastError != "boom" || got.LastRunAt == nil || !got.LastRunAt.Equal(run) {
		t.Errorf("state: %+v", got)
	}

	if err := s.DeleteSource(ctx, src.ID); err != nil {
		t.Fatal(err)
	}
	if states, _ := s.ListSourceStates(ctx); len(states) != 0 {
		t.Errorf("state survived source delete: %d", len(states))
	}
}

func TestSourceRuns(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	src := saveTestSource(t, s, "feed", "rss")

	run := &model.SourceRun{ID: "r1", SourceID: src.ID, Kind: model.SourceRunFetch}
	if err := s.SaveSourceRun(ctx, run); err != nil {
		t.Fatalf("SaveSourceRun: %v", err)
	}
	if err := s.SaveSourceRun(ctx, &model.SourceRun{ID: "r2", SourceID: src.ID, Kind: model.SourceRunBackfill, Status: model.SourceRunRunning}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	run.Status, run.FetchedCount, run.NewCount, run.CompletedAt = model.SourceRunDone, 10, 3, &now
	if err := s.UpdateSourceRun(ctx, run); err != nil {
		t.Fatalf("UpdateSourceRun: %v", err)
	}
	got, err := s.GetSourceRun(ctx, "r1")
	if err != nil || got.Status != model.SourceRunDone || got.FetchedCount != 10 || got.NewCount != 3 || got.CompletedAt == nil {
		t.Errorf("GetSourceRun: %+v, %v", got, err)
	}

	n, err := s.FailUnfinishedSourceRuns(ctx, "interrupted")
	if err != nil || n != 1 {
		t.Errorf("FailUnfinishedSourceRuns: %d, %v", n, err)
	}
	r2, _ := s.GetSourceRun(ctx, "r2")
	if r2.Status != model.SourceRunError || r2.Error != "interrupted" {
		t.Errorf("r2: %+v", r2)
	}
	runs, err := s.ListSourceRuns(ctx, src.ID, 10)
	if err != nil || len(runs) != 2 {
		t.Errorf("ListSourceRuns: %d, %v", len(runs), err)
	}
	if _, err := s.GetSourceRun(ctx, "nope"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("missing run: %v", err)
	}
}

// Two sources emitting the same upstream item store one signal with two
// signal_sources rows; deleting a source keeps the signal.
func TestSignalSources_DedupAcrossSources(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	a := saveTestSource(t, s, "feed A", "rss")
	b := saveTestSource(t, s, "feed B", "x_search")

	newSig := func() *model.Signal {
		return &model.Signal{Kind: model.SignalKindArticle, ExtNS: "rss", ExternalID: "guid-1", Title: "t", PublishedAt: time.Now()}
	}
	s1 := newSig()
	isNew, err := s.SaveSignal(ctx, s1)
	if err != nil || !isNew {
		t.Fatalf("first save: %v %v", isNew, err)
	}
	s2 := newSig()
	isNew, err = s.SaveSignal(ctx, s2)
	if err != nil || isNew || s2.ID != s1.ID {
		t.Fatalf("second save: new=%v id=%d/%d err=%v", isNew, s2.ID, s1.ID, err)
	}
	for _, src := range []*model.Source{a, b, a} {
		if err := s.AddSignalSource(ctx, s1.ID, src.ID); err != nil {
			t.Fatalf("AddSignalSource: %v", err)
		}
	}

	got, err := s.GetSignal(ctx, s1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Sources) != 2 || got.Sources[0].ID != a.ID || got.Sources[0].Name != "feed A" || got.Sources[0].Tier == nil {
		t.Errorf("signal sources: %+v", got.Sources)
	}

	for name, f := range map[string]SignalFilter{
		"source_id":   {SourceID: &b.ID},
		"source_type": {SourceType: "rss"},
	} {
		n, err := s.CountSignals(ctx, f)
		if err != nil || n != 1 {
			t.Errorf("CountSignals by %s: %d, %v", name, n, err)
		}
	}
	other := int64(999)
	if n, _ := s.CountSignals(ctx, SignalFilter{SourceID: &other}); n != 0 {
		t.Errorf("CountSignals by unknown source: %d", n)
	}

	if err := s.DeleteSource(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	refs, _ := s.ListSignalSources(ctx, s1.ID)
	if len(refs) != 1 || refs[0].ID != b.ID {
		t.Errorf("after source delete: %+v", refs)
	}
	if _, err := s.GetSignal(ctx, s1.ID); err != nil {
		t.Errorf("signal deleted with its source: %v", err)
	}
}
