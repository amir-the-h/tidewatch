# Pipeline Backend Assignment UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add UI in PipelinesView for assigning, reordering, and removing Ollama backend assignments per pipeline.

**Architecture:** Three-file change: add API functions to `pipelines.ts`, create a self-contained `PipelineBackendsModal.vue` component, and wire an icon button + the modal into `PipelinesView.vue` for ollama-type pipeline rows only.

**Tech Stack:** Vue 3 Composition API, Naive UI, TypeScript. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-03-28-pipeline-backend-assignment-ui-design.md`

---

## File Map

| Action | File | Purpose |
|--------|------|---------|
| Modify | `web/src/api/pipelines.ts` | Add `PipelineBackend` type + 3 API functions |
| Create | `web/src/components/PipelineBackendsModal.vue` | Self-contained modal for backend assignment |
| Modify | `web/src/views/PipelinesView.vue` | Add icon button + mount modal |

---

### Task 1: Add `PipelineBackend` API type and functions

**Files:**
- Modify: `web/src/api/pipelines.ts`

- [ ] **Step 1: Add the type and functions**

Open `web/src/api/pipelines.ts` and append after the `listModels` export (before the action chains section):

```ts
export interface PipelineBackend {
  pipeline_id: number
  backend_id: number
  priority: number
}

export const listPipelineBackends = (id: number) =>
  apiFetch<PipelineBackend[]>(`/api/pipelines/${id}/backends`)

export const setPipelineBackends = (
  id: number,
  assignments: { backend_id: number; priority: number }[],
) =>
  apiFetch<PipelineBackend[]>(`/api/pipelines/${id}/backends`, {
    method: 'PUT',
    body: JSON.stringify(assignments),
  })

export const removePipelineBackend = (id: number, backendId: number) =>
  apiFetch<void>(`/api/pipelines/${id}/backends/${backendId}`, { method: 'DELETE' })
```

- [ ] **Step 2: Verify TypeScript compiles**

```bash
cd web && npx tsc --noEmit
```

Expected: no errors related to `pipelines.ts`.

- [ ] **Step 3: Commit**

```bash
git add web/src/api/pipelines.ts
git commit -m "feat(ui): add pipeline backend assignment API functions"
```

---

### Task 2: Create `PipelineBackendsModal.vue`

**Files:**
- Create: `web/src/components/PipelineBackendsModal.vue`

- [ ] **Step 1: Create the file**

Create `web/src/components/PipelineBackendsModal.vue` with the full content below:

```vue
<!-- web/src/components/PipelineBackendsModal.vue -->
<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useMessage } from 'naive-ui'
import { listPipelineBackends, setPipelineBackends } from '../api/pipelines'
import { listOllamaBackends } from '../api/ollama'
import type { OllamaBackend } from '../api/ollama'

const props = defineProps<{
  pipelineId: number
  pipelineName: string
  show: boolean
}>()

const emit = defineEmits<{
  (e: 'update:show', value: boolean): void
  (e: 'saved'): void
}>()

const message = useMessage()
const loading = ref(false)
const saving = ref(false)
const assigned = ref<OllamaBackend[]>([])
const allBackends = ref<OllamaBackend[]>([])
const addTarget = ref<number | null>(null)

const available = computed(() =>
  allBackends.value.filter(b => !assigned.value.some(a => a.id === b.id))
)

const availableOptions = computed(() =>
  available.value.map(b => ({ label: b.name, value: b.id }))
)

watch(() => props.show, async (open) => {
  if (!open) return
  loading.value = true
  addTarget.value = null
  try {
    const [pbs, backends] = await Promise.all([
      listPipelineBackends(props.pipelineId),
      listOllamaBackends(),
    ])
    allBackends.value = backends
    const sorted = [...pbs].sort((a, b) => a.priority - b.priority)
    assigned.value = sorted
      .map(pb => backends.find(b => b.id === pb.backend_id))
      .filter((b): b is OllamaBackend => b !== undefined)
  } catch (e: any) {
    message.error(e.message ?? 'Failed to load backends')
  } finally {
    loading.value = false
  }
})

