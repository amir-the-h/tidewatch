<script setup lang="ts">
import { ref, computed, h, onMounted } from 'vue'
import { ChevronLeftIcon, ChevronRightIcon, SearchIcon } from '@lucide/vue'
import {
  Button, DataTable, DatePicker, Input, Select, StatusIndicator, type DataTableColumns,
} from '@/components/ui'
import { useToast } from '@/composables/useToast'
import { listAllExecutions, listActions, type ActionExecution, type Action } from '../../api/actions'

const toast = useToast()

const PER_PAGE = 50

// ── Data ─────────────────────────────────────────────────────────────────────

const executions   = ref<ActionExecution[]>([])
const execsLoading = ref(false)
const execsPage    = ref(1)

const actions = ref<Action[]>([])

// ── Filters ───────────────────────────────────────────────────────────────────

const execFilterActionId = ref<number | null>(null)
const execFilterSignal   = ref('')
const execFilterFrom     = ref<number | null>(null)
const execFilterTo       = ref<number | null>(null)
const execFilterStatus   = ref<'success' | 'error' | null>(null)

const actionOptions = computed(() =>
  actions.value.map(a => ({ label: a.name, value: a.id }))
)
const statusOptions = [
  { label: 'Success', value: 'success' as const },
  { label: 'Error', value: 'error' as const },
]

const hasNextPage = computed(() => executions.value.length === PER_PAGE)

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
      per_page:  PER_PAGE,
      action_id: execFilterActionId.value ?? undefined,
      signal_id: Number(execFilterSignal.value.trim()) || undefined,
      from:      execFilterFrom.value ? new Date(execFilterFrom.value).toISOString() : undefined,
      to:        execFilterTo.value   ? new Date(execFilterTo.value).toISOString()   : undefined,
      status:    execFilterStatus.value ?? undefined,
    }) ?? []
  } catch (e: any) { toast.error(`Could not load executions: ${e.message}`) }
  finally { execsLoading.value = false }
}

function onSearch() { execsPage.value = 1; loadExecutions() }
function goToPage(page: number) { execsPage.value = page; loadExecutions() }

onMounted(async () => {
  try { actions.value = await listActions() } catch {}
  loadExecutions()
})

// ── Table columns ─────────────────────────────────────────────────────────────

const execColumns: DataTableColumns<ActionExecution> = [
  {
    title: 'ID', key: 'id', width: 72,
    render: (row) => h('span', { class: 'font-mono tabular text-muted-foreground' }, String(row.id)),
  },
  {
    title: 'Action', key: 'action_name', ellipsis: { tooltip: true },
    render: (row) => actionName(row.action_id),
  },
  {
    title: 'Pipeline', key: 'pipeline_name', ellipsis: { tooltip: true },
    render: (row) => row.pipeline_name || h('span', { class: 'text-muted-foreground' }, '—'),
  },
  {
    title: 'Signal ID', key: 'signal_id', width: 110,
    render: (row) => h('span', { class: 'font-mono tabular' }, String(row.signal_id)),
  },
  {
    title: 'Status', key: 'status', width: 100,
    render: (row) => h(StatusIndicator, { status: row.status === 'success' ? 'success' : 'destructive' },
      () => row.status === 'success' ? 'Success' : 'Error'),
  },
  {
    title: 'Error', key: 'error', ellipsis: { tooltip: true },
    render: (row) => row.error ? h('span', { class: 'font-mono text-small text-destructive' }, row.error) : null,
  },
  {
    title: 'Executed', key: 'executed_at', width: 170,
    render: (row) => h('span', { class: 'tabular text-muted-foreground' }, new Date(row.executed_at).toLocaleString()),
  },
]
</script>

<template>
  <div class="flex flex-col gap-4">
    <form class="flex flex-wrap items-center gap-2" @submit.prevent="onSearch">
      <div class="w-48">
        <Select v-model="execFilterActionId" :options="actionOptions" placeholder="All actions" clearable filterable />
      </div>
      <div class="w-48">
        <Input v-model="execFilterSignal" placeholder="Signal ID" clearable @enter="onSearch" />
      </div>
      <div class="w-52">
        <DatePicker v-model="execFilterFrom" placeholder="From" clearable />
      </div>
      <div class="w-52">
        <DatePicker v-model="execFilterTo" placeholder="To" clearable />
      </div>
      <div class="w-32">
        <Select v-model="execFilterStatus" :options="statusOptions" placeholder="Any status" clearable />
      </div>
      <Button native-type="submit">
        <template #icon><SearchIcon :stroke-width="1.75" /></template>
        Search
      </Button>
    </form>

    <DataTable
      :columns="execColumns"
      :data="executions"
      :loading="execsLoading"
      :row-key="(row: ActionExecution) => row.id"
      empty-text="No executions match these filters. Executions appear here as actions run on signals."
    />

    <nav class="flex items-center justify-end gap-2 text-small" aria-label="Executions pages">
      <Button variant="outline" :disabled="execsPage <= 1 || execsLoading" @click="goToPage(execsPage - 1)">
        <template #icon><ChevronLeftIcon :stroke-width="1.75" /></template>
        Previous
      </Button>
      <span class="tabular text-muted-foreground">Page {{ execsPage }}</span>
      <Button variant="outline" :disabled="!hasNextPage || execsLoading" @click="goToPage(execsPage + 1)">
        Next
        <template #icon><ChevronRightIcon :stroke-width="1.75" /></template>
      </Button>
    </nav>
  </div>
</template>
