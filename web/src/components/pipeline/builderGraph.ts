// Pipeline builder state: converts the saved graph (api/pipelines) to Vue Flow nodes/edges and back,
// the context the node components read (provide/inject), node-kind display metadata, and the condition
// operators. Edge colours are read from the tokens in styles/main.css at render time, since Vue Flow
// writes them as inline SVG stroke values.
import { inject, type ComputedRef, type InjectionKey } from 'vue'
import type { Component } from 'vue'
import { useVueFlow, type Edge, type EdgeChange, type Node, type NodeChange } from '@vue-flow/core'
import { BellIcon, BotIcon, CpuIcon, GitBranchIcon, PlayIcon, SettingsIcon, WorkflowIcon } from '@lucide/vue'
import { token } from '../../theme/naive'
import type { Action, ActionType } from '../../api/actions'
import type { Pipeline, PipelineCondition, PipelineEdge, PipelineNode } from '../../api/pipelines'

// ── Node kinds ────────────────────────────────────────────────────────────────
// Colour classes are spelled out in full so Tailwind generates them.
export interface KindMeta { label: string; icon: Component; text: string; border: string }

export const kindMeta: Record<'start' | 'condition' | 'pipeline' | ActionType, KindMeta> = {
  start:        { label: 'Start',        icon: PlayIcon,      text: 'text-chart-1',          border: 'border-l-chart-1' },
  condition:    { label: 'Condition',    icon: GitBranchIcon, text: 'text-chart-2',          border: 'border-l-chart-2' },
  pipeline:     { label: 'Sub-pipeline', icon: WorkflowIcon,  text: 'text-chart-3',          border: 'border-l-chart-3' },
  ollama:       { label: 'Ollama',       icon: BotIcon,       text: 'text-chart-5',          border: 'border-l-chart-5' },
  onnx:         { label: 'ONNX',         icon: CpuIcon,       text: 'text-chart-5',          border: 'border-l-chart-5' },
  notification: { label: 'Notification', icon: BellIcon,      text: 'text-chart-4',          border: 'border-l-chart-4' },
  internal:     { label: 'Internal',     icon: SettingsIcon,  text: 'text-muted-foreground', border: 'border-l-muted-foreground' },
}

/** One-line detail for an action: its model, handler or model file. */
export function actionDetail(a: Action): string {
  switch (a.type) {
    case 'ollama':   return a.config?.model ?? ''
    case 'internal': return a.config?.handler ?? ''
    case 'onnx':     return String(a.config?.path ?? '').split('/').pop() ?? ''
    default:         return ''
  }
}

// ── Context shared with the node components ───────────────────────────────────
export interface BuilderContext {
  actions: ComputedRef<Map<number, Action>>
  pipelines: ComputedRef<Map<number, Pipeline>>
  /** Trigger types linked to this pipeline (shown on the Start node). */
  triggers: ComputedRef<string[]>
  /** Opens the node picker to add a step after `nodeId`'s `handle` output. */
  addAfter: (nodeId: string, handle: string) => void
}

export const builderKey: InjectionKey<BuilderContext> = Symbol('pipeline-builder')

export function useBuilder(): BuilderContext {
  const ctx = inject(builderKey)
  if (!ctx) throw new Error('pipeline builder: node rendered outside the builder')
  return ctx
}

/** Edge helpers for a node component: is an output connected, and right-click disconnect of inputs/outputs. */
export function useNodeEdges(id: string) {
  const { edges, removeEdges } = useVueFlow()
  // `handle` limits outputs to one source handle (condition Pass/Fail); omitted means every output.
  const outgoing = (handle?: string) =>
    edges.value.filter(e => e.source === id && (handle === undefined || e.sourceHandle === handle))
  const remove = (list: Edge[]) => { if (list.length) removeEdges(list.map(e => e.id)) }
  return {
    isConnected: (handle?: string) => outgoing(handle).length > 0,
    disconnectSource: (handle?: string) => remove(outgoing(handle)),
    disconnectTarget: () => remove(edges.value.filter(e => e.target === id)),
  }
}

