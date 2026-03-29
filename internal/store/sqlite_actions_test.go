package store

import (
	"context"
	"testing"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

// helpers shared by action tests

func seedActionAndTweet(t *testing.T, s Store) (action *model.Action, tweetID string) {
	t.Helper()
	ctx := context.Background()

	tw := sampleTweet()
	if err := s.SaveTweet(ctx, tw); err != nil {
		t.Fatalf("SaveTweet: %v", err)
	}

	p := samplePipeline()
	if err := s.SavePipeline(ctx, p); err != nil {
		t.Fatalf("SavePipeline: %v", err)
	}

	a := &model.Action{
		Name:       "test-action",
		PipelineID: p.ID,
		Active:     true,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	if err := s.SaveAction(ctx, a); err != nil {
		t.Fatalf("SaveAction: %v", err)
	}

	return a, tw.ID
}

func saveExecution(t *testing.T, s Store, actionID int64, tweetID string) *model.ActionExecution {
	t.Helper()
	e := &model.ActionExecution{
		ActionID:   actionID,
		TweetID:    tweetID,
		Status:     model.ExecutionStatusSuccess,
		ExecutedAt: time.Now().UTC(),
	}
	if err := s.SaveActionExecution(context.Background(), e); err != nil {
		t.Fatalf("SaveActionExecution: %v", err)
	}
	return e
}

func countExecutions(t *testing.T, s Store, tweetID string) int {
	t.Helper()
	execs, err := s.ListActionExecutions(context.Background(), ExecutionFilter{TweetID: &tweetID})
	if err != nil {
		t.Fatalf("ListActionExecutions: %v", err)
	}
	return len(execs)
}

// TestDeleteActionExecutions_AllActions clears all executions for a tweet.
func TestDeleteActionExecutions_AllActions(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	a, tweetID := seedActionAndTweet(t, s)
	saveExecution(t, s, a.ID, tweetID)
	saveExecution(t, s, a.ID, tweetID)

	if n := countExecutions(t, s, tweetID); n != 2 {
		t.Fatalf("expected 2 executions before delete, got %d", n)
	}

	deleted, err := s.DeleteActionExecutions(ctx, []string{tweetID}, nil)
	if err != nil {
		t.Fatalf("DeleteActionExecutions: %v", err)
	}
	if deleted != 2 {
		t.Errorf("expected 2 deleted, got %d", deleted)
	}
	if n := countExecutions(t, s, tweetID); n != 0 {
		t.Errorf("expected 0 executions after delete, got %d", n)
	}
}

// TestDeleteActionExecutions_ScopedToAction clears only executions for the given action.
func TestDeleteActionExecutions_ScopedToAction(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	a1, tweetID := seedActionAndTweet(t, s)

	// Create a second action on the same pipeline
	p2 := samplePipeline()
	p2.Name = "pipeline-2"
	if err := s.SavePipeline(ctx, p2); err != nil {
		t.Fatalf("SavePipeline p2: %v", err)
	}
	a2 := &model.Action{
		Name:       "action-2",
		PipelineID: p2.ID,
		Active:     true,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	if err := s.SaveAction(ctx, a2); err != nil {
		t.Fatalf("SaveAction a2: %v", err)
	}

	saveExecution(t, s, a1.ID, tweetID)
	saveExecution(t, s, a2.ID, tweetID)

	// Clear only a1's executions
	deleted, err := s.DeleteActionExecutions(ctx, []string{tweetID}, []int64{a1.ID})
	if err != nil {
		t.Fatalf("DeleteActionExecutions: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 deleted, got %d", deleted)
	}

	// a2's execution must still exist
	remaining, _ := s.ListActionExecutions(ctx, ExecutionFilter{TweetID: &tweetID})
	if len(remaining) != 1 {
		t.Errorf("expected 1 remaining execution, got %d", len(remaining))
	}
	if remaining[0].ActionID != a2.ID {
		t.Errorf("expected remaining execution to belong to a2 (%d), got action_id %d", a2.ID, remaining[0].ActionID)
	}
}

// TestDeleteActionExecutions_MultipleTweets clears executions for multiple tweets at once.
func TestDeleteActionExecutions_MultipleTweets(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	a, tweetID1 := seedActionAndTweet(t, s)

	// Second tweet
	tw2 := sampleTweet()
	tw2.ID = "tweet-2"
	if err := s.SaveTweet(ctx, tw2); err != nil {
		t.Fatalf("SaveTweet tw2: %v", err)
	}
	tweetID2 := tw2.ID

	saveExecution(t, s, a.ID, tweetID1)
	saveExecution(t, s, a.ID, tweetID2)

	deleted, err := s.DeleteActionExecutions(ctx, []string{tweetID1, tweetID2}, nil)
	if err != nil {
		t.Fatalf("DeleteActionExecutions: %v", err)
	}
	if deleted != 2 {
		t.Errorf("expected 2 deleted, got %d", deleted)
	}
}

// TestDeleteActionExecutions_EmptyResult returns 0 with no error when there is nothing to delete.
func TestDeleteActionExecutions_EmptyResult(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	deleted, err := s.DeleteActionExecutions(ctx, []string{"nonexistent-tweet"}, nil)
	if err != nil {
		t.Fatalf("DeleteActionExecutions: %v", err)
	}
	if deleted != 0 {
		t.Errorf("expected 0 deleted, got %d", deleted)
	}
}

// TestDeleteActionExecutions_MetadataAlsoCleaned verifies metadata is deleted alongside executions.
func TestDeleteActionExecutions_MetadataAlsoCleaned(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	a, tweetID := seedActionAndTweet(t, s)
	e := saveExecution(t, s, a.ID, tweetID)

	if err := s.SaveActionExecutionMetadata(ctx, e.ID, "key1", `"value1"`); err != nil {
		t.Fatalf("SaveActionExecutionMetadata: %v", err)
	}

	meta, _ := s.ListActionExecutionMetadata(ctx, e.ID)
	if len(meta) != 1 {
		t.Fatalf("expected 1 metadata row before delete, got %d", len(meta))
	}

	if _, err := s.DeleteActionExecutions(ctx, []string{tweetID}, nil); err != nil {
		t.Fatalf("DeleteActionExecutions: %v", err)
	}

	meta, _ = s.ListActionExecutionMetadata(ctx, e.ID)
	if len(meta) != 0 {
		t.Errorf("expected 0 metadata rows after delete, got %d", len(meta))
	}
}
