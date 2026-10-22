<!-- web/src/views/PipelineBuilderView.vue — Pipeline builder (admin): a pipeline's DAG on an auto-laid-out
     Vue Flow canvas. Steps are added from a node's "+" (NodePicker); the selected node or connection opens
     in the inspector, floating over the canvas from lg up and in a Sheet below it. -->
<script setup lang="ts">
import { ref, computed, markRaw, nextTick, onBeforeUnmount, provide, watch } from 'vue'
import {
  RouterLink, onBeforeRouteLeave, onBeforeRouteUpdate, useRoute, useRouter,
  type RouteLocationNormalized,
} from 'vue-router'
import {
  VueFlow, useVueFlow, type Connection, type EdgeMouseEvent, type NodeMouseEvent, type NodeTypesObject,
} from '@vue-flow/core'
import '@vue-flow/core/dist/style.css'
import '../components/pipeline/flow-theme.css'
import { ArrowLeftIcon, LoaderCircleIcon, MaximizeIcon, ZoomInIcon, ZoomOutIcon } from '@lucide/vue'
import { AlertDialog, Button, IconButton, Sheet } from '@/components/ui'
import { useToast } from '@/composables/useToast'
import { useMediaQuery } from '@/composables/useMediaQuery'
import PageHeader from '../components/PageHeader.vue'
import StartNode from '../components/pipeline/StartNode.vue'
import ActionNode from '../components/pipeline/ActionNode.vue'
import ConditionNode from '../components/pipeline/ConditionNode.vue'
import PipelineNodeComponent from '../components/pipeline/PipelineNode.vue'
import NodePicker, { type PickedStep } from '../components/pipeline/NodePicker.vue'
import NodeInspector from '../components/pipeline/NodeInspector.vue'
import RunFromNodeDialog from '../components/pipeline/RunFromNodeDialog.vue'
import {
  builderKey, edgeFor, edgeStroke, isStructural, toFlowEdges, toFlowNodes, toSaveRequest,
} from '../components/pipeline/builderGraph'
import { layoutGraph } from '../components/pipeline/layout'
import {
  getPipeline, getPipelineGraph, savePipelineGraph, listPipelines, type Pipeline, type PipelineGraph,
} from '../api/pipelines'
import { listActions, type Action } from '../api/actions'
import { listRules, type Rule } from '../api/rules'
import { listTriggerPipelineLinks, type TriggerPipelineLink } from '../api/trigger_links'

const route  = useRoute()
const router = useRouter()
const toast  = useToast()
const isWide = useMediaQuery('(min-width: 1024px)')
const pipelineId = computed(() => Number(route.params.id))

const errorMessage = (e: unknown) => (e instanceof Error ? e.message : String(e))

const {
  nodes, edges, setNodes, setEdges, addNodes, addEdges, removeNodes, removeEdges,
  onNodesChange, onEdgesChange, onNodesInitialized,
  findNode, findEdge, fitView, zoomIn, zoomOut, setCenter, setViewport, getViewport, dimensions,
  addSelectedNodes, removeSelectedNodes, removeSelectedEdges,
} = useVueFlow()

const nodeTypes: NodeTypesObject = {
  start:     markRaw(StartNode) as any,
  action:    markRaw(ActionNode) as any,
  condition: markRaw(ConditionNode) as any,
  pipeline:  markRaw(PipelineNodeComponent) as any,
}

const pipeline  = ref<Pipeline | null>(null)
const actions   = ref<Action[]>([])
const pipelines = ref<Pipeline[]>([])
const links     = ref<TriggerPipelineLink[]>([])
const rules     = ref<Rule[]>([])
const loading   = ref(true)
const loadError = ref<string | null>(null)
const saving    = ref(false)
const isDirty   = ref(false)

const defaultEdgeOptions = { type: 'smoothstep', animated: false, style: { stroke: edgeStroke('default') } }

// Other pipelines can be sub-pipelines of this one.
const otherPipelines = computed(() => pipelines.value.filter(p => p.id !== pipelineId.value))
const ruleOptions    = computed(() => rules.value.map(r => ({ label: r.name, value: String(r.id) })))

