# Rule-Match Condition Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `rule_matches` and `rule_not_matches` condition operators so pipeline condition nodes can branch based on which X stream rule matched the incoming tweet.

**Architecture:** Inject `rule_ids []int64` from the TweetEvent into `mergedCtx` in the worker manager, then special-case the two new operators in `evaluateConditionsVerbose` — they read the rule ID from `c.Key` (repurposed field) and check membership against `ctx["rule_ids"]`. The frontend replaces the free-text key input with a rule dropdown when either operator is selected.

**Tech Stack:** Go (internal/worker), Vue 3 + Naive UI (web/src/views/PipelineBuilderView.vue), existing `GET /api/rules` endpoint, `go test ./internal/worker/...`

---

## File Map

| File | Change |
|------|--------|
| `internal/worker/manager.go` | Inject `rule_ids` into mergedCtx after well-known fields |
| `internal/worker/pipeline.go` | Intercept `rule_matches`/`rule_not_matches` in `evaluateConditionsVerbose` |
| `internal/worker/pipeline_test.go` | New file — unit tests for the two operators |
| `web/src/views/PipelineBuilderView.vue` | New operators in dropdown; rule selector when active; hide value field |

---

## Task 1: Inject `rule_ids` into mergedCtx

**Files:**
- Modify: `internal/worker/manager.go`

- [ ] **Step 1: Add the injection**

In `manager.go`, the well-known fields block currently ends with:

```go
mergedCtx["tweet_url"] = "https://x.com/" + ev.AuthorHandle + "/status/" + ev.TweetID
```

Add one line immediately after it, before the `enrichContext` call:

```go
mergedCtx["tweet_url"] = "https://x.com/" + ev.AuthorHandle + "/status/" + ev.TweetID
mergedCtx["rule_ids"] = ev.RuleIDs // []int64 — used by rule_matches condition operator
```

`ev.RuleIDs` is `[]int64` and may be nil for non-stream tasks (digest, etc.). The evaluator handles nil gracefully.

- [ ] **Step 2: Verify it compiles**

```bash
docker compose build --build-arg BUILDKIT_INLINE_CACHE=1 iran-watcher 2>&1 | tail -5
```

Expected: `Image iran-watcher:local Built`

- [ ] **Step 3: Commit**

```bash
git add internal/worker/manager.go
git commit -m "feat: inject rule_ids into mergedCtx for condition evaluation"
```

---

## Task 2: New operators in `evaluateConditionsVerbose`

**Files:**
- Modify: `internal/worker/pipeline.go`

The current loop in `evaluateConditionsVerbose` (around line 614) does a ctx key lookup first, then calls `evaluateCondition`. The new operators bypass that lookup entirely — `c.Key` holds the rule ID, not a context key.

- [ ] **Step 1: Add the operator intercept**

In `pipeline.go`, locate `evaluateConditionsVerbose`. Inside the `for _, c := range conditions` loop, add the intercept **before** `val, ok := ctx[c.Key]`:

```go
for _, c := range conditions {
    // rule_matches / rule_not_matches: c.Key is the rule ID (not a ctx key).
    op := strings.ToLower(c.Operator)
    if op == "rule_matches" || op == "rule_not_matches" {
        result := evalRuleMatch(ctx, c.Key, op == "rule_matches")
        if logger != nil {
            logger.Info("pipeline: rule condition evaluated",
                "pipeline_id", pipelineID, "node_id", nodeID,
                "rule_id", c.Key, "op", c.Operator, "passed", result,
            )
        }
        if !result {
            return false
        }
        continue
    }

    val, ok := ctx[c.Key]
    // ... rest of existing loop unchanged
```

- [ ] **Step 2: Add the `evalRuleMatch` helper**

Add this function in `pipeline.go` immediately before or after `evaluateCondition`:

```go
// evalRuleMatch checks whether the rule ID stored in ruleKey (as a decimal string)
// is present in ctx["rule_ids"] ([]int64). If wantMatch is false it negates the result.
// Returns false when rule_ids is absent or ruleKey cannot be parsed.
func evalRuleMatch(ctx map[string]any, ruleKey string, wantMatch bool) bool {
    targetID, err := strconv.ParseInt(ruleKey, 10, 64)
    if err != nil {
        return false
    }
    ids, _ := ctx["rule_ids"].([]int64)
    for _, id := range ids {
        if id == targetID {
            return wantMatch
        }
    }
    return !wantMatch
}
```

(`strconv` is already imported in pipeline.go.)

- [ ] **Step 3: Verify it compiles**

```bash
docker compose build --build-arg BUILDKIT_INLINE_CACHE=1 iran-watcher 2>&1 | tail -5
```

Expected: `Image iran-watcher:local Built`

- [ ] **Step 4: Commit**

```bash
git add internal/worker/pipeline.go
git commit -m "feat: add rule_matches and rule_not_matches condition operators"
```

---

## Task 3: Unit tests for the new operators

**Files:**
- Create: `internal/worker/pipeline_test.go`