function moveUp(i: number) {
  if (i === 0) return
  const a = [...assigned.value]
  ;[a[i - 1], a[i]] = [a[i], a[i - 1]]
  assigned.value = a
}

function moveDown(i: number) {
  if (i === assigned.value.length - 1) return
  const a = [...assigned.value]
  ;[a[i], a[i + 1]] = [a[i + 1], a[i]]
  assigned.value = a
}

function remove(i: number) {
  assigned.value = assigned.value.filter((_, idx) => idx !== i)
}

function addBackend() {
  if (addTarget.value === null) return
  const b = allBackends.value.find(b => b.id === addTarget.value)
  if (!b) return
  assigned.value = [...assigned.value, b]
  addTarget.value = null
}

async function save() {
  saving.value = true
  try {
    await setPipelineBackends(
      props.pipelineId,
      assigned.value.map((b, i) => ({ backend_id: b.id, priority: i + 1 })),
    )
    message.success('Backend assignments saved')
    emit('saved')
    emit('update:show', false)
  } catch (e: any) {
    message.error(e.message ?? 'Save failed')
  } finally {
    saving.value = false
  }
}

function close() {
  if (saving.value) return
  emit('update:show', false)
}
</script>

<template>
  <n-modal
    :show="show"
    :title="`Backend Assignments — ${pipelineName}`"
    preset="card"
    style="width: 480px;"
    :mask-closable="!saving"
    @update:show="close"
  >
    <div v-if="loading" style="display: flex; justify-content: center; padding: 32px 0;">
      <n-spin size="medium" />
    </div>

    <template v-else>
      <!-- Assigned list -->
      <div v-if="assigned.length > 0" style="margin-bottom: 16px;">
        <div
          v-for="(backend, i) in assigned"
          :key="backend.id"
          style="
            display: flex;
            align-items: center;
            gap: 8px;
            padding: 6px 0;
            border-bottom: 1px solid rgba(255,255,255,0.06);
          "
        >
          <n-text depth="3" style="font-size: 12px; width: 18px; text-align: right; flex-shrink: 0;">
            {{ i + 1 }}.
          </n-text>
          <div style="flex: 1; min-width: 0;">
            <n-text style="font-size: 13px; display: block; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;">
              {{ backend.name }}
            </n-text>
            <n-text
              code
              depth="3"
              style="font-size: 11px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; display: block;"
            >
              {{ backend.url }}
            </n-text>
          </div>
          <n-button size="tiny" :disabled="i === 0" @click="moveUp(i)">↑</n-button>
          <n-button size="tiny" :disabled="i === assigned.length - 1" @click="moveDown(i)">↓</n-button>
          <n-button size="tiny" type="error" @click="remove(i)">Remove</n-button>
        </div>
      </div>

      <n-text
        v-else
        depth="3"
        style="font-size: 12px; font-style: italic; display: block; margin-bottom: 16px;"
      >
        No backends assigned — any healthy backend will be used.
      </n-text>

      <!-- Add row -->
      <n-space align="center" :size="8">
        <n-select
          v-model:value="addTarget"
          :options="availableOptions"
          placeholder="Select backend to add"
          style="width: 280px;"
          :disabled="availableOptions.length === 0"
        />
        <n-button
          size="small"
          type="primary"
          :disabled="addTarget === null"
          @click="addBackend"
        >
          + Add
        </n-button>
      </n-space>
    </template>

    <template #footer>
      <n-space justify="end">
        <n-button :disabled="saving" @click="close">Cancel</n-button>
        <n-button type="primary" :loading="saving" :disabled="loading" @click="save">Save</n-button>
      </n-space>
    </template>
  </n-modal>
