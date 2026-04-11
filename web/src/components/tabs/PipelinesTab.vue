<script setup lang="ts">
import { ref, h, onMounted } from 'vue'
import {
  NFlex, NTag, NSpace, NDataTable, NButton, NIcon, NFormItem, NInput, NModal,
  NRadioGroup, NRadio, NSelect, NText, NSpin,
  useMessage,
  type DataTableColumns,
} from 'naive-ui'
import {
  TrashOutline, PauseCircleOutline, CheckmarkCircleOutline, CreateOutline,
  PlayCircleOutline,
} from '@vicons/ionicons5'
import { useRouter } from 'vue-router'
import FormDrawer from '../FormDrawer.vue'
import { iconBtn, iconBtnConfirm, activeTag } from '../../utils/renderActions'
import {
  listPipelines, createPipeline, deletePipeline, activatePipeline, deactivatePipeline,
  runPipeline, getPipelineGraph,
  type Pipeline, type PipelineNode,
} from '../../api/pipelines'

const router = useRouter()
const msg    = useMessage()

// ── Data ─────────────────────────────────────────────────────────────────────

const pipelinesData    = ref<Pipeline[]>([])
const pipelinesLoading = ref(false)

// ── Create form ───────────────────────────────────────────────────────────────

const showPipelineForm = ref(false)
const savingPipeline   = ref(false)
const pipelineFormName = ref('')
const pipelineFormDesc = ref('')

// ── Run modal ─────────────────────────────────────────────────────────────────

const showRunModal    = ref(false)
const runPipelineRow  = ref<Pipeline | null>(null)
const runNewOnly      = ref(false)
const runOrder        = ref<'asc' | 'desc'>('desc')
const runningPipeline = ref(false)
const runStartNodeId  = ref<number | null>(null)
const runGraphNodes   = ref<PipelineNode[]>([])
const runGraphLoading = ref(false)

// ── Functions ─────────────────────────────────────────────────────────────────

async function fetchPipelinesData() {
  pipelinesLoading.value = true
  try { pipelinesData.value = (await listPipelines()) ?? [] }
  catch (e: any) { msg.error(e.message) }
  finally { pipelinesLoading.value = false }
}

function openCreatePipeline() {
  pipelineFormName.value = ''
  pipelineFormDesc.value = ''
  showPipelineForm.value = true
}

async function onSavePipeline() {
  if (!pipelineFormName.value.trim()) { msg.error('Name is required'); return }
  savingPipeline.value = true
  try {
    const p = await createPipeline({ name: pipelineFormName.value, description: pipelineFormDesc.value })
    msg.success('Pipeline created')
    showPipelineForm.value = false
    router.push(`/pipelines/${p.id}/build`)
  } catch (e: any) { msg.error(e.message) }
  finally { savingPipeline.value = false }
}

async function openRunPipeline(row: Pipeline) {
  runPipelineRow.value = row
  runNewOnly.value = false
  runOrder.value = 'desc'
  runStartNodeId.value = null
  runGraphNodes.value = []
  showRunModal.value = true
  runGraphLoading.value = true
  const fetchFor = row.id
  try {
    const graph = await getPipelineGraph(row.id)
    if (runPipelineRow.value?.id !== fetchFor) return
    runGraphNodes.value = graph.nodes
      .filter(n => n.type === 'action' || n.type === 'pipeline')
      .sort((a, b) => a.pos_y !== b.pos_y ? a.pos_y - b.pos_y : a.pos_x - b.pos_x)
  } catch { /* non-fatal — picker stays empty */ }
  finally { runGraphLoading.value = false }
}

async function onConfirmRunPipeline(): Promise<false | void> {
  if (!runPipelineRow.value) return false
  runningPipeline.value = true
  try {
    const res = await runPipeline(
      runPipelineRow.value.id,
      runNewOnly.value,
      runStartNodeId.value ?? undefined,
      runOrder.value === 'asc' ? 'asc' : undefined,
    )
    msg.success(res.async
      ? `Pipeline "${runPipelineRow.value.name}" is running in the background`
      : `Queued ${res.queued} tweets for "${runPipelineRow.value.name}"`)
    showRunModal.value = false
  } catch (e: any) { msg.error(e.message) }
  finally { runningPipeline.value = false }
  return false
}

