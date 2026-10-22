<script setup lang="ts">
import { computed, h, ref } from 'vue'
import { HistoryIcon, PauseIcon, PencilIcon, PowerIcon, TrashIcon } from '@lucide/vue'
import {
  AlertDialog, Badge, Button, Checkbox, DataTable, IconButton, Input, Select, StatusIndicator,
  type DataTableColumns,
} from '@/components/ui'
import { useToast } from '@/composables/useToast'
import { useAuthStore } from '../../stores/auth'
import { useEntitiesStore } from '../../stores/entities'
import type { TrackedEntity } from '../../api/entities'
import { entityTypeFilterOptions } from './entityTypes'
import EntityFormSheet from './EntityFormSheet.vue'
import EntityTracksSheet from './EntityTracksSheet.vue'

const store = useEntitiesStore()
const auth  = useAuthStore()
const toast = useToast()

const errorMessage = (e: unknown) => (e instanceof Error ? e.message : String(e))

// ── Filters ──────────────────────────────────────────────────────────────────

const filterText       = ref('')
const filterEntityType = ref<string | null>(null)
const filterActiveOnly = ref(false)

const filteredEntities = computed(() => {
  let list = store.items
  const q = filterText.value.trim().toLowerCase()
  if (q) list = list.filter(e =>
    e.name.toLowerCase().includes(q) ||
    e.aliases.some(a => a.toLowerCase().includes(q))
  )
  if (filterEntityType.value) list = list.filter(e => e.type === filterEntityType.value)
  if (filterActiveOnly.value) list = list.filter(e => e.active)
  return list
})

const hasFilters = computed(() => !!filterText.value.trim() || !!filterEntityType.value || filterActiveOnly.value)

function clearFilters() {
  filterText.value = ''
  filterEntityType.value = null
  filterActiveOnly.value = false
}

// ── Create / edit ────────────────────────────────────────────────────────────

const showForm   = ref(false)
const editTarget = ref<TrackedEntity | null>(null)

function openCreate() {
  editTarget.value = null
  showForm.value = true
}

function openEdit(e: TrackedEntity) {
  editTarget.value = e
  showForm.value = true
}

defineExpose({ openCreate })

// ── Track history ────────────────────────────────────────────────────────────

const showTracks   = ref(false)
const tracksEntity = ref<TrackedEntity | null>(null)

function openTracks(e: TrackedEntity) {
  tracksEntity.value = e
  showTracks.value = true
}

// ── Activate / delete ────────────────────────────────────────────────────────

async function onToggleActive(e: TrackedEntity) {
  const activate = !e.active
  try {
    if (activate) await store.activate(e.id)
    else await store.deactivate(e.id)
    toast.success(activate ? 'Entity activated' : 'Entity deactivated')
  } catch (err: unknown) {
    toast.error(`Could not ${activate ? 'activate' : 'deactivate'} entity: ${errorMessage(err)}`)
  }
}

const deleteTarget = ref<TrackedEntity | null>(null)
const showDelete   = ref(false)
const deleting     = ref(false)

function askDelete(e: TrackedEntity) {
  deleteTarget.value = e
  showDelete.value = true
}

async function onConfirmDelete() {
  const target = deleteTarget.value
  if (!target) return
  deleting.value = true
  try {
    await store.remove(target.id)
    toast.success('Entity deleted')
    showDelete.value = false
    if (tracksEntity.value?.id === target.id) showTracks.value = false
  } catch (e: unknown) {
    toast.error(`Could not delete entity: ${errorMessage(e)}`)
  } finally {
    deleting.value = false
  }
}

// ── Table columns ────────────────────────────────────────────────────────────

const dash = () => h('span', { class: 'text-muted-foreground' }, '—')