</template>
```

- [ ] **Step 2: Verify TypeScript compiles**

```bash
cd web && npx tsc --noEmit
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add web/src/components/PipelineBackendsModal.vue
git commit -m "feat(ui): add PipelineBackendsModal component"
```

---

### Task 3: Wire the modal into `PipelinesView.vue`

**Files:**
- Modify: `web/src/views/PipelinesView.vue`

- [ ] **Step 1: Add `ServerOutline` to the icon import**

Find this line in `web/src/views/PipelinesView.vue`:

```ts
import {
  TrashOutline, PauseCircleOutline, CheckmarkCircleOutline, CreateOutline,
} from '@vicons/ionicons5'
```

Replace with:

```ts
import {
  TrashOutline, PauseCircleOutline, CheckmarkCircleOutline, CreateOutline, ServerOutline,
} from '@vicons/ionicons5'
```

- [ ] **Step 2: Import `PipelineBackendsModal`**

After the `FormDrawer` import line:

```ts
import FormDrawer from '../components/FormDrawer.vue'
```

Add:

```ts
import PipelineBackendsModal from '../components/PipelineBackendsModal.vue'
```

- [ ] **Step 3: Add modal state and open handler**

After the `onDelete` function, add:

```ts
// ── Backend assignment modal ─────────────────────────────────────────────────
const backendModalPipeline = ref<Pipeline | null>(null)
const showBackendModal = ref(false)

function openBackendsModal(row: Pipeline) {
  backendModalPipeline.value = row
  showBackendModal.value = true
}
```

- [ ] **Step 4: Add backends button to the actions column**

Find the `actions` column render function:

```ts
render: (row) => h(NSpace, { size: 4 }, {
  default: () => [
    iconBtn(CreateOutline, 'Edit', () => openEdit(row)),
```

Replace the full `default: () => [...]` array with:

```ts
default: () => [
  ...(row.type === 'ollama'
    ? [iconBtn(ServerOutline, 'Backends', () => openBackendsModal(row))]
    : []),
  iconBtn(CreateOutline, 'Edit', () => openEdit(row)),
  row.active
    ? iconBtn(PauseCircleOutline, 'Deactivate', () => onDeactivate(row.id))
    : iconBtn(CheckmarkCircleOutline, 'Activate', () => onActivate(row.id), { type: 'primary' }),
  iconBtnConfirm(TrashOutline, 'Delete', 'Delete this pipeline?', () =>
    onDelete(row.id), { type: 'error' }),
],
```

- [ ] **Step 5: Mount the modal in the template**

At the end of the `<template>` block, after the `</FormDrawer>` closing tag and before `</template>`, add:

```html
<PipelineBackendsModal
  v-if="backendModalPipeline"
  v-model:show="showBackendModal"
  :pipeline-id="backendModalPipeline.id"
  :pipeline-name="backendModalPipeline.name"
  @saved="fetchPipelines"
/>
```

- [ ] **Step 6: Verify TypeScript compiles**

```bash
cd web && npx tsc --noEmit
```

Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add web/src/views/PipelinesView.vue
git commit -m "feat(ui): add backend assignment button to ollama pipelines"
```

---

### Task 4: Docker build verification

**Files:** none changed — build only

- [ ] **Step 1: Build the container**

```bash
docker compose build --build-arg BUILDKIT_INLINE_CACHE=1 iran-watcher
```

Expected: build succeeds with exit code 0. Fix any TypeScript or build errors before proceeding.

- [ ] **Step 2: Manual smoke test** (requires running stack)

1. Navigate to **Pipelines** in the UI
2. Confirm ollama-type pipeline rows show a server icon button; ONNX and internal rows do not
3. Click the icon — modal opens, shows spinner then loads
4. With no assignments: see the italic "No backends assigned" message
5. Add a backend from the dropdown → it appears as row 1
6. Add a second → row 2; use ↑/↓ to reorder
7. Click Remove on a row → it disappears
8. Click Save → success toast, modal closes, re-open confirms persistence
9. Click Cancel without saving → changes discarded on reopen

- [ ] **Step 3: Done**

All tasks complete. The pipeline backend assignment UI is fully functional.