// ── Context for the node components ───────────────────────────────────────────
const picker = ref<{ nodeId: string; handle: string } | null>(null)
const pickerOpen = ref(false)

provide(builderKey, {
  actions:   computed(() => new Map(actions.value.map(a => [a.id, a]))),
  pipelines: computed(() => new Map(pipelines.value.map(p => [p.id, p]))),
  triggers:  computed(() => [...new Set(
    links.value.filter(l => l.pipeline_id === pipelineId.value && l.active).map(l => l.trigger_type),
  )]),
  addAfter(nodeId, handle) {
    picker.value = { nodeId, handle }
    pickerOpen.value = true
  },
})

function nodeLabel(id: string): string {
  const n = findNode(id)
  if (!n) return `#${id}`
  if (n.data.label) return n.data.label
  if (n.data.actionId) return actions.value.find(a => a.id === n.data.actionId)?.name ?? `Action #${n.data.actionId}`
  if (n.data.subPipelineId) return pipelines.value.find(p => p.id === n.data.subPipelineId)?.name ?? `Pipeline #${n.data.subPipelineId}`
  return n.type === 'condition' ? 'Condition' : `#${id}`
}

const pickerContext = computed(() => {
  if (!picker.value) return ''
  const { nodeId, handle } = picker.value
  const branch = handle === 'pass' ? ' · Pass' : handle === 'fail' ? ' · Fail' : ''
  return `After ${nodeLabel(nodeId)}${branch}`
})

// ── Load ──────────────────────────────────────────────────────────────────────
let fitAfterLayout = false

async function load() {
  loading.value   = true
  loadError.value = null
  closeInspector()
  try {
    const id = pipelineId.value
    const [p, graph, acts, pipes, lks, rls] = await Promise.all([
      getPipeline(id),
      getPipelineGraph(id),
      listActions(),
      listPipelines(),
      listTriggerPipelineLinks(),
      listRules(),
    ])
    pipeline.value  = p
    actions.value   = acts
    pipelines.value = pipes
    links.value     = lks
    rules.value     = rls

    isDirty.value = false
    setGraph(graph)
  } catch (e: unknown) {
    loadError.value = errorMessage(e)
  } finally {
    loading.value = false
  }
}

function setGraph(graph: PipelineGraph) {
  fitAfterLayout = true
  const flowNodes = toFlowNodes(graph.nodes)
  // A pipeline that was never saved has no Start node yet; add one without marking the graph dirty.
  if (!flowNodes.some(n => n.type === 'start')) {
    flowNodes.unshift({
      id: 'start-0', type: 'start', position: { x: 0, y: 0 }, deletable: false,
      data: { label: 'Start' },
    })
  }
  setNodes(flowNodes)
  setEdges(toFlowEdges(graph.edges))
  // onNodesInitialized doesn't fire again when the replaced nodes keep their ids (Discard, Save).
  scheduleLayout()
}


// ── Auto-layout ───────────────────────────────────────────────────────────────
// Runs once Vue Flow has measured the nodes, and again after every structural change.
function relayout() {
  const positions = layoutGraph(nodes.value, edges.value)
  for (const n of nodes.value) {
    const p = positions[n.id]
    if (p) n.position = p
  }
  if (fitAfterLayout) {
    fitAfterLayout = false
    nextTick(initialView)
  }
}

// Fit the whole graph, unless that makes it unreadable (long chains): then open at Start, readable zoom.
const minReadableZoom = 0.5

async function initialView() {
  await fitView({ padding: 0.2, maxZoom: 1 })
  if (getViewport().zoom >= minReadableZoom) return
  const start = nodes.value.find(n => n.type === 'start')
  if (!start) return
  const zoom = 0.9
  setViewport({
    x: 48 - start.position.x * zoom,
    y: dimensions.value.height / 2 - (start.position.y + (start.dimensions.height || 64) / 2) * zoom,
    zoom,
  })
}

// Two frames, so nodes added this tick have been measured.
function scheduleLayout() {
  nextTick(() => requestAnimationFrame(() => requestAnimationFrame(relayout)))
}

