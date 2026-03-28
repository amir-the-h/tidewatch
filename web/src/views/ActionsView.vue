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
  UnlinkOutline, AddOutline,
} from '@vicons/ionicons5'
import PageHeader from '../components/PageHeader.vue'
import FormDrawer from '../components/FormDrawer.vue'
import { useActionsStore } from '../stores/actions'
import { iconBtn, iconBtnConfirm, activeTag } from '../utils/renderActions'
import { listRules, type Rule } from '../api/rules'
import {
  listPipelines, listTriggers, listActionChains, createActionChain, deleteActionChain,
  type Pipeline, type TriggerInfo, type ActionChain, type ChainCondition,
} from '../api/pipelines'
import type { Action, TriggerType, ActionExecution } from '../api/actions'

const store = useActionsStore()
const msg = useMessage()

// Filter state
const filterText = ref('')
const filterTrigger = ref<TriggerType | null>(null)
const filterActiveOnly = ref(false)

// Form state
const showForm = ref(false)
const saving = ref(false)
const editingId = ref<number | null>(null)
const formName = ref('')
const formTriggerType = ref<TriggerType>('new_tweet')
const formPipelineId = ref<number | null>(null)
const formRuleIds = ref<number[]>([])

// Run state
const runningIds = ref(new Set<number>())

// Executions drawer
const showExecs = ref(false)
const execsActionId = ref<number | null>(null)
const execsActionName = ref('')
const executions = ref<ActionExecution[]>([])
const execsLoading = ref(false)
const execsPage = ref(1)

// Chains state
const expandedRowKeys = ref<number[]>([])
const chainsMap = ref<Record<number, ActionChain[]>>({})
const chainsLoading = ref<Record<number, boolean>>({})

// Add-chain form state
const showAddChain = ref(false)
const addChainActionId = ref<number | null>(null)
const addChainTargetId = ref<number | null>(null)
const addChainConditions = ref<ChainCondition[]>([])
const savingChain = ref(false)

// Select options data
const rules = ref<Rule[]>([])
const pipelines = ref<Pipeline[]>([])
const triggerInfos = ref<TriggerInfo[]>([])

const triggerOptions = computed(() =>
  triggerInfos.value.map(t => ({ label: t.type, value: t.type as TriggerType }))
)

const pipelineOptions = computed(() =>
  pipelines.value.map(p => ({ label: `${p.name} [${p.type}]`, value: p.id }))
)

const ruleOptions = computed(() =>
  rules.value.filter(r => !r.local).map(r => ({ label: `${r.name} (#${r.id})`, value: r.id }))
)

const allActionOptions = computed(() =>
  store.actions.map(a => ({ label: `${a.name} (#${a.id})`, value: a.id }))
)

const filterTriggerOptions = computed(() => triggerOptions.value)

const conditionOpOptions = [
  { label: '==', value: '==' },
  { label: '!=', value: '!=' },
  { label: 'is_empty', value: 'is_empty' },
  { label: 'not_empty', value: 'not_empty' },
  { label: 'contains', value: 'contains' },
  { label: 'not_contains', value: 'not_contains' },
]

const filteredActions = computed(() => {
  let list = store.actions
  if (filterText.value.trim()) {
    const q = filterText.value.toLowerCase()
    list = list.filter(a => a.name.toLowerCase().includes(q))
  }
  if (filterTrigger.value) list = list.filter(a => a.trigger_type === filterTrigger.value)
  if (filterActiveOnly.value) list = list.filter(a => a.active)
  return list
})

onMounted(async () => {
  store.fetchActions()
  store.fetchTwitterStatus().catch(() => {})
  try { rules.value = await listRules() } catch {}
  try { pipelines.value = await listPipelines({ active: true }) } catch {}
  try { triggerInfos.value = await listTriggers() } catch {}
})

// Form helpers
function openCreate() {
  editingId.value = null
  formName.value = ''
  formTriggerType.value = 'new_tweet'
  formPipelineId.value = null
  formRuleIds.value = []
  showForm.value = true
}

