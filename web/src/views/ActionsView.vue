<script setup lang="ts">
import { ref, computed, onMounted, h } from 'vue'
import {
  NSpace, NFlex, NTag, NSelect, NInput, NSwitch, NFormItem,
  NCard, NText, NDivider, NDrawer, NDrawerContent, NDataTable,
  NButton, NIcon, NSpin, NPagination,
  useMessage,
  type DataTableColumns,
} from 'naive-ui'
import {
  CreateOutline, TrashOutline, PauseCircleOutline, PlayOutline,
  CheckmarkCircleOutline, ListOutline, LogoTwitter, LinkOutline,
  UnlinkOutline,
} from '@vicons/ionicons5'
import PageHeader from '../components/PageHeader.vue'
import FormDrawer from '../components/FormDrawer.vue'
import { useActionsStore } from '../stores/actions'
import { iconBtn, iconBtnConfirm, activeTag } from '../utils/renderActions'
import { listRules, type Rule } from '../api/rules'
import { listModels, type Model } from '../api/models'
import type { Action, ActionType, ActionExecution } from '../api/actions'

const store = useActionsStore()
const msg = useMessage()

// Filter state
const filterText = ref('')
const filterType = ref<ActionType | null>(null)
const filterActiveOnly = ref(false)

// Form state
const showForm = ref(false)
const saving = ref(false)
const editingId = ref<number | null>(null)
const formName = ref('')
const formType = ref<ActionType>('pipeline')
const formRuleIds = ref<number[]>([])
const formConfig = ref<Record<string, any>>({})

// Run state
const runningIds = ref(new Set<number>())

// Executions drawer
const showExecs = ref(false)
const execsActionId = ref<number | null>(null)
const execsActionName = ref('')
const executions = ref<ActionExecution[]>([])
const execsLoading = ref(false)
const execsPage = ref(1)

// Select options data
const rules = ref<Rule[]>([])
const models = ref<Model[]>([])

const typeOptions = [
  { label: 'Pipeline', value: 'pipeline' },
  { label: 'Retweet', value: 'retweet' },
  { label: 'Like', value: 'like' },
  { label: 'Quote', value: 'quote' },
]

const typeTagProps: Record<ActionType, { type: 'default' | 'info' | 'success' | 'warning' }> = {
  pipeline: { type: 'default' },
  retweet: { type: 'info' },
  like: { type: 'success' },
  quote: { type: 'warning' },
}

const filteredActions = computed(() => {
  let list = store.actions
  if (filterText.value.trim()) {
    const q = filterText.value.toLowerCase()
    list = list.filter(a => a.name.toLowerCase().includes(q))
  }
  if (filterType.value) list = list.filter(a => a.type === filterType.value)
  if (filterActiveOnly.value) list = list.filter(a => a.active)
  return list
})

const ruleOptions = computed(() =>
  rules.value.filter(r => !r.local).map(r => ({ label: `${r.name} (#${r.id})`, value: r.id }))
)

const modelOptions = computed(() =>
  models.value.filter(m => m.active).map(m => ({ label: `${m.name} (#${m.id})`, value: m.id }))
)

onMounted(async () => {
  store.fetchActions()
  store.fetchTwitterStatus().catch(() => {})
  try { rules.value = await listRules() } catch {}
  try { models.value = await listModels() } catch {}
})

// Form helpers
function openCreate() {
  editingId.value = null
  formName.value = ''
  formType.value = 'pipeline'
  formRuleIds.value = []
  formConfig.value = {}
  showForm.value = true
}

function openEdit(action: Action) {
  editingId.value = action.id
  formName.value = action.name
  formType.value = action.type
  formRuleIds.value = [...(action.rule_ids ?? [])]
  formConfig.value = { ...action.config }
  showForm.value = true
}

