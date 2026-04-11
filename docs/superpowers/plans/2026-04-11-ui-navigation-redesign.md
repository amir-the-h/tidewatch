# UI Navigation Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Consolidate 13 nav items into 11 by merging Search→Tweets, Rules→Sources, Ollama→Actions; promote Actions and Pipelines to separate top-level pages; extract each tab into its own component; fix Executions to show action name + pipeline name.

**Architecture:** Each page view becomes a thin `<n-tabs>` shell that imports focused tab components from `web/src/components/tabs/`. All logic, refs, and API calls live inside each tab component. Backend adds `pipeline_name` to execution responses and exposes the existing `action_id` filter via the API.

**Tech Stack:** Vue 3 + Naive UI + Pinia stores; Go backend with SQLite (correlated subquery for pipeline_name join).

---

## Task 1: Backend — add `pipeline_name` to executions + expose `action_id` filter

**Files:**
- Modify: `internal/model/action.go`
- Modify: `internal/store/sqlite_actions.go`
- Modify: `internal/api/handlers_actions.go`
- Modify: `web/src/api/actions.ts`

- [ ] **Step 1: Add `PipelineName` to the `ActionExecution` model**

In `internal/model/action.go`, change the `ActionExecution` struct:

```go
// ActionExecution is one log entry per action fired on a tweet.
type ActionExecution struct {
	ID           int64             `json:"id"`
	ActionID     int64             `json:"action_id"`
	TweetID      string            `json:"tweet_id"`
	Status       string            `json:"status"`
	Error        string            `json:"error,omitempty"`
	ExecutedAt   time.Time         `json:"executed_at"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	PipelineName string            `json:"pipeline_name,omitempty"`
}
```

- [ ] **Step 2: Update `ListActionExecutions` query to join pipeline name**

In `internal/store/sqlite_actions.go`, replace the `q` string definition in `ListActionExecutions` (currently line 251):

```go
	q := `SELECT ae.id, ae.action_id, ae.tweet_id, ae.status, COALESCE(ae.error,''), ae.executed_at,
	       COALESCE((
	         SELECT p.name
	         FROM pipeline_nodes pn
	         JOIN pipeline_run_tracking prt ON prt.pipeline_id = pn.pipeline_id AND prt.tweet_id = ae.tweet_id
	         JOIN pipelines p ON p.id = pn.pipeline_id
	         WHERE pn.action_id = ae.action_id
	         LIMIT 1
	       ), '') AS pipeline_name
	FROM action_executions ae`
```

Also update the `rows.Scan` call (currently line 275) to scan the new column:

```go
		if err := rows.Scan(&e.ID, &e.ActionID, &e.TweetID, &e.Status, &e.Error, &executedAt, &e.PipelineName); err != nil {
			return nil, fmt.Errorf("store: list action executions: scan: %w", err)
		}
```

- [ ] **Step 3: Expose `action_id` filter in the API handler**

In `internal/api/handlers_actions.go`, inside `handleListAllExecutions` (currently around line 251), add after the existing `tweet_id` check:

```go
	if v := r.URL.Query().Get("action_id"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			filter.ActionID = &n
		}
	}
```

- [ ] **Step 4: Update the frontend `ActionExecution` type and `listAllExecutions`**

In `web/src/api/actions.ts`, add `pipeline_name` to the interface and `action_id` to the filter:

```ts
export interface ActionExecution {
  id: number
  action_id: number
  tweet_id: string
  status: 'success' | 'error'
  error?: string
  executed_at: string
  metadata?: Record<string, string>
  pipeline_name?: string   // NEW
}

export interface ExecutionFilter {
  page?: number
  per_page?: number
  from?: string
  to?: string
  tweet_id?: string
  status?: string
  action_id?: number   // NEW
}
```

In the `listAllExecutions` function body, add `action_id` to the query params object (follow the existing pattern — look for where `tweet_id` is set and add below it):

```ts
  if (filter.action_id) p.action_id = String(filter.action_id)
