# UI Navigation Redesign

**Date:** 2026-04-11  
**Status:** Approved

## Overview

Consolidate the 13-item admin sidebar into 11 items by merging related pages into tabs. Promote Actions and Pipelines to distinct top-level pages. Fix the Executions tab to show human-readable names. Expose all API-supported filters on every page and tab.

## Navigation Changes

### Before (13 items)
Map, Tweets, Search, Rules, Sources, Pipelines (multi-tab), Ollama Backends, Notifications, Tracker, Digest, Chat, Queue, Users

### After (11 items)
Map, Tweets, Sources, Actions, Pipelines, Notifications, Tracker, Digest, Chat, Queue, Users

Removed from top-level nav: Search (→ Tweets tab), Rules (→ Sources tab), Ollama Backends (→ Actions tab).

## Page & Tab Specifications

### Tweets page — `/tweets`

Two tabs: **Tweets** (default) and **Search**.

**Tweets tab**
- Content: unchanged from current `TweetsView.vue`
- Filters (all already present): Rule (select), From (date picker), To (date picker)

**Search tab**
- Content: moved from `SearchView.vue`
- Filters: Rule IDs (multi-select), Start time (date picker), End time (date picker), Since tweet ID (text), Until tweet ID (text), Max results (number), Limit (number of jobs to display)
- Implementation: `SearchTab.vue` component; no separate route needed

---

### Sources page — `/sources`

Two tabs: **Sources** (default) and **Rules**.

**Sources tab**
- Content: unchanged from current `SourcesView.vue`
- Filters (all client-side, already present): Text search (name/value), Type (select), Active only (checkbox)

**Rules tab**
- Content: moved from `RulesView.vue`
- Filters (all client-side, already present): Text search (name/compiled), Local only (checkbox), Active only (checkbox)
- Implementation: `RulesTab.vue` component; no separate route needed

---

### Actions page — `/actions` (new top-level page)

New `ActionsView.vue`. Two tabs: **Actions** (default) and **Ollama Backends**.

**Actions tab**
- Content: the Actions tab from current `PipelinesView.vue`
- Filters:
  - Text search — client-side, already present
  - Active only (toggle) — client-side, already present
  - **Type** (select: onnx / ollama / internal / notification) — **new filter**, API supports `?type=` but it is currently missing from the UI; implemented client-side to match existing pattern

**Ollama Backends tab**
- Content: moved from `OllamaView.vue`
- No API filters; keep as-is

---

### Pipelines page — `/pipelines` (own page, restructured)

`PipelinesView.vue` loses the Actions tab and gains an Executions tab. Three tabs: **Pipelines** (default), **Triggers**, **Executions**.

**Pipelines tab**
- Content: unchanged from current Pipelines tab in `PipelinesView.vue`
- No API filters; keep as-is

**Triggers tab**
- Content: unchanged from current Triggers tab in `PipelinesView.vue`
- No API filters; keep as-is

**Executions tab**
- Content: moved from current Executions tab in `PipelinesView.vue`
- **Column fixes** (current vs. new):
  - `action_id` (number) → **Action name** (resolved client-side from loaded actions list by `action_id`)
  - *(missing)* → **Pipeline name** (requires backend: `GET /api/executions` response to include `pipeline_name` joined via `pipeline_run_tracking`; show "—" if null)
- **Filter additions**:
  - **Action** (select, populated from loaded actions list) — **new**, maps to `action_id` query param (requires backend: add `action_id` filter to `GET /api/executions`)
  - From / To — replace plain ISO text inputs with date pickers
  - Tweet ID — unchanged
  - Status (All / Success / Error) — unchanged

---

## Backend Changes Required

Two small additions to `GET /api/executions`:

1. **`action_id` filter param** — filter executions by a specific action; add to the store handler and SQL query (`WHERE action_id = ?` when provided).