// ── Graph conversion ──────────────────────────────────────────────────────────
// Edges take the colour of the output they leave: pass → success, fail → destructive, else neutral.
export function edgeStroke(handle: string | null | undefined) {
  return token(handle === 'fail' ? 'destructive' : handle === 'pass' ? 'success' : 'input')
}

export function edgeFor(id: string, source: string, handle: string, target: string, targetHandle?: string | null): Edge {
  return {
    id,
    type: 'smoothstep',
    source,
    sourceHandle: handle,
    target,
    ...(targetHandle !== undefined ? { targetHandle } : {}),
    animated: handle === 'pass',
    style: { stroke: edgeStroke(handle) },
  }
}

/** Builds Vue Flow nodes from saved graph nodes. Display names are looked up by the node components. */
export function toFlowNodes(graphNodes: PipelineNode[]): Node[] {
  return graphNodes.map(n => ({
    id:        String(n.id),
    type:      n.type,
    position:  { x: n.pos_x, y: n.pos_y },
    deletable: n.type !== 'start',
    data: {
      label:         n.label || (n.type === 'start' ? 'Start' : ''),
      actionId:      n.action_id,
      subPipelineId: n.sub_pipeline_id,
      conditions:    n.conditions ?? [],
    },
  }))
}

export function toFlowEdges(graphEdges: PipelineEdge[]): Edge[] {
  return graphEdges.map(e =>
    edgeFor(String(e.id), String(e.source_node_id), e.source_handle, String(e.target_node_id)))
}

export function toSaveRequest(nodes: Node[], edges: Edge[]) {
  const nodeRequests = nodes.map(n => ({
    client_id:       n.id,
    type:            (n.type ?? 'action') as 'start' | 'action' | 'pipeline' | 'condition',
    action_id:       n.data.actionId ?? undefined,
    sub_pipeline_id: n.data.subPipelineId ?? undefined,
    // The inspector edits rows live; rows left with an empty key are dropped here.
    conditions:      ((n.data.conditions ?? []) as PipelineCondition[]).filter(c => c.key.trim()),
    label:           n.data.label ?? '',
    pos_x:           n.position.x,
    pos_y:           n.position.y,
  }))
  const edgeRequests = edges.map(e => ({
    source_client_id: e.source,
    source_handle:    e.sourceHandle ?? 'default',
    target_client_id: e.target,
  }))
  return { nodeRequests, edgeRequests }
}

/** Structural changes (add/remove) alter the saved graph; selection and measurement don't. */
export function isStructural(changes: (NodeChange | EdgeChange)[]) {
  return changes.some(c => c.type === 'add' || c.type === 'remove')
}

// ── Conditions ────────────────────────────────────────────────────────────────
export const conditionOpOptions = [
  '==', '!=', '>', '>=', '<', '<=',
  'contains', 'not_contains', 'exists', 'not_exists', 'is_empty', 'not_empty',
  'rule_matches', 'rule_not_matches',
].map(op => ({ label: op, value: op }))

export const numericOps = new Set(['>', '>=', '<', '<='])
export const ruleOps = new Set(['rule_matches', 'rule_not_matches'])
export const valuelessOps = new Set(['is_empty', 'not_empty', 'exists', 'not_exists', 'rule_matches', 'rule_not_matches'])

/** Short text for one condition, e.g. `category == strike`. */
export function conditionSummary(c: PipelineCondition, ruleName?: (id: string) => string | undefined) {
  if (ruleOps.has(c.op)) return `${c.op === 'rule_matches' ? 'matches' : 'not matches'} ${ruleName?.(c.key) ?? `rule #${c.key}`}`
  return valuelessOps.has(c.op) ? `${c.key} ${c.op}` : `${c.key} ${c.op} ${c.value ?? ''}`
}
