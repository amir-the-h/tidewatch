# Clear Executions Design

## Overview

Allow users to delete action execution history (and associated metadata) for one or many tweets, scoped to all actions or a selected subset. This lets users remove stale or bad pipeline outputs and start fresh without deleting the tweets themselves.

## Scope

- Tweet detail drawer: clear all executions or per-action executions for one tweet
- Tweets table: bulk-clear executions across many selected tweets, with optional action filtering

Clearing is a standalone operation — no automatic reprocessing is triggered.

## Backend

### New API Endpoint

```
DELETE /api/executions
Authorization: admin
Body: {
  tweet_ids:  string[]   // required, 1+
  action_ids: int64[]    // optional; empty = all actions
}
Response: { deleted: int }
```

- Deletes `action_execution_metadata` rows first (FK dependency), then `action_executions` rows
- Scoped to `tweet_id IN (tweet_ids)` and, when provided, `action_id IN (action_ids)`
- Runs in a single transaction; returns count of deleted execution rows

### New Store Method

```go
// DeleteActionExecutions deletes execution records (and their metadata) for the
// given tweets. If actionIDs is empty, all actions are cleared.
func (s *Store) DeleteActionExecutions(ctx context.Context, tweetIDs []string, actionIDs []int64) (int64, error)
```

Implementation:
1. Build parameterized `tweet_id IN (?)` clause; add `AND action_id IN (?)` when `actionIDs` non-empty
2. `DELETE FROM action_execution_metadata WHERE execution_id IN (SELECT id FROM action_executions WHERE <filter>)`
3. `DELETE FROM action_executions WHERE <filter>`
4. Both steps in a transaction; return rows affected from step 3

### Frontend API helper

```ts
// web/src/api/tweets.ts (or a new executions.ts)
deleteExecutions(tweetIds: string[], actionIds?: number[]): Promise<{ deleted: number }>
// DELETE /api/executions
```

## Frontend: Tweet Detail Drawer

Location: "Pipeline Runs" section of the tweet detail drawer in `TweetsView.vue`.

### "Clear All" button

- Placed in the Pipeline Runs section header, alongside the section title
- Disabled when there are no executions
- Popconfirm: "Clear all execution history for this tweet?"
- On confirm: calls `deleteExecutions([tweetId])`, shows success count, reloads tweet detail

### Per-action "Clear" button

- Placed in each action group header (next to the action name and execution count)
- Popconfirm: "Clear execution history for [Action Name]?"
- On confirm: calls `deleteExecutions([tweetId], [actionId])`, reloads tweet detail

Both buttons are admin-only (hidden for viewer role).

## Frontend: Tweets Table Bulk Action

Location: bulk action bar that appears when rows are selected in `TweetsView.vue`.

### "Clear Executions" dropdown button

Added alongside the existing Reprocess / Delete / Chat / Delete Noise buttons.

**Dropdown options:**

1. **All Actions**
   - Popconfirm: "Clear all execution history for {N} selected tweets?"
   - On confirm: calls `deleteExecutions(selectedIds)`, shows `{deleted} executions cleared`, clears row selection

2. **Select Actions...**
   - Opens a modal with a checklist of all actions (fetched from `GET /api/actions`)
   - User selects one or more actions
   - Confirm button: "Clear Executions"
   - Popconfirm inside modal or on confirm click: "Clear executions for {M} action(s) across {N} tweets?"
   - On confirm: calls `deleteExecutions(selectedIds, selectedActionIds)`, shows count message, clears row selection, closes modal

Admin-only. No tweet rows are deleted — only execution history.

## Data Flow

```
User triggers clear
  → Popconfirm
    → DELETE /api/executions {tweet_ids, action_ids?}
      → Store.DeleteActionExecutions(tweetIDs, actionIDs)
        → DELETE action_execution_metadata (scoped)
        → DELETE action_executions (scoped)
        → return deleted count
      ← { deleted: N }
    ← success toast
  → Reload tweet detail (drawer) or clear selection (bulk)
```

## Error Handling

- If the request fails, show an error notification; do not reload or clear selection
- Empty result (`deleted: 0`) is not an error — show "0 executions cleared" toast

## Testing

- Unit test `DeleteActionExecutions` with: all actions, scoped action IDs, multiple tweet IDs, empty result
- UI: manual verification that drawer reloads after clear, bulk action clears selection on success
