# Pipeline Backend Assignment UI

**Date:** 2026-03-28
**Status:** Approved

## Problem

The backend API for assigning Ollama backends to pipelines is fully implemented (`GET/PUT/DELETE /api/pipelines/{id}/backends`), but there is no UI for it. Users must use the CLI or raw HTTP calls to configure which Ollama backend(s) an Ollama-type pipeline prefers.

## Goal

Add UI in the Pipelines view that lets admins assign, reorder, and remove Ollama backend assignments for a pipeline, without leaving the page.

## Scope

Frontend only. No backend changes required.

---

## Architecture

### Files changed

| File | Change |
|------|--------|
| `web/src/api/pipelines.ts` | Add `PipelineBackend` type + 3 API functions |
| `web/src/components/PipelineBackendsModal.vue` | New component (self-contained modal) |
| `web/src/views/PipelinesView.vue` | Add icon button in actions column for ollama-type rows |

---

## API Layer (`web/src/api/pipelines.ts`)

Add to the existing file:

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

The modal always saves via `setPipelineBackends` (full replace). `removePipelineBackend` is included for completeness.

---

## Component: `PipelineBackendsModal.vue`

### Props / emits

```ts
props: { pipelineId: number; pipelineName: string; show: boolean }
emits: ['update:show', 'saved']
```

### Data fetching

On `watch(show, open)`:
1. Fetch `listPipelineBackends(pipelineId)` and `listOllamaBackends()` in parallel.
2. Sort assignments by `priority` ascending.
3. Build working array: `assigned = assignments.map(a => allBackends.find(b => b.id === a.backend_id))` (filtered to only those that resolve).

### Internal state

```ts
assigned: ref<OllamaBackend[]>([])   // ordered; index 0 = priority 1
allBackends: ref<OllamaBackend[]>([])
addTarget: ref<number | null>(null)  // selected backend_id in the "add" dropdown
loading: ref(false)
saving: ref(false)
```

### Computed

```ts
available = allBackends.filter(b => !assigned.includes(b))  // for add dropdown
```

### Operations

- **Move up**: swap `assigned[i]` with `assigned[i-1]`; ↑ disabled when `i === 0`
- **Move down**: swap `assigned[i]` with `assigned[i+1]`; ↓ disabled when `i === assigned.length - 1`
- **Remove**: splice from `assigned`
- **Add**: push `allBackends.find(b => b.id === addTarget)` onto `assigned`; clear `addTarget`
- **Save**: `setPipelineBackends(pipelineId, assigned.map((b, i) => ({ backend_id: b.id, priority: i + 1 })))`, then emit `saved` and close

### Layout

```
┌─ Backend Assignments — "{pipelineName}" ──────────────────┐
│                                                            │
│  [spinner while loading]                                   │
│                                                            │
│  ┌──────────────────────────────────────────────────────┐ │
│  │ 1.  local-gpu   http://gpu1:11434   [↑][↓] [Remove] │ │
│  │ 2.  fallback    http://gpu2:11434   [↑][↓] [Remove] │ │
│  └──────────────────────────────────────────────────────┘ │
│                                                            │
│  [Select backend to add ▼]  [+ Add]                        │
│                                                            │
│  ℹ  No backends assigned = falls back to any healthy       │
│     backend.                                               │
│                                         [Cancel]  [Save]  │
└────────────────────────────────────────────────────────────┘
```

- ↑ disabled on first row; ↓ disabled on last row
- "Select backend" dropdown only lists unassigned backends
- "+ Add" disabled when `addTarget` is null or `available` is empty
- Empty assigned list shows a subtle info note (not an error)
- Modal width: 480px; not closable while saving

---

## PipelinesView changes

### Actions column

In the `render` function for the `actions` column, add a server/backends icon button before the edit button, rendered only when `row.type === 'ollama'`:

```ts
row.type === 'ollama'
  ? iconBtn(ServerOutline, 'Backends', () => openBackendsModal(row))
  : null
```

### New state

```ts
const backendModalPipeline = ref<Pipeline | null>(null)
const showBackendModal = ref(false)

function openBackendsModal(row: Pipeline) {
  backendModalPipeline.value = row
  showBackendModal.value = true
}
```

### Template addition

```html
<PipelineBackendsModal
  v-if="backendModalPipeline"
  v-model:show="showBackendModal"
  :pipeline-id="backendModalPipeline.id"
  :pipeline-name="backendModalPipeline.name"
  @saved="fetchPipelines"
/>
```

---

## Error handling

- Fetch errors in the modal: show inline error message, keep modal open
- Save errors: `message.error(...)`, keep modal open
- If a `backend_id` from `listPipelineBackends` doesn't resolve in `listOllamaBackends` (deleted backend), silently drop it from the working list — it will be removed on next Save

---

## Testing

No new unit tests required (pure UI wiring over already-tested API endpoints). Manual test checklist:

1. Ollama-type pipeline row shows the backends icon; ONNX/internal rows do not
2. Modal opens, loads existing assignments in priority order
3. ↑/↓ reorders correctly; boundary buttons are disabled
4. Remove drops a backend from the list
5. Add dropdown only shows unassigned backends; adding appends to list
6. Save persists correct `priority` values; modal closes
7. Cancel discards in-memory changes
8. Empty state message shown when no backends assigned
