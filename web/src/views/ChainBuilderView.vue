<script setup lang="ts">
import { ref, computed, onMounted, markRaw } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  VueFlow, useVueFlow, type Node, type Edge,
  MiniMap, Controls, Background,
} from '@vue-flow/core'
import '@vue-flow/core/dist/style.css'
import '@vue-flow/core/dist/theme-default.css'
import {
  NButton, NFlex, NText, NInput, NSelect, NDivider, NInputNumber,
  NSpin, NScrollbar, NIcon, useMessage,
} from 'naive-ui'
import { ArrowBackOutline } from '@vicons/ionicons5'
import StartNode from '../components/chain/StartNode.vue'
import ActionNode from '../components/chain/ActionNode.vue'
import ConditionNode from '../components/chain/ConditionNode.vue'
import { getChain, getChainGraph, saveChainGraph, type ChainCondition } from '../api/chains'
import { listActions } from '../api/actions'
import type { Action } from '../api/actions'
import type { Chain } from '../api/chains'

const route  = useRoute()
const router = useRouter()
const msg    = useMessage()
const chainId = Number(route.params.id)

const {
  nodes, edges,
  setNodes, setEdges,
  addNodes,
  onNodesChange, onEdgesChange,
  applyNodeChanges, applyEdgeChanges,
  findNode,
} = useVueFlow()

const nodeTypes = {
  start:     markRaw(StartNode),
  action:    markRaw(ActionNode),
  condition: markRaw(ConditionNode),
}

const chain        = ref<Chain | null>(null)
const actions      = ref<Action[]>([])
const loading      = ref(true)
const saving       = ref(false)
const dirty        = ref(false)
const searchText   = ref('')
const selectedNodeId = ref<string | null>(null)

const panel = ref<'palette' | 'edit'>('palette')

const editLabel      = ref('')
const editActionId   = ref<number | null>(null)
const editConditions = ref<ChainCondition[]>([])

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
]

const numericOps = new Set(['>', '>=', '<', '<='])

onMounted(async () => {
  try {
    const [c, graph, acts] = await Promise.all([
      getChain(chainId),
      getChainGraph(chainId),
      listActions(),
    ])
    chain.value = c
    actions.value = acts

    const vfNodes: Node[] = graph.nodes.map(n => ({
      id:       String(n.id),
      type:     n.type,
      position: { x: n.pos_x, y: n.pos_y },
      data: {
        label:        n.label || (n.type === 'start' ? 'Start' : ''),
        actionId:     n.action_id,
        conditions:   n.conditions ?? [],
        pipelineType: n.action_id
          ? acts.find(a => a.id === n.action_id)?.name
          : undefined,
      },
    }))

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
  dirty.value = true
})

onEdgesChange((changes) => {
  applyEdgeChanges(changes)
  dirty.value = true
})

function addActionNode(action: Action) {
  const id = `action-${Date.now()}`
  addNodes([{
    id,
    type: 'action',
    position: { x: 300, y: 200 },
    data: { label: action.name, actionId: action.id, pipelineType: `#${action.id}` },
  }])
  dirty.value = true
}

function addConditionNode() {
  const id = `condition-${Date.now()}`
  addNodes([{
    id,
    type: 'condition',
    position: { x: 400, y: 300 },
    data: { label: 'Condition', conditions: [] },
  }])
  dirty.value = true
}

function onNodeClick(_event: MouseEvent, node: Node) {
  if (node.type === 'start') return
  selectedNodeId.value = node.id
  editLabel.value = node.data.label ?? ''
  editActionId.value = node.data.actionId ?? null
  editConditions.value = (node.data.conditions ?? []).map((c: ChainCondition) => ({ ...c }))
  panel.value = 'edit'
}

function closeEdit() {
  selectedNodeId.value = null
  panel.value = 'palette'
}

function applyEdit() {
  if (!selectedNodeId.value) return
  const node = findNode(selectedNodeId.value)
  if (!node) return
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
  dirty.value = true
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
    const nodeRequests = nodes.map(n => ({
      client_id:  n.id,
      type:       (n.type ?? 'action') as 'start' | 'action' | 'condition',
      action_id:  n.data.actionId ?? undefined,
      conditions: n.data.conditions ?? [],
      label:      n.data.label ?? '',
      pos_x:      n.position.x,
      pos_y:      n.position.y,
    }))
    const edgeRequests = edges.map(e => ({
      source_client_id: e.source,
      source_handle:    e.sourceHandle ?? 'default',
      target_client_id: e.target,
    }))
    const saved = await saveChainGraph(chainId, nodeRequests, edgeRequests)
    setNodes(saved.nodes.map(n => ({
      id:       String(n.id),
      type:     n.type,
      position: { x: n.pos_x, y: n.pos_y },
      data: { label: n.label, actionId: n.action_id, conditions: n.conditions ?? [] },
    })))
    setEdges(saved.edges.map(e => ({
      id:           String(e.id),
      source:       String(e.source_node_id),
      sourceHandle: e.source_handle,
      target:       String(e.target_node_id),
      animated:     e.source_handle === 'pass',
      style:        { stroke: e.source_handle === 'fail' ? '#f38ba8' : e.source_handle === 'pass' ? '#a6e3a1' : '#585b70' },
    })))
    dirty.value = false
    msg.success('Chain saved')
  } catch (e: any) {
    msg.error(e.message)
  } finally {
    saving.value = false
  }
}

