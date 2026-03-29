# Clear Executions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the ability to delete action execution history (and metadata) for one or many tweets, scoped to all actions or a selected subset — from the tweet detail drawer and as a bulk action in the tweets table.

**Architecture:** Single `DELETE /api/executions` endpoint accepts `{tweet_ids, action_ids?}` and calls a new `DeleteActionExecutions` store method that deletes metadata then executions in a transaction. The frontend adds a "Clear All" + per-action "Clear" buttons to the detail drawer, and a "Clear Executions" dropdown to the bulk action bar (with "All Actions" and "Select Actions..." options).

**Tech Stack:** Go (net/http, chi), SQLite (modernc.org/sqlite), Vue 3 + Naive UI, TypeScript

---

## File Map

| File | Change |
|------|--------|
| `internal/store/store.go` | Add `DeleteActionExecutions` to Store interface |
| `internal/store/sqlite_actions.go` | Implement `DeleteActionExecutions` |
| `internal/store/sqlite_actions_test.go` | **Create** — unit tests for `DeleteActionExecutions` |
| `internal/api/handlers_tweets.go` | Add `handleDeleteExecutions` handler |
| `internal/api/server.go` | Register `DELETE /api/executions` route |
| `web/src/api/tweets.ts` | Add `deleteExecutions` API function |
| `web/src/views/TweetsView.vue` | Add clear buttons in drawer + dropdown in bulk action bar |

---

## Task 1: Store interface + implementation

**Files:**
- Modify: `internal/store/store.go` (ActionExecutions section, ~line 239-252)
- Modify: `internal/store/sqlite_actions.go` (after `ListActionExecutionMetadata`, ~line 272)

- [ ] **Step 1: Add method to Store interface**

In `internal/store/store.go`, in the `// ActionExecutions` section, add after `SaveActionExecutionMetadata`:

```go
// DeleteActionExecutions deletes all execution records and their metadata for
// the given tweets. If actionIDs is non-empty, only executions for those actions
// are deleted. Returns the count of deleted execution rows.
DeleteActionExecutions(ctx context.Context, tweetIDs []string, actionIDs []int64) (int64, error)
```

- [ ] **Step 2: Implement in sqlite_actions.go**

In `internal/store/sqlite_actions.go`, add after `SaveActionExecutionMetadata` (before `ListActionExecutionMetadata`):

```go
func (s *sqliteStore) DeleteActionExecutions(ctx context.Context, tweetIDs []string, actionIDs []int64) (int64, error) {
	if len(tweetIDs) == 0 {
		return 0, nil
	}

	// Build tweet_id IN (?) placeholders
	tweetPlaceholders := strings.Repeat("?,", len(tweetIDs))
	tweetPlaceholders = tweetPlaceholders[:len(tweetPlaceholders)-1]

	args := make([]any, 0, len(tweetIDs)+len(actionIDs))
	for _, id := range tweetIDs {
		args = append(args, id)
	}

	filter := fmt.Sprintf("tweet_id IN (%s)", tweetPlaceholders)
	if len(actionIDs) > 0 {
		actionPlaceholders := strings.Repeat("?,", len(actionIDs))
		actionPlaceholders = actionPlaceholders[:len(actionPlaceholders)-1]
		filter += fmt.Sprintf(" AND action_id IN (%s)", actionPlaceholders)
		for _, id := range actionIDs {
			args = append(args, id)
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("store: delete action executions: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Delete metadata first (FK constraint: metadata → executions)
	if _, err := tx.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM action_execution_metadata
		             WHERE execution_id IN (SELECT id FROM action_executions WHERE %s)`, filter),
		args...); err != nil {
		return 0, fmt.Errorf("store: delete action executions: delete metadata: %w", err)
	}

	res, err := tx.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM action_executions WHERE %s`, filter),
		args...)
	if err != nil {
		return 0, fmt.Errorf("store: delete action executions: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("store: delete action executions: commit: %w", err)
	}

	n, _ := res.RowsAffected()
	return n, nil
}
```

- [ ] **Step 3: Verify it compiles**

```bash
cd /mnt/shared/projects/personal/iran-watcher
go build ./internal/store/...
```

Expected: no output (success).

---

## Task 2: Store tests

**Files:**
- Create: `internal/store/sqlite_actions_test.go`