2. **`pipeline_name` in response** — join `pipeline_run_tracking` → `pipelines` to resolve the pipeline name for each execution row; add `pipeline_name string` (nullable) to the `ActionExecution` response struct. When an execution has no associated pipeline run (e.g., manual reprocess), return `null` / empty string.

---

## Route Changes

| Old route | New route | Note |
|-----------|-----------|------|
| `/search` | removed | Content lives at `/tweets?tab=search` |
| `/rules` | removed | Content lives at `/sources?tab=rules` |
| `/ollama` | removed | Content lives at `/actions?tab=ollama` |
| `/pipelines?tab=actions` | `/actions` | Actions is now its own page |
| `/pipelines?tab=pipelines` | `/pipelines` | Default tab |
| `/pipelines?tab=executions` | `/pipelines?tab=executions` | Moved to Pipelines page |
| `/pipelines/:id/build` | `/pipelines/:id/build` | Unchanged |

No backwards-compatibility redirects.

---

## Component Architecture

Each tab is a self-contained Vue component rather than inlined markup inside the parent view. This keeps view files small and focused on tab orchestration only.

The parent view (`TweetsView.vue`, `SourcesView.vue`, etc.) owns only:
- The `<n-tabs>` shell and tab routing (query-param `?tab=`)
- Shared state that must flow between tabs (if any)

All logic, refs, API calls, and template markup for a tab live in the tab component.

### Tab component naming convention

`web/src/components/tabs/<TabName>Tab.vue`

Examples: `TweetsTab.vue`, `SearchTab.vue`, `SourcesTab.vue`, `RulesTab.vue`, `ActionsTab.vue`, `OllamaTab.vue`, `PipelinesTab.vue`, `TriggersTab.vue`, `ExecutionsTab.vue`

---

## Files Affected

### New view files
- `web/src/views/ActionsView.vue` — tab shell only; imports ActionsTab + OllamaTab

### New tab components (`web/src/components/tabs/`)
- `TweetsTab.vue` — extracted from TweetsView (tweets list, filters, bulk actions)
- `SearchTab.vue` — extracted from SearchView (search job form + job list)
- `SourcesTab.vue` — extracted from SourcesView (sources list, filters, CRUD)
- `RulesTab.vue` — extracted from RulesView (rules list, filters, CRUD)
- `ActionsTab.vue` — extracted from PipelinesView Actions tab (actions list, filters including new Type filter, CRUD drawer)
- `OllamaTab.vue` — extracted from OllamaView (backends list, CRUD)
- `PipelinesTab.vue` — extracted from PipelinesView Pipelines tab (pipelines list, CRUD)
- `TriggersTab.vue` — extracted from PipelinesView Triggers tab (trigger links list, CRUD)
- `ExecutionsTab.vue` — extracted from PipelinesView Executions tab (fixed columns, improved filters)

### Modified view files
- `web/src/views/TweetsView.vue` — replace body with `<n-tabs>` shell importing TweetsTab + SearchTab
- `web/src/views/SourcesView.vue` — replace body with `<n-tabs>` shell importing SourcesTab + RulesTab
- `web/src/views/PipelinesView.vue` — replace body with `<n-tabs>` shell importing PipelinesTab + TriggersTab + ExecutionsTab; remove all Actions tab code
- `web/src/router/index.ts` — add `/actions` route (ActionsView); remove `/search`, `/rules`, `/ollama` routes; remove all legacy redirect routes
- `web/src/App.vue` — remove Search, Rules, Ollama Backends from sidebar; add Actions entry

### Backend files
- `internal/store/` — add `action_id` filter and `pipeline_name` join to `ListExecutions` query
- `internal/api/` — pass `action_id` query param through to store; add `pipeline_name` field to `ActionExecution` response

### Deleted files
- `web/src/views/SearchView.vue` — replaced by SearchTab.vue
- `web/src/views/RulesView.vue` — replaced by RulesTab.vue
- `web/src/views/OllamaView.vue` — replaced by OllamaTab.vue
