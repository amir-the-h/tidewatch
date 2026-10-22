package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/amir-the-h/tidewatch/internal/model"
	"github.com/amir-the-h/tidewatch/migrations"
	_ "modernc.org/sqlite"
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	s, err := NewSQLiteStore(":memory:", migrations.FS)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func sampleSignal() *model.Signal {
	return &model.Signal{
		Kind:        model.SignalKindPost,
		ExtNS:       "x",
		ExternalID:  "tweet-1",
		Author:      "handle1",
		AuthorID:    "author-1",
		Text:        "Hello Iran",
		URL:         "https://x.com/handle1/status/tweet-1",
		Attrs:       map[string]any{"x_tweet_id": "tweet-1"},
		RawJSON:     []byte(`{"id":"tweet-1"}`),
		PublishedAt: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
		FetchedAt:   time.Date(2026, 1, 1, 12, 1, 0, 0, time.UTC),
	}
}

// mustSaveSignal stores a post signal with the given external ID and publish
// time and returns it with its ID set.
func mustSaveSignal(t *testing.T, s Store, externalID string, publishedAt time.Time) *model.Signal {
	t.Helper()
	sig := &model.Signal{
		Kind: model.SignalKindPost, ExtNS: "x", ExternalID: externalID,
		Author: "h-" + externalID, Text: "text " + externalID, PublishedAt: publishedAt,
	}
	if _, err := s.SaveSignal(context.Background(), sig); err != nil {
		t.Fatalf("SaveSignal %s: %v", externalID, err)
	}
	return sig
}

