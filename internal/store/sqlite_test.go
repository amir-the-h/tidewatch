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

func samplePipeline() *model.Pipeline {
	return &model.Pipeline{
		Name:   "test-pipeline",
		Type:   model.PipelineTypeOllama,
		Active: true,
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

func TestListTweets_HasEntityTrack(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	tw1 := &model.Tweet{ID: "t1", AuthorID: "a1", AuthorHandle: "h1", Text: "tweet 1", RawJSON: []byte(`{}`), CreatedAt: time.Now()}
	tw2 := &model.Tweet{ID: "t2", AuthorID: "a2", AuthorHandle: "h2", Text: "tweet 2", RawJSON: []byte(`{}`), CreatedAt: time.Now()}
	if err := s.SaveTweet(ctx, tw1); err != nil {
		t.Fatalf("SaveTweet tw1: %v", err)
	}
	if err := s.SaveTweet(ctx, tw2); err != nil {
		t.Fatalf("SaveTweet tw2: %v", err)
	}

	// Create a tracked entity so the foreign key constraint is satisfied.
	entity := &model.TrackedEntity{Name: "test-entity", Type: "PER", Active: true, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := s.SaveTrackedEntity(ctx, entity); err != nil {
		t.Fatalf("SaveTrackedEntity: %v", err)
	}

	// Only tw1 has an entity track
	if err := s.SaveEntityTrack(ctx, &model.EntityTrack{EntityID: entity.ID, TweetID: "t1", MatchSource: "ner", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("SaveEntityTrack: %v", err)
	}

	all, err := s.ListTweets(ctx, TweetFilter{Page: 1, PerPage: 100})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 tweets, got %d", len(all))
	}

	tracked, err := s.ListTweets(ctx, TweetFilter{Page: 1, PerPage: 100, HasEntityTrack: true})
	if err != nil {
		t.Fatalf("list tracked: %v", err)
	}
	if len(tracked) != 1 {
		t.Fatalf("expected 1 tracked tweet, got %d", len(tracked))
	}
	if tracked[0].ID != "t1" {
		t.Errorf("expected tweet t1, got %s", tracked[0].ID)
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
