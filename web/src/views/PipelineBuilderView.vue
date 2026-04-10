<script setup lang="ts">
import { ref, computed, onMounted, markRaw } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  VueFlow, useVueFlow, type Node, type Edge, type NodeTypesObject, type NodeMouseEvent,
} from '@vue-flow/core'
import '@vue-flow/core/dist/style.css'
import '@vue-flow/core/dist/theme-default.css'
import {
  NButton, NText, NInput, NSelect, NDivider, NInputNumber,
  NSpin, NScrollbar, NIcon, useMessage,
  NModal, NSpace, NRadioGroup, NRadio,
} from 'naive-ui'
import { ArrowBackOutline } from '@vicons/ionicons5'
import StartNode from '../components/pipeline/StartNode.vue'
import ActionNode from '../components/pipeline/ActionNode.vue'
import ConditionNode from '../components/pipeline/ConditionNode.vue'
import PipelineNodeComponent from '../components/pipeline/PipelineNode.vue'
import {
  getPipeline, getPipelineGraph, savePipelineGraph, listPipelines, runPipeline,
  type Pipeline, type PipelineCondition,
} from '../api/pipelines'
import { listActions } from '../api/actions'
import type { Action } from '../api/actions'
import { listRules, type Rule } from '../api/rules'

const route  = useRoute()
const router = useRouter()
const msg    = useMessage()
const pipelineId = Number(route.params.id)

const {
  nodes, edges,
  setNodes, setEdges,
  addNodes, addEdges,
  removeNodes,
  onNodesChange, onEdgesChange,
  applyNodeChanges, applyEdgeChanges,
  findNode,
} = useVueFlow()

const nodeTypes: NodeTypesObject = {
  start:     markRaw(StartNode) as any,
  action:    markRaw(ActionNode) as any,
  condition: markRaw(ConditionNode) as any,
  pipeline:  markRaw(PipelineNodeComponent) as any,
}

const pipeline      = ref<Pipeline | null>(null)
const actions       = ref<Action[]>([])
const pipelines     = ref<Pipeline[]>([])
const loading       = ref(true)
const saving        = ref(false)
const isDirty       = ref(false)

// Run-from-node state (Option B — invoked from graph editor node buttons)
const showRunFromNodeModal = ref(false)
const runFromNodeId        = ref<string | null>(null)
const runFromNodeLabel     = ref('')
const runFromNodeNewOnly   = ref(false)
const runningFromNode      = ref(false)

function withRunCallback(data: Record<string, any>): Record<string, any> {
  return { ...data, onRunFromNode: handleRunFromNode }
}

function handleRunFromNode(nodeId: string) {
  const node = findNode(nodeId)
  runFromNodeId.value    = nodeId
  runFromNodeLabel.value = node?.data?.label ?? `Node #${nodeId}`
  runFromNodeNewOnly.value = false
  showRunFromNodeModal.value = true
}

async function onConfirmRunFromNode(): Promise<false | void> {
  if (!runFromNodeId.value) return false
  const numId = parseInt(runFromNodeId.value, 10)
  if (isNaN(numId)) { msg.error('Invalid node id'); return false }
  runningFromNode.value = true
  try {
    const res = await runPipeline(pipelineId, runFromNodeNewOnly.value, numId)
    msg.success(res.async
      ? `Running from "${runFromNodeLabel.value}" in the background`
      : `Queued ${res.queued} tweets from "${runFromNodeLabel.value}"`)
    showRunFromNodeModal.value = false
  } catch (e: any) { msg.error(e.message) }
  finally { runningFromNode.value = false }
  return false
}

const searchText    = ref('')
const selectedNodeId = ref<string | null>(null)

const panel = ref<'palette' | 'edit'>('palette')

const selectedNode = computed(() =>
  selectedNodeId.value ? findNode(selectedNodeId.value) : undefined
)

const editLabel          = ref('')
const editActionId       = ref<number | null>(null)
const editSubPipelineId  = ref<number | null>(null)
const editConditions     = ref<PipelineCondition[]>([])

const currentPipelineId  = computed(() => Number(route.params.id))
const availablePipelines = computed(() => pipelines.value.filter(p => p.id !== currentPipelineId.value))

const filteredActions = computed(() =>
  actions.value.filter(a =>
    !searchText.value || a.name.toLowerCase().includes(searchText.value.toLowerCase())
  )
)