```

- [ ] **Step 5: Commit**

```bash
git add internal/model/action.go internal/store/sqlite_actions.go internal/api/handlers_actions.go web/src/api/actions.ts
git commit -m "feat(executions): expose action_id filter; join pipeline_name in response"
```

---

## Task 2: Create `TweetsTab.vue`

**Files:**
- Create: `web/src/components/tabs/TweetsTab.vue`

Move all content from `web/src/views/TweetsView.vue` verbatim into the new component. The only change is removing `<n-h2>Tweets</n-h2>` from the template (the parent view's `PageHeader` will handle the page title).

- [ ] **Step 1: Create `web/src/components/tabs/TweetsTab.vue`**

Copy the entire `<script setup>`, `<template>`, and `<style scoped>` from `TweetsView.vue` into `TweetsTab.vue`. Remove the `<n-h2>Tweets</n-h2>` line from the template top. The rest of the content is identical.

The component's template root becomes `<n-space vertical size="large">` containing filters, bulk toolbar, data table, detail drawer, and modal — all unchanged.

- [ ] **Step 2: Commit**

```bash
git add web/src/components/tabs/TweetsTab.vue
git commit -m "feat(tabs): add TweetsTab component"
```

---

## Task 3: Create `SearchTab.vue`

**Files:**
- Create: `web/src/components/tabs/SearchTab.vue`

Move all content from `web/src/views/SearchView.vue` into the new component.

- [ ] **Step 1: Create `web/src/components/tabs/SearchTab.vue`**

Copy the entire `<script setup>` and `<template>` from `SearchView.vue` verbatim into `SearchTab.vue`. Remove the `<n-h2>Search</n-h2>` (or equivalent heading) from the template top if present.

- [ ] **Step 2: Commit**

```bash
git add web/src/components/tabs/SearchTab.vue
git commit -m "feat(tabs): add SearchTab component"
```

---

## Task 4: Refactor `TweetsView.vue` into a tab shell

**Files:**
- Modify: `web/src/views/TweetsView.vue`

Replace the entire file content with a thin tab shell that imports `TweetsTab` and `SearchTab`.

- [ ] **Step 1: Replace `TweetsView.vue` with the tab shell**

```vue
<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NTabs, NTabPane } from 'naive-ui'
import PageHeader from '../components/PageHeader.vue'
import TweetsTab from '../components/tabs/TweetsTab.vue'
import SearchTab from '../components/tabs/SearchTab.vue'

const route  = useRoute()
const router = useRouter()

const activeTab = ref((route.query.tab as string) || 'tweets')
function switchTab(tab: string) {
  activeTab.value = tab
  router.replace({ query: { tab } })
}

// Keep activeTab in sync if URL changes externally
watch(() => route.query.tab, (tab) => {
  if (tab && tab !== activeTab.value) activeTab.value = tab as string
})
</script>

<template>
  <PageHeader title="Tweets" />
  <n-tabs :value="activeTab" type="line" animated style="margin-top: -8px;" @update:value="switchTab">
    <n-tab-pane name="tweets" tab="Tweets">
      <TweetsTab />
    </n-tab-pane>
    <n-tab-pane name="search" tab="Search">
      <SearchTab />
    </n-tab-pane>
  </n-tabs>
</template>
```

- [ ] **Step 2: Commit**

```bash
git add web/src/views/TweetsView.vue
git commit -m "refactor(tweets): convert TweetsView to tab shell with TweetsTab + SearchTab"
```

---

## Task 5: Create `SourcesTab.vue`

**Files:**
- Create: `web/src/components/tabs/SourcesTab.vue`

Move all content from `web/src/views/SourcesView.vue` into the new component.

- [ ] **Step 1: Create `web/src/components/tabs/SourcesTab.vue`**

Copy the entire `<script setup>` and `<template>` from `SourcesView.vue` into `SourcesTab.vue`. Remove the `<n-h2>Sources</n-h2>` heading from the template top.

- [ ] **Step 2: Commit**

```bash
git add web/src/components/tabs/SourcesTab.vue
git commit -m "feat(tabs): add SourcesTab component"
```

---

## Task 6: Create `RulesTab.vue`

**Files:**
- Create: `web/src/components/tabs/RulesTab.vue`

Move all content from `web/src/views/RulesView.vue` into the new component.

- [ ] **Step 1: Create `web/src/components/tabs/RulesTab.vue`**

Copy the entire `<script setup>` and `<template>` from `RulesView.vue` into `RulesTab.vue`. Remove the `<n-h2>Rules</n-h2>` heading.

- [ ] **Step 2: Commit**

```bash
git add web/src/components/tabs/RulesTab.vue
git commit -m "feat(tabs): add RulesTab component"
```

---

## Task 7: Refactor `SourcesView.vue` into a tab shell

**Files:**
- Modify: `web/src/views/SourcesView.vue`

- [ ] **Step 1: Replace `SourcesView.vue` with the tab shell**

```vue
<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NTabs, NTabPane } from 'naive-ui'
import PageHeader from '../components/PageHeader.vue'
import SourcesTab from '../components/tabs/SourcesTab.vue'
import RulesTab from '../components/tabs/RulesTab.vue'

