<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue'
import { PauseIcon, PencilIcon, PowerIcon, TrashIcon } from '@lucide/vue'
import {
  AlertDialog, Button, DataTable, FormField, IconButton, Input, NumberField, Sheet, StatusIndicator,
  Tooltip, type DataTableColumns,
} from '@/components/ui'
import { useToast } from '@/composables/useToast'
import {
  listOllamaBackends,
  createOllamaBackend,
  updateOllamaBackend,
  deleteOllamaBackend,
  enableOllamaBackend,
  disableOllamaBackend,
} from '../../api/ollama'
import { useOllamaStore } from '../../stores/ollama'
import type { OllamaBackend } from '../../stores/ollama'

const toast       = useToast()
const ollamaStore = useOllamaStore()

const errorMessage = (e: unknown) => (e instanceof Error ? e.message : String(e))

// ── Data ─────────────────────────────────────────────────────────────────────
// Live health, score and active counts arrive over WebSocket into the store; this only seeds it.

const loading   = ref(false)
const loadError = ref('')

async function refresh() {
  loadError.value = ''
  try { ollamaStore.setBackends(await listOllamaBackends()) }
  catch (e: unknown) { loadError.value = errorMessage(e) }
}

async function load() {
  loading.value = true
  await refresh()
  loading.value = false
}

// ── Create / edit ────────────────────────────────────────────────────────────

const showForm          = ref(false)
const editTarget        = ref<OllamaBackend | null>(null)
const formName          = ref('')
const formUrl           = ref('')
const formMaxConcurrent = ref<number | null>(1)
const saving            = ref(false)

function openCreate() {
  editTarget.value        = null
  formName.value          = ''
  formUrl.value           = ''
  formMaxConcurrent.value = 1
  showForm.value          = true
}

function openEdit(b: OllamaBackend) {
  editTarget.value        = b
  formName.value          = b.name
  formUrl.value           = b.url
  formMaxConcurrent.value = b.max_concurrent ?? 1
  showForm.value          = true
}

defineExpose({ openCreate })

async function onSave() {
  if (!formUrl.value.trim()) { toast.error('Enter the backend URL'); return }
  const maxConcurrent = Math.max(1, formMaxConcurrent.value || 1)
  saving.value = true
  try {
    if (editTarget.value) {
      await updateOllamaBackend(editTarget.value.id, {
        name:           formName.value.trim() || undefined,
        url:            formUrl.value.trim(),
        max_concurrent: maxConcurrent,
      })
      toast.success('Backend updated')
    } else {
      await createOllamaBackend(formName.value.trim(), formUrl.value.trim(), maxConcurrent)
      toast.success('Backend added')
    }
    showForm.value = false
    await refresh()
  } catch (e: unknown) {
    toast.error(`Could not save backend: ${errorMessage(e)}`)
  } finally {
    saving.value = false
  }
}

// ── Enable / delete ──────────────────────────────────────────────────────────

async function onToggleEnabled(b: OllamaBackend) {
  try {
    if (b.enabled) await disableOllamaBackend(b.id)
    else await enableOllamaBackend(b.id)
    toast.success(b.enabled ? 'Backend disabled' : 'Backend enabled')
    await refresh()
  } catch (e: unknown) {
    toast.error(`Could not ${b.enabled ? 'disable' : 'enable'} backend: ${errorMessage(e)}`)
  }
}

const deleteTarget = ref<OllamaBackend | null>(null)
const showDelete   = ref(false)
const deleting     = ref(false)

function askDelete(b: OllamaBackend) {
  deleteTarget.value = b
  showDelete.value = true
}

async function onConfirmDelete() {
  const target = deleteTarget.value
  if (!target) return
  deleting.value = true
  try {
    await deleteOllamaBackend(target.id)
    toast.success('Backend deleted')
    showDelete.value = false
    await refresh()
  } catch (e: unknown) {
    toast.error(`Could not delete backend: ${errorMessage(e)}`)
  } finally {
    deleting.value = false
  }
}

// ── Derived ──────────────────────────────────────────────────────────────────

const maxScore = computed(() => Math.max(1, ...ollamaStore.backends.map(b => b.score)))

function backendStatus(b: OllamaBackend): { status: 'success' | 'info' | 'warning' | 'destructive' | 'idle'; label: string } {
  const max = b.max_concurrent || 1
  if (!b.enabled) return { status: 'idle', label: 'Disabled' }
  if (!b.healthy) return { status: 'destructive', label: 'Unhealthy' }
  if (b.active_count >= max) return { status: 'warning', label: 'At capacity' }
  if (b.active_count > 0) return { status: 'info', label: 'Busy' }
  return { status: 'success', label: 'Healthy' }
}