async function onSave() {
  saving.value = true
  try {
    const config = buildConfig()
    if (editingId.value) {
      await store.updateAction(editingId.value, {
        name: formName.value,
        rule_ids: formRuleIds.value,
        config,
      })
      msg.success('Action updated')
    } else {
      await store.createAction({
        name: formName.value,
        type: formType.value,
        rule_ids: formRuleIds.value,
        config,
      })
      msg.success('Action created')
    }
    showForm.value = false
  } catch (e: any) {
    msg.error(e.message)
  } finally {
    saving.value = false
  }
}

function buildConfig(): Record<string, any> {
  if (formType.value === 'pipeline') {
    return { model_id: formConfig.value.model_id ?? 0 }
  }
  if (formType.value === 'quote') {
    return { template: formConfig.value.template ?? '' }
  }
  return {}
}

async function onRun(action: Action) {
  runningIds.value = new Set([...runningIds.value, action.id])
  try {
    const res = await store.run(action.id)
    msg.success(`Queued ${res.queued} tweets for ${action.name}`)
  } catch (e: any) {
    msg.error(e.message)
  } finally {
    runningIds.value = new Set([...runningIds.value].filter(id => id !== action.id))
  }
}

// Executions
async function openExecutions(action: Action) {
  execsActionId.value = action.id
  execsActionName.value = action.name
  execsPage.value = 1
  showExecs.value = true
  await loadExecutions()
}

async function loadExecutions() {
  if (!execsActionId.value) return
  execsLoading.value = true
  try {
    executions.value = await store.fetchExecutions(execsActionId.value, {
      page: execsPage.value,
      per_page: 20,
    }) ?? []
  } catch (e: any) {
    msg.error(e.message)
  } finally {
    execsLoading.value = false
  }
}

function onExecsPageChange(page: number) {
  execsPage.value = page
  loadExecutions()
}

// Table columns
const columns: DataTableColumns<Action> = [
  { title: 'ID', key: 'id', width: 60, sorter: 'default' },
  { title: 'Name', key: 'name', sorter: 'default', ellipsis: { tooltip: true } },
  {
    title: 'Type', key: 'type', width: 100, sorter: 'default',
    render: (row) => h(NTag, { size: 'small', type: typeTagProps[row.type]?.type ?? 'default' },
      { default: () => row.type }),
  },
  {
    title: 'Active', key: 'active', width: 80,
    render: (row) => activeTag(row.active),
  },
  {
    title: 'Rules', key: 'rule_ids', width: 70,
    render: (row) => h(NTag, { size: 'small', round: true }, { default: () => String(row.rule_ids?.length ?? 0) }),
  },
  {
    title: 'Created', key: 'created_at', width: 160, sorter: 'default',
    render: (row) => new Date(row.created_at).toLocaleString(),
  },
  {
    title: 'Actions', key: 'actions', width: 220,
    render: (row) => h(NSpace, { size: 4 }, {
      default: () => [
        row.active
          ? iconBtn(PlayOutline, 'Run on all tweets', () => onRun(row), { type: 'primary', loading: runningIds.value.has(row.id) })
          : null,
        iconBtn(ListOutline, 'Executions', () => openExecutions(row)),
        iconBtn(CreateOutline, 'Edit', () => openEdit(row)),
        row.active
          ? iconBtn(PauseCircleOutline, 'Deactivate', () =>
              store.deactivate(row.id).catch((e: any) => msg.error(e.message)))
          : iconBtn(CheckmarkCircleOutline, 'Activate', () =>
              store.activate(row.id).catch((e: any) => msg.error(e.message)), { type: 'primary' }),
        iconBtnConfirm(TrashOutline, 'Delete', 'Delete this action?', () =>
          store.remove(row.id).catch((e: any) => msg.error(e.message)), { type: 'error' }),
      ],
    }),
  },
]

