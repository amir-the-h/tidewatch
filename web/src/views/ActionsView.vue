<script setup lang="ts">
import { ref, computed, onMounted, h } from 'vue'
import {
  NSpace, NFlex, NTag, NSelect, NInput, NSwitch, NFormItem,
  NCard, NText, NDrawer, NDrawerContent, NDataTable, NTabs, NTabPane,
  NButton, NIcon, NSpin, NPagination, NModal, NRadioGroup, NRadio,
  useMessage,
  type DataTableColumns,
} from 'naive-ui'
import {
  CreateOutline, TrashOutline, PauseCircleOutline, PlayCircleOutline,
  CheckmarkCircleOutline, ListOutline, LogoTwitter, LinkOutline,
  UnlinkOutline, AddOutline,
} from '@vicons/ionicons5'
import { useRoute, useRouter } from 'vue-router'
import PageHeader from '../components/PageHeader.vue'
import FormDrawer from '../components/FormDrawer.vue'
import { useActionsStore } from '../stores/actions'
import { useChainsStore } from '../stores/chains'
import { iconBtn, iconBtnConfirm, activeTag } from '../utils/renderActions'
import { listPipelines, type Pipeline } from '../api/pipelines'
import type { Action, ActionExecution } from '../api/actions'
import type { Chain } from '../api/chains'
import {
  listTriggers, listTriggerChainLinks, createTriggerChainLink,
  deleteTriggerChainLink, activateTriggerChainLink, deactivateTriggerChainLink,
  type TriggerInfo, type TriggerChainLink,
} from '../api/trigger_links'
import { listChains } from '../api/chains'

const route  = useRoute()
const router = useRouter()

// Active tab — driven by ?tab= query param so the chain builder can return to "Chains"
const activeTab = computed({
  get: () => (route.query.tab as string) || 'actions',
  set: (v) => router.replace({ query: { ...route.query, tab: v } }),
})

const actionsStore = useActionsStore()
const chainsStore  = useChainsStore()
const msg = useMessage()

// ── Actions tab ───────────────────────────────────────────────────────────────

const filterText       = ref('')
const filterActiveOnly = ref(false)
const showForm         = ref(false)
const saving           = ref(false)
const editingId        = ref<number | null>(null)
const formName         = ref('')
const formPipelineId   = ref<number | null>(null)

const showExecs       = ref(false)
const execsActionId   = ref<number | null>(null)
const execsActionName = ref('')
const executions      = ref<ActionExecution[]>([])
const execsLoading    = ref(false)
const execsPage       = ref(1)

const pipelines = ref<Pipeline[]>([])

const pipelineOptions = computed(() =>
  pipelines.value.map(p => ({ label: `${p.name} [${p.type}]`, value: p.id }))
)

const filteredActions = computed(() => {
  let list = actionsStore.actions
  if (filterText.value.trim()) {
    const q = filterText.value.toLowerCase()
    list = list.filter(a => a.name.toLowerCase().includes(q))
  }
  if (filterActiveOnly.value) list = list.filter(a => a.active)
  return list
})

function openCreate() {
  editingId.value = null
  formName.value = ''
  formPipelineId.value = null
  showForm.value = true
}

function openEdit(row: Action) {
  editingId.value = row.id
  formName.value = row.name
  formPipelineId.value = row.pipeline_id
  showForm.value = true
}