const columns = computed<DataTableColumns<TrackedEntity>>(() => [
  {
    title: 'Name', key: 'name', width: 220, sorter: 'default',
    render: (row) => h('div', { class: 'min-w-0' }, [
      h('div', { class: 'font-medium' }, row.name),
      row.notes ? h('div', { class: 'text-small text-muted-foreground' }, row.notes) : null,
    ]),
  },
  {
    title: 'Type', key: 'type', width: 90, sorter: 'default',
    render: (row) => row.type ? h(Badge, { variant: 'secondary' }, () => row.type) : dash(),
  },
  {
    title: 'Aliases', key: 'aliases',
    render: (row) => row.aliases.length
      ? h('div', { class: 'flex flex-wrap gap-1' }, row.aliases.map(a => h(Badge, { key: a, variant: 'outline' }, () => a)))
      : dash(),
  },
  {
    title: 'Tracks', key: 'track_count', width: 90, align: 'right',
    sorter: (a, b) => (a.track_count ?? 0) - (b.track_count ?? 0),
    render: (row) => h('span', { class: 'tabular' }, String(row.track_count ?? 0)),
  },
  {
    title: 'Status', key: 'active', width: 110,
    sorter: (a, b) => Number(a.active) - Number(b.active),
    render: (row) => h(StatusIndicator, { status: row.active ? 'success' : 'idle' },
      () => row.active ? 'Active' : 'Inactive'),
  },
  {
    title: '', key: 'actions', width: auth.isAdmin ? 150 : 60, align: 'right',
    render: (row) => h('div', { class: 'flex items-center justify-end gap-1' }, [
      h(IconButton, { icon: HistoryIcon, label: 'View tracks', onClick: () => openTracks(row) }),
      ...(auth.isAdmin ? [
        h(IconButton, { icon: PencilIcon, label: 'Edit', onClick: () => openEdit(row) }),
        h(IconButton, {
          icon: row.active ? PauseIcon : PowerIcon,
          label: row.active ? 'Deactivate' : 'Activate',
          onClick: () => onToggleActive(row),
        }),
        h(IconButton, { icon: TrashIcon, label: 'Delete', onClick: () => askDelete(row) }),
      ] : []),
    ]),
  },
])
</script>

<template>
  <div class="flex flex-col gap-3">
    <div class="flex flex-wrap items-center gap-2">
      <div class="w-64">
        <Input v-model="filterText" placeholder="Search name or alias" clearable />
      </div>
      <div class="w-44">
        <Select v-model="filterEntityType" :options="entityTypeFilterOptions" placeholder="All types" clearable />
      </div>
      <Checkbox v-model="filterActiveOnly">Active only</Checkbox>
    </div>

    <div
      v-if="store.error"
      class="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-destructive p-4 text-small"
    >
      <span class="text-destructive">Could not load tracked entities: {{ store.error }}</span>
      <Button variant="outline" @click="store.fetchEntities()">Retry</Button>
    </div>

    <DataTable
      v-else
      :columns="columns"
      :data="filteredEntities"
      :loading="store.loading"
      :row-key="(row: TrackedEntity) => row.id"
      :page-size="50"
    >
      <template #empty>
        <div v-if="hasFilters && store.items.length" class="flex flex-col items-center gap-3 py-8 text-center">
          <p class="m-0 text-muted-foreground">No tracked entities match these filters.</p>
          <Button variant="outline" @click="clearFilters">Clear filters</Button>
        </div>
        <div v-else class="flex flex-col items-center gap-3 py-8 text-center">
          <p class="m-0 text-muted-foreground">
            No tracked entities yet.
            {{ auth.isAdmin ? 'Add one, or promote a candidate, to start matching it in signals.' : 'They appear here once an admin adds them.' }}
          </p>
          <Button v-if="auth.isAdmin" @click="openCreate">New entity</Button>
        </div>
      </template>
    </DataTable>
  </div>

  <EntityFormSheet v-if="auth.isAdmin" v-model:open="showForm" :entity="editTarget" />
  <EntityTracksSheet v-model:open="showTracks" :entity="tracksEntity" />

  <AlertDialog
    v-model:open="showDelete"
    :title="`Delete ${deleteTarget?.name ?? 'entity'}?`"
    description="The entity is removed with its track history and connections. This can't be undone."
    action-label="Delete entity"
    :loading="deleting"
    @confirm="onConfirmDelete"
  />
</template>