const route  = useRoute()
const router = useRouter()

const activeTab = ref((route.query.tab as string) || 'sources')
function switchTab(tab: string) {
  activeTab.value = tab
  router.replace({ query: { tab } })
}

watch(() => route.query.tab, (tab) => {
  if (tab && tab !== activeTab.value) activeTab.value = tab as string
})
</script>

<template>
  <PageHeader title="Sources" />
  <n-tabs :value="activeTab" type="line" animated style="margin-top: -8px;" @update:value="switchTab">
    <n-tab-pane name="sources" tab="Sources">
      <SourcesTab />
    </n-tab-pane>
    <n-tab-pane name="rules" tab="Rules">
      <RulesTab />
    </n-tab-pane>
  </n-tabs>
</template>
```

- [ ] **Step 2: Commit**

```bash
git add web/src/views/SourcesView.vue
git commit -m "refactor(sources): convert SourcesView to tab shell with SourcesTab + RulesTab"
```

---

## Task 8: Create `ActionsTab.vue`

**Files:**
- Create: `web/src/components/tabs/ActionsTab.vue`

Extract all Actions tab logic from `PipelinesView.vue`. Adds a new **Type** filter (onnx/ollama/internal/notification) that was missing from the UI despite the API supporting it.

- [ ] **Step 1: Create `web/src/components/tabs/ActionsTab.vue`**

The `<script setup>` takes everything from `PipelinesView.vue` that belongs to the Actions tab:
- All `import` statements currently in PipelinesView that are used by the Actions tab
- All `ref`/`computed`/`function` declarations from the `// ── Actions tab ──` section (lines 51–567)
- The `onMounted` logic that belongs to actions: `fetchActions()`, `actionsStore.fetchTwitterStatus()`, `handlers`, `models`, `channels`, `providers`
- The `watch(formUsecase, ...)` watcher

Add the new **Type filter** alongside the existing filters:

```ts
const filterType = ref<string | null>(null)

const filteredActions = computed(() => {
  let list = actionsData.value
  const q = filterText.value.trim().toLowerCase()
  if (q) list = list.filter(a => a.name.toLowerCase().includes(q))
  if (filterActiveOnly.value) list = list.filter(a => a.active)
  if (filterType.value) list = list.filter(a => a.type === filterType.value)
  return list
})
```

In the template, add the Type select next to the existing filters in the toolbar:

```html
<n-select
  v-model:value="filterType"
  :options="[
    { label: 'All types', value: null },
    { label: 'ONNX',         value: 'onnx'         },
    { label: 'Ollama',       value: 'ollama'        },
    { label: 'Internal',     value: 'internal'      },
    { label: 'Notification', value: 'notification'  },
  ]"
  style="width: 140px"
  clearable
  placeholder="All types"
/>
```

The template contains:
- Twitter connection card
- Filter toolbar (text search + active toggle + **new type select** + New Action button)
- `<n-data-table>` for the actions list
- `<FormDrawer>` for Create/Edit action (the large drawer from lines 1018–1322)
- `<ActionBackendsModal>` (lines 1324–1331)

- [ ] **Step 2: Commit**