async function onActivatePipeline(id: number) {
  try {
    await activatePipeline(id)
    const p = pipelinesData.value.find(p => p.id === id)
    if (p) p.active = true
  } catch (e: any) { msg.error(e.message) }
}

async function onDeactivatePipeline(id: number) {
  try {
    await deactivatePipeline(id)
    const p = pipelinesData.value.find(p => p.id === id)
    if (p) p.active = false
  } catch (e: any) { msg.error(e.message) }
}

async function onDeletePipeline(id: number) {
  try {
    await deletePipeline(id)
    pipelinesData.value = pipelinesData.value.filter(p => p.id !== id)
  } catch (e: any) { msg.error(e.message) }
}

// ── Table columns ─────────────────────────────────────────────────────────────

const pipelineColumns: DataTableColumns<Pipeline> = [
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
          ? iconBtn(PlayCircleOutline, 'Run', () => openRunPipeline(row), { type: 'primary' })
          : null,
        h(NButton, { size: 'tiny', onClick: () => router.push(`/pipelines/${row.id}/build`) }, {
          icon: () => h(NIcon, null, { default: () => h(CreateOutline) }),
          default: () => 'Build',
        }),
        row.active
          ? iconBtn(PauseCircleOutline, 'Deactivate', () => onDeactivatePipeline(row.id))
          : iconBtn(CheckmarkCircleOutline, 'Activate', () => onActivatePipeline(row.id), { type: 'primary' }),
        iconBtnConfirm(TrashOutline, 'Delete', 'Delete this pipeline?', () =>
          onDeletePipeline(row.id), { type: 'error' }),
      ],
    }),
  },
]

onMounted(() => { fetchPipelinesData() })
</script>

<template>
  <n-flex justify="end" style="margin-bottom: 12px;">
    <n-button type="primary" size="small" @click="openCreatePipeline">New Pipeline</n-button>
  </n-flex>

  <n-data-table
    :columns="pipelineColumns"
    :data="pipelinesData"
    :loading="pipelinesLoading"
    :bordered="false"
    :row-key="(row: Pipeline) => row.id"
    size="small"
  />

  <!-- New Pipeline Drawer -->
  <FormDrawer v-model:show="showPipelineForm" title="New Pipeline" :loading="savingPipeline" @save="onSavePipeline">
    <n-form-item label="Name">
      <n-input v-model:value="pipelineFormName" placeholder="Pipeline name" />
    </n-form-item>
    <n-form-item label="Description">
      <n-input v-model:value="pipelineFormDesc" placeholder="Optional description" type="textarea" :rows="2" />
    </n-form-item>
  </FormDrawer>

  <!-- Run Pipeline Modal -->
  <n-modal
    v-model:show="showRunModal"
    preset="dialog"
    :title="`Run pipeline — ${runPipelineRow?.name}`"
    positive-text="Run"
    negative-text="Cancel"
    :loading="runningPipeline"
    @positive-click="onConfirmRunPipeline"
  >
    <n-space vertical :size="12" style="margin-top: 8px;">
      <n-form-item label="Start from node" :show-feedback="false">
        <n-select
          v-model:value="runStartNodeId"
          :loading="runGraphLoading"
          :options="runGraphNodes.map(n => ({
            label: n.label || `Node #${n.id}`,
            value: n.id,
          }))"
          placeholder="Start of pipeline (default)"
          clearable
        />
      </n-form-item>
      <n-text v-if="runStartNodeId" depth="3" style="font-size: 12px;">
        Prior detection results for each tweet will be available as context.
      </n-text>
      <n-radio-group v-model:value="runNewOnly">
        <n-space vertical :size="8">
          <n-radio :value="false">All eligible tweets</n-radio>
          <n-radio :value="true">
            {{ runStartNodeId
              ? "Only tweets not yet processed by the selected node's action"
              : "Only tweets without existing executions for this pipeline's actions" }}
          </n-radio>
        </n-space>
      </n-radio-group>
      <n-radio-group v-model:value="runOrder">
        <n-space vertical :size="8">
          <n-radio value="desc">Newest first (default)</n-radio>
          <n-radio value="asc">Oldest first</n-radio>
        </n-space>
      </n-radio-group>
    </n-space>
  </n-modal>
</template>
