package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/migrations"
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

func sampleTweet() *model.Tweet {
	return &model.Tweet{
		ID:           "tweet-1",
		AuthorID:     "author-1",
		AuthorHandle: "handle1",
		Text:         "Hello Iran",
		RawJSON:      []byte(`{"id":"tweet-1"}`),
		CreatedAt:    time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
		FetchedAt:    time.Date(2026, 1, 1, 12, 1, 0, 0, time.UTC),
	}
}

func sampleSource() *model.Source {
	return &model.Source{
		Name:      "Iran keyword",
		Type:      model.SourceTypeKeyword,
		Class:     model.SourceClassAtom,
		Operator:  "",
		Value:     "Iran",
		Active:    true,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func sampleRule() *model.Rule {
	return &model.Rule{
		Name:          "iran-rule",
		Expression:    json.RawMessage(`{"src":1}`),
		CompiledValue: "Iran",
		XRuleID:       "",
		Active:        false,
		CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

// 1. TestSaveTweet_GetTweet
func TestSaveTweet_GetTweet(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	tw := sampleTweet()
	if err := s.SaveTweet(ctx, tw); err != nil {
		t.Fatalf("SaveTweet: %v", err)
	}

	got, err := s.GetTweet(ctx, tw.ID)
	if err != nil {
		t.Fatalf("GetTweet: %v", err)
	}

	if got.ID != tw.ID {
		t.Errorf("ID: got %q, want %q", got.ID, tw.ID)
	}
	if got.AuthorID != tw.AuthorID {
		t.Errorf("AuthorID: got %q, want %q", got.AuthorID, tw.AuthorID)
	}
	if got.AuthorHandle != tw.AuthorHandle {
		t.Errorf("AuthorHandle: got %q, want %q", got.AuthorHandle, tw.AuthorHandle)
	}
	if got.Text != tw.Text {
		t.Errorf("Text: got %q, want %q", got.Text, tw.Text)
	}
	if string(got.RawJSON) != string(tw.RawJSON) {
		t.Errorf("RawJSON: got %q, want %q", got.RawJSON, tw.RawJSON)
	}
	if !got.CreatedAt.Equal(tw.CreatedAt) {
		t.Errorf("CreatedAt: got %v, want %v", got.CreatedAt, tw.CreatedAt)
	}
	if !got.FetchedAt.Equal(tw.FetchedAt) {
		t.Errorf("FetchedAt: got %v, want %v", got.FetchedAt, tw.FetchedAt)
	}
}

// 2. TestSaveTweet_Idempotent
func TestSaveTweet_Idempotent(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	tw := sampleTweet()
	if err := s.SaveTweet(ctx, tw); err != nil {
		t.Fatalf("first SaveTweet: %v", err)
	}
	if err := s.SaveTweet(ctx, tw); err != nil {
		t.Fatalf("second SaveTweet: %v", err)
	}

	tweets, err := s.ListTweets(ctx, TweetFilter{})
	if err != nil {
		t.Fatalf("ListTweets: %v", err)
	}
	if len(tweets) != 1 {
		t.Errorf("expected 1 tweet, got %d", len(tweets))
	}
}

// 3. TestSaveSource_ListSources_ActiveFilter
func TestSaveSource_ListSources_ActiveFilter(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	active := sampleSource()
	active.Active = true
	if err := s.SaveSource(ctx, active); err != nil {
		t.Fatalf("SaveSource active: %v", err)
	}

	inactive := sampleSource()
	inactive.Value = "nuclear"
	inactive.Name = "nuclear keyword"
	inactive.Active = false
	if err := s.SaveSource(ctx, inactive); err != nil {
		t.Fatalf("SaveSource inactive: %v", err)
	}

	trueVal := true
	sources, err := s.ListSources(ctx, SourceFilter{Active: &trueVal})
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}
	if len(sources) != 1 {
		t.Errorf("expected 1 active source, got %d", len(sources))
	}
	if !sources[0].Active {
		t.Error("expected returned source to be active")
	}
}

// 4. TestDeleteSource
func TestDeleteSource(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	src := sampleSource()
	if err := s.SaveSource(ctx, src); err != nil {
		t.Fatalf("SaveSource: %v", err)
	}

	if err := s.DeleteSource(ctx, src.ID); err != nil {
		t.Fatalf("DeleteSource: %v", err)
	}

	got, err := s.GetSource(ctx, src.ID)
	if err == nil && got != nil {
		t.Error("expected error or nil after deleting source, got non-nil source")
	}
}

// 5. TestSaveAndGetSource — new fields
func TestSaveAndGetSource(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	src := &model.Source{
		Name:      "BBC Persian",
		Type:      model.SourceTypeAccount,
		Class:     model.SourceClassAtom,
		Operator:  "from:",
		Value:     "BBCPersian",
		Active:    true,
		CreatedAt: time.Now().UTC().Truncate(time.Second),
		UpdatedAt: time.Now().UTC().Truncate(time.Second),
	}
	if err := s.SaveSource(ctx, src); err != nil {
		t.Fatalf("SaveSource: %v", err)
	}
	if src.ID == 0 {
		t.Fatal("expected non-zero ID after save")
	}
	got, err := s.GetSource(ctx, src.ID)
	if err != nil {
		t.Fatalf("GetSource: %v", err)
	}
	if got.Name != "BBC Persian" || got.Operator != "from:" || got.Class != model.SourceClassAtom {
		t.Errorf("unexpected source: %+v", got)
	}
}

// 6. TestSetSourceActive
func TestSetSourceActive(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	src := sampleSource()
	_ = s.SaveSource(ctx, src)
	if err := s.SetSourceActive(ctx, src.ID, false); err != nil {
		t.Fatalf("SetSourceActive: %v", err)
	}
	got, _ := s.GetSource(ctx, src.ID)
	if got.Active {
		t.Error("expected Active=false")
	}
}

// 7. TestActiveRuleRefCount
func TestActiveRuleRefCount(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	src := sampleSource()
	_ = s.SaveSource(ctx, src)
	n, err := s.ActiveRuleRefCount(ctx, src.ID)
	if err != nil {
		t.Fatalf("ActiveRuleRefCount: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 refs, got %d", n)
	}
}

// 8. TestSaveAndGetRule
func TestSaveAndGetRule(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	rule := sampleRule()
	if err := s.SaveRule(ctx, rule); err != nil {
		t.Fatalf("SaveRule: %v", err)
	}
	if rule.ID == 0 {
		t.Fatal("expected non-zero ID after save")
	}
	got, err := s.GetRule(ctx, rule.ID)
	if err != nil {
		t.Fatalf("GetRule: %v", err)
	}
	if got.Name != "iran-rule" {
		t.Errorf("Name: got %q, want %q", got.Name, "iran-rule")
	}
	if got.CompiledValue != "Iran" {
		t.Errorf("CompiledValue: got %q, want %q", got.CompiledValue, "Iran")
	}
}

// 9. TestGetRuleByXRuleID
func TestGetRuleByXRuleID(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	rule := sampleRule()
	rule.XRuleID = "x-rule-123"
	if err := s.SaveRule(ctx, rule); err != nil {
		t.Fatalf("SaveRule: %v", err)
	}

	got, err := s.GetRuleByXRuleID(ctx, "x-rule-123")
	if err != nil {
		t.Fatalf("GetRuleByXRuleID: %v", err)
	}
	if got.ID != rule.ID {
		t.Errorf("ID: got %d, want %d", got.ID, rule.ID)
	}
}

// 10. TestTweetRuleCount
func TestTweetRuleCount(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	tw := sampleTweet()
	_ = s.SaveTweet(ctx, tw)

	rule := sampleRule()
	_ = s.SaveRule(ctx, rule)

	tr := &model.TweetRule{TweetID: tw.ID, RuleID: rule.ID}
	if err := s.SaveTweetRule(ctx, tr); err != nil {
		t.Fatalf("SaveTweetRule: %v", err)
	}

	count, err := s.TweetRuleCount(ctx, rule.ID)
	if err != nil {
		t.Fatalf("TweetRuleCount: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1, got %d", count)
	}
}

// 11. TestSaveTweetRule_GetTweet_PopulatesRules
func TestSaveTweetRule_GetTweet_PopulatesRules(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	tw := sampleTweet()
	_ = s.SaveTweet(ctx, tw)

	rule := sampleRule()
	_ = s.SaveRule(ctx, rule)

	tr := &model.TweetRule{TweetID: tw.ID, RuleID: rule.ID}
	if err := s.SaveTweetRule(ctx, tr); err != nil {
		t.Fatalf("SaveTweetRule: %v", err)
	}

	got, err := s.GetTweet(ctx, tw.ID)
	if err != nil {
		t.Fatalf("GetTweet: %v", err)
	}
	if len(got.Rules) != 1 {
		t.Errorf("expected 1 rule on tweet, got %d", len(got.Rules))
	}
	if got.Rules[0].ID != rule.ID {
		t.Errorf("rule ID: got %d, want %d", got.Rules[0].ID, rule.ID)
	}
}

// 12. TestSavePipeline_SetActive_ListPipelines
func TestSavePipeline_SetActive_ListPipelines(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	p := &model.Pipeline{
		Name:   "sentiment-v1",
		Type:   model.PipelineTypeONNX,
		Config: json.RawMessage(`{"path":"/models/sentiment.onnx"}`),
		Active: false,
	}
	if err := s.SavePipeline(ctx, p); err != nil {
		t.Fatalf("SavePipeline: %v", err)
	}

	if err := s.SetPipelineActive(ctx, p.ID, true); err != nil {
		t.Fatalf("SetPipelineActive: %v", err)
	}

	pipelines, err := s.ListPipelines(ctx, PipelineFilter{})
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

func openTestDB(t *testing.T) (*sql.DB, error) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, nil
}