```bash
git add web/src/components/tabs/ActionsTab.vue
git commit -m "feat(tabs): add ActionsTab component with new Type filter"
```

---

## Task 9: Create `OllamaTab.vue`

**Files:**
- Create: `web/src/components/tabs/OllamaTab.vue`

Move all content from `web/src/views/OllamaView.vue` into the new component.

- [ ] **Step 1: Create `web/src/components/tabs/OllamaTab.vue`**

Copy the entire `<script setup>` and `<template>` from `OllamaView.vue` into `OllamaTab.vue`. Remove the `<PageHeader>` and any top-level heading — the parent view provides the page title.

- [ ] **Step 2: Commit**

```bash
git add web/src/components/tabs/OllamaTab.vue
git commit -m "feat(tabs): add OllamaTab component"
```

---

## Task 10: Create `ActionsView.vue`

**Files:**
- Create: `web/src/views/ActionsView.vue`

New top-level page for `/actions`. Tab shell with Actions and Ollama Backends.

- [ ] **Step 1: Create `web/src/views/ActionsView.vue`**

```vue
<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NTabs, NTabPane } from 'naive-ui'
import PageHeader from '../components/PageHeader.vue'
import ActionsTab from '../components/tabs/ActionsTab.vue'
import OllamaTab from '../components/tabs/OllamaTab.vue'

const route  = useRoute()
const router = useRouter()

const activeTab = ref((route.query.tab as string) || 'actions')
function switchTab(tab: string) {
  activeTab.value = tab
  router.replace({ query: { tab } })
}

watch(() => route.query.tab, (tab) => {
  if (tab && tab !== activeTab.value) activeTab.value = tab as string
})
</script>

<template>
  <PageHeader title="Actions" />
  <n-tabs :value="activeTab" type="line" animated style="margin-top: -8px;" @update:value="switchTab">
    <n-tab-pane name="actions" tab="Actions">
      <ActionsTab />
    </n-tab-pane>
    <n-tab-pane name="ollama" tab="Ollama Backends">
      <OllamaTab />
    </n-tab-pane>
  </n-tabs>
</template>
```

- [ ] **Step 2: Commit**

```bash
git add web/src/views/ActionsView.vue
git commit -m "feat: add ActionsView page with Actions + Ollama Backends tabs"
```

---

## Task 11: Create `PipelinesTab.vue`

**Files:**
- Create: `web/src/components/tabs/PipelinesTab.vue`

Extract the Pipelines list section from `PipelinesView.vue` (the pipeline CRUD, run modal, pipeline table — NOT the triggers section).

- [ ] **Step 1: Create `web/src/components/tabs/PipelinesTab.vue`**

The `<script setup>` contains:
- Imports: `listPipelines`, `createPipeline`, `deletePipeline`, `activatePipeline`, `deactivatePipeline`, `runPipeline`, `getPipelineGraph`, relevant types from `../api/pipelines`
- All `ref`/`computed`/`function` from the `// ── Pipelines tab ──` section (lines 569–708), **excluding** the triggers/links section (lines 710–784)
- `useMessage`, `useRouter`
- `onMounted(() => { fetchPipelinesData() })`

The template contains:
- New Pipeline button
- `<n-data-table>` for pipelines (using `pipelineColumns`, `pipelinesData`)
- `<FormDrawer>` for New Pipeline (lines 1333–1341)
- Run Pipeline `<n-modal>` (lines 1343–1386)

- [ ] **Step 2: Commit**

```bash
git add web/src/components/tabs/PipelinesTab.vue
git commit -m "feat(tabs): add PipelinesTab component"
```

---

## Task 12: Create `TriggersTab.vue`

**Files:**
- Create: `web/src/components/tabs/TriggersTab.vue`

Extract the trigger-pipeline links section from `PipelinesView.vue`. Needs its own pipeline list to resolve names.

- [ ] **Step 1: Create `web/src/components/tabs/TriggersTab.vue`**

