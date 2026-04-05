package store

import (
	"context"
	"testing"
	"time"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

func TestMarkPipelineNodeLeafComplete(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// Create an action, a pipeline, and a pipeline node to act as parent
	a := &model.Action{Name: "test-act", Type: model.ActionTypeInternal, Config: []byte(`{}`), Active: true, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := s.SaveAction(ctx, a); err != nil {
		t.Fatalf("SaveAction: %v", err)
	}

	p := &model.Pipeline{Name: "test-pipe", Active: true, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := s.SavePipeline(ctx, p); err != nil {
		t.Fatalf("SavePipeline: %v", err)
	}

	// Save a minimal graph with a start node (parent) and two action leaf nodes
	graph, err := s.SavePipelineGraph(ctx, p.ID, []*model.PipelineNodeRequest{
		{ClientID: "s", Type: model.PipelineNodeTypeStart, Label: "start"},
		{ClientID: "a1", Type: model.PipelineNodeTypeAction, ActionID: &a.ID, Label: "leaf1"},
		{ClientID: "a2", Type: model.PipelineNodeTypeAction, ActionID: &a.ID, Label: "leaf2"},
	}, []*model.PipelineEdgeRequest{
		{SourceClientID: "s", SourceHandle: "default", TargetClientID: "a1"},
		{SourceClientID: "s", SourceHandle: "default", TargetClientID: "a2"},
	})
	if err != nil {
		t.Fatalf("SavePipelineGraph: %v", err)
	}

	parentNodeID := graph.Nodes[0].ID // start node acts as parent
	leaf1ID := graph.Nodes[1].ID
	leaf2ID := graph.Nodes[2].ID
	tweetID := "tweet-fan-in"

	// Insert execution tracking rows
	if err := s.InsertPipelineNodeExecution(ctx, parentNodeID, tweetID, leaf1ID); err != nil {
		t.Fatalf("InsertPipelineNodeExecution leaf1: %v", err)
	}
	if err := s.InsertPipelineNodeExecution(ctx, parentNodeID, tweetID, leaf2ID); err != nil {
		t.Fatalf("InsertPipelineNodeExecution leaf2: %v", err)
	}

	// Complete leaf1 — not all done yet
	allDone, err := s.MarkPipelineNodeLeafComplete(ctx, parentNodeID, tweetID, leaf1ID)
	if err != nil {
		t.Fatalf("MarkPipelineNodeLeafComplete leaf1: %v", err)
	}
	if allDone {
		t.Error("expected allDone=false after completing only 1 of 2 leaves")
	}

	// Complete leaf2 — now all done
	allDone, err = s.MarkPipelineNodeLeafComplete(ctx, parentNodeID, tweetID, leaf2ID)
	if err != nil {
		t.Fatalf("MarkPipelineNodeLeafComplete leaf2: %v", err)
	}
	if !allDone {
		t.Error("expected allDone=true after completing both leaves")
	}
}

// helpers shared by action tests

func seedActionAndTweet(t *testing.T, s Store) (action *model.Action, tweetID string) {
	t.Helper()
	ctx := context.Background()

	tw := sampleTweet()
	if err := s.SaveTweet(ctx, tw); err != nil {
		t.Fatalf("SaveTweet: %v", err)
	}

	a := sampleAction()
	a.Name = "test-action-seed"
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

	// Create a second action
	a2 := sampleAction()
	a2.Name = "action-2"
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

// ── BulkDeleteActions ────────────────────────────────────────────────────────

func TestBulkDeleteActions_All(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	a1 := sampleAction()
	a1.Name = "action-1"
	if err := s.SaveAction(ctx, a1); err != nil {
		t.Fatalf("SaveAction a1: %v", err)
	}
	a2 := sampleAction()
	a2.Name = "action-2"
	if err := s.SaveAction(ctx, a2); err != nil {
		t.Fatalf("SaveAction a2: %v", err)
	}

	n, err := s.BulkDeleteActions(ctx, BulkDeleteActionsFilter{})
	if err != nil {
		t.Fatalf("BulkDeleteActions: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 deleted, got %d", n)
	}
	actions, _ := s.ListActions(ctx, ActionFilter{})
	if len(actions) != 0 {
		t.Errorf("expected 0 actions remaining, got %d", len(actions))
	}
}

func TestBulkDeleteActions_ByIDs(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	a1 := sampleAction()
	a1.Name = "action-1"
	if err := s.SaveAction(ctx, a1); err != nil {
		t.Fatalf("SaveAction a1: %v", err)
	}
	a2 := sampleAction()
	a2.Name = "action-2"
	if err := s.SaveAction(ctx, a2); err != nil {
		t.Fatalf("SaveAction a2: %v", err)
	}

	n, err := s.BulkDeleteActions(ctx, BulkDeleteActionsFilter{IDs: []int64{a1.ID}})
	if err != nil {
		t.Fatalf("BulkDeleteActions: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	actions, _ := s.ListActions(ctx, ActionFilter{})
	if len(actions) != 1 || actions[0].ID != a2.ID {
		t.Errorf("expected only a2 remaining")
	}
}

func TestBulkDeleteActions_ByType(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	a1 := sampleAction() // type = model.ActionTypeInternal
	a1.Name = "internal-action"
	if err := s.SaveAction(ctx, a1); err != nil {
		t.Fatalf("SaveAction a1: %v", err)
	}
	a2 := sampleAction()
	a2.Name = "onnx-action"
	a2.Type = model.ActionTypeONNX
	a2.Config = []byte(`{"model":"test"}`)
	if err := s.SaveAction(ctx, a2); err != nil {
		t.Fatalf("SaveAction a2: %v", err)
	}

	n, err := s.BulkDeleteActions(ctx, BulkDeleteActionsFilter{Type: model.ActionTypeInternal})
	if err != nil {
		t.Fatalf("BulkDeleteActions: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 deleted, got %d", n)
	}
	actions, _ := s.ListActions(ctx, ActionFilter{})
	if len(actions) != 1 || actions[0].ID != a2.ID {
		t.Errorf("expected only ONNX action remaining")
	}
}
