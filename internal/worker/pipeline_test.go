package worker

import (
	"testing"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

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