async function onSave() {
  if (!formName.value.trim()) { msg.error('Name is required'); return }
  if (!formPipelineId.value) { msg.error('Pipeline is required'); return }
  saving.value = true
  try {
    if (editingId.value) {
      await actionsStore.updateAction(editingId.value, { name: formName.value, pipeline_id: formPipelineId.value })
    } else {
      await actionsStore.createAction({ name: formName.value, pipeline_id: formPipelineId.value })
    }
    msg.success(editingId.value ? 'Action updated' : 'Action created')
    showForm.value = false
  } catch (e: any) { msg.error(e.message) }
  finally { saving.value = false }
}

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
    executions.value = await actionsStore.fetchExecutions(execsActionId.value, {
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

const actionColumns: DataTableColumns<Action> = [
  { title: 'ID', key: 'id', width: 60, sorter: 'default' },
  { title: 'Name', key: 'name', sorter: 'default', ellipsis: { tooltip: true } },
  {
    title: 'Pipeline', key: 'pipeline_id', width: 80,
    render: (row) => h(NTag, { size: 'small', round: true }, { default: () => `#${row.pipeline_id}` }),
  },
  {
    title: 'Active', key: 'active', width: 80,
    render: (row) => activeTag(row.active),
  },
  {
    title: 'Created', key: 'created_at', width: 160, sorter: 'default',
    render: (row) => new Date(row.created_at).toLocaleString(),
  },
  {
    title: 'Actions', key: 'actions', width: 220,
    render: (row) => h(NSpace, { size: 4 }, {
      default: () => [
        iconBtn(ListOutline, 'Executions', () => openExecutions(row)),
        iconBtn(CreateOutline, 'Edit', () => openEdit(row)),
        row.active
          ? iconBtn(PauseCircleOutline, 'Deactivate', () =>
              actionsStore.deactivate(row.id).catch((e: any) => msg.error(e.message)))
          : iconBtn(CheckmarkCircleOutline, 'Activate', () =>
              actionsStore.activate(row.id).catch((e: any) => msg.error(e.message)), { type: 'primary' }),
        iconBtnConfirm(TrashOutline, 'Delete', 'Delete this action?', () =>
          actionsStore.remove(row.id).catch((e: any) => msg.error(e.message)), { type: 'error' }),
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

// ── Chains tab ────────────────────────────────────────────────────────────────

const showChainForm      = ref(false)
const savingChain        = ref(false)
const chainFormName      = ref('')
const chainFormDesc      = ref('')

const showRunModal     = ref(false)
const runChainRow      = ref<Chain | null>(null)
const runNewOnly       = ref(false)
const runningChain     = ref(false)

function openCreateChain() {
  chainFormName.value = ''
  chainFormDesc.value = ''
  showChainForm.value = true
}

async function onSaveChain() {
  if (!chainFormName.value.trim()) { msg.error('Name is required'); return }
  savingChain.value = true
  try {
    const c = await chainsStore.create({ name: chainFormName.value, description: chainFormDesc.value })
    msg.success('Chain created')
    showChainForm.value = false
    router.push(`/chains/${c.id}/edit`)
  } catch (e: any) { msg.error(e.message) }
  finally { savingChain.value = false }
}

function openRunChain(row: Chain) {
  runChainRow.value = row
  runNewOnly.value = false
  showRunModal.value = true
}

async function onConfirmRunChain(): Promise<false | void> {
  if (!runChainRow.value) return false
  runningChain.value = true
  try {
    const res = await chainsStore.run(runChainRow.value.id, runNewOnly.value)
    msg.success(`Queued ${res.queued} tweets for "${runChainRow.value.name}"`)
    showRunModal.value = false
  } catch (e: any) {
    msg.error(e.message)
  } finally {
    runningChain.value = false
  }
  return false
}

const chainColumns: DataTableColumns<Chain> = [
  { title: 'ID', key: 'id', width: 60, sorter: 'default' },
  { title: 'Name', key: 'name', sorter: 'default', ellipsis: { tooltip: true } },
  {
    title: 'Active', key: 'active', width: 80,
    render: (row) => activeTag(row.active),
  },
  {
    title: 'Nodes', key: 'node_count', width: 80,
    render: (row) => h(NTag, { size: 'small', round: true }, { default: () => String(row.node_count) }),
  },
  {
    title: 'Created', key: 'created_at', width: 160, sorter: 'default',
    render: (row) => new Date(row.created_at).toLocaleString(),
  },
  {
    title: 'Actions', key: 'actions', width: 240,
    render: (row) => h(NSpace, { size: 4 }, {
      default: () => [
        row.active
          ? iconBtn(PlayCircleOutline, 'Run', () => openRunChain(row), { type: 'primary' })
          : null,
        h(NButton, { size: 'tiny', onClick: () => router.push(`/chains/${row.id}/edit`) }, {
          icon: () => h(NIcon, null, { default: () => h(CreateOutline) }),
          default: () => 'Edit',
        }),
        row.active
          ? iconBtn(PauseCircleOutline, 'Deactivate', () =>
              chainsStore.deactivate(row.id).catch((e: any) => msg.error(e.message)))
          : iconBtn(CheckmarkCircleOutline, 'Activate', () =>
              chainsStore.activate(row.id).catch((e: any) => msg.error(e.message)), { type: 'primary' }),
        iconBtnConfirm(TrashOutline, 'Delete', 'Delete this chain?', () =>
          chainsStore.remove(row.id).catch((e: any) => msg.error(e.message)), { type: 'error' }),
      ],
    }),
  },
]

// ── Triggers tab ──────────────────────────────────────────────────────────────

const triggers      = ref<TriggerInfo[]>([])
const links         = ref<TriggerChainLink[]>([])
const allChains     = ref<Chain[]>([])
const triggersLoading = ref(false)

const showLinkForm   = ref(false)
const linkFormTrigger = ref('')
const linkFormChainId = ref<number | null>(null)
const savingLink      = ref(false)

const linksByTrigger = computed(() => {
  const map: Record<string, TriggerChainLink[]> = {}
  for (const l of links.value) {
    if (!map[l.trigger_type]) map[l.trigger_type] = []
    map[l.trigger_type].push(l)
  }
  return map
})

const chainSelectOptions = computed(() =>
  allChains.value.map(c => ({ label: c.name, value: c.id }))
)

function openAddLink(triggerType: string) {
  linkFormTrigger.value = triggerType
  linkFormChainId.value = null
  showLinkForm.value = true
}

async function onSaveLink() {
  if (!linkFormChainId.value) { msg.error('Chain is required'); return }
  savingLink.value = true
  try {
    const l = await createTriggerChainLink({ trigger_type: linkFormTrigger.value, chain_id: linkFormChainId.value })
    links.value.push(l)
    msg.success('Link created')
    showLinkForm.value = false
  } catch (e: any) { msg.error(e.message) }
  finally { savingLink.value = false }
}

async function removeLink(id: number) {
  try {
    await deleteTriggerChainLink(id)
    links.value = links.value.filter(l => l.id !== id)
  } catch (e: any) { msg.error(e.message) }
}

async function toggleLink(link: TriggerChainLink) {
  try {
    if (link.active) {
      await deactivateTriggerChainLink(link.id)
      link.active = false
    } else {
      await activateTriggerChainLink(link.id)
      link.active = true
    }
  } catch (e: any) { msg.error(e.message) }
}

function chainName(chainId: number) {
  return allChains.value.find(c => c.id === chainId)?.name ?? `#${chainId}`
}

// ── Mount ─────────────────────────────────────────────────────────────────────

onMounted(async () => {
  actionsStore.fetchActions()
  actionsStore.fetchTwitterStatus().catch(() => {})
  try { pipelines.value = await listPipelines({ active: true }) } catch {}
  chainsStore.fetchChains()

  triggersLoading.value = true
  try {
    const [t, l, c] = await Promise.all([listTriggers(), listTriggerChainLinks(), listChains()])
    triggers.value  = t ?? []
    links.value     = l ?? []
    allChains.value = c ?? []
  } catch (e: any) { msg.error(e.message) }
  finally { triggersLoading.value = false }
})
</script>

<template>
  <PageHeader title="Actions" />

  <n-tabs v-model:value="activeTab" type="line" animated style="margin-top: -8px;">

    <!-- ── Actions ────────────────────────────────────────────────────────── -->
    <n-tab-pane name="actions" tab="Actions">

      <!-- Twitter Connection -->
      <n-card size="small" style="margin-bottom: 12px;">
        <n-flex align="center" justify="space-between">
          <n-flex align="center" :size="8">
            <n-icon size="20" :component="LogoTwitter" />
            <template v-if="actionsStore.twitterLoading">
              <n-spin size="small" />
            </template>
            <template v-else-if="actionsStore.twitterStatus">
              <n-text>Connected as <n-text strong>@{{ actionsStore.twitterStatus.username }}</n-text></n-text>
            </template>
            <template v-else>
              <n-text depth="3">Twitter account not connected</n-text>
            </template>
          </n-flex>
          <n-button
            v-if="!actionsStore.twitterLoading && actionsStore.twitterStatus"
            size="small" type="error" ghost
            @click="actionsStore.disconnectTwitter().then(() => msg.success('Disconnected')).catch((e: any) => msg.error(e.message))"
          >
            <template #icon><n-icon><UnlinkOutline /></n-icon></template>
            Disconnect
          </n-button>
          <n-button
            v-else-if="!actionsStore.twitterLoading"
            size="small" type="primary"
            @click="actionsStore.startTwitterAuth().catch((e: any) => msg.error(e.message))"
          >
            <template #icon><n-icon><LinkOutline /></n-icon></template>
            Connect Twitter
          </n-button>
        </n-flex>
      </n-card>

      <!-- Toolbar -->
      <n-flex style="margin-bottom: 12px;" :size="12" align="center" justify="space-between">
        <n-flex :size="12" align="center">
          <n-input v-model:value="filterText" placeholder="Filter by name…" clearable style="width: 200px" />
          <n-switch v-model:value="filterActiveOnly" size="small" />
          <n-text depth="3" style="font-size: 13px">Active only</n-text>
        </n-flex>
        <n-button type="primary" size="small" @click="openCreate">New Action</n-button>
      </n-flex>

      <n-data-table
        :columns="actionColumns"
        :data="filteredActions"
        :loading="actionsStore.loading"
        :bordered="false"
        :row-key="(row: Action) => row.id"
        size="small"
      />
    </n-tab-pane>

    <!-- ── Chains ─────────────────────────────────────────────────────────── -->
    <n-tab-pane name="chains" tab="Chains">
      <n-flex justify="end" style="margin-bottom: 12px;">
        <n-button type="primary" size="small" @click="openCreateChain">New Chain</n-button>
      </n-flex>

      <n-data-table
        :columns="chainColumns"
        :data="chainsStore.chains"
        :loading="chainsStore.loading"
        :bordered="false"
        :row-key="(row: Chain) => row.id"
        size="small"
      />
    </n-tab-pane>

    <!-- ── Triggers ───────────────────────────────────────────────────────── -->
    <n-tab-pane name="triggers" tab="Triggers">
      <n-spin v-if="triggersLoading" />
      <div v-else>
        <n-card
          v-for="trigger in triggers"
          :key="trigger.type"
          size="small"
          style="margin-bottom: 12px;"
        >
          <n-flex align="center" justify="space-between" style="margin-bottom: 8px;">
            <div>
              <n-tag type="info" size="small" style="font-family: monospace; margin-right: 8px;">
                {{ trigger.type }}
              </n-tag>
              <n-text depth="3" style="font-size: 12px;">{{ trigger.description }}</n-text>
            </div>
            <n-button size="tiny" ghost type="primary" @click="openAddLink(trigger.type)">
              <template #icon><n-icon><AddOutline /></n-icon></template>
              Connect chain
            </n-button>
          </n-flex>

          <n-flex v-if="linksByTrigger[trigger.type]?.length" :size="6" wrap>
            <n-flex
              v-for="link in linksByTrigger[trigger.type]"
              :key="link.id"
              align="center" :size="4"
              style="
                background: #313244; border-radius: 4px; padding: 2px 6px;
                font-size: 12px;
              "
            >
              <n-switch :value="link.active" size="small" @update:value="toggleLink(link)" />
              <span :style="{ color: link.active ? '#cdd6f4' : '#6c7086' }">{{ chainName(link.chain_id) }}</span>
              <n-button text size="tiny" style="color: #f38ba8;" @click="removeLink(link.id)">×</n-button>
            </n-flex>
          </n-flex>
          <n-text v-else depth="3" style="font-size: 12px;">No chains connected.</n-text>
        </n-card>
      </div>
    </n-tab-pane>

  </n-tabs>

  <!-- Create/Edit Action Drawer -->
  <FormDrawer
    v-model:show="showForm"
    :title="editingId ? 'Edit Action' : 'New Action'"
    :loading="saving"
    @save="onSave"
  >
    <n-form-item label="Name">
      <n-input v-model:value="formName" placeholder="Action name" />
    </n-form-item>
    <n-form-item label="Pipeline">
      <n-select v-model:value="formPipelineId" :options="pipelineOptions" placeholder="Select pipeline" clearable />
    </n-form-item>
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

  <!-- New Chain Drawer -->
  <FormDrawer v-model:show="showChainForm" title="New Chain" :loading="savingChain" @save="onSaveChain">
    <n-form-item label="Name">
      <n-input v-model:value="chainFormName" placeholder="Chain name" />
    </n-form-item>
    <n-form-item label="Description">
      <n-input v-model:value="chainFormDesc" placeholder="Optional description" type="textarea" :rows="2" />
    </n-form-item>
  </FormDrawer>

  <!-- Run Chain Modal -->
  <n-modal
    v-model:show="showRunModal"
    preset="dialog"
    :title="`Run chain — ${runChainRow?.name}`"
    positive-text="Run"
    negative-text="Cancel"
    :loading="runningChain"
    @positive-click="onConfirmRunChain"
  >
    <n-radio-group v-model:value="runNewOnly" style="margin-top: 8px;">
      <n-space vertical :size="8">
        <n-radio :value="false">All eligible tweets</n-radio>
        <n-radio :value="true">Only tweets without existing executions for this chain's actions</n-radio>
      </n-space>
    </n-radio-group>
  </n-modal>

  <!-- Connect Chain to Trigger Drawer -->
  <FormDrawer
    v-model:show="showLinkForm"
    :title="`Connect chain to ${linkFormTrigger}`"
    save-label="Connect"
    :loading="savingLink"
    @save="onSaveLink"
  >
    <n-form-item label="Chain">
      <n-select
        v-model:value="linkFormChainId"
        :options="chainSelectOptions"
        placeholder="Select a chain"
        clearable
      />
    </n-form-item>
  </FormDrawer>
</template>