- [ ] **Step 1: Create the test file**

```go
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
		Name:        "test-action",
		TriggerType: model.TriggerTypeNewTweet,
		PipelineID:  p.ID,
		Active:      true,
		RuleIDs:     []int64{},
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
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
		Name: "action-2", TriggerType: model.TriggerTypeNewTweet,
		PipelineID: p2.ID, Active: true, RuleIDs: []int64{},
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
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
```

- [ ] **Step 2: Run the tests — expect FAIL (method not on interface yet if run before Task 1, or PASS if Task 1 is done)**

```bash
cd /mnt/shared/projects/personal/iran-watcher
go test ./internal/store/... -run TestDeleteActionExecutions -v
```

Expected after Task 1 is complete: all 5 tests PASS.

- [ ] **Step 3: Commit**

```bash
cd /mnt/shared/projects/personal/iran-watcher
git add internal/store/store.go internal/store/sqlite_actions.go internal/store/sqlite_actions_test.go
git commit -m "feat: add DeleteActionExecutions store method"
```

---

## Task 3: API handler + route registration

**Files:**
- Modify: `internal/api/handlers_tweets.go` (add handler at end of file)
- Modify: `internal/api/server.go` (register route in admin group, after existing execution routes ~line 302)

- [ ] **Step 1: Add handler to handlers_tweets.go**

Add at the end of `internal/api/handlers_tweets.go`:

