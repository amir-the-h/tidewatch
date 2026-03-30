# Chain Run Feature — Design Spec

**Date:** 2026-03-30
**Status:** Approved

## Summary

Move the "Run" button from individual actions to chains. Running a chain manually dispatches all historically eligible tweets through the chain's start node, replicating what the stream listener does via `PushInitialChainTasks` but scoped to one chain and driven on demand.

## Motivation

The current action-level run bypasses chain logic entirely — it re-queues tweets directly to the action, skipping all condition nodes and chain context. Chain-level run is the correct primitive: it respects the full DAG, sets `ChainID`/`ChainNodeID` on each task, and produces chain-aware execution context.

---

## Backend

### New endpoint

```
POST /api/chains/{id}/run
```

**Request body:**
```json
{ "new_only": false }
```

**Response:** `202 Accepted`
```json
{ "queued": 42 }
```

**Logic:**

1. Load the chain by ID; return 404 if not found, 422 if not active.
2. Load the chain's active `TriggerChainLinks`.
3. Load the chain graph; walk from the `start` node to collect first-level action nodes (same graph traversal as `PushInitialChainTasks`). If no action nodes are reachable, return `{ "queued": 0 }`.
4. For each active trigger link, fetch eligible tweets by trigger type:
   - `new_tweet` → all tweets (up to 5000, `ListTweets` page 1)
   - `new_categorization` → tweets that have at least one `action_execution` row (join via `action_executions`)
   - `entity_matched` → tweets that have at least one entity track row
   - `chain_only`, `digest_requested`, or any unrecognised type → skip
5. Deduplicate eligible tweet IDs across all trigger links.
6. If `new_only: true`: query `action_executions` where `action_id IN (IDs of the chain's action nodes)`; collect the resulting tweet IDs as an exclusion set; remove them from eligible tweets.
7. For each eligible tweet × each first-level action node, push a `Task`:
   - `TriggerType`: the trigger link's type
   - `TweetID`, `Text`, `AuthorHandle` from the tweet
   - `ActionID`: the action node's `action_id`
   - `ChainNodeID`: the action node's node ID
   - `ChainID`: the chain ID
   - `Context`: empty map
8. Return `{ "queued": N }`.

**Handler location:** `internal/api/handlers_chains.go` (new function `handleRunChain`).

**Route registration:** `POST /api/chains/{id}/run` alongside existing chain routes in `server.go`.

### No new store methods required

All data access uses existing methods: `GetChain`, `ListTriggerChainLinks`, `GetChainGraph`, `ListTweets`, `ListActionExecutions` (filtered by action IDs), and existing entity-track queries.

---

## Frontend

### Remove from Actions

- Remove the `PlayOutline` run button from the `actionColumns` render function in `ActionsView.vue`.
- Remove the `runningIds` ref and `onRun` function.
- Remove the `run` action from `useActionsStore` and `runAction` from `web/src/api/actions.ts`.

### Add to Chains

Both `ActionsView.vue` (Chains tab) and `ChainsView.vue` get a Run button in their chain table action column.

**Interaction flow:**
1. User clicks Run on a chain row.
2. A small modal opens with:
   - Title: `Run chain — [chain name]`
   - Radio/toggle: **All eligible tweets** (default) | **Only tweets without existing executions**
   - A "Run" confirm button and "Cancel".
3. On confirm: call `POST /api/chains/{id}/run`, show spinner on the button.
4. On success: toast `Queued N tweets for "[chain name]"`.
5. On error: toast the error message.

**New API function** (`web/src/api/chains.ts`):
```ts
export const runChain = (id: number, newOnly: boolean) =>
  apiFetch<{ queued: number }>(`/api/chains/${id}/run`, {
    method: 'POST',
    body: JSON.stringify({ new_only: newOnly }),
  })
```

**Chains store** (`web/src/stores/chains.ts`) gets:
```ts
async run(id: number, newOnly: boolean): Promise<{ queued: number }>
```

---

## CLI

New file: `cli/cmd/chain.go`

```
iran-watcher chain run <id> [--new-only]
```

**Examples:**
```bash
iran-watcher chain run 3
iran-watcher chain run 3 --new-only
```

Output: `Queued N tweets for chain #3`

The `chainCmd` root command is registered in `root.go` alongside `actionCmd`, `pipelineCmd`, etc.

---

## CLAUDE.md updates

- **Key Design Decisions**: Add note that `POST /api/actions/{id}/run` is removed; replaced by `POST /api/chains/{id}/run`.
- **Route inventory**: Add `POST /api/chains/{id}/run` entry.

---

## Out of scope

- Pagination beyond 5000 tweets (consistent with existing action run).
- Per-trigger-type tweet filtering beyond the four cases above.
- Chain execution history / status tracking at the chain level (separate concern).
