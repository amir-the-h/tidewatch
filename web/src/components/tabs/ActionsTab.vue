<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue'
import { PauseIcon, PencilIcon, PowerIcon, ServerIcon, TrashIcon } from '@lucide/vue'
import {
  AlertDialog, Badge, Button, Card, Checkbox, DataTable, IconButton, Input, Select, Skeleton,
  StatusIndicator, type DataTableColumns,
} from '@/components/ui'
import { useToast } from '@/composables/useToast'
import {
  listActions, deleteAction, activateAction, deactivateAction,
  type Action, type ActionType,
} from '../../api/actions'
import { useActionsStore } from '../../stores/actions'
import ActionFormSheet from '../actions/ActionFormSheet.vue'
import ActionBackendsDialog from '../actions/ActionBackendsDialog.vue'

const toast = useToast()
const actionsStore = useActionsStore()

const errorMessage = (e: unknown) => (e instanceof Error ? e.message : String(e))

// ── Data ─────────────────────────────────────────────────────────────────────

const actionsData    = ref<Action[]>([])
const actionsLoading = ref(false)
const loadError      = ref('')

async function fetchActions() {
  actionsLoading.value = true
  loadError.value = ''
  try { actionsData.value = await listActions() }
  catch (e: unknown) { loadError.value = errorMessage(e) }
  finally { actionsLoading.value = false }
}

// ── Filters ──────────────────────────────────────────────────────────────────

const typeOptions: { label: string; value: ActionType }[] = [
  { label: 'ONNX',         value: 'onnx'         },
  { label: 'Ollama',       value: 'ollama'       },
  { label: 'Internal',     value: 'internal'     },
  { label: 'Notification', value: 'notification' },
]

const filterText       = ref('')
const filterType       = ref<ActionType | null>(null)
const filterActiveOnly = ref(false)

const filteredActions = computed(() => {
  let list = actionsData.value
  const q = filterText.value.trim().toLowerCase()
  if (q) list = list.filter(a => a.name.toLowerCase().includes(q))
  if (filterType.value) list = list.filter(a => a.type === filterType.value)
  if (filterActiveOnly.value) list = list.filter(a => a.active)
  return list
})

const hasFilters = computed(() => !!filterText.value.trim() || !!filterType.value || filterActiveOnly.value)

function clearFilters() {
  filterText.value = ''
  filterType.value = null
  filterActiveOnly.value = false
}

// ── Create / edit ────────────────────────────────────────────────────────────

const showForm   = ref(false)
const editAction = ref<Action | null>(null)

function openCreate() {
  editAction.value = null
  showForm.value = true
}

function openEdit(row: Action) {
  editAction.value = row
  showForm.value = true
}

defineExpose({ openCreate })

// ── Activate / delete ────────────────────────────────────────────────────────

async function onToggleActive(row: Action) {
  try {
    if (row.active) await deactivateAction(row.id)
    else await activateAction(row.id)
    row.active = !row.active
    toast.success(row.active ? 'Action activated' : 'Action deactivated')
  } catch (e: unknown) { toast.error(`Could not ${row.active ? 'deactivate' : 'activate'} action: ${errorMessage(e)}`) }
}

const deleteTarget = ref<Action | null>(null)
const showDelete   = ref(false)
const deleting     = ref(false)

function askDelete(row: Action) {
  deleteTarget.value = row
  showDelete.value = true
}

async function onConfirmDelete() {
  const target = deleteTarget.value
  if (!target) return
  deleting.value = true
  try {
    await deleteAction(target.id)
    actionsData.value = actionsData.value.filter(a => a.id !== target.id)
    toast.success('Action deleted')
    showDelete.value = false
  } catch (e: unknown) { toast.error(`Could not delete action: ${errorMessage(e)}`) }
  finally { deleting.value = false }
}

// ── Ollama backend priority ──────────────────────────────────────────────────

const backendsAction   = ref<Action | null>(null)
const showBackends     = ref(false)

function openBackends(row: Action) {
  backendsAction.value = row
  showBackends.value = true
}

// ── Twitter account ──────────────────────────────────────────────────────────

const showDisconnect = ref(false)
const disconnecting  = ref(false)

async function onConnectTwitter() {
  try { await actionsStore.startTwitterAuth() }
  catch (e: unknown) { toast.error(`Could not start Twitter sign-in: ${errorMessage(e)}`) }
}

async function onConfirmDisconnect() {
  disconnecting.value = true
  try {
    await actionsStore.disconnectTwitter()
    toast.success('Twitter account disconnected')
    showDisconnect.value = false
  } catch (e: unknown) { toast.error(`Could not disconnect: ${errorMessage(e)}`) }
  finally { disconnecting.value = false }
}

// ── Table columns ────────────────────────────────────────────────────────────

const typeLabel = Object.fromEntries(typeOptions.map(o => [o.value, o.label])) as Record<ActionType, string>