const conditionOpOptions = [
  { label: '==', value: '==' }, { label: '!=', value: '!=' },
  { label: '>', value: '>' }, { label: '>=', value: '>=' },
  { label: '<', value: '<' }, { label: '<=', value: '<=' },
  { label: 'contains', value: 'contains' }, { label: 'not_contains', value: 'not_contains' },
  { label: 'is_empty', value: 'is_empty' }, { label: 'not_empty', value: 'not_empty' },
  { label: 'rule_matches', value: 'rule_matches' },
  { label: 'rule_not_matches', value: 'rule_not_matches' },
]

const numericOps = new Set(['>', '>=', '<', '<='])
const ruleOps = new Set(['rule_matches', 'rule_not_matches'])
const availableRules = ref<Rule[]>([])
const ruleOptions = computed(() =>
  availableRules.value.map(r => ({ label: r.name, value: String(r.id) }))
)

onMounted(async () => {
  try {
    const [p, graph, acts, pipes] = await Promise.all([
      getPipeline(pipelineId),
      getPipelineGraph(pipelineId),
      listActions(),
      listPipelines(),
    ])
    pipeline.value  = p
    actions.value   = acts
    pipelines.value = pipes

    const vfNodes: Node[] = graph.nodes.map(n => {
      const baseData = {
        label:           n.label || (n.type === 'start' ? 'Start' : ''),
        actionId:        n.action_id,
        subPipelineId:   n.sub_pipeline_id,
        subPipelineName: n.sub_pipeline_id
          ? pipes.find(pp => pp.id === n.sub_pipeline_id)?.name
          : undefined,
        conditions:      n.conditions ?? [],
        pipelineType:    n.action_id
          ? acts.find(a => a.id === n.action_id)?.name
          : undefined,
      }
      return {
        id:       String(n.id),
        type:     n.type,
        position: { x: n.pos_x, y: n.pos_y },
        data: (n.type === 'action' || n.type === 'pipeline') ? withRunCallback(baseData) : baseData,
      }
    })

    const vfEdges: Edge[] = graph.edges.map(e => ({
      id:           String(e.id),
      source:       String(e.source_node_id),
      sourceHandle: e.source_handle,
      target:       String(e.target_node_id),
      animated:     e.source_handle === 'pass',
      style:        { stroke: e.source_handle === 'fail' ? '#f38ba8' : e.source_handle === 'pass' ? '#a6e3a1' : '#585b70' },
    }))

    setNodes(vfNodes)
    setEdges(vfEdges)

    if (!vfNodes.some(n => n.type === 'start')) {
      addNodes([{
        id: 'start-0', type: 'start', position: { x: 80, y: 200 },
        data: { label: 'Start' },
      }])
    }
  } catch (e: any) {
    msg.error(e.message)
  } finally {
    loading.value = false
  }
})

onNodesChange((changes) => {
  applyNodeChanges(changes)
  isDirty.value = true
})

onEdgesChange((changes) => {
  applyEdgeChanges(changes)
  isDirty.value = true
})

function onConnect(params: any) {
  const handle = params.sourceHandle ?? 'default'
  addEdges([{
    id: `e-${Date.now()}`,
    source: params.source,
    sourceHandle: handle,
    target: params.target,
    targetHandle: params.targetHandle,
    animated: handle === 'pass',
    style: { stroke: handle === 'fail' ? '#f38ba8' : handle === 'pass' ? '#a6e3a1' : '#585b70' },
  }])
  isDirty.value = true
}

function addActionNode(action: Action) {
  const id = `action-${Date.now()}`
  addNodes([{
    id,
    type: 'action',
    position: { x: 300, y: 200 },
    data: withRunCallback({ label: action.name, actionId: action.id, pipelineType: `#${action.id}` }),
  }])
  isDirty.value = true
}

function addConditionNode() {
  const id = `condition-${Date.now()}`
  addNodes([{
    id,
    type: 'condition',
    position: { x: 400, y: 300 },
    data: { label: 'Condition', conditions: [] },
  }])
  isDirty.value = true
}

function addPipelineNode(pipe: Pipeline) {
  const id = `pipeline-${Date.now()}`
  addNodes([{
    id,
    type: 'pipeline',
    position: { x: 200, y: 200 },
    data: withRunCallback({ label: pipe.name, subPipelineId: pipe.id, subPipelineName: pipe.name }),
  }])
  isDirty.value = true
}