Tests are in `package worker` (same package as the code), so `evaluateConditionsVerbose` and `evalRuleMatch` are directly accessible.

- [ ] **Step 1: Create the test file**

```go
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
```

- [ ] **Step 2: Run tests to verify they pass**

```bash
go test ./internal/worker/... -run TestEvalRuleMatch -v
```

Expected output:
```
--- PASS: TestEvalRuleMatch (0.00s)
    --- PASS: TestEvalRuleMatch/match_present_rule
    --- PASS: TestEvalRuleMatch/match_absent_rule
    --- PASS: TestEvalRuleMatch/not_match_present_rule
    --- PASS: TestEvalRuleMatch/not_match_absent_rule
    --- PASS: TestEvalRuleMatch/invalid_key
    --- PASS: TestEvalRuleMatch/invalid_key_not_match
--- PASS: TestEvalRuleMatch_NilRuleIDs (0.00s)
--- PASS: TestEvaluateConditionsVerbose_RuleMatches (0.00s)
PASS
```

- [ ] **Step 3: Commit**

```bash
git add internal/worker/pipeline_test.go
git commit -m "test: add unit tests for rule_matches and rule_not_matches operators"
```

---

## Task 4: Frontend — rule selector in condition editor

**Files:**
- Modify: `web/src/views/PipelineBuilderView.vue`

- [ ] **Step 1: Import `listRules` and `Rule` type**

Near the top of the `<script setup>` imports (around line 26 where other API imports are), add:

```typescript
import { listRules, type Rule } from '../api/rules'
```

- [ ] **Step 2: Add `availableRules` ref and define the rule operator set**

After the existing `const conditionOpOptions = [...]` block (around line 82), add:

```typescript
const ruleOps = new Set(['rule_matches', 'rule_not_matches'])
const availableRules = ref<Rule[]>([])
const ruleOptions = computed(() =>
  availableRules.value.map(r => ({ label: r.name, value: String(r.id) }))
)
```

- [ ] **Step 3: Add the two new operators to `conditionOpOptions`**

```typescript
const conditionOpOptions = [
  { label: '==', value: '==' }, { label: '!=', value: '!=' },
  { label: '>', value: '>' }, { label: '>=', value: '>=' },
  { label: '<', value: '<' }, { label: '<=', value: '<=' },
  { label: 'contains', value: 'contains' }, { label: 'not_contains', value: 'not_contains' },
  { label: 'is_empty', value: 'is_empty' }, { label: 'not_empty', value: 'not_empty' },
  { label: 'rule_matches', value: 'rule_matches' },
  { label: 'rule_not_matches', value: 'rule_not_matches' },
]
```

- [ ] **Step 4: Fetch rules when the edit panel opens**

In the `onNodeClick` function (around line 197), add a rules fetch when the panel switches to edit mode. After `panel.value = 'edit'`, add:

```typescript
if (availableRules.value.length === 0) {
  listRules().then(r => { availableRules.value = r })
}
```

- [ ] **Step 5: Replace key input with rule selector for rule operators**

In the condition editor template (around line 478), replace:

```html
<n-input v-model:value="cond.key" size="small" placeholder="key" style="margin-bottom: 3px;" />
```

with:

```html
<n-select
  v-if="ruleOps.has(cond.op)"
  v-model:value="cond.key"
  :options="ruleOptions"
  size="small"
  placeholder="Select rule…"
  style="margin-bottom: 3px;"
/>
<n-input
  v-else
  v-model:value="cond.key"
  size="small"
  placeholder="key"
  style="margin-bottom: 3px;"
/>
```

- [ ] **Step 6: Hide value field for rule operators**

The value field currently shows for any op that isn't `is_empty`/`not_empty`. Extend both the `NInputNumber` and `NInput` guards:

```html
<n-input-number
  v-if="numericOps.has(cond.op)"
  ...
/>
<n-input
  v-else-if="!['is_empty','not_empty','rule_matches','rule_not_matches'].includes(cond.op)"
  v-model:value="cond.value"
  size="small" placeholder="value" style="margin-top: 3px;"
/>
```

- [ ] **Step 7: Build the UI container to verify no errors**

```bash
docker compose build --build-arg BUILDKIT_INLINE_CACHE=1 iran-watcher-ui 2>&1 | tail -5
```

Expected: `Image iran-watcher-ui:local Built`

- [ ] **Step 8: Commit**

```bash
git add web/src/views/PipelineBuilderView.vue
git commit -m "feat: rule_matches/rule_not_matches condition UI — rule selector in pipeline builder"
```

---

## Task 5: Final build and push

- [ ] **Step 1: Build both images**

```bash
docker compose build --build-arg BUILDKIT_INLINE_CACHE=1 2>&1 | tail -5
```

Expected: both `iran-watcher:local` and `iran-watcher-ui:local` built successfully.

- [ ] **Step 2: Run all Go tests**

```bash
go test ./internal/worker/... -v 2>&1 | tail -20
```

Expected: all tests pass.

- [ ] **Step 3: Push**

```bash
git push
```