function timeAgo(iso: string): string {
  const diff = Math.floor((Date.now() - new Date(iso).getTime()) / 1000)
  if (diff < 60) return 'just now'
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`
  return `${Math.floor(diff / 86400)}d ago`
}

// ── Table columns ────────────────────────────────────────────────────────────

const columns: DataTableColumns<OllamaBackend> = [
  {
    title: 'Backend', key: 'name', sorter: 'default',
    render: (b) => h('div', { class: 'min-w-0' }, [
      h('div', { class: 'truncate font-medium' }, b.name),
      h('div', { class: 'truncate font-mono text-small text-muted-foreground' }, b.url),
    ]),
  },
  {
    title: 'Status', key: 'healthy', width: 120,
    render: (b) => {
      const s = backendStatus(b)
      return h(StatusIndicator, { status: s.status }, () => s.label)
    },
  },
  {
    title: 'Slots', key: 'active_count', width: 80,
    render: (b) => h('span', { class: 'font-mono tabular' }, `${b.active_count}/${b.max_concurrent}`),
  },
  {
    title: 'Speed', key: 'score', width: 180, sorter: (a, b) => a.score - b.score,
    render: (b) => b.score > 0
      ? h('div', { class: 'flex items-center gap-2' }, [
          h('div', { class: 'h-1 flex-1 overflow-hidden rounded-sm bg-muted' }, [
            h('progress', {
              class: 'speed-bar block h-full w-full',
              max: maxScore.value,
              value: b.score,
              'aria-label': `${b.score.toFixed(1)} tokens per second`,
            }),
          ]),
          h('span', { class: 'w-20 text-right font-mono tabular text-small' }, `${b.score.toFixed(1)} tok/s`),
        ])
      : h('span', { class: 'text-small text-muted-foreground' }, 'Not measured'),
  },
  {
    title: 'Requests', key: 'request_count', width: 100, sorter: (a, b) => a.request_count - b.request_count,
    render: (b) => h('span', { class: 'font-mono tabular' }, String(b.request_count)),
  },
  {
    title: 'Last active', key: 'last_active_at', width: 110,
    render: (b) => b.last_active_at
      ? h(Tooltip, { content: new Date(b.last_active_at).toLocaleString() },
          () => h('span', { class: 'tabular text-muted-foreground' }, timeAgo(b.last_active_at!)))
      : h('span', { class: 'text-muted-foreground' }, 'Never'),
  },
  {
    title: '', key: 'actions', width: 120, align: 'right',
    render: (b) => h('div', { class: 'flex items-center justify-end gap-1' }, [
      h(IconButton, { icon: PencilIcon, label: 'Edit', onClick: () => openEdit(b) }),
      h(IconButton, {
        icon: b.enabled ? PauseIcon : PowerIcon,
        label: b.enabled ? 'Disable' : 'Enable',
        onClick: () => onToggleEnabled(b),
      }),
      h(IconButton, { icon: TrashIcon, label: 'Delete', onClick: () => askDelete(b) }),
    ]),
  },
]

onMounted(load)
</script>

<template>
  <div
    v-if="loadError"
    class="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-destructive p-4 text-small"
  >
    <span class="text-destructive">Could not load backends: {{ loadError }}</span>
    <Button variant="outline" @click="load">Retry</Button>
  </div>

  <DataTable
    v-else
    :columns="columns"
    :data="ollamaStore.backends"
    :loading="loading"
    :row-key="(b: OllamaBackend) => b.id"
  >
    <template #empty>
      <div class="flex flex-col items-center gap-3 py-8 text-center">
        <p class="m-0 text-muted-foreground">No Ollama backends yet. Add one to run Ollama actions, digests and chat.</p>
        <Button @click="openCreate">Add backend</Button>
      </div>
    </template>
  </DataTable>

  <Sheet
    v-model:open="showForm"
    :title="editTarget ? 'Edit backend' : 'Add backend'"
    description="An Ollama server that runs Ollama actions."
  >
    <form class="flex flex-col gap-3 p-4" @submit.prevent="onSave">
      <FormField label="URL" for="backend-url" required>
        <Input id="backend-url" v-model="formUrl" placeholder="http://localhost:11434" class="font-mono" />
      </FormField>
      <FormField label="Name" for="backend-name" description="Defaults to the URL.">
        <Input id="backend-name" v-model="formName" placeholder="e.g. GPU box" />
      </FormField>
      <FormField
        label="Slots"
        for="backend-slots"
        description="Concurrent requests, 1 to 32. Match OLLAMA_NUM_PARALLEL on the server."
      >
        <NumberField id="backend-slots" v-model="formMaxConcurrent" :min="1" :max="32" />
      </FormField>
    </form>
    <template #footer>
      <Button variant="outline" size="default" :disabled="saving" @click="showForm = false">Cancel</Button>
      <Button size="default" :loading="saving" @click="onSave">{{ editTarget ? 'Save backend' : 'Add backend' }}</Button>
    </template>
  </Sheet>

  <AlertDialog
    v-model:open="showDelete"
    :title="`Delete ${deleteTarget?.name ?? 'backend'}?`"
    description="The backend is removed. This can't be undone."
    action-label="Delete backend"
    :loading="deleting"
    @confirm="onConfirmDelete"
  />
</template>

<style scoped>
/* Speed bar: a native <progress> so the fill width needs no inline style. */
.speed-bar { appearance: none; border: 0; background: transparent; }
.speed-bar::-webkit-progress-bar { background: transparent; }
.speed-bar::-webkit-progress-value { background: var(--primary); }
.speed-bar::-moz-progress-bar { background: var(--primary); }
</style>
