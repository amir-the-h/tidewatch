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
		Type:      model.SourceTypeKeyword,
		Value:     "Iran",
		XRuleID:   "rule-1",
		Active:    true,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

// 1. TestSaveTweet_GetTweet — save a tweet, get it back, verify all fields equal
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

// 2. TestSaveTweet_Idempotent — save same tweet twice, no error, still 1 row
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

// 3. TestSaveSource_ListSources_ActiveFilter — save 2 sources (1 active, 1 inactive),
// list with Active=true, get only 1
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

// 4. TestDeleteSource — save then delete a source, GetSource returns error
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

// 5. TestSaveTweetSource_GetTweet_PopulatesSources — save tweet + source + tweetSource,
// GetTweet returns tweet with Sources populated
func TestSaveTweetSource_GetTweet_PopulatesSources(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	tw := sampleTweet()
	if err := s.SaveTweet(ctx, tw); err != nil {
		t.Fatalf("SaveTweet: %v", err)
	}

	src := sampleSource()
	if err := s.SaveSource(ctx, src); err != nil {
		t.Fatalf("SaveSource: %v", err)
	}

	ts := &model.TweetSource{TweetID: tw.ID, SourceID: src.ID}
	if err := s.SaveTweetSource(ctx, ts); err != nil {
		t.Fatalf("SaveTweetSource: %v", err)
	}

	got, err := s.GetTweet(ctx, tw.ID)
	if err != nil {
		t.Fatalf("GetTweet: %v", err)
	}
	if len(got.Sources) != 1 {
		t.Errorf("expected 1 source on tweet, got %d", len(got.Sources))
	}
	if got.Sources[0].ID != src.ID {
		t.Errorf("source ID: got %d, want %d", got.Sources[0].ID, src.ID)
	}
}

// 6. TestSaveModel_SetActive_ListModels — save model, SetModelActive(true), ListModels returns it active
func TestSaveModel_SetActive_ListModels(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	m := &model.Model{
		Name:      "sentiment-v1",
		Version:   "1.0.0",
		Path:      "/models/sentiment.onnx",
		Type:      model.ModelTypeSentiment,
		Active:    false,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.SaveModel(ctx, m); err != nil {
		t.Fatalf("SaveModel: %v", err)
	}

	if err := s.SetModelActive(ctx, m.ID, true); err != nil {
		t.Fatalf("SetModelActive: %v", err)
	}

	models, err := s.ListModels(ctx)
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	if !models[0].Active {
		t.Error("expected model to be active")
	}
}

// 7. TestSaveCategorization_ListByTweetID — save tweet + categorization,
// ListCategorizations by tweet ID returns 1
func TestSaveCategorization_ListByTweetID(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	tw := sampleTweet()
	if err := s.SaveTweet(ctx, tw); err != nil {
		t.Fatalf("SaveTweet: %v", err)
	}

	c := &model.Categorization{
		TweetID:      tw.ID,
		ModelName:    "classifier",
		ModelVersion: "1.0",
		Results:      json.RawMessage(`{"label":"politics","score":0.9}`),
		CreatedAt:    time.Now().UTC(),
	}
	if err := s.SaveCategorization(ctx, c); err != nil {
		t.Fatalf("SaveCategorization: %v", err)
	}

	cats, err := s.ListCategorizations(ctx, CategorizationFilter{TweetID: &tw.ID})
	if err != nil {
		t.Fatalf("ListCategorizations: %v", err)
	}
	if len(cats) != 1 {
		t.Errorf("expected 1 categorization, got %d", len(cats))
	}
	if cats[0].TweetID != tw.ID {
		t.Errorf("TweetID: got %q, want %q", cats[0].TweetID, tw.ID)
	}
}

// 8. TestListUncategorizedTweets_NoCategories — save tweet with no categorization, returns it
func TestListUncategorizedTweets_NoCategories(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	tw := sampleTweet()
	if err := s.SaveTweet(ctx, tw); err != nil {
		t.Fatalf("SaveTweet: %v", err)
	}

	tweets, err := s.ListUncategorizedTweets(ctx, nil)
	if err != nil {
		t.Fatalf("ListUncategorizedTweets: %v", err)
	}
	if len(tweets) != 1 {
		t.Errorf("expected 1 uncategorized tweet, got %d", len(tweets))
	}
	if tweets[0].ID != tw.ID {
		t.Errorf("tweet ID: got %q, want %q", tweets[0].ID, tw.ID)
	}
}

// 9. TestListUncategorizedTweets_AllCovered — save tweet + categorization for model "m1",
// ListUncategorizedTweets(["m1"]) returns empty
func TestListUncategorizedTweets_AllCovered(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	tw := sampleTweet()
	if err := s.SaveTweet(ctx, tw); err != nil {
		t.Fatalf("SaveTweet: %v", err)
	}

	c := &model.Categorization{
		TweetID:      tw.ID,
		ModelName:    "m1",
		ModelVersion: "1.0",
		Results:      json.RawMessage(`{}`),
		CreatedAt:    time.Now().UTC(),
	}
	if err := s.SaveCategorization(ctx, c); err != nil {
		t.Fatalf("SaveCategorization: %v", err)
	}

	tweets, err := s.ListUncategorizedTweets(ctx, []string{"m1"})
	if err != nil {
		t.Fatalf("ListUncategorizedTweets: %v", err)
	}
	if len(tweets) != 0 {
		t.Errorf("expected 0 tweets, got %d", len(tweets))
	}
}

// 10. TestRunMigrations_Idempotent — call RunMigrations twice on same DB, no error
func TestRunMigrations_Idempotent(t *testing.T) {
	s1, err := NewSQLiteStore(":memory:", migrations.FS)
	if err != nil {
		t.Fatalf("first NewSQLiteStore: %v", err)
	}
	_ = s1.Close()

	// The :memory: DB is gone with Close, so we test idempotency by running
	// RunMigrations twice on the same open db connection.
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