const columns: DataTableColumns<Action> = [
  {
    title: 'ID', key: 'id', width: 64, sorter: 'default',
    render: (row) => h('span', { class: 'font-mono tabular text-muted-foreground' }, String(row.id)),
  },
  {
    title: 'Name', key: 'name', sorter: 'default', ellipsis: { tooltip: true },
    render: (row) => h('span', { class: 'font-medium' }, row.name),
  },
  {
    title: 'Type', key: 'type', width: 160, sorter: 'default',
    render: (row) => {
      const uc = row.type === 'ollama' ? row.config?.usecase : null
      return h('div', { class: 'flex items-center gap-1' }, [
        h(Badge, { variant: 'secondary' }, () => typeLabel[row.type] ?? row.type),
        uc && uc !== 'custom' ? h(Badge, { variant: 'outline' }, () => uc) : null,
      ])
    },
  },
  {
    title: 'Status', key: 'active', width: 110,
    render: (row) => h(StatusIndicator, { status: row.active ? 'success' : 'idle' },
      () => row.active ? 'Active' : 'Inactive'),
  },
  {
    title: 'Created', key: 'created_at', width: 170, sorter: 'default',
    render: (row) => h('span', { class: 'tabular text-muted-foreground' }, new Date(row.created_at).toLocaleString()),
  },
  {
    title: '', key: 'actions', width: 150, align: 'right',
    render: (row) => h('div', { class: 'flex items-center justify-end gap-1' }, [
      row.type === 'ollama'
        ? h(IconButton, { icon: ServerIcon, label: 'Backend priority', onClick: () => openBackends(row) })
        : null,
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

onMounted(() => {
  fetchActions()
  actionsStore.fetchTwitterStatus().catch(() => {})
})
</script>

<template>
  <div class="flex flex-col gap-3">
    <Card title="Twitter account" description="Used by the twitter_retweet, twitter_like and twitter_quote handlers.">
      <template #action>
        <template v-if="!actionsStore.twitterLoading">
          <Button v-if="actionsStore.twitterStatus" variant="outline" @click="showDisconnect = true">Disconnect</Button>
          <Button v-else variant="outline" @click="onConnectTwitter">Connect Twitter</Button>
        </template>
      </template>
      <Skeleton v-if="actionsStore.twitterLoading" class="h-5 w-48" />
      <StatusIndicator v-else-if="actionsStore.twitterStatus" status="success">
        Connected as @{{ actionsStore.twitterStatus.username }}
      </StatusIndicator>
      <StatusIndicator v-else status="idle">Not connected</StatusIndicator>
    </Card>

    <div class="flex flex-wrap items-center gap-2">
      <div class="w-64">
        <Input v-model="filterText" placeholder="Search name" clearable />
      </div>
      <div class="w-44">
        <Select v-model="filterType" :options="typeOptions" placeholder="All types" clearable />
      </div>
      <Checkbox v-model="filterActiveOnly">Active only</Checkbox>
    </div>

    <div
      v-if="loadError"
      class="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-destructive p-4 text-small"
    >
      <span class="text-destructive">Could not load actions: {{ loadError }}</span>
      <Button variant="outline" @click="fetchActions">Retry</Button>
    </div>

    <DataTable
      v-else
      :columns="columns"
      :data="filteredActions"
      :loading="actionsLoading"
      :row-key="(row: Action) => row.id"
      :page-size="20"
    >
      <template #empty>
        <div v-if="hasFilters && actionsData.length" class="flex flex-col items-center gap-3 py-8 text-center">
          <p class="m-0 text-muted-foreground">No actions match these filters.</p>
          <Button variant="outline" @click="clearFilters">Clear filters</Button>
        </div>
        <div v-else class="flex flex-col items-center gap-3 py-8 text-center">
          <p class="m-0 text-muted-foreground">No actions yet. Create one, then add it to a pipeline to run it on tweets.</p>
          <Button @click="openCreate">New action</Button>
        </div>
      </template>
    </DataTable>
  </div>

  <ActionFormSheet v-model:open="showForm" :action="editAction" :actions="actionsData" @saved="fetchActions" />

  <ActionBackendsDialog
    v-if="backendsAction"
    v-model:open="showBackends"
    :action-id="backendsAction.id"
    :action-name="backendsAction.name"
  />

  <AlertDialog
    v-model:open="showDelete"
    :title="`Delete ${deleteTarget?.name ?? 'action'}?`"
    description="The action is removed. This can't be undone."
    action-label="Delete action"
    :loading="deleting"
    @confirm="onConfirmDelete"
  />

  <AlertDialog
    v-model:open="showDisconnect"
    :title="`Disconnect @${actionsStore.twitterStatus?.username ?? ''}?`"
    description="Retweet, like and quote actions fail until an account is connected again."
    action-label="Disconnect"
    :loading="disconnecting"
    @confirm="onConfirmDisconnect"
  />
</template>
