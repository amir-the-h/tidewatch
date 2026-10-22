<!-- Inspector for the selected node or connection. Edits apply to the node live (the graph stays unsaved
     until Save); emits `change` so the view can mark it dirty. -->
<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import type { Edge, GraphNode } from '@vue-flow/core'
import { ExternalLinkIcon, PlayIcon, PlusIcon, Trash2Icon, XIcon } from '@lucide/vue'
import { Button, FormField, IconButton, Input, NumberField, Select } from '@/components/ui'
import type { PipelineCondition } from '../../api/pipelines'
import {
  actionDetail, conditionOpOptions, kindMeta, numericOps, ruleOps, useBuilder, valuelessOps,
} from './builderGraph'
import NodeHeader from './NodeHeader.vue'

const props = defineProps<{
  node?: GraphNode
  edge?: Edge
  /** Label of a node by id, for describing a connection. */
  nodeLabel: (id: string) => string
  ruleOptions: { label: string; value: string }[]
}>()

const emit = defineEmits<{
  change: []
  close: []
  deleteNode: []
  deleteEdge: []
  runFrom: []
}>()

const { actions, pipelines } = useBuilder()

const actionOptions = computed(() =>
  [...actions.value.values()]
    .sort((a, b) => a.name.localeCompare(b.name))
    .map(a => ({ label: `${a.name}${a.active ? '' : ' (inactive)'} #${a.id}`, value: a.id }))
)
const pipelineOptions = computed(() =>
  [...pipelines.value.values()]
    .sort((a, b) => a.name.localeCompare(b.name))
    .map(p => ({ label: `${p.name}${p.active ? '' : ' (inactive)'} #${p.id}`, value: p.id }))
)

const action = computed(() => (props.node?.data.actionId ? actions.value.get(props.node.data.actionId) : undefined))
const sub    = computed(() => (props.node?.data.subPipelineId ? pipelines.value.get(props.node.data.subPipelineId) : undefined))

const meta = computed(() => {
  const t = props.node?.type
  if (t === 'action') return kindMeta[action.value?.type ?? 'internal']
  if (t === 'condition' || t === 'pipeline' || t === 'start') return kindMeta[t]
  return kindMeta.internal
})

// Run-from-node needs a saved node id; nodes added since the last save have client ids.
const canRun = computed(() => !!props.node && /^\d+$/.test(props.node.id))

const conditions = computed<PipelineCondition[]>(() => props.node?.data.conditions ?? [])

function set(field: 'label' | 'actionId' | 'subPipelineId', value: unknown) {
  if (!props.node) return
  props.node.data = { ...props.node.data, [field]: value }
  emit('change')
}

function setConditions(next: PipelineCondition[]) {
  if (!props.node) return
  props.node.data = { ...props.node.data, conditions: next }
  emit('change')
}

function updateCondition(idx: number, patch: Partial<PipelineCondition>) {
  setConditions(conditions.value.map((c, i) => (i === idx ? { ...c, ...patch } : c)))
}

const handleLabel = (h: string | null | undefined) => (h === 'pass' ? 'Pass' : h === 'fail' ? 'Fail' : '')
</script>