```go
func (s *Server) handleDeleteExecutions(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TweetIDs  []string `json:"tweet_ids"`
		ActionIDs []int64  `json:"action_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.TweetIDs) == 0 {
		writeError(w, http.StatusBadRequest, "tweet_ids is required")
		return
	}
	n, err := s.store.DeleteActionExecutions(r.Context(), req.TweetIDs, req.ActionIDs)
	if err != nil {
		s.logger.Error("api: delete executions", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete executions")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"deleted": n})
}
```

- [ ] **Step 2: Register route in server.go**

In `internal/api/server.go`, in the admin group, after the existing execution log routes (~line 302):

```go
// Action execution log
r.Get("/actions/{id}/executions", srv.handleListActionExecutions)
r.Get("/executions", srv.handleListAllExecutions)
r.Get("/executions/{id}/metadata", srv.handleGetExecutionMetadata)
r.Delete("/executions", srv.handleDeleteExecutions)   // ← add this line
```

- [ ] **Step 3: Verify it compiles**

```bash
cd /mnt/shared/projects/personal/iran-watcher
go build ./internal/api/...
```

Expected: no output (success).

- [ ] **Step 4: Commit**

```bash
cd /mnt/shared/projects/personal/iran-watcher
git add internal/api/handlers_tweets.go internal/api/server.go
git commit -m "feat: add DELETE /api/executions endpoint"
```

---

## Task 4: Frontend API helper

**Files:**
- Modify: `web/src/api/tweets.ts` (add function at end)

- [ ] **Step 1: Add deleteExecutions to tweets.ts**

At the end of `web/src/api/tweets.ts`, add:

```ts
export const deleteExecutions = (tweetIds: string[], actionIds?: number[]) =>
  apiFetch<{ deleted: number }>('/api/executions', {
    method: 'DELETE',
    body: JSON.stringify({ tweet_ids: tweetIds, action_ids: actionIds ?? [] }),
  })
```

- [ ] **Step 2: Verify TypeScript compiles**

```bash
cd /mnt/shared/projects/personal/iran-watcher/web
npm run build 2>&1 | tail -20
```

Expected: build succeeds (exit 0).

- [ ] **Step 3: Commit**

```bash
cd /mnt/shared/projects/personal/iran-watcher
git add web/src/api/tweets.ts
git commit -m "feat: add deleteExecutions API helper"
```

---

## Task 5: Tweet detail drawer — clear buttons

**Files:**
- Modify: `web/src/views/TweetsView.vue`

This task adds:
1. A reactive `clearingExecutions` ref for loading state
2. `clearAllExecutions(tweetId)` function — clears all actions for one tweet
3. `clearActionExecutions(tweetId, actionId)` function — clears one action for one tweet
4. "Clear All" button in the Pipeline Runs section header (with popconfirm)
5. Per-action "Clear" button in each action group header (with popconfirm)

- [ ] **Step 1: Import deleteExecutions and add reactive state**

In the `<script setup>` section, update the import from `../api/tweets`:

```ts
import { listTweets, getTweet, reprocessTweets, deleteTweets, deleteNoiseTweets, deleteExecutions } from '../api/tweets'
```

Wait — `TweetsView.vue` uses the store, not the API directly. Check how it calls store methods. Looking at the view: it calls `store.remove()`, `store.reprocess()`, etc. The `deleteExecutions` call should go directly through the API (there's no need to add it to the tweets store — it doesn't affect the tweets list state). So import it directly.

Add to the `<script setup>` imports block (alongside the existing imports):

```ts
import { deleteExecutions } from '../api/tweets'
```

Then add near the other `ref` declarations (after `deletingNoise`):

```ts
const clearingExecutions = ref(false)
```

- [ ] **Step 2: Add clearAllExecutions and clearActionExecutions functions**

Add after the `reprocessOne` function (~line 170):

```ts
async function clearAllExecutions(tweetId: string) {
  clearingExecutions.value = true
  try {
    const { deleted } = await deleteExecutions([tweetId])
    msg.success(`Cleared ${deleted} execution${deleted !== 1 ? 's' : ''}`)
    detail.value = await store.getTweet(tweetId)
  } catch (e: any) {
    msg.error(e.message)
  } finally {
    clearingExecutions.value = false
  }
}

async function clearActionExecutions(tweetId: string, actionId: number) {
  clearingExecutions.value = true
  try {
    const { deleted } = await deleteExecutions([tweetId], [actionId])
    msg.success(`Cleared ${deleted} execution${deleted !== 1 ? 's' : ''}`)
    detail.value = await store.getTweet(tweetId)
  } catch (e: any) {
    msg.error(e.message)
  } finally {
    clearingExecutions.value = false
  }
}
```

- [ ] **Step 3: Add "Clear All" button to Pipeline Runs section header**

In the `<template>`, find the Pipeline Runs heading and add a "Clear All" button next to it. Replace:

```html
          <!-- Pipeline Executions — grouped by action -->
          <template v-if="detail.actions?.length">
            <n-h3 style="margin-top: 16px">Pipeline Runs</n-h3>
```

With:

```html
          <!-- Pipeline Executions — grouped by action -->
          <template v-if="detail.actions?.length">
            <div style="display: flex; align-items: center; gap: 8px; margin-top: 16px">
              <n-h3 style="margin: 0">Pipeline Runs</n-h3>
              <n-popconfirm @positive-click="clearAllExecutions(detail.tweet.id)">
                <template #trigger>
                  <n-button size="tiny" type="error" secondary :loading="clearingExecutions">
                    Clear All
                  </n-button>
                </template>
                Clear all execution history for this tweet?
              </n-popconfirm>
            </div>
```

- [ ] **Step 4: Add per-action "Clear" button to each action group header**

Find the action group header in the template and add a clear button. Replace:

```html
              <!-- Action header -->
              <div class="action-group-header">
                <span class="action-group-name">{{ group.action_name || `action #${group.action_id}` }}</span>
                <span class="action-group-count">{{ group.executions.length }} run{{ group.executions.length !== 1 ? 's' : '' }}</span>
              </div>
```

With:

```html
              <!-- Action header -->
              <div class="action-group-header">
                <span class="action-group-name">{{ group.action_name || `action #${group.action_id}` }}</span>
                <span class="action-group-count">{{ group.executions.length }} run{{ group.executions.length !== 1 ? 's' : '' }}</span>
                <n-popconfirm @positive-click="clearActionExecutions(detail.tweet.id, group.action_id)" style="margin-left: auto">
                  <template #trigger>
                    <n-button size="tiny" type="error" secondary :loading="clearingExecutions">
                      Clear
                    </n-button>
                  </template>
                  Clear execution history for {{ group.action_name || `action #${group.action_id}` }}?
                </n-popconfirm>
              </div>
```

Also add `flex: 1` to the action-group-header CSS so the margin-left: auto on the button works:

In `<style scoped>`, update `.action-group-header`:

```css
.action-group-header {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 10px;
  background: var(--n-table-header-color);
  border-bottom: 1px solid var(--n-border-color);
  flex: 1;
}
```

- [ ] **Step 5: Verify build**

```bash
cd /mnt/shared/projects/personal/iran-watcher/web
npm run build 2>&1 | tail -20
```

Expected: build succeeds.

- [ ] **Step 6: Commit**

```bash
cd /mnt/shared/projects/personal/iran-watcher
git add web/src/views/TweetsView.vue
git commit -m "feat: add clear execution buttons to tweet detail drawer"
```

---

## Task 6: Tweets table bulk action — Clear Executions dropdown

**Files:**
- Modify: `web/src/views/TweetsView.vue`

This task adds a "Clear Executions" dropdown to the bulk action bar with:
- "All Actions" option (direct popconfirm)
- "Select Actions..." option (opens a modal with action checklist)

- [ ] **Step 1: Add imports and reactive state for bulk clear**

In `<script setup>`, add the `listActions` import from the actions API. Add at top of script near other imports:

```ts
import { listActions } from '../api/actions'
import type { Action } from '../api/actions'
```

Add reactive state near the other refs:

```ts
const clearingBulk          = ref(false)
const showSelectActionsModal = ref(false)
const allActions             = ref<Action[]>([])
const selectedActionIds      = ref<number[]>([])
const loadingActions         = ref(false)
```

- [ ] **Step 2: Add bulkClearAll and bulkClearSelected functions**

Add after `clearActionExecutions`:

```ts
async function bulkClearAll() {
  const ids = checkedRowKeys.value as string[]
  clearingBulk.value = true
  try {
    const { deleted } = await deleteExecutions(ids)
    checkedRowKeys.value = []
    msg.success(`Cleared ${deleted} execution${deleted !== 1 ? 's' : ''}`)
  } catch (e: any) {
    msg.error(e.message)
  } finally {
    clearingBulk.value = false
  }
}

async function openSelectActionsModal() {
  showSelectActionsModal.value = true
  loadingActions.value = true
  try {
    allActions.value = await listActions()
  } catch (e: any) {
    msg.error('Failed to load actions: ' + e.message)
  } finally {
    loadingActions.value = false
  }
}

async function bulkClearSelected() {
  if (!selectedActionIds.value.length) {
    msg.warning('Select at least one action')
    return
  }
  const ids = checkedRowKeys.value as string[]
  clearingBulk.value = true
  try {
    const { deleted } = await deleteExecutions(ids, selectedActionIds.value)
    checkedRowKeys.value = []
    selectedActionIds.value = []
    showSelectActionsModal.value = false
    msg.success(`Cleared ${deleted} execution${deleted !== 1 ? 's' : ''}`)
  } catch (e: any) {
    msg.error(e.message)
  } finally {
    clearingBulk.value = false
  }
}
```

- [ ] **Step 3: Add "Clear Executions" dropdown to bulk action bar**

In `<template>`, find the bulk action toolbar `<n-space v-if="hasSelection" ...>` and add the dropdown after the Chat button:

```html
      <n-dropdown
        trigger="click"
        :options="[
          { label: 'All Actions', key: 'all' },
          { label: 'Select Actions…', key: 'select' },
        ]"
        @select="(key: string) => key === 'all' ? null : openSelectActionsModal()"
      >
        <n-tooltip trigger="hover">
          <template #trigger>
            <n-button size="small" :loading="clearingBulk">
              <template #icon><n-icon><TrashBinOutline /></n-icon></template>
            </n-button>
          </template>
          Clear executions
        </n-tooltip>
      </n-dropdown>