onNodesInitialized(relayout)

// Vue Flow applies the changes itself (applyDefault); these only track edits.
onNodesChange((changes) => {
  if (isStructural(changes)) { isDirty.value = true; scheduleLayout() }
})

onEdgesChange((changes) => {
  if (isStructural(changes)) { isDirty.value = true; scheduleLayout() }
})

function onConnect(params: Connection) {
  const handle = params.sourceHandle ?? 'default'
  addEdges([edgeFor(`e-${Date.now()}`, params.source, handle, params.target, params.targetHandle)])
}

// ── Add a step from a node's "+" ──────────────────────────────────────────────
function onPick(step: PickedStep) {
  if (!picker.value) return
  const { nodeId, handle } = picker.value
  const source = findNode(nodeId)
  const id = `${step.kind}-${Date.now()}`
  const data =
    step.kind === 'action'   ? { label: '', actionId: step.action.id } :
    step.kind === 'pipeline' ? { label: '', subPipelineId: step.pipeline.id } :
                               { label: 'Condition', conditions: [] }
  const position = source ? { x: source.position.x + 300, y: source.position.y } : { x: 0, y: 0 }

  addNodes([{ id, type: step.kind, position, data }])
  addEdges([edgeFor(`e-${Date.now()}`, nodeId, handle, id)])
  picker.value = null

  // Select the new step and bring it into view once it has been laid out.
  nextTick(() => requestAnimationFrame(() => requestAnimationFrame(() => {
    const node = findNode(id)
    if (!node) return
    selectNode(id)
    setCenter(
      node.position.x + (node.dimensions.width || 240) / 2,
      node.position.y + (node.dimensions.height || 64) / 2,
      { zoom: getViewport().zoom, duration: 200 },
    )
  })))
}

// ── Selection and inspector ───────────────────────────────────────────────────
const selectedNodeId = ref<string | null>(null)
const selectedEdgeId = ref<string | null>(null)
const selectedNode   = computed(() => (selectedNodeId.value ? findNode(selectedNodeId.value) : undefined))
const selectedEdge   = computed(() => (selectedEdgeId.value ? findEdge(selectedEdgeId.value) : undefined))
const inspectorOpen  = computed({
  get: () => !!(selectedNode.value || selectedEdge.value),
  set: open => { if (!open) closeInspector() },
})

function selectNode(id: string) {
  selectedEdgeId.value = null
  selectedNodeId.value = id
  removeSelectedEdges(edges.value.filter(e => e.selected))
  const node = findNode(id)
  if (node && !node.selected) addSelectedNodes([node])
}

function onNodeClick({ node }: NodeMouseEvent) {
  selectedEdgeId.value = null
  selectedNodeId.value = node.id
}

function onEdgeClick({ edge }: EdgeMouseEvent) {
  selectedNodeId.value = null
  selectedEdgeId.value = edge.id
}

function closeInspector() {
  selectedNodeId.value = null
  selectedEdgeId.value = null
  removeSelectedNodes(nodes.value.filter(n => n.selected))
  removeSelectedEdges(edges.value.filter(e => e.selected))
}

function deleteSelectedNode() {
  if (!selectedNodeId.value) return
  removeNodes([selectedNodeId.value])
  closeInspector()
}

function deleteSelectedEdge() {
  if (!selectedEdgeId.value) return
  removeEdges([selectedEdgeId.value])
  closeInspector()
}

// ── Run from node ─────────────────────────────────────────────────────────────
const runOpen      = ref(false)
const runNodeId    = ref<string | null>(null)
const runNodeLabel = ref('')

// The backfill runs the saved graph, so unsaved changes are saved first (Run from here → save prompt).
const saveBeforeRunOpen = ref(false)
let pendingRunNodeId: string | null = null

function openRunFrom() {
  if (!selectedNodeId.value) return
  if (isDirty.value) {
    pendingRunNodeId = selectedNodeId.value
    saveBeforeRunOpen.value = true
    return
  }
  showRunDialog(selectedNodeId.value)
}

function showRunDialog(nodeId: string) {
  runNodeId.value    = nodeId
  runNodeLabel.value = nodeLabel(nodeId)
  runOpen.value      = true
}

