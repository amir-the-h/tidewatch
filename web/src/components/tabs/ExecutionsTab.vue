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

const actions = ref<Action[]>([])

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