function openEdit(action: Action) {
  editingId.value = action.id
  formName.value = action.name
  formTriggerType.value = action.trigger_type
  formPipelineId.value = action.pipeline_id
  formRuleIds.value = [...(action.rule_ids ?? [])]
  showForm.value = true
}

async function onSave() {
  if (!formPipelineId.value) {
    msg.error('Pipeline is required')
    return
  }
  saving.value = true
  try {
    if (editingId.value) {
      await store.updateAction(editingId.value, {
        name: formName.value,
        pipeline_id: formPipelineId.value,
        rule_ids: formRuleIds.value,
      })
      msg.success('Action updated')
    } else {
      await store.createAction({
        name: formName.value,
        trigger_type: formTriggerType.value,
        pipeline_id: formPipelineId.value,
        rule_ids: formRuleIds.value,
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

// Chains
async function onExpandChange(keys: Array<string | number>) {
  const numKeys = keys.map(Number)
  const newKey = numKeys.find(k => !expandedRowKeys.value.includes(k))
  expandedRowKeys.value = numKeys
  if (newKey !== undefined && !chainsMap.value[newKey]) {
    await loadChains(newKey)
  }
}

async function loadChains(actionId: number) {
  chainsLoading.value = { ...chainsLoading.value, [actionId]: true }
  try {
    const chains = await listActionChains(actionId)
    chainsMap.value = { ...chainsMap.value, [actionId]: chains ?? [] }
  } catch (e: any) {
    msg.error(e.message)
  } finally {
    chainsLoading.value = { ...chainsLoading.value, [actionId]: false }
  }
}

function openAddChain(actionId: number) {
  addChainActionId.value = actionId
  addChainTargetId.value = null
  addChainConditions.value = []
  showAddChain.value = true
}

function addConditionRow() {
  addChainConditions.value = [...addChainConditions.value, { key: '', op: '==', value: '' }]
}

function removeConditionRow(idx: number) {
  addChainConditions.value = addChainConditions.value.filter((_, i) => i !== idx)
}

async function onSaveChain() {
  if (!addChainActionId.value || !addChainTargetId.value) {
    msg.error('Target action is required')
    return
  }
  savingChain.value = true
  try {
    const conditions = addChainConditions.value.filter(c => c.key.trim() !== '')
    await createActionChain(addChainActionId.value, {
      target_action_id: addChainTargetId.value,
      conditions,
    })
    msg.success('Chain added')
    showAddChain.value = false
    await loadChains(addChainActionId.value)
  } catch (e: any) {
    msg.error(e.message)
  } finally {
    savingChain.value = false
  }
}

async function onRemoveChain(actionId: number, chainId: number) {
  try {
    await deleteActionChain(actionId, chainId)
    chainsMap.value = {
      ...chainsMap.value,
      [actionId]: (chainsMap.value[actionId] ?? []).filter(c => c.id !== chainId),
    }
    msg.success('Chain removed')
  } catch (e: any) {
    msg.error(e.message)
  }
}

function formatConditions(conditions: ChainCondition[]): string {
  if (!conditions || !conditions.length) return '(always)'
  return conditions.map(c => {
    if (c.op === 'is_empty' || c.op === 'not_empty') return `${c.key} ${c.op}`
    return `${c.key} ${c.op} "${c.value}"`
  }).join(', ')
}

function renderChainsPanel(row: Action) {
  const chains = chainsMap.value[row.id] ?? []
  const loading = chainsLoading.value[row.id]

  return h('div', { style: 'padding: 4px 8px 8px' }, [
    h(NFlex, { align: 'center', justify: 'space-between', style: 'margin-bottom: 8px' }, {
      default: () => [
        h(NText, { depth: 3, style: 'font-size: 13px' }, { default: () => `Chains from ${row.name}` }),
        h(NButton, { size: 'tiny', type: 'primary', ghost: true, onClick: () => openAddChain(row.id) }, {
          icon: () => h(NIcon, null, { default: () => h(AddOutline) }),
          default: () => 'Add Chain',
        }),
      ],
    }),
    loading ? h(NSpin, { size: 'small' }) : null,
    !loading && !chains.length
      ? h(NText, { depth: 3, style: 'font-size: 12px' }, { default: () => 'No chains defined.' })
      : null,
    ...(!loading ? chains.map(chain =>
      h('div', {
        key: chain.id,
        style: 'display: flex; align-items: center; gap: 8px; padding: 4px 0; font-size: 13px;',
      }, [
        h(NText, null, { default: () => `→ Action #${chain.target_action_id}` }),
        h(NTag, { size: 'small', bordered: false, style: 'font-size: 11px; font-family: monospace;' },
          { default: () => formatConditions(chain.conditions) }),
        h(NButton, {
          size: 'tiny', type: 'error', ghost: true, style: 'margin-left: auto',
          onClick: () => onRemoveChain(row.id, chain.id),
        }, { default: () => 'Remove' }),
      ])
    ) : []),
  ])
}

// Table columns
const columns: DataTableColumns<Action> = [
  {
    type: 'expand',
    expandable: () => true,
    renderExpand: (row) => renderChainsPanel(row),
  },
  { title: 'ID', key: 'id', width: 60, sorter: 'default' },
  { title: 'Name', key: 'name', sorter: 'default', ellipsis: { tooltip: true } },
  {
    title: 'Trigger', key: 'trigger_type', width: 160, sorter: 'default',
    render: (row) => h(NTag, { size: 'small', type: 'info' }, { default: () => row.trigger_type }),
  },
  {
    title: 'Pipeline', key: 'pipeline_id', width: 80,
    render: (row) => h(NTag, { size: 'small', round: true }, { default: () => `#${row.pipeline_id}` }),
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
      v-model:value="filterTrigger"
      :options="filterTriggerOptions"
      placeholder="Trigger type"
      clearable
      style="width: 180px"
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
    :expanded-row-keys="expandedRowKeys"
    size="small"
    @update:expanded-row-keys="onExpandChange"
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
    <n-form-item label="Trigger Type">
      <n-select
        v-model:value="formTriggerType"
        :options="triggerOptions"
        :disabled="!!editingId"
        placeholder="Select trigger"
      />
    </n-form-item>
    <n-form-item label="Pipeline">
      <n-select
        v-model:value="formPipelineId"
        :options="pipelineOptions"
        placeholder="Select pipeline"
        clearable
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
  </FormDrawer>

  <!-- Add Chain Drawer -->
  <FormDrawer
    v-model:show="showAddChain"
    title="Add Chain"
    save-label="Add Chain"
    :loading="savingChain"
    @save="onSaveChain"
  >
    <n-form-item label="Target Action">
      <n-select
        v-model:value="addChainTargetId"
        :options="allActionOptions.filter(o => o.value !== addChainActionId)"
        placeholder="Select action to trigger next"
        clearable
      />
    </n-form-item>

    <n-divider style="margin: 8px 0">
      <n-text depth="3" style="font-size: 12px">Conditions (all must pass — leave empty to always trigger)</n-text>
    </n-divider>

    <div
      v-for="(cond, idx) in addChainConditions"
      :key="idx"
      style="display: flex; gap: 6px; margin-bottom: 6px; align-items: flex-start;"
    >
      <n-input v-model:value="cond.key" placeholder="key" style="flex: 2" size="small" />
      <n-select
        v-model:value="cond.op"
        :options="conditionOpOptions"
        style="flex: 1.5; min-width: 110px"
        size="small"
      />
      <n-input
        v-if="cond.op !== 'is_empty' && cond.op !== 'not_empty'"
        v-model:value="cond.value"
        placeholder="value"
        style="flex: 2"
        size="small"
      />
      <div v-else style="flex: 2" />
      <n-button size="small" type="error" ghost @click="removeConditionRow(idx)">×</n-button>
    </div>
    <n-button size="small" dashed style="width: 100%; margin-top: 4px" @click="addConditionRow">
      + Add condition
    </n-button>
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