async function onSaveAndRun() {
  const nodeId = pendingRunNodeId
  if (!nodeId) return
  // Saving replaces every node row, so find the node again by its position in the request.
  const index = nodes.value.findIndex(n => n.id === nodeId)
  const saved = await onSave()
  saveBeforeRunOpen.value = false
  pendingRunNodeId = null
  const savedNode = saved?.nodes[index]
  if (savedNode) showRunDialog(String(savedNode.id))
}

// ── Save / discard ────────────────────────────────────────────────────────────
// Resolves the saved graph (nodes in request order), or null when saving failed.
async function onSave(): Promise<PipelineGraph | null> {
  saving.value = true
  try {
    const { nodeRequests, edgeRequests } = toSaveRequest(nodes.value, edges.value)
    const saved = await savePipelineGraph(pipelineId.value, nodeRequests, edgeRequests)
    closeInspector()
    setGraph(saved)
    isDirty.value = false
    toast.success('Saved pipeline')
    return saved
  } catch (e: unknown) {
    toast.error(`Could not save pipeline: ${errorMessage(e)}`)
    return null
  } finally {
    saving.value = false
  }
}

const discardOpen = ref(false)

async function onDiscard() {
  discardOpen.value = false
  closeInspector()
  try {
    setGraph(await getPipelineGraph(pipelineId.value))
    isDirty.value = false
  } catch (e: unknown) {
    toast.error(`Could not discard changes: ${errorMessage(e)}`)
  }
}

// ── Leave guard for unsaved changes ───────────────────────────────────────────
const leaveOpen = ref(false)
let leaveTarget: string | null = null
let leaveConfirmed = false

function guard(to: RouteLocationNormalized) {
  if (!isDirty.value || leaveConfirmed) {
    leaveConfirmed = false
    return true
  }
  leaveTarget = to.fullPath
  leaveOpen.value = true
  return false
}

onBeforeRouteLeave(guard)
onBeforeRouteUpdate(guard)

function confirmLeave() {
  leaveConfirmed = true
  leaveOpen.value = false
  if (leaveTarget) router.push(leaveTarget)
}

function onBeforeUnload(e: BeforeUnloadEvent) {
  if (isDirty.value) e.preventDefault()
}
window.addEventListener('beforeunload', onBeforeUnload)
onBeforeUnmount(() => window.removeEventListener('beforeunload', onBeforeUnload))

const isEmpty = computed(() => !loading.value && !loadError.value && nodes.value.length <= 1 && edges.value.length === 0)

// Last, so load() can use everything declared above; reloads when a sub-pipeline link changes the id.
watch(pipelineId, load, { immediate: true })
</script>

