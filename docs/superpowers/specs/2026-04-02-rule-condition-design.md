# Rule-Match Condition Design

**Date:** 2026-04-02  
**Status:** Approved

## Problem

Pipeline condition nodes evaluate `(key, operator, value)` triples against `mergedCtx`, a string-keyed map built from prior action outputs and well-known tweet fields. There is no way to branch based on which X stream rule matched the incoming tweet. Users who want different processing paths per rule must maintain separate pipelines.

## Goal

Add a `rule_matches` / `rule_not_matches` operator that lets a condition node branch on whether a tweet was matched by a specific stream rule. No schema changes required.

---

## Backend

### 1. Inject `rule_ids` into `mergedCtx` (`internal/worker/manager.go`)

After the existing well-known field injections (`tweet_id`, `tweet`, etc.), inject:

```go
mergedCtx["rule_ids"] = ev.RuleIDs   // []int64
```

`ev.RuleIDs` is already populated by both the stream listener (multi-rule matches) and the search backfill client (single-rule). Tasks that are not tweet-events (digest, etc.) won't have this key; the new operator gracefully returns `false` when it is absent.

### 2. New operators in condition evaluator (`internal/worker/pipeline.go`)

In `evaluateConditionsVerbose`, before the normal `ctx[c.Key]` lookup, intercept `rule_matches` and `rule_not_matches`:

```
operator == "rule_matches"     →  true if rule ID in ev.RuleIDs
operator == "rule_not_matches" →  true if rule ID NOT in ev.RuleIDs
```

`c.Key` is repurposed to hold the target rule ID as a decimal string (e.g. `"42"`). No `c.Value` is used. If `rule_ids` is absent from ctx or `c.Key` cannot be parsed as int64, the operator returns `false`.

Stored condition wire format (unchanged struct, repurposed fields):

```json
{ "key": "42", "op": "rule_matches" }
```

`evaluateCondition` is not changed — the intercept happens one level up in `evaluateConditionsVerbose` before the key-lookup block.

---

## Frontend

### `PipelineBuilderView.vue`

**Operator dropdown** — add two new entries to `conditionOpOptions`:

```typescript
{ label: 'rule_matches',     value: 'rule_matches'     },
{ label: 'rule_not_matches', value: 'rule_not_matches' },
```

**Key field** — when operator is `rule_matches` or `rule_not_matches`, replace the free-text key `NInput` with an `NSelect` populated from `GET /api/rules`. The selected value is the rule ID cast to string, stored in `cond.key`. Rules are fetched once when the edit panel opens and cached in a local ref `availableRules`.

**Value field** — hidden for `rule_matches` and `rule_not_matches` (same treatment as `is_empty` / `not_empty`).

**Loading existing conditions** — when opening the edit panel, if a condition already has `op == "rule_matches"` or `op == "rule_not_matches"`, the rule selector pre-selects by matching `cond.key` against rule IDs from the fetched list.

---

## Data Flow Summary

```
Stream listener / search backfill
  → TweetEvent { RuleIDs: [3, 7] }
  → manager.go injects mergedCtx["rule_ids"] = []int64{3, 7}
  → condition node evaluates { key: "3", op: "rule_matches" }
  → evaluateConditionsVerbose intercepts rule_matches
  → parses key "3" → 3
  → checks 3 ∈ [3, 7] → true → pass branch executes
```

---

## Error Handling

| Situation | Behaviour |
|-----------|-----------|
| `rule_ids` absent from ctx (non-tweet task) | returns `false` |
| `c.Key` not a valid int64 | returns `false` |
| Rule ID not in `rule_ids` list | returns `false` (normal fail) |
| Rules API unavailable in UI | selector shows empty list; operator can still be saved if key already set |