function onNodeClick({ node }: NodeMouseEvent) {
  if (node.type === 'start') return
  selectedNodeId.value = node.id
  editLabel.value = node.data.label ?? ''
  editActionId.value = node.data.actionId ?? null
  editSubPipelineId.value = node.data.subPipelineId ?? null
  editConditions.value = (node.data.conditions ?? []).map((c: PipelineCondition) => ({ ...c }))
  panel.value = 'edit'
  if (availableRules.value.length === 0) {
    listRules().then(r => { availableRules.value = r })
  }
}

function closeEdit() {
  selectedNodeId.value = null
  panel.value = 'palette'
}

function deleteSelectedNode() {
  if (!selectedNodeId.value) return
  removeNodes([selectedNodeId.value])
  selectedNodeId.value = null
  panel.value = 'palette'
  isDirty.value = true
}

function applyEdit() {
  if (!selectedNodeId.value) return
  const node = findNode(selectedNodeId.value)
  if (!node) return

  if (node.type === 'pipeline') {
    const pipe = pipelines.value.find(p => p.id === editSubPipelineId.value)
    node.data = {
      ...node.data,
      label: editLabel.value,
      subPipelineId: editSubPipelineId.value,
      subPipelineName: pipe?.name,
    }
  } else {
    node.data = {
      ...node.data,
      label: editLabel.value,
      actionId: editActionId.value,
      conditions: editConditions.value.filter(c => c.key.trim()),
    }
    if (node.type === 'action' && editActionId.value) {
      const a = actions.value.find(a => a.id === editActionId.value)
      if (a) node.data.pipelineType = a.name
    }
  }
  isDirty.value = true
  closeEdit()
}

function addConditionRow() {
  editConditions.value = [...editConditions.value, { key: '', op: '==', value: '' }]
}

function removeConditionRow(idx: number) {
  editConditions.value = editConditions.value.filter((_, i) => i !== idx)
}

async function onSave() {
  saving.value = true
  try {
    const nodeRequests = nodes.value.map(n => ({
      client_id:       n.id,
      type:            (n.type ?? 'action') as 'start' | 'action' | 'pipeline' | 'condition',
      action_id:       n.data.actionId ?? undefined,
      sub_pipeline_id: n.data.subPipelineId ?? undefined,
      conditions:      n.data.conditions ?? [],
      label:           n.data.label ?? '',
      pos_x:           n.position.x,
      pos_y:           n.position.y,
    }))
    const edgeRequests = edges.value.map(e => ({
      source_client_id: e.source,
      source_handle:    e.sourceHandle ?? 'default',
      target_client_id: e.target,
    }))
    const saved = await savePipelineGraph(pipelineId, nodeRequests, edgeRequests)
    setNodes(saved.nodes.map(n => {
      const baseData = {
        label:           n.label,
        actionId:        n.action_id,
        subPipelineId:   n.sub_pipeline_id,
        subPipelineName: n.sub_pipeline_id
          ? pipelines.value.find(p => p.id === n.sub_pipeline_id)?.name
          : undefined,
        conditions:      n.conditions ?? [],
        pipelineType:    n.action_id
          ? actions.value.find(a => a.id === n.action_id)?.name
          : undefined,
      }
      return {
        id:       String(n.id),
        type:     n.type,
        position: { x: n.pos_x, y: n.pos_y },
        data: (n.type === 'action' || n.type === 'pipeline') ? withRunCallback(baseData) : baseData,
      }
    }))
    setEdges(saved.edges.map(e => ({
      id:           String(e.id),
      source:       String(e.source_node_id),
      sourceHandle: e.source_handle,
      target:       String(e.target_node_id),
      animated:     e.source_handle === 'pass',
      style:        { stroke: e.source_handle === 'fail' ? '#f38ba8' : e.source_handle === 'pass' ? '#a6e3a1' : '#585b70' },
    })))
    isDirty.value = false
    msg.success('Pipeline saved')
  } catch (e: any) {
    msg.error(e.message)
  } finally {
    saving.value = false
  }
}

async function onDiscard() {
  loading.value = true
  try {
    const graph = await getPipelineGraph(pipelineId)
    setNodes(graph.nodes.map(n => {
      const baseData = {
        label:           n.label,
        actionId:        n.action_id,
        subPipelineId:   n.sub_pipeline_id,
        subPipelineName: n.sub_pipeline_id
          ? pipelines.value.find(p => p.id === n.sub_pipeline_id)?.name
          : undefined,
        conditions:      n.conditions ?? [],
        pipelineType:    n.action_id
          ? actions.value.find(a => a.id === n.action_id)?.name
          : undefined,
      }
      return {
        id: String(n.id), type: n.type, position: { x: n.pos_x, y: n.pos_y },
        data: (n.type === 'action' || n.type === 'pipeline') ? withRunCallback(baseData) : baseData,
      }
    }))
    setEdges(graph.edges.map(e => ({
      id: String(e.id), source: String(e.source_node_id),
      sourceHandle: e.source_handle, target: String(e.target_node_id),
    })))
    isDirty.value = false
  } catch (e: any) { msg.error(e.message) }
  finally { loading.value = false }
}