<template>
  <!-- Connection -->
  <div v-if="edge && !node" class="flex flex-col gap-3 p-4">
    <div class="flex items-start justify-between gap-2">
      <h2 class="m-0 text-title font-semibold">Connection</h2>
      <IconButton :icon="XIcon" label="Close" @click="emit('close')" />
    </div>
    <p class="m-0 text-muted-foreground">
      <span class="text-foreground">{{ nodeLabel(edge.source) }}</span>
      <template v-if="handleLabel(edge.sourceHandle)"> · {{ handleLabel(edge.sourceHandle) }}</template>
      → <span class="text-foreground">{{ nodeLabel(edge.target) }}</span>
    </p>
    <Button variant="destructive" @click="emit('deleteEdge')">
      <template #icon><Trash2Icon :stroke-width="1.75" /></template>
      Delete connection
    </Button>
  </div>

  <!-- Node -->
  <div v-else-if="node" class="flex min-h-0 flex-1 flex-col">
    <div class="flex items-start justify-between gap-2 p-4 pb-2">
      <div class="min-w-0">
        <NodeHeader :meta="meta" />
        <h2 class="m-0 mt-0.5 truncate text-title font-semibold">
          {{ node.data.label || action?.name || sub?.name || meta.label }}
        </h2>
      </div>
      <IconButton :icon="XIcon" label="Close" @click="emit('close')" />
    </div>

    <div class="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto px-4 pb-4">
      <p v-if="node.type === 'start'" class="m-0 text-muted-foreground">
        Every run of this pipeline begins here. Link triggers to it on the Pipelines page's Triggers tab.
      </p>

      <FormField v-if="node.type !== 'start'" label="Label" for="node-label" description="Shown on the node. Leave empty to use the action or pipeline name.">
        <Input id="node-label" :model-value="node.data.label ?? ''" placeholder="Node label" @update:model-value="v => set('label', v)" />
      </FormField>

      <template v-if="node.type === 'action'">
        <FormField label="Action">
          <Select :model-value="node.data.actionId ?? null" :options="actionOptions" filterable @update:model-value="v => set('actionId', v)" />
        </FormField>
        <dl v-if="action" class="m-0 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 rounded-md border border-border p-3 text-small">
          <dt class="text-muted-foreground">Type</dt><dd class="m-0">{{ kindMeta[action.type].label }}</dd>
          <template v-if="actionDetail(action)">
            <dt class="text-muted-foreground">{{ action.type === 'internal' ? 'Handler' : 'Model' }}</dt>
            <dd class="m-0 truncate font-mono">{{ actionDetail(action) }}</dd>
          </template>
          <dt class="text-muted-foreground">Status</dt>
          <dd class="m-0" :class="action.active ? 'text-success' : 'text-muted-foreground'">{{ action.active ? 'Active' : 'Inactive' }}</dd>
        </dl>
        <RouterLink v-if="action" to="/actions" class="inline-flex items-center gap-1 text-small text-primary no-underline hover:underline">
          Configure in Actions <ExternalLinkIcon class="size-3.5" :stroke-width="1.75" />
        </RouterLink>
      </template>

      <template v-if="node.type === 'pipeline'">
        <FormField label="Sub-pipeline">
          <Select :model-value="node.data.subPipelineId ?? null" :options="pipelineOptions" filterable @update:model-value="v => set('subPipelineId', v)" />
        </FormField>
        <RouterLink v-if="sub" :to="`/pipelines/${sub.id}/build`" class="inline-flex items-center gap-1 text-small text-primary no-underline hover:underline">
          Open {{ sub.name }} <ExternalLinkIcon class="size-3.5" :stroke-width="1.75" />
        </RouterLink>
      </template>

      <section v-if="node.type === 'condition'" class="flex flex-col gap-2">
        <div>
          <h3 class="m-0 text-small font-medium">Rules</h3>
          <p class="m-0 text-small text-muted-foreground">Every rule must hold for Pass, otherwise Fail. With no rules, it always passes.</p>
        </div>
        <div
          v-for="(cond, idx) in conditions"
          :key="idx"
          class="flex flex-col gap-1.5 rounded-md border border-border p-2"
        >
          <div class="flex items-center gap-1">
            <div class="min-w-0 flex-1">
              <Select
                v-if="ruleOps.has(cond.op)"
                :model-value="cond.key || null"
                :options="ruleOptions"
                placeholder="Select rule…"
                filterable
                @update:model-value="v => updateCondition(idx, { key: v ?? '' })"
              />
              <Input v-else :model-value="cond.key" placeholder="key" class="font-mono" @update:model-value="v => updateCondition(idx, { key: v })" />
            </div>
            <IconButton :icon="XIcon" label="Remove rule" @click="setConditions(conditions.filter((_, i) => i !== idx))" />
          </div>
          <Select :model-value="cond.op" :options="conditionOpOptions" @update:model-value="v => updateCondition(idx, { op: v ?? '==' })" />
          <NumberField
            v-if="numericOps.has(cond.op)"
            :model-value="cond.value === '' || cond.value == null ? null : Number(cond.value)"
            placeholder="value"
            @update:model-value="v => updateCondition(idx, { value: v == null ? '' : String(v) })"
          />
          <Input v-else-if="!valuelessOps.has(cond.op)" :model-value="cond.value ?? ''" placeholder="value" @update:model-value="v => updateCondition(idx, { value: v })" />
        </div>
        <Button variant="outline" @click="setConditions([...conditions, { key: '', op: '==', value: '' }])">
          <template #icon><PlusIcon :stroke-width="1.75" /></template>
          Add rule
        </Button>
      </section>
    </div>

    <div v-if="node.type !== 'start'" class="flex flex-col gap-2 border-t border-border p-4">
      <Button v-if="node.type === 'action' || node.type === 'pipeline'" variant="outline" :disabled="!canRun" @click="emit('runFrom')">
        <template #icon><PlayIcon :stroke-width="1.75" /></template>
        Run from here
      </Button>
      <p v-if="(node.type === 'action' || node.type === 'pipeline') && !canRun" class="m-0 text-small text-muted-foreground">
        Save the pipeline to run from this node.
      </p>
      <Button variant="destructive" @click="emit('deleteNode')">
        <template #icon><Trash2Icon :stroke-width="1.75" /></template>
        Delete node
      </Button>
    </div>
  </div>
</template>