The `<script setup>` contains:
- Imports: `listTriggers`, `listTriggerPipelineLinks`, `createTriggerPipelineLink`, `deleteTriggerPipelineLink`, `activateTriggerPipelineLink`, `deactivateTriggerPipelineLink`, types from `../api/trigger_links`; also `listPipelines`, type `Pipeline` from `../api/pipelines`
- All `ref`/`computed`/`function` from the triggers section (lines 710–784)
- A local `pipelinesData = ref<Pipeline[]>([])` for name resolution
- `onMounted` that fetches both triggers/links and pipelines:

```ts
onMounted(async () => {
  const [t, l, pipelines] = await Promise.all([
    listTriggers(),
    listTriggerPipelineLinks(),
    listPipelines(),
  ])
  triggers.value = t ?? []
  links.value    = l ?? []
  pipelinesData.value = pipelines ?? []
})
```

The template contains the trigger cards section (lines 927–983 of the current pipelines tab pane, excluding the pipeline CRUD part at the top of that pane) and the Connect Pipeline drawer (lines 1388–1415).

- [ ] **Step 2: Commit**

```bash
git add web/src/components/tabs/TriggersTab.vue
git commit -m "feat(tabs): add TriggersTab component"
```

---

## Task 13: Create `ExecutionsTab.vue`

**Files:**
- Create: `web/src/components/tabs/ExecutionsTab.vue`

Rewrite of the executions tab with: action name column (resolved client-side), pipeline name column (from API), action filter dropdown, proper date pickers for from/to.

- [ ] **Step 1: Create `web/src/components/tabs/ExecutionsTab.vue`**

```vue
<script setup lang="ts">
import { ref, computed, h, onMounted } from 'vue'
import {
  NFlex, NDataTable, NSelect, NInput, NButton, NTag, NPagination, NDatePicker,
  useMessage, type DataTableColumns,
} from 'naive-ui'
import { listAllExecutions, listActions, type ActionExecution, type Action } from '../../api/actions'

const msg = useMessage()

// ── Data ─────────────────────────────────────────────────────────────────────

const executions   = ref<ActionExecution[]>([])
const execsLoading = ref(false)
const execsPage    = ref(1)

const actions      = ref<Action[]>([])

// ── Filters ───────────────────────────────────────────────────────────────────

const execFilterActionId = ref<number | null>(null)
const execFilterTweet    = ref('')
const execFilterFrom     = ref<number | null>(null)
const execFilterTo       = ref<number | null>(null)
const execFilterStatus   = ref('')

const actionOptions = computed(() =>
  actions.value.map(a => ({ label: a.name, value: a.id }))
)

// ── Helpers ───────────────────────────────────────────────────────────────────

function actionName(actionId: number): string {
  return actions.value.find(a => a.id === actionId)?.name ?? `#${actionId}`
}

// ── Load ──────────────────────────────────────────────────────────────────────

async function loadExecutions() {
  execsLoading.value = true
  try {
    executions.value = await listAllExecutions({
      page:      execsPage.value,
      per_page:  50,
      action_id: execFilterActionId.value ?? undefined,
      tweet_id:  execFilterTweet.value    || undefined,
      from:      execFilterFrom.value ? new Date(execFilterFrom.value).toISOString() : undefined,
      to:        execFilterTo.value   ? new Date(execFilterTo.value).toISOString()   : undefined,
      status:    execFilterStatus.value   || undefined,
    })
  } catch (e: any) { msg.error(e.message) }
  finally { execsLoading.value = false }
}

function onSearch() { execsPage.value = 1; loadExecutions() }
function onExecsPageChange(page: number) { execsPage.value = page; loadExecutions() }

onMounted(async () => {
  try { actions.value = await listActions() } catch {}
  loadExecutions()
})

// ── Table columns ─────────────────────────────────────────────────────────────

