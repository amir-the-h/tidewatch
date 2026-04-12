package worker

import (
	"context"
	"testing"

	"github.com/amir-the-h/iran-watcher/internal/inference"
	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/queue"
)

// graphStore is a test-local store that overrides GetPipelineGraph and
// GetPipelineIDForNode while delegating everything else to mockStore.
type graphStore struct {
	mockStore
	graphByID         map[int64]*model.PipelineGraph
	pipelineIDForNode int64
}

func (g *graphStore) GetPipelineGraph(_ context.Context, id int64) (*model.PipelineGraph, error) {
	if gr, ok := g.graphByID[id]; ok {
		return gr, nil
	}
	return nil, nil
}

func (g *graphStore) GetPipelineIDForNode(_ context.Context, _ int64) (int64, error) {
	return g.pipelineIDForNode, nil
}

// mockPipeline satisfies inference.Pipeline for tests that only need dispatch
// (never actually run the pipeline).
type mockPipeline struct{ name string }

func (m *mockPipeline) Name() string                                                      { return m.name }
func (m *mockPipeline) Type() inference.PipelineType                                      { return inference.PipelineTypeInternal }
func (m *mockPipeline) Run(_ context.Context, _ inference.PipelineInput) (map[string]any, error) {
	return nil, nil
}
func (m *mockPipeline) Close() error { return nil }

func TestPushTasksFromNode_ActionNode(t *testing.T) {
	actionID := int64(7)
	graph := &model.PipelineGraph{
		Nodes: []*model.PipelineNode{
			{ID: 1, PipelineID: 10, Type: model.PipelineNodeTypeStart},
			{ID: 2, PipelineID: 10, Type: model.PipelineNodeTypeAction, ActionID: &actionID},
			{ID: 3, PipelineID: 10, Type: model.PipelineNodeTypeAction, ActionID: &actionID},
		},
		Edges: []*model.PipelineEdge{
			{ID: 1, PipelineID: 10, SourceNodeID: 1, TargetNodeID: 2},
			{ID: 2, PipelineID: 10, SourceNodeID: 2, TargetNodeID: 3},
		},
	}

	mp := &mockProducer{}
	cache := &ActionCache{entries: make(map[int64]*CacheEntry)}
	cache.entries[actionID] = &CacheEntry{
		Action:   &model.Action{ID: actionID, Name: "test", Active: true},
		Pipeline: &mockPipeline{name: "mock"},
	}

	startNode := graph.Nodes[2] // node ID 3 — second action, not the start
	pushed := PushTasksFromNode(
		context.Background(), &mockStore{}, mp, cache,
		graph, 10, "new_tweet",
		[]byte(`{"tweet_id":"t1","text":"hello","author_handle":"u1"}`),
		"t1", startNode, nil,
	)
	if pushed != 1 {
		t.Fatalf("expected 1 pushed, got %d", pushed)
	}
	if mp.pushed[0].PipelineNodeID != 3 {
		t.Errorf("expected PipelineNodeID=3, got %d", mp.pushed[0].PipelineNodeID)
	}
	if mp.pushed[0].ActionID != 7 {
		t.Errorf("expected ActionID=7, got %d", mp.pushed[0].ActionID)
	}
}

func TestEvalRuleMatch(t *testing.T) {
	ctx := map[string]any{
		"rule_ids": []int64{3, 7, 42},
	}

	cases := []struct {
		name      string
		ruleKey   string
		wantMatch bool
		want      bool
	}{
		{"match present rule", "7", true, true},
		{"match absent rule", "99", true, false},
		{"not_match present rule", "3", false, false},
		{"not_match absent rule", "5", false, true},
		{"invalid key", "abc", true, false},
		{"invalid key not_match", "abc", false, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := evalRuleMatch(ctx, tc.ruleKey, tc.wantMatch)
			if got != tc.want {
				t.Errorf("evalRuleMatch(ctx, %q, %v) = %v, want %v", tc.ruleKey, tc.wantMatch, got, tc.want)
			}
		})
	}
}

func TestEvalRuleMatch_NilRuleIDs(t *testing.T) {
	ctx := map[string]any{} // rule_ids absent (non-tweet task)

	if evalRuleMatch(ctx, "3", true) {
		t.Error("rule_matches should return false when rule_ids absent")
	}
	if !evalRuleMatch(ctx, "3", false) {
		t.Error("rule_not_matches should return true when rule_ids absent (rule is not matched)")
	}
}

func TestEvaluateConditionsVerbose_RuleMatches(t *testing.T) {
	ctx := map[string]any{
		"rule_ids": []int64{10, 20},
	}

	passCond := []model.PipelineCondition{
		{Key: "10", Operator: "rule_matches"},
	}
	if !evaluateConditionsVerbose(passCond, ctx, 0, 0, nil) {
		t.Error("rule_matches with matched rule should pass")
	}

	failCond := []model.PipelineCondition{
		{Key: "99", Operator: "rule_matches"},
	}
	if evaluateConditionsVerbose(failCond, ctx, 0, 0, nil) {
		t.Error("rule_matches with unmatched rule should fail")
	}

	notMatchPass := []model.PipelineCondition{
		{Key: "99", Operator: "rule_not_matches"},
	}
	if !evaluateConditionsVerbose(notMatchPass, ctx, 0, 0, nil) {
		t.Error("rule_not_matches with unmatched rule should pass")
	}

	notMatchFail := []model.PipelineCondition{
		{Key: "10", Operator: "rule_not_matches"},
	}
	if evaluateConditionsVerbose(notMatchFail, ctx, 0, 0, nil) {
		t.Error("rule_not_matches with matched rule should fail")
	}
}

