<script setup lang="ts">
import { computed, h, ref } from 'vue'
import { PauseIcon, PencilIcon, PowerIcon, TrashIcon } from '@lucide/vue'
import {
  AlertDialog, Badge, Button, Checkbox, DataTable, IconButton, Input, Select, StatusIndicator,
  type DataTableColumns,
} from '@/components/ui'
import { useToast } from '@/composables/useToast'
import type { NotificationProvider } from '../../api/notifications'
import { useNotificationsStore } from '../../stores/notifications'
import ProviderFormSheet from './ProviderFormSheet.vue'
import { providerTypeLabel, providerTypeOptions, type ProviderType } from './providerTypes'

const store = useNotificationsStore()
const toast = useToast()

const errorMessage = (e: unknown) => (e instanceof Error ? e.message : String(e))

const channelCount = computed(() => {
  const m: Record<number, number> = {}
  store.channels.forEach(c => { m[c.provider_id] = (m[c.provider_id] ?? 0) + 1 })
  return m
})

// ── Filters ──────────────────────────────────────────────────────────────────

const filterText       = ref('')
const filterType       = ref<ProviderType | null>(null)
const filterActiveOnly = ref(false)

const filteredProviders = computed(() => {
  let list = store.providers
  const q = filterText.value.trim().toLowerCase()
  if (q) list = list.filter(p => p.name.toLowerCase().includes(q))
  if (filterType.value) list = list.filter(p => p.type === filterType.value)
  if (filterActiveOnly.value) list = list.filter(p => p.active)
  return list
})

const hasFilters = computed(() => !!filterText.value.trim() || !!filterType.value || filterActiveOnly.value)

function clearFilters() {
  filterText.value = ''
  filterType.value = null
  filterActiveOnly.value = false
}

// ── Create / edit ────────────────────────────────────────────────────────────

const showForm     = ref(false)
const editProvider = ref<NotificationProvider | null>(null)

function openCreate() {
  editProvider.value = null
  showForm.value = true
}

function openEdit(row: NotificationProvider) {
  editProvider.value = row
  showForm.value = true
}

defineExpose({ openCreate })

// ── Activate / delete ────────────────────────────────────────────────────────

async function onToggleActive(row: NotificationProvider) {
  const activating = !row.active
  try {
    if (activating) await store.activateProvider(row.id)
    else await store.deactivateProvider(row.id)
    toast.success(activating ? 'Provider activated' : 'Provider deactivated')
  } catch (e: unknown) {
    toast.error(`Could not ${activating ? 'activate' : 'deactivate'} provider: ${errorMessage(e)}`)
  }
}

const deleteTarget = ref<NotificationProvider | null>(null)
const showDelete   = ref(false)
const deleting     = ref(false)

const deleteDescription = computed(() => {
  const n = deleteTarget.value ? channelCount.value[deleteTarget.value.id] ?? 0 : 0
  if (!n) return 'The provider is removed. This can\'t be undone.'
  return `The provider and its ${n === 1 ? 'channel' : `${n} channels`} are removed. This can't be undone.`
})

function askDelete(row: NotificationProvider) {
  deleteTarget.value = row
  showDelete.value = true
}

async function onConfirmDelete() {
  const target = deleteTarget.value
  if (!target) return
  deleting.value = true
  try {
    await store.removeProvider(target.id)
    toast.success('Provider deleted')
    showDelete.value = false
  } catch (e: unknown) {
    toast.error(`Could not delete provider: ${errorMessage(e)}`)
  } finally {
    deleting.value = false
  }
}

// ── Table columns ────────────────────────────────────────────────────────────

const columns: DataTableColumns<NotificationProvider> = [
  {
    title: 'ID', key: 'id', width: 64, sorter: 'default',
    render: (row) => h('span', { class: 'font-mono tabular text-muted-foreground' }, String(row.id)),
  },
  {
    title: 'Name', key: 'name', sorter: 'default', ellipsis: { tooltip: true },
    render: (row) => h('span', { class: 'font-medium' }, row.name),
  },
  {
    title: 'Type', key: 'type', width: 120, sorter: 'default',
    render: (row) => h(Badge, { variant: 'secondary' }, () => providerTypeLabel[row.type] ?? row.type),
  },
  {
    title: 'Channels', key: 'channels', width: 100, align: 'right',
    sorter: (a, b) => (channelCount.value[a.id] ?? 0) - (channelCount.value[b.id] ?? 0),
    render: (row) => h('span', { class: 'tabular text-muted-foreground' }, String(channelCount.value[row.id] ?? 0)),
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
      h(IconButton, {
        icon: row.active ? PauseIcon : PowerIcon,
        label: row.active ? 'Deactivate' : 'Activate',
        onClick: () => onToggleActive(row),
      }),
      h(IconButton, { icon: TrashIcon, label: 'Delete', onClick: () => askDelete(row) }),
    ]),
  },
]
</script>

<template>
  <div class="flex flex-col gap-3">
    <div class="flex flex-wrap items-center gap-2">
      <div class="w-64">
        <Input v-model="filterText" placeholder="Search name" clearable />
      </div>
      <div class="w-44">
        <Select v-model="filterType" :options="providerTypeOptions" placeholder="All types" clearable />
      </div>
      <Checkbox v-model="filterActiveOnly">Active only</Checkbox>
    </div>

    <div
      v-if="store.error"
      class="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-destructive p-4 text-small"
    >
      <span class="text-destructive">Could not load notifications: {{ store.error }}</span>
      <Button variant="outline" @click="store.fetchProviders(); store.fetchChannels()">Retry</Button>
    </div>

    <DataTable
      v-else
      :columns="columns"
      :data="filteredProviders"
      :loading="store.loading"
      :row-key="(row: NotificationProvider) => row.id"
      :page-size="20"
    >
      <template #empty>
        <div v-if="hasFilters && store.providers.length" class="flex flex-col items-center gap-3 py-8 text-center">
          <p class="m-0 text-muted-foreground">No providers match these filters.</p>
          <Button variant="outline" @click="clearFilters">Clear filters</Button>
        </div>
        <div v-else class="flex flex-col items-center gap-3 py-8 text-center">
          <p class="m-0 text-muted-foreground">No providers yet. Add your Pushover, Telegram or Discord credentials to start sending notifications.</p>
          <Button @click="openCreate">New provider</Button>
        </div>
      </template>
    </DataTable>
  </div>

  <ProviderFormSheet v-model:open="showForm" :provider="editProvider" />

  <AlertDialog
    v-model:open="showDelete"
    :title="`Delete ${deleteTarget?.name ?? 'provider'}?`"
    :description="deleteDescription"
    action-label="Delete provider"
    :loading="deleting"
    @confirm="onConfirmDelete"
  />
</template>