const execColumns: DataTableColumns<ActionExecution> = [
  { title: 'ID',            key: 'id',            width: 60 },
  {
    title: 'Action', key: 'action_name', ellipsis: { tooltip: true },
    render: (row) => actionName(row.action_id),
  },
  {
    title: 'Pipeline', key: 'pipeline_name', ellipsis: { tooltip: true },
    render: (row) => row.pipeline_name || '—',
  },
  { title: 'Tweet ID', key: 'tweet_id', width: 200, ellipsis: { tooltip: true } },
  {
    title: 'Status', key: 'status', width: 90,
    render: (row) => h(NTag, { size: 'small', type: row.status === 'success' ? 'success' : 'error' },
      { default: () => row.status }),
  },
  { title: 'Error', key: 'error', ellipsis: { tooltip: true } },
  {
    title: 'Executed', key: 'executed_at', width: 160,
    render: (row) => new Date(row.executed_at).toLocaleString(),
  },
]
</script>

<template>
  <n-flex style="margin-bottom: 12px;" :size="12" align="center" wrap>
    <n-select
      v-model:value="execFilterActionId"
      :options="actionOptions"
      placeholder="All actions"
      clearable
      filterable
      style="width: 180px"
    />
    <n-input v-model:value="execFilterTweet" placeholder="Tweet ID…" clearable style="width: 180px" />
    <n-date-picker v-model:value="execFilterFrom" type="datetime" placeholder="From" clearable style="width: 200px" />
    <n-date-picker v-model:value="execFilterTo"   type="datetime" placeholder="To"   clearable style="width: 200px" />
    <n-select
      v-model:value="execFilterStatus"
      :options="[{ label: 'All', value: '' }, { label: 'Success', value: 'success' }, { label: 'Error', value: 'error' }]"
      style="width: 120px"
    />
    <n-button size="small" type="primary" @click="onSearch">Search</n-button>
  </n-flex>

  <n-data-table
    :columns="execColumns"
    :data="executions"
    :loading="execsLoading"
    :bordered="false"
    size="small"
  />
  <n-flex justify="end" style="margin-top: 12px">
    <n-pagination
      :page="execsPage"
      :page-size="50"
      :item-count="executions.length === 50 ? execsPage * 50 + 1 : (execsPage - 1) * 50 + executions.length"
      @update:page="onExecsPageChange"
    />
  </n-flex>
</template>
```

- [ ] **Step 2: Commit**

```bash
git add web/src/components/tabs/ExecutionsTab.vue
git commit -m "feat(tabs): add ExecutionsTab with action name, pipeline name, and date picker filters"
```

---

## Task 14: Create `PipelinesTab.vue` and `TriggersTab.vue` and refactor `PipelinesView.vue`

**Files:**
- Modify: `web/src/views/PipelinesView.vue`

Replace `PipelinesView.vue` with a thin tab shell. The Actions tab content has already moved to `ActionsTab.vue` (Task 8). Import `PipelinesTab`, `TriggersTab`, `ExecutionsTab`.

- [ ] **Step 1: Replace `PipelinesView.vue` with the tab shell**

```vue
<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NTabs, NTabPane } from 'naive-ui'
import PageHeader from '../components/PageHeader.vue'
import PipelinesTab from '../components/tabs/PipelinesTab.vue'
import TriggersTab from '../components/tabs/TriggersTab.vue'
import ExecutionsTab from '../components/tabs/ExecutionsTab.vue'

const route  = useRoute()
const router = useRouter()

const activeTab = ref((route.query.tab as string) || 'pipelines')
function switchTab(tab: string) {
  activeTab.value = tab
  router.replace({ query: { tab } })
}

watch(() => route.query.tab, (tab) => {
  if (tab && tab !== activeTab.value) activeTab.value = tab as string
})
</script>

<template>
  <PageHeader title="Pipelines" />
  <n-tabs :value="activeTab" type="line" animated style="margin-top: -8px;" @update:value="switchTab">
    <n-tab-pane name="pipelines" tab="Pipelines">
      <PipelinesTab />
    </n-tab-pane>
    <n-tab-pane name="triggers" tab="Triggers">
      <TriggersTab />
    </n-tab-pane>
    <n-tab-pane name="executions" tab="Executions">
      <ExecutionsTab />
    </n-tab-pane>
  </n-tabs>