func TestPushInitialTasksForGraph_InitialTasksHaveDepthZero(t *testing.T) {
	actionID := int64(10)
	graph := &model.PipelineGraph{
		Nodes: []*model.PipelineNode{
			{ID: 1, PipelineID: 5, Type: model.PipelineNodeTypeStart},
			{ID: 2, PipelineID: 5, Type: model.PipelineNodeTypeAction, ActionID: &actionID},
		},
		Edges: []*model.PipelineEdge{
			{ID: 1, PipelineID: 5, SourceNodeID: 1, TargetNodeID: 2},
		},
	}
	mp := &mockProducer{}
	cache := &ActionCache{entries: make(map[int64]*CacheEntry)}
	cache.entries[actionID] = &CacheEntry{
		Action:   &model.Action{ID: actionID, Name: "test", Active: true},
		Pipeline: &mockPipeline{name: "mock"},
	}

	pushed := PushInitialTasksForGraph(
		context.Background(), &mockStore{}, mp, cache,
		graph, 5, "new_tweet",
		[]byte(`{"tweet_id":"t1","text":"hello","author_handle":"u1"}`),
		"t1", nil,
	)
	if pushed != 1 {
		t.Fatalf("expected 1 pushed, got %d", pushed)
	}
	if mp.pushed[0].Depth != 0 {
		t.Errorf("initial task Depth = %d, want 0", mp.pushed[0].Depth)
	}
}

func TestPushContinuationTasks_DepthIncremented(t *testing.T) {
	actionID := int64(20)
	graph := &model.PipelineGraph{
		Nodes: []*model.PipelineNode{
			{ID: 1, PipelineID: 7, Type: model.PipelineNodeTypeStart},
			{ID: 2, PipelineID: 7, Type: model.PipelineNodeTypeAction, ActionID: &actionID},
			{ID: 3, PipelineID: 7, Type: model.PipelineNodeTypeAction, ActionID: &actionID},
		},
		Edges: []*model.PipelineEdge{
			{ID: 1, PipelineID: 7, SourceNodeID: 1, TargetNodeID: 2},
			{ID: 2, PipelineID: 7, SourceNodeID: 2, TargetNodeID: 3},
		},
	}
	mp := &mockProducer{}
	cache := &ActionCache{entries: make(map[int64]*CacheEntry)}
	cache.entries[actionID] = &CacheEntry{
		Action:   &model.Action{ID: actionID, Name: "test", Active: true},
		Pipeline: &mockPipeline{name: "mock"},
	}
	st := &graphStore{
		graphByID:         map[int64]*model.PipelineGraph{7: graph},
		pipelineIDForNode: 7,
	}

	// Simulate a task at depth 2 completing at node 2.
	parentTask := &queue.Task{
		TriggerType:    "new_tweet",
		ActionID:       actionID,
		PipelineNodeID: 2,
		PipelineID:     7,
		Depth:          2,
		Event:          []byte(`{"tweet_id":"t1","text":"hello","author_handle":"u1"}`),
		Context:        map[string]any{},
	}

	PushContinuationTasks(context.Background(), st, mp, cache, parentTask, map[string]any{}, nil)

	if len(mp.pushed) != 1 {
		t.Fatalf("expected 1 continuation task, got %d", len(mp.pushed))
	}
	if mp.pushed[0].Depth != 3 {
		t.Errorf("continuation task Depth = %d, want 3 (parent.Depth+1)", mp.pushed[0].Depth)
	}
}

func TestDispatchInitialNode_SubPipelineDepthPlusOne(t *testing.T) {
	subActionID := int64(30)
	subPipelineID := int64(99)
	subGraph := &model.PipelineGraph{
		Nodes: []*model.PipelineNode{
			{ID: 10, PipelineID: subPipelineID, Type: model.PipelineNodeTypeStart},
			{ID: 11, PipelineID: subPipelineID, Type: model.PipelineNodeTypeAction, ActionID: &subActionID},
		},
		Edges: []*model.PipelineEdge{
			{ID: 1, PipelineID: subPipelineID, SourceNodeID: 10, TargetNodeID: 11},
		},
	}
	parentPipelineNodeID := int64(5)
	graph := &model.PipelineGraph{
		Nodes: []*model.PipelineNode{
			{ID: 1, PipelineID: 50, Type: model.PipelineNodeTypeStart},
			{ID: parentPipelineNodeID, PipelineID: 50, Type: model.PipelineNodeTypePipeline, SubPipelineID: &subPipelineID},
		},
		Edges: []*model.PipelineEdge{
			{ID: 1, PipelineID: 50, SourceNodeID: 1, TargetNodeID: parentPipelineNodeID},
		},
	}

	mp := &mockProducer{}
	cache := &ActionCache{entries: make(map[int64]*CacheEntry)}
	cache.entries[subActionID] = &CacheEntry{
		Action:   &model.Action{ID: subActionID, Name: "sub", Active: true},
		Pipeline: &mockPipeline{name: "sub"},
	}
	st := &graphStore{
		graphByID: map[int64]*model.PipelineGraph{
			50:              graph,
			subPipelineID:   subGraph,
		},
	}

	pushed := PushInitialTasksForGraph(
		context.Background(), st, mp, cache,
		graph, 50, "new_tweet",
		[]byte(`{"tweet_id":"t2","text":"test","author_handle":"u2"}`),
		"t2", nil,
	)
	if pushed != 1 {
		t.Fatalf("expected 1 pushed, got %d", pushed)
	}
	if mp.pushed[0].Depth != 1 {
		t.Errorf("sub-pipeline entry task Depth = %d, want 1 (depth 0 + 1)", mp.pushed[0].Depth)
	}
}