```

Wait — the "All Actions" option needs a popconfirm. Naive UI's `n-dropdown` doesn't support nested popconfirm natively on items. Use a different approach: show an inline `n-popconfirm` that wraps only the "All Actions" trigger, and handle "Select Actions…" separately.

Replace the above with two separate buttons in a group:

```html
      <n-button-group size="small">
        <n-popconfirm @positive-click="bulkClearAll">
          <template #trigger>
            <n-tooltip trigger="hover">
              <template #trigger>
                <n-button size="small" :loading="clearingBulk">
                  <template #icon><n-icon><TrashBinOutline /></n-icon></template>
                  Clear All
                </n-button>
              </template>
              Clear all executions for selected tweets
            </n-tooltip>
          </template>
          Clear all execution history for {{ checkedRowKeys.length }} tweet{{ checkedRowKeys.length !== 1 ? 's' : '' }}?
        </n-popconfirm>
        <n-tooltip trigger="hover">
          <template #trigger>
            <n-button size="small" @click="openSelectActionsModal">
              Select Actions…
            </n-button>
          </template>
          Clear executions for specific actions
        </n-tooltip>
      </n-button-group>
```

- [ ] **Step 4: Add "Select Actions" modal**

Before the closing `</n-space>` tag of the main `<n-space vertical>` wrapper, add the modal:

```html
    <!-- Select Actions modal for bulk clear -->
    <n-modal v-model:show="showSelectActionsModal" preset="card" title="Clear Executions — Select Actions" style="max-width: 480px">
      <n-spin v-if="loadingActions" />
      <template v-else>
        <p style="margin: 0 0 12px; font-size: 13px; color: var(--n-text-color-2)">
          Select which actions to clear for {{ checkedRowKeys.length }} selected tweet{{ checkedRowKeys.length !== 1 ? 's' : '' }}.
        </p>
        <n-checkbox-group v-model:value="selectedActionIds">
          <n-space vertical size="small">
            <n-checkbox
              v-for="action in allActions"
              :key="action.id"
              :value="action.id"
              :label="action.name"
            />
          </n-space>
        </n-checkbox-group>
      </template>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showSelectActionsModal = false; selectedActionIds = []">Cancel</n-button>
          <n-popconfirm @positive-click="bulkClearSelected">
            <template #trigger>
              <n-button type="error" :disabled="!selectedActionIds.length" :loading="clearingBulk">
                Clear Executions
              </n-button>
            </template>
            Clear executions for {{ selectedActionIds.length }} action{{ selectedActionIds.length !== 1 ? 's' : '' }} across {{ checkedRowKeys.length }} tweet{{ checkedRowKeys.length !== 1 ? 's' : '' }}?
          </n-popconfirm>
        </n-space>
      </template>
    </n-modal>
