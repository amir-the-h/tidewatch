<script setup lang="ts">
import { ref, computed, h, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { PauseIcon, PlayIcon, PowerIcon, TrashIcon, WorkflowIcon } from '@lucide/vue'
import {
  AlertDialog, Badge, Button, DataTable, Dialog, FormField, IconButton, Input, RadioGroup,
  Select, Sheet, StatusIndicator, Textarea, type DataTableColumns,
} from '@/components/ui'
import { useToast } from '@/composables/useToast'
import {
  listPipelines, createPipeline, deletePipeline, activatePipeline, deactivatePipeline,
  runPipeline, getPipelineGraph,
  type Pipeline, type PipelineNode,
} from '../../api/pipelines'

const router = useRouter()
const toast  = useToast()

// ── Data ─────────────────────────────────────────────────────────────────────

const pipelinesData    = ref<Pipeline[]>([])
const pipelinesLoading = ref(false)
const loadError        = ref('')

// ── Create form ───────────────────────────────────────────────────────────────

const showPipelineForm = ref(false)
const savingPipeline   = ref(false)
const pipelineFormName = ref('')
const pipelineFormDesc = ref('')

// ── Run dialog ────────────────────────────────────────────────────────────────

const showRunModal    = ref(false)
const runPipelineRow  = ref<Pipeline | null>(null)
const runNewOnly      = ref(false)
const runOrder        = ref<'asc' | 'desc'>('desc')
const runningPipeline = ref(false)
const runStartNodeId  = ref<number | null>(null)
const runGraphNodes   = ref<PipelineNode[]>([])
const runGraphLoading = ref(false)

const runNodeOptions = computed(() =>
  runGraphNodes.value.map(n => ({ label: n.label || `Node #${n.id}`, value: n.id }))
)
const runScopeOptions = computed(() => [
  { label: 'All eligible tweets', value: false },
  {
    label: runStartNodeId.value
      ? "Only tweets not yet processed by the selected node's action"
      : "Only tweets without existing executions for this pipeline's actions",
    value: true,
  },
])
const runOrderOptions = [
  { label: 'Newest first', value: 'desc' as const },
  { label: 'Oldest first', value: 'asc' as const },
]

// ── Delete confirm ────────────────────────────────────────────────────────────

const deleteTarget = ref<Pipeline | null>(null)
const showDelete   = ref(false)
const deleting     = ref(false)

// ── Functions ─────────────────────────────────────────────────────────────────

async function fetchPipelinesData() {
  pipelinesLoading.value = true
  loadError.value = ''
  try { pipelinesData.value = (await listPipelines()) ?? [] }
  catch (e: any) { loadError.value = e.message }
  finally { pipelinesLoading.value = false }
}

function openCreate() {
  pipelineFormName.value = ''
  pipelineFormDesc.value = ''
  showPipelineForm.value = true
}

defineExpose({ openCreate })

async function onSavePipeline() {
  if (!pipelineFormName.value.trim()) { toast.error('Enter a name for the pipeline'); return }
  savingPipeline.value = true
  try {
    const p = await createPipeline({ name: pipelineFormName.value, description: pipelineFormDesc.value })
    toast.success('Pipeline created')
    showPipelineForm.value = false
    router.push(`/pipelines/${p.id}/build`)
  } catch (e: any) { toast.error(`Could not create pipeline: ${e.message}`) }
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

async function onConfirmRunPipeline() {
  if (!runPipelineRow.value) return
  runningPipeline.value = true
  try {
    const res = await runPipeline(
      runPipelineRow.value.id,
      runNewOnly.value,
      runStartNodeId.value ?? undefined,
      runOrder.value === 'asc' ? 'asc' : undefined,
    )
    toast.success(res.async
      ? `Backfill started for "${runPipelineRow.value.name}"`
      : `Queued ${res.queued} tweets for "${runPipelineRow.value.name}"`)
    showRunModal.value = false
  } catch (e: any) { toast.error(`Could not start backfill: ${e.message}`) }
  finally { runningPipeline.value = false }
}

async function onToggleActive(row: Pipeline) {
  try {
    if (row.active) await deactivatePipeline(row.id)
    else await activatePipeline(row.id)
    row.active = !row.active
    toast.success(row.active ? 'Pipeline activated' : 'Pipeline deactivated')
  } catch (e: any) { toast.error(e.message) }
}

function askDelete(row: Pipeline) {
  deleteTarget.value = row
  showDelete.value = true
}

async function onConfirmDelete() {
  const target = deleteTarget.value
  if (!target) return
  deleting.value = true
  try {
    await deletePipeline(target.id)
    pipelinesData.value = pipelinesData.value.filter(p => p.id !== target.id)
    toast.success('Pipeline deleted')
    showDelete.value = false
  } catch (e: any) { toast.error(`Could not delete pipeline: ${e.message}`) }
  finally { deleting.value = false }
}

// ── Table columns ─────────────────────────────────────────────────────────────

const pipelineColumns: DataTableColumns<Pipeline> = [
  {
    title: 'ID', key: 'id', width: 64, sorter: 'default',
    render: (row) => h('span', { class: 'font-mono tabular text-muted-foreground' }, String(row.id)),
  },
  {
    title: 'Name', key: 'name', sorter: 'default', ellipsis: { tooltip: true },
    render: (row) => h('span', { class: 'font-medium' }, row.name),
  },
  {
    title: 'Status', key: 'active', width: 110,
    render: (row) => h(StatusIndicator, { status: row.active ? 'success' : 'idle' },
      () => row.active ? 'Active' : 'Inactive'),
  },
  {
    title: 'Nodes', key: 'node_count', width: 80,
    render: (row) => h(Badge, { variant: 'secondary' }, () => String(row.node_count)),
  },
  {
    title: 'Created', key: 'created_at', width: 170, sorter: 'default',
    render: (row) => h('span', { class: 'tabular text-muted-foreground' }, new Date(row.created_at).toLocaleString()),
  },
  {
    title: '', key: 'actions', width: 210, align: 'right',
    render: (row) => h('div', { class: 'flex items-center justify-end gap-1' }, [
      row.active
        ? h(Button, { variant: 'outline', onClick: () => openRunPipeline(row) }, {
          icon: () => h(PlayIcon, { strokeWidth: 1.75 }),
          default: () => 'Run',
        })
        : null,
      h(Button, { variant: 'outline', onClick: () => router.push(`/pipelines/${row.id}/build`) }, {
        icon: () => h(WorkflowIcon, { strokeWidth: 1.75 }),
        default: () => 'Build',
      }),
      h(IconButton, {
        icon: row.active ? PauseIcon : PowerIcon,
        label: row.active ? 'Deactivate' : 'Activate',
        onClick: () => onToggleActive(row),
      }),
      h(IconButton, { icon: TrashIcon, label: 'Delete', onClick: () => askDelete(row) }),
    ]),
  },
]

onMounted(() => { fetchPipelinesData() })
</script>

<template>
  <div
    v-if="loadError"
    class="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-destructive p-4 text-small"
  >
    <span class="text-destructive">Could not load pipelines: {{ loadError }}</span>
    <Button variant="outline" @click="fetchPipelinesData">Retry</Button>
  </div>

  <DataTable
    v-else
    :columns="pipelineColumns"
    :data="pipelinesData"
    :loading="pipelinesLoading"
    :row-key="(row: Pipeline) => row.id"
  >
    <template #empty>
      <div class="flex flex-col items-center gap-3 py-8 text-center">
        <p class="m-0 text-muted-foreground">No pipelines yet. Create one to start processing tweets from a trigger.</p>
        <Button @click="openCreate">New pipeline</Button>
      </div>
    </template>
  </DataTable>

  <!-- New pipeline -->
  <Sheet v-model:open="showPipelineForm" title="New pipeline" description="You'll add actions in the builder next.">
    <form class="flex flex-col gap-3 p-4" @submit.prevent="onSavePipeline">
      <FormField label="Name" for="pipeline-name" required>
        <Input id="pipeline-name" v-model="pipelineFormName" placeholder="Pipeline name" />
      </FormField>
      <FormField label="Description" for="pipeline-desc">
        <Textarea id="pipeline-desc" v-model="pipelineFormDesc" placeholder="Optional description" :rows="2" />
      </FormField>
    </form>
    <template #footer>
      <Button variant="outline" size="default" @click="showPipelineForm = false">Cancel</Button>
      <Button size="default" :loading="savingPipeline" @click="onSavePipeline">Create and open builder</Button>
    </template>
  </Sheet>

  <!-- Run backfill -->
  <Dialog
    v-model:open="showRunModal"
    :title="`Run ${runPipelineRow?.name ?? 'pipeline'}`"
    description="Dispatch stored tweets through this pipeline."
  >
    <div class="flex flex-col gap-4">
      <FormField
        label="Start from"
        :description="runStartNodeId ? 'Prior results for each tweet are available as context.' : undefined"
      >
        <Select
          v-model="runStartNodeId"
          :options="runNodeOptions"
          :loading="runGraphLoading"
          placeholder="Start of pipeline"
          clearable
        />
      </FormField>
      <FormField label="Tweets">
        <RadioGroup v-model="runNewOnly" :options="runScopeOptions" name="run-scope" />
      </FormField>
      <FormField label="Order">
        <RadioGroup v-model="runOrder" :options="runOrderOptions" name="run-order" />
      </FormField>
    </div>
    <template #footer>
      <Button variant="outline" :disabled="runningPipeline" @click="showRunModal = false">Cancel</Button>
      <Button :loading="runningPipeline" @click="onConfirmRunPipeline">
        <template #icon><PlayIcon :stroke-width="1.75" /></template>
        Run backfill
      </Button>
    </template>
  </Dialog>

  <!-- Delete confirm -->
  <AlertDialog
    v-model:open="showDelete"
    :title="`Delete ${deleteTarget?.name ?? 'pipeline'}?`"
    description="The pipeline and its graph are removed. This can't be undone."
    action-label="Delete pipeline"
    :loading="deleting"
    @confirm="onConfirmDelete"
  />
</template>
