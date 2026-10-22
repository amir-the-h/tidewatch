package worker

import (
	"context"
	"testing"

	"github.com/amir-the-h/tidewatch/internal/inference"
	"github.com/amir-the-h/tidewatch/internal/model"
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

func (m *mockPipeline) Name() string                 { return m.name }
func (m *mockPipeline) Type() inference.PipelineType { return inference.PipelineTypeInternal }
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
		graph, 10, "new_signal",
		[]byte(`{"signal_id":1,"kind":"post"}`),
		1, startNode, nil,
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
		"x_rule_ids": []int64{3, 7, 42},
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
	ctx := map[string]any{} // x_rule_ids absent (non-X signal)

	if evalRuleMatch(ctx, "3", true) {
		t.Error("rule_matches should return false when x_rule_ids absent")
	}
	if !evalRuleMatch(ctx, "3", false) {
		t.Error("rule_not_matches should return true when x_rule_ids absent (rule is not matched)")
	}
}

func TestEvaluateConditionsVerbose_RuleMatches(t *testing.T) {
	ctx := map[string]any{
		"x_rule_ids": []any{float64(10), float64(20)}, // JSON round-trip shape
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