</template>
```

- [ ] **Step 2: Commit**

```bash
git add web/src/views/PipelinesView.vue
git commit -m "refactor(pipelines): convert PipelinesView to tab shell with Pipelines + Triggers + Executions tabs"
```

---

## Task 15: Update router, nav, and delete retired views

**Files:**
- Modify: `web/src/router/index.ts`
- Modify: `web/src/App.vue`
- Delete: `web/src/views/SearchView.vue`
- Delete: `web/src/views/RulesView.vue`
- Delete: `web/src/views/OllamaView.vue`

- [ ] **Step 1: Update `web/src/router/index.ts`**

Replace the routes array content with:

```ts
  routes: [
    { path: '/login',   component: () => import('../views/LoginView.vue'), meta: { public: true } },
    { path: '/',        redirect: '/map' },
    { path: '/map',     component: () => import('../views/MapView.vue') },
    { path: '/tweets',  component: () => import('../views/TweetsView.vue') },
    { path: '/sources', component: () => import('../views/SourcesView.vue') },
    { path: '/actions', component: () => import('../views/ActionsView.vue') },
    { path: '/pipelines', component: () => import('../views/PipelinesView.vue') },
    { path: '/pipelines/:id/build', component: () => import('../views/PipelineBuilderView.vue') },
    { path: '/notifications', component: () => import('../views/NotificationsView.vue') },
    { path: '/tracked-entities', redirect: '/tracker' },
    { path: '/tracker', component: () => import('../views/TrackerView.vue') },
    { path: '/queue',   component: () => import('../views/QueueView.vue') },
    { path: '/failures', redirect: '/queue' },
    { path: '/summarize', component: () => import('../views/SummarizeView.vue') },
    { path: '/chat',    component: () => import('../views/ChatView.vue') },
    { path: '/users',   component: () => import('../views/UsersView.vue') },
  ],
```

Removed routes: `/search`, `/rules`, `/ollama`, `/actions` (redirect), `/chains`, `/chains/:id/edit`, `/triggers`.

- [ ] **Step 2: Update `web/src/App.vue` sidebar nav**

Replace `adminMenuOptions` array:

```ts
const adminMenuOptions = [
  { label: 'Map',           key: '/map'           },
  { label: 'Tweets',        key: '/tweets'        },
  { label: 'Sources',       key: '/sources'       },
  { label: 'Actions',       key: '/actions'       },
  { label: 'Pipelines',     key: '/pipelines'     },
  { label: 'Notifications', key: '/notifications' },
  { label: 'Tracker',       key: '/tracker'       },
  { label: 'Digest',        key: '/summarize'     },
  { label: 'Chat',          key: '/chat'          },
  { label: 'Queue',         key: '/queue'         },
  { label: 'Users',         key: '/users'         },
]
```

- [ ] **Step 3: Delete retired view files**

```bash
git rm web/src/views/SearchView.vue web/src/views/RulesView.vue web/src/views/OllamaView.vue
```

- [ ] **Step 4: Commit**

```bash
git add web/src/router/index.ts web/src/App.vue
git commit -m "feat(nav): consolidate to 11 nav items; add /actions route; remove retired views"
```

---

## Task 16: Build verification

- [ ] **Step 1: Build the Docker image**

```bash
docker compose build --build-arg BUILDKIT_INLINE_CACHE=1 iran-watcher
```

Expected: build completes with no errors. TypeScript compilation and Vite build must pass.

- [ ] **Step 2: Start the app and smoke-test all tabs**

```bash
docker compose up -d
```

Verify in browser:
- `/tweets` shows Tweets tab (tweet list) and Search tab (search form + job list)
- `/sources` shows Sources tab and Rules tab
- `/actions` shows Actions tab (with new Type filter dropdown) and Ollama Backends tab
- `/pipelines` shows Pipelines tab, Triggers tab, and Executions tab
- Executions tab shows Action name column, Pipeline name column, date picker filters for From/To, and Action filter dropdown
- Sidebar has exactly 11 items (Map, Tweets, Sources, Actions, Pipelines, Notifications, Tracker, Digest, Chat, Queue, Users)
- Old routes `/search`, `/rules`, `/ollama` no longer exist (404)

- [ ] **Step 3: Commit if any minor fixes were needed during smoke test**

```bash
git add -p
git commit -m "fix: post-build smoke-test fixes"
```