async function onDiscard() {
  loading.value = true
  try {
    const graph = await getChainGraph(chainId)
    setNodes(graph.nodes.map(n => ({
      id: String(n.id), type: n.type, position: { x: n.pos_x, y: n.pos_y },
      data: { label: n.label, actionId: n.action_id, conditions: n.conditions ?? [] },
    })))
    setEdges(graph.edges.map(e => ({
      id: String(e.id), source: String(e.source_node_id),
      sourceHandle: e.source_handle, target: String(e.target_node_id),
    })))
    dirty.value = false
  } catch (e: any) { msg.error(e.message) }
  finally { loading.value = false }
}

const actionOptions = computed(() =>
  actions.value.map(a => ({ label: `${a.name} (#${a.id})`, value: a.id }))
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
      <n-button text @click="router.push('/chains')">
        <template #icon><n-icon><ArrowBackOutline /></n-icon></template>
        Chains
      </n-button>
      <n-text strong style="color: #cdd6f4">{{ chain?.name ?? '…' }}</n-text>
      <n-text v-if="dirty" depth="3" style="font-size: 12px; color: #f9e2af">● unsaved</n-text>
      <div style="margin-left: auto; display: flex; gap: 8px;">
        <n-button size="small" @click="onDiscard" :disabled="saving || !dirty">Discard</n-button>
        <n-button size="small" type="primary" @click="onSave" :loading="saving" :disabled="!dirty">Save</n-button>
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
          style="background: #181825; height: 100%;"
        >
          <Background variant="dots" :gap="20" :size="1" pattern-color="#313244" />
          <MiniMap style="background: #1e1e2e; border: 1px solid #313244;" />
          <Controls style="background: #1e1e2e; border: 1px solid #313244;" />
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

            <template v-if="findNode(selectedNodeId ?? '')?.type === 'action'">
              <n-text depth="3" style="font-size: 11px; display: block; margin-bottom: 6px;">Action</n-text>
              <n-select
                v-model:value="editActionId"
                :options="actionOptions"
                size="small"
                clearable
                style="margin-bottom: 10px;"
              />
            </template>

            <template v-if="findNode(selectedNodeId ?? '')?.type === 'condition'">
              <n-divider style="margin: 8px 0">
                <n-text depth="3" style="font-size: 11px;">Conditions</n-text>
              </n-divider>
              <div v-for="(cond, idx) in editConditions" :key="idx" style="margin-bottom: 6px;">
                <n-input v-model:value="cond.key" size="small" placeholder="key" style="margin-bottom: 3px;" />
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
                  v-else-if="cond.op !== 'is_empty' && cond.op !== 'not_empty'"
                  v-model:value="cond.value"
                  size="small" placeholder="value" style="margin-top: 3px;"
                />
              </div>
              <n-button size="small" dashed block @click="addConditionRow" style="margin-bottom: 10px;">
                + Add condition
              </n-button>
            </template>

            <n-button type="primary" size="small" block @click="applyEdit">Apply</n-button>
          </div>
        </template>
      </div>
    </div>
  </div>
</template>