const actionOptions = computed(() =>
  actions.value.map(a => ({ label: `${a.name} (#${a.id})`, value: a.id }))
)

const pipelineOptions = computed(() =>
  availablePipelines.value.map(p => ({ label: `${p.name} (#${p.id})`, value: p.id }))
)
</script>

<template>
  <div style="display: flex; flex-direction: column; height: 100vh; background: #181825;">
    <!-- Header bar -->
    <div style="
      height: 48px; display: flex; align-items: center; gap: 12px;
      padding: 0 16px; background: #1e1e2e;
      border-bottom: 1px solid rgba(255,255,255,0.08); flex-shrink: 0;
    ">
      <n-button text @click="router.push('/pipelines')">
        <template #icon><n-icon><ArrowBackOutline /></n-icon></template>
        Pipelines
      </n-button>
      <n-text strong style="color: #cdd6f4">{{ pipeline?.name ?? '…' }}</n-text>
      <n-text v-if="isDirty" depth="3" style="font-size: 12px; color: #f9e2af">● unsaved</n-text>
      <div style="margin-left: auto; display: flex; gap: 8px;">
        <n-button size="small" @click="onDiscard" :disabled="saving || !isDirty">Discard</n-button>
        <n-button size="small" type="primary" @click="onSave" :loading="saving" :disabled="!isDirty">Save</n-button>
      </div>
    </div>

    <!-- Main content: canvas + right panel -->
    <div style="flex: 1; display: flex; overflow: hidden;">
      <!-- Vue Flow canvas -->
      <div style="flex: 1; position: relative;">
        <n-spin v-if="loading" style="position: absolute; inset: 0; display: flex; align-items: center; justify-content: center;" />
        <VueFlow
          v-else
          :node-types="nodeTypes"
          :default-edge-options="{ animated: false, style: { stroke: '#585b70' } }"
          fit-view-on-init
          @node-click="onNodeClick"
          @connect="onConnect"
          style="background: #181825; height: 100%;"
        >
        </VueFlow>
      </div>

      <!-- Right panel -->
      <div style="
        width: 240px; background: #1e1e2e;
        border-left: 1px solid rgba(255,255,255,0.08);
        display: flex; flex-direction: column; overflow: hidden;
      ">
        <!-- Palette mode -->
        <template v-if="panel === 'palette'">
          <div style="padding: 12px;">
            <n-button size="small" dashed block @click="addConditionNode" style="margin-bottom: 10px;">
              + Condition node
            </n-button>
            <n-input v-model:value="searchText" placeholder="Search actions…" size="small" clearable />
          </div>
          <n-scrollbar style="flex: 1;">
            <div style="padding: 0 12px 12px;">
              <!-- Actions section -->
              <div style="font-size: 11px; color: #6c7086; margin-bottom: 4px; text-transform: uppercase; letter-spacing: 0.5px;">Actions</div>
              <div
                v-for="action in filteredActions"
                :key="action.id"
                @click="addActionNode(action)"
                style="
                  padding: 6px 8px; margin-bottom: 4px; border-radius: 4px;
                  cursor: pointer; font-size: 12px; color: #cdd6f4;
                  background: #181825; border: 1px solid #313244;
                  user-select: none;
                "
                :style="{ opacity: action.active ? 1 : 0.5 }"
              >
                {{ action.name }}
                <span style="font-size: 10px; color: #6c7086; float: right;">#{{ action.id }}</span>
              </div>

              <!-- Sub-Pipelines section -->
              <div v-if="availablePipelines.length > 0" style="margin-top: 12px;">
                <div style="font-size: 11px; color: #6c7086; margin-bottom: 4px; text-transform: uppercase; letter-spacing: 0.5px;">Sub-Pipelines</div>
                <div
                  v-for="pipe in availablePipelines"
                  :key="pipe.id"
                  @click="addPipelineNode(pipe)"
                  style="
                    padding: 6px 8px; margin-bottom: 4px; border-radius: 4px;
                    cursor: pointer; font-size: 12px; color: #a78bfa;
                    background: #1e1533; border: 1px solid #4c1d95;
                    user-select: none;
                  "
                  :style="{ opacity: pipe.active ? 1 : 0.5 }"
                >
                  {{ pipe.name }}
                  <span style="font-size: 10px; color: #6c7086; float: right;">#{{ pipe.id }}</span>
                </div>
              </div>
            </div>
          </n-scrollbar>
        </template>

        <!-- Edit mode -->
        <template v-else>
          <div style="padding: 12px; flex: 1; overflow-y: auto;">
            <n-button text size="small" @click="closeEdit" style="margin-bottom: 8px; color: #a6adc8;">
              ← Back to palette
            </n-button>

            <n-text depth="3" style="font-size: 11px; display: block; margin-bottom: 6px;">Label</n-text>
            <n-input v-model:value="editLabel" size="small" placeholder="Node label" style="margin-bottom: 10px;" />

            <template v-if="selectedNode?.type === 'action'">
              <n-text depth="3" style="font-size: 11px; display: block; margin-bottom: 6px;">Action</n-text>
              <n-select
                v-model:value="editActionId"
                :options="actionOptions"
                size="small"
                clearable
                style="margin-bottom: 10px;"
              />
            </template>

            <template v-if="selectedNode?.type === 'pipeline'">
              <n-text depth="3" style="font-size: 11px; display: block; margin-bottom: 6px;">Sub-Pipeline</n-text>
              <n-select
                v-model:value="editSubPipelineId"
                :options="pipelineOptions"
                size="small"
                clearable
                style="margin-bottom: 10px;"
              />
            </template>

            <template v-if="selectedNode?.type === 'condition'">
              <n-divider style="margin: 8px 0">
                <n-text depth="3" style="font-size: 11px;">Conditions</n-text>
              </n-divider>
              <div v-for="(cond, idx) in editConditions" :key="idx" style="margin-bottom: 6px;">
                <n-select
                  v-if="ruleOps.has(cond.op)"
                  v-model:value="cond.key"
                  :options="ruleOptions"
                  size="small"
                  placeholder="Select rule…"
                  style="margin-bottom: 3px;"
                />
                <n-input
                  v-else
                  v-model:value="cond.key"
                  size="small"
                  placeholder="key"
                  style="margin-bottom: 3px;"
                />
                <div style="display: flex; gap: 4px;">
                  <n-select v-model:value="cond.op" :options="conditionOpOptions" size="small" style="flex: 1;" />
                  <n-button size="small" type="error" ghost @click="removeConditionRow(idx)">×</n-button>
                </div>
                <n-input-number
                  v-if="numericOps.has(cond.op)"
                  :value="cond.value === '' ? null : Number(cond.value)"
                  @update:value="v => cond.value = v == null ? '' : String(v)"
                  size="small" placeholder="value" style="width: 100%; margin-top: 3px;"
                />
                <n-input
                  v-else-if="!['is_empty','not_empty','rule_matches','rule_not_matches'].includes(cond.op)"
                  v-model:value="cond.value"
                  size="small" placeholder="value" style="margin-top: 3px;"
                />
              </div>
              <n-button size="small" dashed block @click="addConditionRow" style="margin-bottom: 10px;">
                + Add condition
              </n-button>
            </template>

            <n-button type="primary" size="small" block @click="applyEdit">Apply</n-button>
            <n-button type="error" size="small" block @click="deleteSelectedNode" style="margin-top: 8px;">Delete node</n-button>
          </div>
        </template>
      </div>
    </div>
  </div>

  <!-- Run from Node Modal (Option B) -->
  <n-modal
    v-model:show="showRunFromNodeModal"
    preset="dialog"
    :title="`Run from: ${runFromNodeLabel}`"
    positive-text="Run"
    negative-text="Cancel"
    :loading="runningFromNode"
    @positive-click="onConfirmRunFromNode"
  >
    <n-space vertical :size="12" style="margin-top: 8px;">
      <n-text depth="3" style="font-size: 12px;">
        Prior detection results for each tweet will be available as context.
      </n-text>
      <n-radio-group v-model:value="runFromNodeNewOnly">
        <n-space vertical :size="8">
          <n-radio :value="false">All eligible tweets</n-radio>
          <n-radio :value="true">Only tweets not yet processed by this node's action</n-radio>
        </n-space>
      </n-radio-group>
    </n-space>
  </n-modal>
</template>
