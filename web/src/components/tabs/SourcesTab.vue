<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue'
import { PauseIcon, PencilIcon, TrashIcon } from '@lucide/vue'
import {
  AlertDialog, Badge, Button, Checkbox, DataTable, FormField, IconButton, Input, Select, Sheet,
  StatusIndicator, type DataTableColumns,
} from '@/components/ui'
import { useToast } from '@/composables/useToast'
import { useSourcesStore } from '../../stores/sources'
import type { Source } from '../../api/sources'
import { ApiError } from '../../api/client'

// SourceType → operator mapping (mirrors Go model)
const SOURCE_TYPES = [
  { label: 'keyword',        value: 'keyword',        operator: '' },
  { label: 'phrase',         value: 'phrase',         operator: '' },
  { label: 'hashtag',        value: 'hashtag',        operator: '' },
  { label: 'cashtag',        value: 'cashtag',        operator: '' },
  { label: 'mention',        value: 'mention',        operator: '' },
  { label: 'account (from)', value: 'account',        operator: 'from:' },
  { label: 'to_account',     value: 'to_account',     operator: 'to:' },
  { label: 'retweets_of',    value: 'retweets_of',    operator: 'retweets_of:' },
  { label: 'url',            value: 'url',            operator: 'url:' },
  { label: 'url_contains',   value: 'url_contains',   operator: 'url_contains:' },
  { label: 'lang',           value: 'lang',           operator: 'lang:' },
  { label: 'is',             value: 'is',             operator: 'is:' },
  { label: 'has',            value: 'has',            operator: 'has:' },
]
const typeOptions = SOURCE_TYPES.map(t => ({ label: t.label, value: t.value }))

const store = useSourcesStore()
const toast = useToast()

// ── Filters ───────────────────────────────────────────────────────────────────

const filterText       = ref('')
const filterType       = ref<string | null>(null)
const filterActiveOnly = ref(false)

const filteredSources = computed(() => {
  let list = store.sources
  const q = filterText.value.trim().toLowerCase()
  if (q) list = list.filter(s => s.name.toLowerCase().includes(q) || s.value.toLowerCase().includes(q))
  if (filterType.value) list = list.filter(s => s.type === filterType.value)
  if (filterActiveOnly.value) list = list.filter(s => s.active)
  return list
})

const hasFilters = computed(() => !!filterText.value.trim() || !!filterType.value || filterActiveOnly.value)

function clearFilters() {
  filterText.value = ''
  filterType.value = null
  filterActiveOnly.value = false
}

// ── Create / edit form ────────────────────────────────────────────────────────

const showForm = ref(false)
const editId   = ref<number | null>(null)
const form     = ref({ name: '', type: 'keyword' as string | null, value: '' })
const saving   = ref(false)

const operatorFor = (type: string | null) =>
  SOURCE_TYPES.find(t => t.value === type)?.operator ?? ''

const valuePreview = computed(() => `${operatorFor(form.value.type)}${form.value.value.trim()}`)

function openCreate() {
  editId.value = null
  form.value = { name: '', type: 'keyword', value: '' }
  showForm.value = true
}

defineExpose({ openCreate })

function openEdit(row: Source) {
  editId.value = row.id
  form.value = { name: row.name, type: row.type, value: row.value }
  showForm.value = true
}

async function onSave() {
  if (!form.value.name.trim()) { toast.error('Enter a name for the source'); return }
  if (!form.value.type) { toast.error('Select a source type'); return }
  if (!form.value.value.trim()) { toast.error('Enter the value to match'); return }
  const body = { name: form.value.name, type: form.value.type, value: form.value.value }
  saving.value = true
  try {
    if (editId.value != null) {
      await store.update(editId.value, body)
      toast.success('Source updated')
    } else {
      await store.createSource(body)
      toast.success('Source created')
    }
    showForm.value = false
  } catch (e: any) {
    toast.error(`Could not ${editId.value != null ? 'update' : 'create'} source: ${e.message}`)
  } finally {
    saving.value = false
  }
}

// ── Deactivate / delete ───────────────────────────────────────────────────────

async function onDeactivate(row: Source) {
  try {
    await store.deactivate(row.id)
    toast.success('Source deactivated')
  } catch (e: any) {
    if (e instanceof ApiError && e.status === 409)
      toast.error('Could not deactivate source: an active rule uses it. Deactivate that rule first.')
    else
      toast.error(`Could not deactivate source: ${e.message}`)
  }
}

const deleteTarget = ref<Source | null>(null)
const showDelete   = ref(false)
const deleting     = ref(false)

function askDelete(row: Source) {
  deleteTarget.value = row
  showDelete.value = true
}

async function onConfirmDelete() {
  const target = deleteTarget.value
  if (!target) return
  deleting.value = true
  try {
    await store.remove(target.id)
    toast.success('Source deleted')
    showDelete.value = false
  } catch (e: any) {
    if (e instanceof ApiError && e.status === 409)
      toast.error('Could not delete source: a rule uses it. Remove it from that rule first.')
    else
      toast.error(`Could not delete source: ${e.message}`)
  } finally {
    deleting.value = false
  }
}

