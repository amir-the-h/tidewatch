<script setup lang="ts">
import { computed, h, ref } from 'vue'
import { PauseIcon, PencilIcon, PowerIcon, TrashIcon } from '@lucide/vue'
import {
  AlertDialog, Badge, Button, Checkbox, DataTable, IconButton, Input, Select, StatusIndicator,
  type DataTableColumns,
} from '@/components/ui'
import { useToast } from '@/composables/useToast'
import type { NotificationChannel, NotificationProvider } from '../../api/notifications'
import { useNotificationsStore } from '../../stores/notifications'
import ChannelFormSheet from './ChannelFormSheet.vue'
import { providerTypeLabel } from './providerTypes'

defineEmits<{ 'add-provider': [] }>()

const store = useNotificationsStore()
const toast = useToast()

const errorMessage = (e: unknown) => (e instanceof Error ? e.message : String(e))

const providerById = computed(() => {
  const m: Record<number, NotificationProvider> = {}
  store.providers.forEach(p => { m[p.id] = p })
  return m
})

/** The provider-specific delivery target shown in the table, e.g. the Telegram chat ID. */
function target(c: NotificationChannel): string {
  const cfg = c.config ?? {}
  switch (providerById.value[c.provider_id]?.type) {
    case 'telegram': return cfg.chat_id ?? ''
    case 'discord':  return cfg.forum_channel_id ?? ''
    case 'pushover': return cfg.device ?? ''
    default:         return ''
  }
}

// ── Filters ──────────────────────────────────────────────────────────────────

const filterText       = ref('')
const filterProvider   = ref<number | null>(null)
const filterActiveOnly = ref(false)

const providerFilterOptions = computed(() => store.providers.map(p => ({ label: p.name, value: p.id })))

const filteredChannels = computed(() => {
  let list = store.channels
  const q = filterText.value.trim().toLowerCase()
  if (q) list = list.filter(c => c.name.toLowerCase().includes(q))
  if (filterProvider.value) list = list.filter(c => c.provider_id === filterProvider.value)
  if (filterActiveOnly.value) list = list.filter(c => c.active)
  return list
})

const hasFilters = computed(() => !!filterText.value.trim() || !!filterProvider.value || filterActiveOnly.value)

function clearFilters() {
  filterText.value = ''
  filterProvider.value = null
  filterActiveOnly.value = false
}

// ── Create / edit ────────────────────────────────────────────────────────────

const showForm    = ref(false)
const editChannel = ref<NotificationChannel | null>(null)

function openCreate() {
  editChannel.value = null
  showForm.value = true
}

function openEdit(row: NotificationChannel) {
  editChannel.value = row
  showForm.value = true
}

defineExpose({ openCreate })

// ── Activate / delete ────────────────────────────────────────────────────────

async function onToggleActive(row: NotificationChannel) {
  const activating = !row.active
  try {
    if (activating) await store.activateChannel(row.id)
    else await store.deactivateChannel(row.id)
    toast.success(activating ? 'Channel activated' : 'Channel deactivated')
  } catch (e: unknown) {
    toast.error(`Could not ${activating ? 'activate' : 'deactivate'} channel: ${errorMessage(e)}`)
  }
}

const deleteTarget = ref<NotificationChannel | null>(null)
const showDelete   = ref(false)
const deleting     = ref(false)

function askDelete(row: NotificationChannel) {
  deleteTarget.value = row
  showDelete.value = true
}

async function onConfirmDelete() {
  const target = deleteTarget.value
  if (!target) return
  deleting.value = true
  try {
    await store.removeChannel(target.id)
    toast.success('Channel deleted')
    showDelete.value = false
  } catch (e: unknown) {
    toast.error(`Could not delete channel: ${errorMessage(e)}`)
  } finally {
    deleting.value = false
  }
}

// ── Table columns ────────────────────────────────────────────────────────────

const columns: DataTableColumns<NotificationChannel> = [
  {
    title: 'ID', key: 'id', width: 64, sorter: 'default',
    render: (row) => h('span', { class: 'font-mono tabular text-muted-foreground' }, String(row.id)),
  },
  {
    title: 'Name', key: 'name', sorter: 'default', ellipsis: { tooltip: true },
    render: (row) => h('span', { class: 'font-medium' }, row.name),
  },
  {
    title: 'Provider', key: 'provider_id', width: 220, ellipsis: { tooltip: true },
    render: (row) => {
      const p = providerById.value[row.provider_id]
      if (!p) return h('span', { class: 'font-mono tabular text-muted-foreground' }, String(row.provider_id))
      return h('span', { class: 'inline-flex items-center gap-2' }, [
        h('span', null, p.name),
        h(Badge, { variant: 'secondary' }, () => providerTypeLabel[p.type] ?? p.type),
      ])
    },
  },
  {
    title: 'Delivers to', key: 'target', width: 200, ellipsis: { tooltip: true },
    render: (row) => {
      const t = target(row)
      return t
        ? h('span', { class: 'font-mono text-muted-foreground' }, t)
        : h('span', { class: 'text-muted-foreground' }, providerById.value[row.provider_id]?.type === 'pushover' ? 'All devices' : '—')
    },
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
        <Select v-model="filterProvider" :options="providerFilterOptions" placeholder="All providers" clearable />
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
      :data="filteredChannels"
      :loading="store.loading"
      :row-key="(row: NotificationChannel) => row.id"
      :page-size="20"
    >
      <template #empty>
        <div v-if="hasFilters && store.channels.length" class="flex flex-col items-center gap-3 py-8 text-center">
          <p class="m-0 text-muted-foreground">No channels match these filters.</p>
          <Button variant="outline" @click="clearFilters">Clear filters</Button>
        </div>
        <div v-else-if="!store.providers.length" class="flex flex-col items-center gap-3 py-8 text-center">
          <p class="m-0 text-muted-foreground">No providers yet. Add a provider first, then a channel that sends through it.</p>
          <Button @click="$emit('add-provider')">Go to providers</Button>
        </div>
        <div v-else class="flex flex-col items-center gap-3 py-8 text-center">
          <p class="m-0 text-muted-foreground">No channels yet. Add one, then send to it from a notification action.</p>
          <Button @click="openCreate">New channel</Button>
        </div>
      </template>
    </DataTable>
  </div>

  <ChannelFormSheet v-model:open="showForm" :channel="editChannel" />

  <AlertDialog
    v-model:open="showDelete"
    :title="`Delete ${deleteTarget?.name ?? 'channel'}?`"
    description="The channel is removed. This can't be undone."
    action-label="Delete channel"
    :loading="deleting"
    @confirm="onConfirmDelete"
  />
</template>