```

- [ ] **Step 5: Add NCheckbox, NCheckboxGroup, NButtonGroup, NModal, NDropdown to template imports**

Check whether the components are already imported via auto-import (Naive UI is typically registered globally in this project). Search for how components are registered:

```bash
grep -r "NCheckbox\|NModal\|NButtonGroup" /mnt/shared/projects/personal/iran-watcher/web/src/main.ts 2>/dev/null || grep -r "naive-ui" /mnt/shared/projects/personal/iran-watcher/web/src/main.ts
```

If Naive UI is globally registered (likely), no per-component import is needed and these components work as-is. If not, add them to the existing `import { ..., NCheckbox, NCheckboxGroup, NButtonGroup, NModal } from 'naive-ui'` import in the script.

- [ ] **Step 6: Verify build**

```bash
cd /mnt/shared/projects/personal/iran-watcher/web
npm run build 2>&1 | tail -20
```

Expected: build succeeds.

- [ ] **Step 7: Commit**

```bash
cd /mnt/shared/projects/personal/iran-watcher
git add web/src/views/TweetsView.vue
git commit -m "feat: add bulk clear executions to tweets table"
```

---

## Task 7: Docker build verification

- [ ] **Step 1: Build the container**

```bash
cd /mnt/shared/projects/personal/iran-watcher
docker compose build --build-arg BUILDKIT_INLINE_CACHE=1 iran-watcher 2>&1 | tail -30
```

Expected: `Successfully built` / `✓ iran-watcher` with exit 0.

- [ ] **Step 2: Run all Go tests**

```bash
cd /mnt/shared/projects/personal/iran-watcher
go test ./internal/store/... -v -run TestDeleteActionExecutions 2>&1
```

Expected: 5 tests PASS.

---

## Self-review notes

- `DeleteActionExecutions` with empty `tweetIDs` returns early (safe no-op)
- Metadata deleted before executions (FK constraint order)
- Both operations in a single transaction (atomic)
- The `args` slice is built correctly: tweet IDs first, then action IDs — the same order as the SQL placeholders
- Modal resets `selectedActionIds` on cancel
- `clearingExecutions` is shared between "clear all" and per-action clear — only one can run at a time, which is fine since they both affect the same tweet
- `bulkClearAll` and `bulkClearSelected` both clear `checkedRowKeys` on success (no tweet rows deleted)