// ── Table columns ─────────────────────────────────────────────────────────────

const columns: DataTableColumns<Source> = [
  {
    title: 'ID', key: 'id', width: 64, sorter: 'default',
    render: (row) => h('span', { class: 'font-mono tabular text-muted-foreground' }, String(row.id)),
  },
  {
    title: 'Name', key: 'name', sorter: 'default', ellipsis: { tooltip: true },
    render: (row) => h('span', { class: 'font-medium' }, row.name),
  },
  {
    title: 'Type', key: 'type', width: 130, sorter: 'default',
    render: (row) => h(Badge, { variant: 'secondary' }, () => row.type),
  },
  {
    title: 'Value', key: 'value', sorter: 'default', ellipsis: { tooltip: true },
    render: (row) => h('span', { class: 'font-mono' }, [
      row.operator ? h('span', { class: 'text-muted-foreground' }, row.operator) : null,
      row.value,
    ]),
  },
  {
    title: 'Status', key: 'active', width: 110,
    sorter: (a, b) => Number(a.active) - Number(b.active),
    render: (row) => h(StatusIndicator, { status: row.active ? 'success' : 'idle' },
      () => row.active ? 'Active' : 'Inactive'),
  },
  {
    title: '', key: 'actions', width: 120, align: 'right',
    render: (row) => h('div', { class: 'flex items-center justify-end gap-1' }, [
      h(IconButton, { icon: PencilIcon, label: 'Edit', onClick: () => openEdit(row) }),
      row.active
        ? h(IconButton, { icon: PauseIcon, label: 'Deactivate', onClick: () => onDeactivate(row) })
        : null,
      h(IconButton, { icon: TrashIcon, label: 'Delete', onClick: () => askDelete(row) }),
    ]),
  },
]

onMounted(() => store.fetchSources())
</script>

<template>
  <div class="flex flex-col gap-3">
    <div class="flex flex-wrap items-center gap-2">
      <div class="w-64">
        <Input v-model="filterText" placeholder="Search name or value" clearable />
      </div>
      <div class="w-44">
        <Select v-model="filterType" :options="typeOptions" placeholder="All types" clearable />
      </div>
      <Checkbox v-model="filterActiveOnly">Active only</Checkbox>
    </div>

    <div
      v-if="store.error"
      class="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-destructive p-4 text-small"
    >
      <span class="text-destructive">Could not load sources: {{ store.error }}</span>
      <Button variant="outline" @click="store.fetchSources()">Retry</Button>
    </div>

    <DataTable
      v-else
      :columns="columns"
      :data="filteredSources"
      :loading="store.loading"
      :row-key="(row: Source) => row.id"
      :page-size="20"
    >
      <template #empty>
        <div v-if="hasFilters && store.sources.length" class="flex flex-col items-center gap-3 py-8 text-center">
          <p class="m-0 text-muted-foreground">No sources match these filters.</p>
          <Button variant="outline" @click="clearFilters">Clear filters</Button>
        </div>
        <div v-else class="flex flex-col items-center gap-3 py-8 text-center">
          <p class="m-0 text-muted-foreground">No sources yet. Add a keyword, account or hashtag to build rules from.</p>
          <Button @click="openCreate">New source</Button>
        </div>
      </template>
    </DataTable>
  </div>

  <!-- Create / edit source -->
  <Sheet
    v-model:open="showForm"
    :title="editId != null ? 'Edit source' : 'New source'"
    description="A single search term or operator. Combine sources into rules on the Rules tab."
  >
    <form class="flex flex-col gap-3 p-4" @submit.prevent="onSave">
      <FormField label="Name" for="source-name" required>
        <Input id="source-name" v-model="form.name" placeholder="e.g. Earthquake keyword" />
      </FormField>
      <FormField label="Type" required>
        <Select v-model="form.type" :options="typeOptions" filterable />
      </FormField>
      <FormField
        label="Value"
        for="source-value"
        required
        :description="valuePreview ? `Sent to X as ${valuePreview}` : undefined"
      >
        <Input id="source-value" v-model="form.value" placeholder="e.g. earthquake" />
      </FormField>
    </form>
    <template #footer>
      <Button variant="outline" size="default" @click="showForm = false">Cancel</Button>
      <Button size="default" :loading="saving" @click="onSave">
        {{ editId != null ? 'Save source' : 'Create source' }}
      </Button>
    </template>
  </Sheet>

  <!-- Delete confirm -->
  <AlertDialog
    v-model:open="showDelete"
    :title="`Delete ${deleteTarget?.name ?? 'source'}?`"
    description="The source is removed. Sources used by a rule can't be deleted. This can't be undone."
    action-label="Delete source"
    :loading="deleting"
    @confirm="onConfirmDelete"
  />
</template>