const execColumns: DataTableColumns<ActionExecution> = [
  { title: 'ID', key: 'id', width: 60 },
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
  <PageHeader title="Actions" add-label="New Action" @add="openCreate" />

  <!-- Twitter Connection -->
  <n-card size="small" style="margin: 12px 0">
    <n-flex align="center" justify="space-between">
      <n-flex align="center" :size="8">
        <n-icon size="20" :component="LogoTwitter" />
        <template v-if="store.twitterLoading">
          <n-spin size="small" />
        </template>
        <template v-else-if="store.twitterStatus">
          <n-text>Connected as <n-text strong>@{{ store.twitterStatus.username }}</n-text></n-text>
        </template>
        <template v-else>
          <n-text depth="3">Twitter account not connected</n-text>
        </template>
      </n-flex>
      <n-button
        v-if="!store.twitterLoading && store.twitterStatus"
        size="small"
        type="error"
        ghost
        @click="store.disconnectTwitter().then(() => msg.success('Disconnected')).catch((e: any) => msg.error(e.message))"
      >
        <template #icon><n-icon><UnlinkOutline /></n-icon></template>
        Disconnect
      </n-button>
      <n-button
        v-else-if="!store.twitterLoading"
        size="small"
        type="primary"
        @click="store.startTwitterAuth().catch((e: any) => msg.error(e.message))"
      >
        <template #icon><n-icon><LinkOutline /></n-icon></template>
        Connect Twitter
      </n-button>
    </n-flex>
  </n-card>

  <!-- Filters -->
  <n-flex style="margin-bottom: 12px" :size="12" align="center">
    <n-input v-model:value="filterText" placeholder="Filter by name…" clearable style="width: 200px" />
    <n-select
      v-model:value="filterType"
      :options="typeOptions"
      placeholder="Type"
      clearable
      style="width: 140px"
    />
    <n-switch v-model:value="filterActiveOnly" size="small" />
    <n-text depth="3" style="font-size: 13px">Active only</n-text>
  </n-flex>

  <!-- Table -->
  <n-data-table
    :columns="columns"
    :data="filteredActions"
    :loading="store.loading"
    :bordered="false"
    :row-key="(row: Action) => row.id"
    size="small"
  />

  <!-- Create/Edit Drawer -->
  <FormDrawer
    v-model:show="showForm"
    :title="editingId ? 'Edit Action' : 'New Action'"
    :loading="saving"
    @save="onSave"
  >
    <n-form-item label="Name">
      <n-input v-model:value="formName" placeholder="Action name" />
    </n-form-item>
    <n-form-item label="Type">
      <n-select
        v-model:value="formType"
        :options="typeOptions"
        :disabled="!!editingId"
      />
    </n-form-item>
    <n-form-item label="Rule Filters">
      <n-select
        v-model:value="formRuleIds"
        :options="ruleOptions"
        multiple
        clearable
        placeholder="All rules (no filter)"
      />
    </n-form-item>

    <n-divider style="margin: 8px 0" />

    <!-- Pipeline config -->
    <n-form-item v-if="formType === 'pipeline'" label="Model">
      <n-select
        :value="formConfig.model_id ?? null"
        @update:value="(v: number) => formConfig.model_id = v"
        :options="modelOptions"
        placeholder="Select model"
      />
    </n-form-item>

    <!-- Quote config -->
    <n-form-item v-if="formType === 'quote'" label="Quote Text Template">
      <n-input
        :value="formConfig.template ?? ''"
        @update:value="(v: string) => formConfig.template = v"
        type="textarea"
        placeholder="Quote text…"
        :rows="3"
      />
    </n-form-item>

    <!-- Retweet / Like: no extra config -->
    <n-text v-if="formType === 'retweet' || formType === 'like'" depth="3" style="font-size: 13px">
      No additional configuration needed for {{ formType }} actions.
    </n-text>
  </FormDrawer>

  <!-- Executions Drawer -->
  <n-drawer v-model:show="showExecs" width="min(640px, calc(100vw - 16px))">
    <n-drawer-content :title="`Executions — ${execsActionName}`" closable>
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
          :page-size="20"
          :item-count="executions.length === 20 ? execsPage * 20 + 1 : (execsPage - 1) * 20 + executions.length"
          @update:page="onExecsPageChange"
        />
      </n-flex>
    </n-drawer-content>
  </n-drawer>
</template>