func sampleSource() *model.XTerm {
	return &model.XTerm{
		Name:      "Iran keyword",
		Type:      model.XTermTypeKeyword,
		Class:     model.XTermClassAtom,
		Operator:  "",
		Value:     "Iran",
		Active:    true,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func sampleRule() *model.XRule {
	return &model.XRule{
		Name:          "iran-rule",
		Expression:    json.RawMessage(`{"src":1}`),
		CompiledValue: "Iran",
		XRuleID:       "",
		Active:        false,
		CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func sampleAction() *model.Action {
	return &model.Action{
		Name:      "test-action",
		Type:      model.ActionTypeInternal,
		Config:    json.RawMessage(`{"handler":"entity_matcher"}`),
		Active:    true,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

// 1. TestSaveSignal_GetSignal
func TestSaveSignal_GetSignal(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	sig := sampleSignal()
	isNew, err := s.SaveSignal(ctx, sig)
	if err != nil {
		t.Fatalf("SaveSignal: %v", err)
	}
	if !isNew || sig.ID == 0 {
		t.Fatalf("SaveSignal: isNew=%v id=%d, want new row with an ID", isNew, sig.ID)
	}

	got, err := s.GetSignal(ctx, sig.ID)
	if err != nil {
		t.Fatalf("GetSignal: %v", err)
	}

	if got.Kind != sig.Kind || got.ExtNS != sig.ExtNS || got.ExternalID != sig.ExternalID {
		t.Errorf("identity: got %s/%s/%s", got.Kind, got.ExtNS, got.ExternalID)
	}
	if got.Author != sig.Author || got.AuthorID != sig.AuthorID {
		t.Errorf("author: got %q/%q", got.Author, got.AuthorID)
	}
	if got.Text != sig.Text || got.URL != sig.URL {
		t.Errorf("text/url: got %q/%q", got.Text, got.URL)
	}
	if got.Attrs["x_tweet_id"] != "tweet-1" {
		t.Errorf("attrs: got %v", got.Attrs)
	}
	if got.Lat != nil || got.Lon != nil {
		t.Errorf("lat/lon: got %v/%v, want nil", got.Lat, got.Lon)
	}
	if string(got.RawJSON) != string(sig.RawJSON) {
		t.Errorf("RawJSON: got %q, want %q", got.RawJSON, sig.RawJSON)
	}
	if !got.PublishedAt.Equal(sig.PublishedAt) {
		t.Errorf("PublishedAt: got %v, want %v", got.PublishedAt, sig.PublishedAt)
	}
	if !got.FetchedAt.Equal(sig.FetchedAt) {
		t.Errorf("FetchedAt: got %v, want %v", got.FetchedAt, sig.FetchedAt)
	}
}

// 2. TestSaveSignal_Dedup: the same (ext_ns, external_id) is stored once and
// the second save reports isNew=false with the existing ID.
func TestSaveSignal_Dedup(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	first := sampleSignal()
	if _, err := s.SaveSignal(ctx, first); err != nil {
		t.Fatalf("first SaveSignal: %v", err)
	}
	second := sampleSignal()
	second.Text = "seen again by search"
	isNew, err := s.SaveSignal(ctx, second)
	if err != nil {
		t.Fatalf("second SaveSignal: %v", err)
	}
	if isNew {
		t.Error("second SaveSignal: isNew=true, want false")
	}
	if second.ID != first.ID {
		t.Errorf("second SaveSignal: ID %d, want existing %d", second.ID, first.ID)
	}

	// A different namespace with the same external ID is a different signal.
	other := sampleSignal()
	other.ExtNS = "rss"
	if isNew, err := s.SaveSignal(ctx, other); err != nil || !isNew {
		t.Errorf("other namespace: isNew=%v err=%v, want new", isNew, err)
	}

	signals, err := s.ListSignals(ctx, SignalFilter{})
	if err != nil {
		t.Fatalf("ListSignals: %v", err)
	}
	if len(signals) != 2 {
		t.Errorf("expected 2 signals, got %d", len(signals))
	}
}

// TestDeleteSignals_Cascades checks that deleting a signal removes its
// executions, their metadata, entity tracks and rule matches.
func TestDeleteSignals_Cascades(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	sig := mustSaveSignal(t, s, "c1", time.Now())
	action := sampleAction()
	if err := s.SaveAction(ctx, action); err != nil {
		t.Fatalf("SaveAction: %v", err)
	}
	exec := &model.ActionExecution{ActionID: action.ID, SignalID: sig.ID, Status: model.ExecutionStatusSuccess, ExecutedAt: time.Now()}
	if err := s.SaveActionExecution(ctx, exec); err != nil {
		t.Fatalf("SaveActionExecution: %v", err)
	}
	if err := s.SaveActionExecutionMetadata(ctx, exec.ID, "label", `"strike"`); err != nil {
		t.Fatalf("SaveActionExecutionMetadata: %v", err)
	}
	entity := &model.TrackedEntity{Name: "e", Type: "PER", Active: true, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := s.SaveTrackedEntity(ctx, entity); err != nil {
		t.Fatalf("SaveTrackedEntity: %v", err)
	}
	if err := s.SaveEntityTrack(ctx, &model.EntityTrack{EntityID: entity.ID, SignalID: sig.ID, MatchSource: "text"}); err != nil {
		t.Fatalf("SaveEntityTrack: %v", err)
	}
	rule := sampleRule()
	if err := s.SaveXRule(ctx, rule); err != nil {
		t.Fatalf("SaveXRule: %v", err)
	}
	if err := s.SaveXRuleMatch(ctx, &model.XRuleMatch{SignalID: sig.ID, RuleID: rule.ID}); err != nil {
		t.Fatalf("SaveXRuleMatch: %v", err)
	}

	n, err := s.DeleteSignals(ctx, []int64{sig.ID})
	if err != nil || n != 1 {
		t.Fatalf("DeleteSignals: n=%d err=%v", n, err)
	}

	db := s.(*sqliteStore).db
	for _, table := range []string{"action_executions", "action_execution_metadata", "entity_tracks", "x_rule_matches"} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Errorf("%s: %d rows left after signal delete, want 0", table, count)
		}
	}
}

// 3. TestSaveSource_ListSources_ActiveFilter
func TestSaveXTerm_ListXTerms_ActiveFilter(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	active := sampleSource()
	active.Active = true
	if err := s.SaveXTerm(ctx, active); err != nil {
		t.Fatalf("SaveXTerm active: %v", err)
	}

	inactive := sampleSource()
	inactive.Value = "nuclear"
	inactive.Name = "nuclear keyword"
	inactive.Active = false
	if err := s.SaveXTerm(ctx, inactive); err != nil {
		t.Fatalf("SaveXTerm inactive: %v", err)
	}

	trueVal := true
	sources, err := s.ListXTerms(ctx, XTermFilter{Active: &trueVal})
	if err != nil {
		t.Fatalf("ListXTerms: %v", err)
	}
	if len(sources) != 1 {
		t.Errorf("expected 1 active source, got %d", len(sources))
	}
	if !sources[0].Active {
		t.Error("expected returned source to be active")
	}
}

// 4. TestDeleteSource
func TestDeleteXTerm(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	src := sampleSource()
	if err := s.SaveXTerm(ctx, src); err != nil {
		t.Fatalf("SaveXTerm: %v", err)
	}

	if err := s.DeleteXTerm(ctx, src.ID); err != nil {
		t.Fatalf("DeleteXTerm: %v", err)
	}

	got, err := s.GetXTerm(ctx, src.ID)
	if err == nil && got != nil {
		t.Error("expected error or nil after deleting source, got non-nil source")
	}
}

// 5. TestSaveAndGetSource — new fields
func TestSaveAndGetXTerm(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	src := &model.XTerm{
		Name:      "BBC Persian",
		Type:      model.XTermTypeAccount,
		Class:     model.XTermClassAtom,
		Operator:  "from:",
		Value:     "BBCPersian",
		Active:    true,
		CreatedAt: time.Now().UTC().Truncate(time.Second),
		UpdatedAt: time.Now().UTC().Truncate(time.Second),
	}
	if err := s.SaveXTerm(ctx, src); err != nil {
		t.Fatalf("SaveXTerm: %v", err)
	}
	if src.ID == 0 {
		t.Fatal("expected non-zero ID after save")
	}
	got, err := s.GetXTerm(ctx, src.ID)
	if err != nil {
		t.Fatalf("GetXTerm: %v", err)
	}
	if got.Name != "BBC Persian" || got.Operator != "from:" || got.Class != model.XTermClassAtom {
		t.Errorf("unexpected source: %+v", got)
	}
}

// 6. TestSetSourceActive
func TestSetXTermActive(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	src := sampleSource()
	_ = s.SaveXTerm(ctx, src)
	if err := s.SetXTermActive(ctx, src.ID, false); err != nil {
		t.Fatalf("SetXTermActive: %v", err)
	}
	got, _ := s.GetXTerm(ctx, src.ID)
	if got.Active {
		t.Error("expected Active=false")
	}
}

// 7. TestActiveRuleRefCount
func TestActiveRuleRefCount(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	src := sampleSource()
	_ = s.SaveXTerm(ctx, src)
	n, err := s.ActiveRuleRefCount(ctx, src.ID)
	if err != nil {
		t.Fatalf("ActiveRuleRefCount: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 refs, got %d", n)
	}
}

// 8. TestSaveAndGetRule
func TestSaveAndGetXRule(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	rule := sampleRule()
	if err := s.SaveXRule(ctx, rule); err != nil {
		t.Fatalf("SaveXRule: %v", err)
	}
	if rule.ID == 0 {
		t.Fatal("expected non-zero ID after save")
	}
	got, err := s.GetXRule(ctx, rule.ID)
	if err != nil {
		t.Fatalf("GetXRule: %v", err)
	}
	if got.Name != "iran-rule" {
		t.Errorf("Name: got %q, want %q", got.Name, "iran-rule")
	}
	if got.CompiledValue != "Iran" {
		t.Errorf("CompiledValue: got %q, want %q", got.CompiledValue, "Iran")
	}
}

// 9. TestGetRuleByXRuleID
func TestGetXRuleByXRuleID(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	rule := sampleRule()
	rule.XRuleID = "x-rule-123"
	if err := s.SaveXRule(ctx, rule); err != nil {
		t.Fatalf("SaveXRule: %v", err)
	}

	got, err := s.GetXRuleByXRuleID(ctx, "x-rule-123")
	if err != nil {
		t.Fatalf("GetXRuleByXRuleID: %v", err)
	}
	if got.ID != rule.ID {
		t.Errorf("ID: got %d, want %d", got.ID, rule.ID)
	}
}

// 10. TestXRuleMatchCount
func TestXRuleMatchCount(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	sig := sampleSignal()
	_, _ = s.SaveSignal(ctx, sig)

	rule := sampleRule()
	_ = s.SaveXRule(ctx, rule)

	if err := s.SaveXRuleMatch(ctx, &model.XRuleMatch{SignalID: sig.ID, RuleID: rule.ID}); err != nil {
		t.Fatalf("SaveXRuleMatch: %v", err)
	}

	count, err := s.XRuleMatchCount(ctx, rule.ID)
	if err != nil {
		t.Fatalf("XRuleMatchCount: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1, got %d", count)
	}
}

// 11. TestSaveXRuleMatch_GetSignal_PopulatesRules
func TestSaveXRuleMatch_GetSignal_PopulatesRules(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	sig := sampleSignal()
	_, _ = s.SaveSignal(ctx, sig)

	rule := sampleRule()
	_ = s.SaveXRule(ctx, rule)

	if err := s.SaveXRuleMatch(ctx, &model.XRuleMatch{SignalID: sig.ID, RuleID: rule.ID}); err != nil {
		t.Fatalf("SaveXRuleMatch: %v", err)
	}

	got, err := s.GetSignal(ctx, sig.ID)
	if err != nil {
		t.Fatalf("GetSignal: %v", err)
	}
	if len(got.XRules) != 1 {
		t.Fatalf("expected 1 rule on signal, got %d", len(got.XRules))
	}
	if got.XRules[0].ID != rule.ID {
		t.Errorf("rule ID: got %d, want %d", got.XRules[0].ID, rule.ID)
	}
}

// 12. TestSavePipeline_SetActive_ListPipelines
func TestSavePipeline_SetActive_ListPipelines(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	p := &model.Pipeline{
		Name:   "sentiment-dag",
		Active: false,
	}
	if err := s.SavePipeline(ctx, p); err != nil {
		t.Fatalf("SavePipeline: %v", err)
	}

	if err := s.SetPipelineActive(ctx, p.ID, true); err != nil {
		t.Fatalf("SetPipelineActive: %v", err)
	}

	pipelines, err := s.ListPipelines(ctx)
	if err != nil {
		t.Fatalf("ListPipelines: %v", err)
	}
	if len(pipelines) != 1 {
		t.Fatalf("expected 1 pipeline, got %d", len(pipelines))
	}
	if !pipelines[0].Active {
		t.Error("expected pipeline to be active")
	}
}

// 16. TestRunMigrations_Idempotent
func TestRunMigrations_Idempotent(t *testing.T) {
	s1, err := NewSQLiteStore(":memory:", migrations.FS)
	if err != nil {
		t.Fatalf("first NewSQLiteStore: %v", err)
	}
	_ = s1.Close()

	db, err := openTestDB(t)
	if err != nil {
		t.Fatalf("openTestDB: %v", err)
	}

	if err := RunMigrations(db, migrations.FS); err != nil {
		t.Fatalf("first RunMigrations: %v", err)
	}
	if err := RunMigrations(db, migrations.FS); err != nil {
		t.Fatalf("second RunMigrations: %v", err)
	}
}

// TestRunMigrations_RefusesOldSchema: a database that recorded a migration
// which is no longer embedded (pre phase 1) is refused, not upgraded.
func TestRunMigrations_RefusesOldSchema(t *testing.T) {
	db, err := openTestDB(t)
	if err != nil {
		t.Fatalf("openTestDB: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE schema_migrations (filename TEXT PRIMARY KEY, applied_at DATETIME NOT NULL DEFAULT (datetime('now')))`); err != nil {
		t.Fatalf("create schema_migrations: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO schema_migrations (filename) VALUES ('001_initial.sql')`); err != nil {
		t.Fatalf("seed schema_migrations: %v", err)
	}
	err = RunMigrations(db, migrations.FS)
	if err == nil || !strings.Contains(err.Error(), "older schema") {
		t.Fatalf("RunMigrations: err=%v, want older-schema error", err)
	}
}

func TestListSignals_HasEntityTrack(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	sig1 := mustSaveSignal(t, s, "t1", time.Now())
	mustSaveSignal(t, s, "t2", time.Now())

	// Create a tracked entity so the foreign key constraint is satisfied.
	entity := &model.TrackedEntity{Name: "test-entity", Type: "PER", Active: true, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := s.SaveTrackedEntity(ctx, entity); err != nil {
		t.Fatalf("SaveTrackedEntity: %v", err)
	}

	// Only sig1 has an entity track
	if err := s.SaveEntityTrack(ctx, &model.EntityTrack{EntityID: entity.ID, SignalID: sig1.ID, MatchSource: "ner", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("SaveEntityTrack: %v", err)
	}

	all, err := s.ListSignals(ctx, SignalFilter{Page: 1, PerPage: 100})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 signals, got %d", len(all))
	}

	tracked, err := s.ListSignals(ctx, SignalFilter{Page: 1, PerPage: 100, HasEntityTrack: true})
	if err != nil {
		t.Fatalf("list tracked: %v", err)
	}
	if len(tracked) != 1 {
		t.Fatalf("expected 1 tracked signal, got %d", len(tracked))
	}
	if tracked[0].ID != sig1.ID {
		t.Errorf("expected signal %d, got %d", sig1.ID, tracked[0].ID)
	}
}

func TestListSignals_OrderAscAndKind(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	old := mustSaveSignal(t, s, "t_old", time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	new_ := mustSaveSignal(t, s, "t_new", time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC))

	// Default (no OrderAsc): newest first.
	desc, err := s.ListSignals(ctx, SignalFilter{Page: 1, PerPage: 100})
	if err != nil {
		t.Fatalf("list desc: %v", err)
	}
	if len(desc) != 2 || desc[0].ID != new_.ID || desc[1].ID != old.ID {
		t.Errorf("expected newest-first, got %+v", desc)
	}

	// OrderAsc: oldest first.
	asc, err := s.ListSignals(ctx, SignalFilter{Page: 1, PerPage: 100, OrderAsc: true})
	if err != nil {
		t.Fatalf("list asc: %v", err)
	}
	if len(asc) != 2 || asc[0].ID != old.ID || asc[1].ID != new_.ID {
		t.Errorf("expected oldest-first, got %+v", asc)
	}

	article := model.SignalKindArticle
	n, err := s.CountSignals(ctx, SignalFilter{Kind: &article})
	if err != nil || n != 0 {
		t.Errorf("CountSignals(kind=article): n=%d err=%v, want 0", n, err)
	}
}

func openTestDB(t *testing.T) (*sql.DB, error) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, nil
}

func TestEntityCandidate_InsertAndGet(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c := &model.EntityCandidate{
		Word:          "Tehran",
		Type:          "LOC",
		AvgConfidence: 0.9,
		Sources:       []string{"ner-pipeline"},
		FirstSeenAt:   time.Now().UTC(),
		LastSeenAt:    time.Now().UTC(),
	}
	if err := s.UpsertEntityCandidate(ctx, c); err != nil {
		t.Fatalf("UpsertEntityCandidate (insert): %v", err)
	}
	if c.ID == 0 {
		t.Fatal("expected non-zero ID after insert")
	}
	if c.OccurrenceCount != 1 {
		t.Errorf("occurrence_count: got %d, want 1", c.OccurrenceCount)
	}
	if c.Status != "pending" {
		t.Errorf("status: got %q, want \"pending\"", c.Status)
	}

	got, err := s.GetEntityCandidate(ctx, c.ID)
	if err != nil {
		t.Fatalf("GetEntityCandidate: %v", err)
	}
	if got.Word != "Tehran" {
		t.Errorf("word: got %q, want \"Tehran\"", got.Word)
	}
}

func TestEntityCandidate_UpsertUpdatesPendingOnly(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c := &model.EntityCandidate{
		Word:          "IRGC",
		Type:          "ORG",
		AvgConfidence: 0.8,
		Sources:       []string{"pipe-a"},
		FirstSeenAt:   time.Now().UTC(),
		LastSeenAt:    time.Now().UTC(),
	}
	if err := s.UpsertEntityCandidate(ctx, c); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	// Second upsert — should increment count and recalculate avg
	c2 := &model.EntityCandidate{
		Word:          "IRGC",
		Type:          "ORG",
		AvgConfidence: 0.6,
		Sources:       []string{"pipe-b"},
		FirstSeenAt:   time.Now().UTC(),
		LastSeenAt:    time.Now().UTC(),
	}
	if err := s.UpsertEntityCandidate(ctx, c2); err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if c2.OccurrenceCount != 2 {
		t.Errorf("occurrence_count: got %d, want 2", c2.OccurrenceCount)
	}
	// avg = (0.8*1 + 0.6) / 2 = 0.7
	if c2.AvgConfidence < 0.69 || c2.AvgConfidence > 0.71 {
		t.Errorf("avg_confidence: got %f, want ~0.7", c2.AvgConfidence)
	}
	if len(c2.Sources) != 2 {
		t.Errorf("sources len: got %d, want 2", len(c2.Sources))
	}

	// Promote the candidate, then upsert again — count should NOT change
	if err := s.UpdateEntityCandidateStatus(ctx, c2.ID, "promoted", nil, true); err != nil {
		t.Fatalf("UpdateEntityCandidateStatus: %v", err)
	}
	c3 := &model.EntityCandidate{
		Word:          "IRGC",
		Type:          "ORG",
		AvgConfidence: 0.95,
		Sources:       []string{"pipe-c"},
		FirstSeenAt:   time.Now().UTC(),
		LastSeenAt:    time.Now().UTC(),
	}
	if err := s.UpsertEntityCandidate(ctx, c3); err != nil {
		t.Fatalf("third upsert on promoted: %v", err)
	}
	// Status is promoted — should not have updated count.
	got, _ := s.GetEntityCandidate(ctx, c2.ID)
	if got.OccurrenceCount != 2 {
		t.Errorf("promoted candidate count changed: got %d, want 2", got.OccurrenceCount)
	}
	// Regression guard: c3 struct must reflect stored values, not stale caller values.
	if c3.OccurrenceCount != 2 {
		t.Errorf("c3.OccurrenceCount: got %d, want 2 (stale struct not populated)", c3.OccurrenceCount)
	}
	if c3.AvgConfidence < 0.69 || c3.AvgConfidence > 0.71 {
		t.Errorf("c3.AvgConfidence: got %f, want ~0.7 (stale struct not populated)", c3.AvgConfidence)
	}
	if len(c3.Sources) != 2 {
		t.Errorf("c3.Sources len: got %d, want 2 (stale struct not populated)", len(c3.Sources))
	}
}

func TestEntityCandidate_ListAndCount(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, word := range []string{"Tehran", "IRGC", "Khamenei"} {
		c := &model.EntityCandidate{
			Word:          word,
			Type:          "LOC",
			AvgConfidence: 0.8,
			Sources:       []string{"test"},
			FirstSeenAt:   time.Now().UTC(),
			LastSeenAt:    time.Now().UTC(),
		}
		if err := s.UpsertEntityCandidate(ctx, c); err != nil {
			t.Fatalf("upsert %s: %v", word, err)
		}
	}

	pending := "pending"
	list, err := s.ListEntityCandidates(ctx, EntityCandidateFilter{Status: &pending, Page: 1, PerPage: 10})
	if err != nil {
		t.Fatalf("ListEntityCandidates: %v", err)
	}
	if len(list) != 3 {
		t.Errorf("list len: got %d, want 3", len(list))
	}

	n, err := s.CountEntityCandidates(ctx, EntityCandidateFilter{Status: &pending})
	if err != nil {
		t.Fatalf("CountEntityCandidates: %v", err)
	}
	if n != 3 {
		t.Errorf("count: got %d, want 3", n)
	}
}

func TestEntityCandidate_Delete(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c := &model.EntityCandidate{
		Word:          "test",
		Type:          "MISC",
		AvgConfidence: 0.5,
		Sources:       []string{"x"},
		FirstSeenAt:   time.Now().UTC(),
		LastSeenAt:    time.Now().UTC(),
	}
	if err := s.UpsertEntityCandidate(ctx, c); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := s.DeleteEntityCandidate(ctx, c.ID); err != nil {
		t.Fatalf("DeleteEntityCandidate: %v", err)
	}
	_, err := s.GetEntityCandidate(ctx, c.ID)
	if err == nil {
		t.Fatal("expected error after delete, got nil")
	}
}

func seedTrackedEntity(t *testing.T, s Store, name string) *model.TrackedEntity {
	t.Helper()
	e := &model.TrackedEntity{Name: name, Type: "PER", Aliases: []string{}, Active: true}
	if err := s.SaveTrackedEntity(context.Background(), e); err != nil {
		t.Fatalf("SaveTrackedEntity(%q): %v", name, err)
	}
	return e
}

func TestUpsertEntityConnection_NewPair(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	a := seedTrackedEntity(t, s, "Alpha")
	b := seedTrackedEntity(t, s, "Beta")

	if err := s.UpsertEntityConnection(ctx, a.ID, b.ID); err != nil {
		t.Fatalf("UpsertEntityConnection: %v", err)
	}

	conns, err := s.ListEntityConnections(ctx)
	if err != nil {
		t.Fatalf("ListEntityConnections: %v", err)
	}
	if len(conns) != 1 {
		t.Fatalf("want 1 connection, got %d", len(conns))
	}
	if conns[0].Weight != 1 {
		t.Errorf("want weight=1, got %d", conns[0].Weight)
	}
}

func TestUpsertEntityConnection_IncrementsWeight(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	a := seedTrackedEntity(t, s, "Alpha")
	b := seedTrackedEntity(t, s, "Beta")

	for i := 0; i < 3; i++ {
		if err := s.UpsertEntityConnection(ctx, a.ID, b.ID); err != nil {
			t.Fatalf("UpsertEntityConnection (iter %d): %v", i, err)
		}
	}

	conns, _ := s.ListEntityConnections(ctx)
	if conns[0].Weight != 3 {
		t.Errorf("want weight=3, got %d", conns[0].Weight)
	}
}

func TestUpsertEntityConnection_NormalizesOrder(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	a := seedTrackedEntity(t, s, "Alpha")
	b := seedTrackedEntity(t, s, "Beta")

	// Insert with reversed order — should still produce one row.
	_ = s.UpsertEntityConnection(ctx, b.ID, a.ID)
	_ = s.UpsertEntityConnection(ctx, a.ID, b.ID)

	conns, _ := s.ListEntityConnections(ctx)
	if len(conns) != 1 {
		t.Fatalf("want 1 connection (deduped), got %d", len(conns))
	}
	if conns[0].Weight != 2 {
		t.Errorf("want weight=2, got %d", conns[0].Weight)
	}
}

func TestListEntityConnections_JoinsNames(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	a := seedTrackedEntity(t, s, "Alpha")
	b := seedTrackedEntity(t, s, "Beta")
	_ = s.UpsertEntityConnection(ctx, a.ID, b.ID)

	conns, _ := s.ListEntityConnections(ctx)
	if conns[0].FromName == "" || conns[0].ToName == "" {
		t.Errorf("want joined names, got from=%q to=%q", conns[0].FromName, conns[0].ToName)
	}
}

func TestDeleteAllEntityConnections(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	a := seedTrackedEntity(t, s, "Alpha")
	b := seedTrackedEntity(t, s, "Beta")
	_ = s.UpsertEntityConnection(ctx, a.ID, b.ID)

	if err := s.DeleteAllEntityConnections(ctx); err != nil {
		t.Fatalf("DeleteAllEntityConnections: %v", err)
	}

	conns, _ := s.ListEntityConnections(ctx)
	if len(conns) != 0 {
		t.Errorf("want 0 connections after delete, got %d", len(conns))
	}
}

func TestGetEntityConnectionSubgraph_Depth1(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	a := seedTrackedEntity(t, s, "Alpha")
	b := seedTrackedEntity(t, s, "Beta")
	c := seedTrackedEntity(t, s, "Gamma")

	_ = s.UpsertEntityConnection(ctx, a.ID, b.ID) // A-B weight=1
	_ = s.UpsertEntityConnection(ctx, b.ID, c.ID) // B-C weight=1

	conns, err := s.GetEntityConnectionSubgraph(ctx, a.ID, 1, 1)
	if err != nil {
		t.Fatalf("GetEntityConnectionSubgraph: %v", err)
	}
	// Depth 1 from A: reachable = {A, B}. Edge A-B qualifies; B-C does not (C unreachable).
	if len(conns) != 1 {
		t.Fatalf("want 1 edge, got %d", len(conns))
	}
	if conns[0].FromID != a.ID || conns[0].ToID != b.ID {
		t.Errorf("want edge A-B, got from_id=%d to_id=%d", conns[0].FromID, conns[0].ToID)
	}
}

func TestGetEntityConnectionSubgraph_Depth2(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	a := seedTrackedEntity(t, s, "Alpha")
	b := seedTrackedEntity(t, s, "Beta")
	c := seedTrackedEntity(t, s, "Gamma")

	_ = s.UpsertEntityConnection(ctx, a.ID, b.ID)
	_ = s.UpsertEntityConnection(ctx, b.ID, c.ID)

	conns, err := s.GetEntityConnectionSubgraph(ctx, a.ID, 2, 1)
	if err != nil {
		t.Fatalf("GetEntityConnectionSubgraph: %v", err)
	}
	// Depth 2: reachable = {A, B, C}. Both edges qualify.
	if len(conns) != 2 {
		t.Fatalf("want 2 edges, got %d: %+v", len(conns), conns)
	}
}

func TestGetEntityConnectionSubgraph_MinWeightFilters(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	a := seedTrackedEntity(t, s, "Alpha")
	b := seedTrackedEntity(t, s, "Beta")
	c := seedTrackedEntity(t, s, "Gamma")

	_ = s.UpsertEntityConnection(ctx, a.ID, b.ID) // weight=1 after one upsert
	// Upsert b-c three times to get weight=3
	for i := 0; i < 3; i++ {
		_ = s.UpsertEntityConnection(ctx, b.ID, c.ID)
	}

	conns, err := s.GetEntityConnectionSubgraph(ctx, a.ID, 2, 2)
	if err != nil {
		t.Fatalf("GetEntityConnectionSubgraph: %v", err)
	}
	// min_weight=2: A-B (weight=1) excluded from adjacency, so only {A} reachable.
	// Result: empty (no edges meet threshold AND have both endpoints reachable).
	if len(conns) != 0 {
		t.Fatalf("want 0 edges, got %d", len(conns))
	}
}

func TestGetEntityConnectionSubgraph_UnknownEntityReturnsEmpty(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	a := seedTrackedEntity(t, s, "Alpha")
	b := seedTrackedEntity(t, s, "Beta")
	_ = s.UpsertEntityConnection(ctx, a.ID, b.ID)

	conns, err := s.GetEntityConnectionSubgraph(ctx, 9999, 2, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(conns) != 0 {
		t.Fatalf("want empty for unknown entity, got %d", len(conns))
	}
}

func TestGetEntityConnectionSubgraph_EntityWithNoConnectionsReturnsEmpty(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	a := seedTrackedEntity(t, s, "Alpha")

	conns, err := s.GetEntityConnectionSubgraph(ctx, a.ID, 2, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(conns) != 0 {
		t.Fatalf("want empty slice, got %d", len(conns))
	}
}

// ── BulkDeleteEntityCandidates ───────────────────────────────────────────────

func seedCandidate(t *testing.T, s Store, word, typ, status string, conf float64) *model.EntityCandidate {
	t.Helper()
	c := &model.EntityCandidate{
		Word:          word,
		Type:          typ,
		AvgConfidence: conf,
		Sources:       []string{"test"},
		FirstSeenAt:   time.Now().UTC(),
		LastSeenAt:    time.Now().UTC(),
	}
	if err := s.UpsertEntityCandidate(context.Background(), c); err != nil {
		t.Fatalf("UpsertEntityCandidate(%q): %v", word, err)
	}
	// Update status if it's not "pending" (UpsertEntityCandidate always sets to "pending")
	if status != "pending" {
		if err := s.UpdateEntityCandidateStatus(context.Background(), c.ID, status, nil, false); err != nil {
			t.Fatalf("UpdateEntityCandidateStatus(%q): %v", word, err)
		}
		c.Status = status
	} else {
		c.Status = "pending"
	}
	return c
}

func TestBulkDeleteEntityCandidates_All(t *testing.T) {
	s := newTestStore(t)
	seedCandidate(t, s, "Tehran", "I-LOC", "pending", 0.9)
	seedCandidate(t, s, "Rouhani", "I-PER", "pending", 0.8)

	n, err := s.BulkDeleteEntityCandidates(context.Background(), BulkDeleteEntityCandidatesFilter{})
	if err != nil {
		t.Fatalf("BulkDeleteEntityCandidates: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 deleted, got %d", n)
	}
	pending := "pending"
	count, _ := s.CountEntityCandidates(context.Background(), EntityCandidateFilter{Status: &pending})
	if count != 0 {
		t.Errorf("expected 0 remaining, got %d", count)
	}
}

func TestBulkDeleteEntityCandidates_ByIDs(t *testing.T) {
	s := newTestStore(t)
	c1 := seedCandidate(t, s, "Tehran", "I-LOC", "pending", 0.9)
	c2 := seedCandidate(t, s, "Rouhani", "I-PER", "pending", 0.8)

	n, err := s.BulkDeleteEntityCandidates(context.Background(), BulkDeleteEntityCandidatesFilter{IDs: []int64{c1.ID}})
	if err != nil {
		t.Fatalf("BulkDeleteEntityCandidates: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	// c2 must still exist
	if _, err := s.GetEntityCandidate(context.Background(), c2.ID); err != nil {
		t.Errorf("c2 should still exist: %v", err)
	}
}

func TestBulkDeleteEntityCandidates_ByStatus(t *testing.T) {
	s := newTestStore(t)
	seedCandidate(t, s, "Tehran", "I-LOC", "pending", 0.9)
	c2 := seedCandidate(t, s, "Rouhani", "I-PER", "promoted", 0.95)

	n, err := s.BulkDeleteEntityCandidates(context.Background(), BulkDeleteEntityCandidatesFilter{Status: "pending"})
	if err != nil {
		t.Fatalf("BulkDeleteEntityCandidates: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	// promoted must survive
	if _, err := s.GetEntityCandidate(context.Background(), c2.ID); err != nil {
		t.Errorf("promoted candidate should still exist: %v", err)
	}
}

func TestBulkDeleteEntityCandidates_ByType(t *testing.T) {
	s := newTestStore(t)
	seedCandidate(t, s, "Tehran", "I-LOC", "pending", 0.9)
	c2 := seedCandidate(t, s, "Rouhani", "I-PER", "pending", 0.8)

	n, err := s.BulkDeleteEntityCandidates(context.Background(), BulkDeleteEntityCandidatesFilter{Type: "I-LOC"})
	if err != nil {
		t.Fatalf("BulkDeleteEntityCandidates: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	if _, err := s.GetEntityCandidate(context.Background(), c2.ID); err != nil {
		t.Errorf("I-PER candidate should still exist: %v", err)
	}
}

func TestBulkDeleteEntityCandidates_ByConfidenceRange(t *testing.T) {
	s := newTestStore(t)
	seedCandidate(t, s, "Tehran", "I-LOC", "pending", 0.3)
	c2 := seedCandidate(t, s, "Rouhani", "I-PER", "pending", 0.9)

	maxConf := 0.5
	n, err := s.BulkDeleteEntityCandidates(context.Background(), BulkDeleteEntityCandidatesFilter{MaxConfidence: &maxConf})
	if err != nil {
		t.Fatalf("BulkDeleteEntityCandidates: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	if _, err := s.GetEntityCandidate(context.Background(), c2.ID); err != nil {
		t.Errorf("high-confidence candidate should still exist: %v", err)
	}
}

func TestBulkDeleteEntityCandidates_NoMatch(t *testing.T) {
	s := newTestStore(t)

	n, err := s.BulkDeleteEntityCandidates(context.Background(), BulkDeleteEntityCandidatesFilter{Status: "pending"})
	if err != nil {
		t.Fatalf("BulkDeleteEntityCandidates: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 deleted, got %d", n)
	}
}

// ── BulkDeleteXRules ──────────────────────────────────────────────────────────

func TestBulkDeleteXRules_All(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	r1 := sampleRule()
	r1.Name = "rule-1"
	if err := s.SaveXRule(ctx, r1); err != nil {
		t.Fatalf("SaveXRule: %v", err)
	}
	r2 := sampleRule()
	r2.Name = "rule-2"
	if err := s.SaveXRule(ctx, r2); err != nil {
		t.Fatalf("SaveXRule: %v", err)
	}

	n, err := s.BulkDeleteXRules(ctx, BulkDeleteXRulesFilter{})
	if err != nil {
		t.Fatalf("BulkDeleteXRules: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 deleted, got %d", n)
	}
	rules, _ := s.ListXRules(ctx, XRuleFilter{})
	if len(rules) != 0 {
		t.Errorf("expected 0 rules remaining, got %d", len(rules))
	}
}

func TestBulkDeleteXRules_ByIDs(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	r1 := sampleRule()
	r1.Name = "rule-1"
	if err := s.SaveXRule(ctx, r1); err != nil {
		t.Fatalf("SaveXRule r1: %v", err)
	}
	r2 := sampleRule()
	r2.Name = "rule-2"
	if err := s.SaveXRule(ctx, r2); err != nil {
		t.Fatalf("SaveXRule r2: %v", err)
	}

	n, err := s.BulkDeleteXRules(ctx, BulkDeleteXRulesFilter{IDs: []int64{r1.ID}})
	if err != nil {
		t.Fatalf("BulkDeleteXRules: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	rules, _ := s.ListXRules(ctx, XRuleFilter{})
	if len(rules) != 1 || rules[0].ID != r2.ID {
		t.Errorf("expected only r2 remaining")
	}
}

func TestBulkDeleteXRules_ByLocal(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	r1 := sampleRule()
	r1.Name = "local-rule"
	r1.Local = true
	_ = s.SaveXRule(ctx, r1)
	r2 := sampleRule()
	r2.Name = "remote-rule"
	r2.Local = false
	_ = s.SaveXRule(ctx, r2)

	localTrue := true
	n, err := s.BulkDeleteXRules(ctx, BulkDeleteXRulesFilter{Local: &localTrue})
	if err != nil {
		t.Fatalf("BulkDeleteXRules: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	rules, _ := s.ListXRules(ctx, XRuleFilter{})
	if len(rules) != 1 || rules[0].ID != r2.ID {
		t.Errorf("expected only remote rule remaining")
	}
}

// ── BulkDeleteXTerms ────────────────────────────────────────────────────────

func TestBulkDeleteXTerms_All(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	src1 := sampleSource()
	src1.Name = "src-1"
	_ = s.SaveXTerm(ctx, src1)
	src2 := sampleSource()
	src2.Name = "src-2"
	_ = s.SaveXTerm(ctx, src2)

	n, err := s.BulkDeleteXTerms(ctx, BulkDeleteXTermsFilter{})
	if err != nil {
		t.Fatalf("BulkDeleteXTerms: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 deleted, got %d", n)
	}
	srcs, _ := s.ListXTerms(ctx, XTermFilter{})
	if len(srcs) != 0 {
		t.Errorf("expected 0 sources remaining, got %d", len(srcs))
	}
}

func TestBulkDeleteXTerms_ByType(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	src1 := sampleSource()
	src1.Name = "kw-src"
	src1.Type = model.XTermTypeKeyword
	if err := s.SaveXTerm(ctx, src1); err != nil {
		t.Fatalf("SaveXTerm src1: %v", err)
	}
	src2 := sampleSource()
	src2.Name = "ph-src"
	src2.Type = model.XTermTypePhrase
	if err := s.SaveXTerm(ctx, src2); err != nil {
		t.Fatalf("SaveXTerm src2: %v", err)
	}

	n, err := s.BulkDeleteXTerms(ctx, BulkDeleteXTermsFilter{Type: model.XTermTypeKeyword})
	if err != nil {
		t.Fatalf("BulkDeleteXTerms: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	srcs, _ := s.ListXTerms(ctx, XTermFilter{})
	if len(srcs) != 1 || srcs[0].ID != src2.ID {
		t.Errorf("expected only phrase source remaining")
	}
}

// ── BulkDeleteTrackedEntities ────────────────────────────────────────────────

func TestBulkDeleteTrackedEntities_All(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedTrackedEntity(t, s, "Tehran")
	seedTrackedEntity(t, s, "Rouhani")

	n, err := s.BulkDeleteTrackedEntities(ctx, BulkDeleteTrackedEntitiesFilter{})
	if err != nil {
		t.Fatalf("BulkDeleteTrackedEntities: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 deleted, got %d", n)
	}
	entities, _ := s.ListTrackedEntities(ctx, TrackedEntityFilter{})
	if len(entities) != 0 {
		t.Errorf("expected 0 entities remaining, got %d", len(entities))
	}
}

func TestBulkDeleteTrackedEntities_ByType(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	e1 := seedTrackedEntity(t, s, "Tehran") // type = "PER" from seedTrackedEntity
	e2 := &model.TrackedEntity{Name: "IRGC", Type: "ORG", Aliases: []string{}, Active: true}
	_ = s.SaveTrackedEntity(ctx, e2)

	n, err := s.BulkDeleteTrackedEntities(ctx, BulkDeleteTrackedEntitiesFilter{Type: "PER"})
	if err != nil {
		t.Fatalf("BulkDeleteTrackedEntities: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	entities, _ := s.ListTrackedEntities(ctx, TrackedEntityFilter{})
	if len(entities) != 1 || entities[0].ID != e2.ID {
		t.Errorf("expected only ORG entity remaining, e1.ID=%d", e1.ID)
	}
}

func TestBulkDeleteXTerms_ByIDs(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	src1 := sampleSource()
	src1.Name = "src-1"
	if err := s.SaveXTerm(ctx, src1); err != nil {
		t.Fatalf("SaveXTerm src1: %v", err)
	}
	src2 := sampleSource()
	src2.Name = "src-2"
	if err := s.SaveXTerm(ctx, src2); err != nil {
		t.Fatalf("SaveXTerm src2: %v", err)
	}

	n, err := s.BulkDeleteXTerms(ctx, BulkDeleteXTermsFilter{IDs: []int64{src1.ID}})
	if err != nil {
		t.Fatalf("BulkDeleteXTerms: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	srcs, _ := s.ListXTerms(ctx, XTermFilter{})
	if len(srcs) != 1 || srcs[0].ID != src2.ID {
		t.Errorf("expected only src2 remaining")
	}
}

func TestBulkDeleteTrackedEntities_ByIDs(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	e1 := seedTrackedEntity(t, s, "Tehran")
	e2 := seedTrackedEntity(t, s, "Rouhani")

	n, err := s.BulkDeleteTrackedEntities(ctx, BulkDeleteTrackedEntitiesFilter{IDs: []int64{e1.ID}})
	if err != nil {
		t.Fatalf("BulkDeleteTrackedEntities: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	entities, _ := s.ListTrackedEntities(ctx, TrackedEntityFilter{})
	if len(entities) != 1 || entities[0].ID != e2.ID {
		t.Errorf("expected only e2 remaining")
	}
}

// ── BulkDeleteDigestJobs ─────────────────────────────────────────────────────

func seedDigestJob(t *testing.T, s Store, status model.DigestStatus) *model.DigestJob {
	t.Helper()
	job := &model.DigestJob{
		ID:        fmt.Sprintf("digest-%d", time.Now().UnixNano()),
		Status:    status,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.SaveDigestJob(context.Background(), job); err != nil {
		t.Fatalf("SaveDigestJob: %v", err)
	}
	return job
}

func TestBulkDeleteDigestJobs_All(t *testing.T) {
	s := newTestStore(t)
	seedDigestJob(t, s, model.DigestStatusDone)
	seedDigestJob(t, s, model.DigestStatusError)

	n, err := s.BulkDeleteDigestJobs(context.Background(), BulkDeleteDigestJobsFilter{})
	if err != nil {
		t.Fatalf("BulkDeleteDigestJobs: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 deleted, got %d", n)
	}
}

func TestBulkDeleteDigestJobs_ByStatus(t *testing.T) {
	s := newTestStore(t)
	seedDigestJob(t, s, model.DigestStatusDone)
	j2 := seedDigestJob(t, s, model.DigestStatusError)

	n, err := s.BulkDeleteDigestJobs(context.Background(), BulkDeleteDigestJobsFilter{Status: model.DigestStatusDone})
	if err != nil {
		t.Fatalf("BulkDeleteDigestJobs: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	if _, err := s.GetDigestJob(context.Background(), j2.ID); err != nil {
		t.Errorf("error digest job should still exist: %v", err)
	}
}

func TestBulkDeleteDigestJobs_ByIDs(t *testing.T) {
	s := newTestStore(t)
	j1 := seedDigestJob(t, s, model.DigestStatusDone)
	j2 := seedDigestJob(t, s, model.DigestStatusDone)

	n, err := s.BulkDeleteDigestJobs(context.Background(), BulkDeleteDigestJobsFilter{IDs: []string{j1.ID}})
	if err != nil {
		t.Fatalf("BulkDeleteDigestJobs: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	if _, err := s.GetDigestJob(context.Background(), j2.ID); err != nil {
		t.Errorf("j2 should still exist: %v", err)
	}
}