<template>
  <div class="flex h-full min-h-0 flex-col">
    <div class="px-4 pt-4 pb-4 sm:px-6 sm:pt-6">
      <RouterLink
        to="/pipelines"
        class="mb-2 inline-flex items-center gap-1 text-small text-muted-foreground no-underline hover:text-foreground"
      >
        <ArrowLeftIcon class="size-3.5" :stroke-width="1.75" /> Pipelines
      </RouterLink>
      <PageHeader
        :title="pipeline?.name ?? `Pipeline #${pipelineId}`"
        description="Add steps with + on a node; drag from an output to an input to join branches."
      >
        <div class="flex flex-wrap items-center gap-2">
          <span v-if="isDirty" class="flex items-center gap-1.5 text-small text-warning">
            <span class="size-2 rounded-full bg-warning" aria-hidden="true" /> Unsaved changes
          </span>
          <Button variant="outline" :disabled="saving || !isDirty" @click="discardOpen = true">Discard</Button>
          <Button :loading="saving" :disabled="!isDirty" @click="onSave">Save</Button>
        </div>
      </PageHeader>

      <div
        v-if="loadError"
        class="mt-4 flex flex-wrap items-center justify-between gap-2 rounded-lg border border-destructive p-4 text-small"
      >
        <span class="text-destructive">Could not load pipeline: {{ loadError }}</span>
        <div class="flex items-center gap-2">
          <Button variant="outline" @click="load">Retry</Button>
          <Button variant="ghost" @click="loadError = null">Dismiss</Button>
        </div>
      </div>
    </div>

    <div class="relative min-h-0 flex-1 border-t border-border">
      <VueFlow
        class="tw-flow h-full"
        :node-types="nodeTypes"
        :default-edge-options="defaultEdgeOptions"
        :nodes-draggable="false"
        :delete-key-code="['Backspace', 'Delete']"
        :min-zoom="0.2"
        :max-zoom="1.5"
        @node-click="onNodeClick"
        @edge-click="onEdgeClick"
        @pane-click="closeInspector"
        @connect="onConnect"
      />

      <div v-if="loading" class="absolute inset-0 z-10 flex items-center justify-center bg-background">
        <span class="flex items-center gap-2 rounded-md border border-border bg-popover px-3 py-2 text-muted-foreground">
          <LoaderCircleIcon class="size-4 animate-spin" :stroke-width="1.75" /> Loading pipeline…
        </span>
      </div>

      <p
        v-if="isEmpty"
        class="pointer-events-none absolute inset-x-4 top-4 z-10 mx-auto my-0 max-w-sm rounded-lg border border-border bg-popover p-3 text-center text-muted-foreground"
      >
        Click <span class="text-foreground">+</span> on Start to add the first step.
      </p>

      <div class="absolute bottom-3 left-3 z-10 flex gap-1 rounded-md border border-border bg-card p-1">
        <IconButton :icon="ZoomInIcon" label="Zoom in" @click="zoomIn({ duration: 150 })" />
        <IconButton :icon="ZoomOutIcon" label="Zoom out" @click="zoomOut({ duration: 150 })" />
        <IconButton :icon="MaximizeIcon" label="Fit to screen" @click="fitView({ padding: 0.2, maxZoom: 1, duration: 200 })" />
      </div>

      <aside
        v-if="isWide && inspectorOpen"
        class="absolute top-3 right-3 bottom-3 z-10 flex w-80 flex-col overflow-hidden rounded-lg border border-border bg-card shadow-xl"
      >
        <NodeInspector
          :node="selectedNode"
          :edge="selectedEdge"
          :node-label="nodeLabel"
          :rule-options="ruleOptions"
          @change="isDirty = true"
          @close="closeInspector"
          @delete-node="deleteSelectedNode"
          @delete-edge="deleteSelectedEdge"
          @run-from="openRunFrom"
        />
      </aside>
    </div>

    <Sheet v-if="!isWide" v-model:open="inspectorOpen" width="min(360px, 100vw)">
      <NodeInspector
        :node="selectedNode"
        :edge="selectedEdge"
        :node-label="nodeLabel"
        :rule-options="ruleOptions"
        @change="isDirty = true"
        @close="closeInspector"
        @delete-node="deleteSelectedNode"
        @delete-edge="deleteSelectedEdge"
        @run-from="openRunFrom"
      />
    </Sheet>

    <NodePicker
      v-model:open="pickerOpen"
      :actions="actions"
      :pipelines="otherPipelines"
      :context="pickerContext"
      @pick="onPick"
    />

    <RunFromNodeDialog
      v-model:open="runOpen"
      :pipeline-id="pipelineId"
      :node-id="runNodeId"
      :node-label="runNodeLabel"
    />

    <AlertDialog
      v-model:open="saveBeforeRunOpen"
      title="Save before running?"
      description="Running from a node uses the saved pipeline. Save your changes first so the run matches the canvas."
      action-label="Save and continue"
      variant="default"
      :loading="saving"
      @confirm="onSaveAndRun"
    />

    <AlertDialog
      v-model:open="discardOpen"
      title="Discard unsaved changes?"
      description="The graph goes back to its last saved state."
      action-label="Discard changes"
      @confirm="onDiscard"
    />

    <AlertDialog
      v-model:open="leaveOpen"
      title="Leave without saving?"
      description="Your unsaved changes to this pipeline will be lost."
      action-label="Leave page"
      @confirm="confirmLeave"
    />
  </div>
</template>
