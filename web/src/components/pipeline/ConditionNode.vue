<script setup lang="ts">
import { computed } from 'vue'
import { Handle, Position, useVueFlow } from '@vue-flow/core'
import type { PipelineCondition } from '../../api/pipelines'
import AddStepButton from './AddStepButton.vue'
import NodeHeader from './NodeHeader.vue'
import { conditionSummary, kindMeta } from './builderGraph'

const props = defineProps<{
  id: string
  data: { label: string; conditions?: PipelineCondition[] }
}>()

const { edges, removeEdges } = useVueFlow()

const rules   = computed(() => (props.data.conditions ?? []).filter(c => c.key.trim()))
const outputs = [
  { handle: 'pass', label: 'Pass', text: 'text-success' },
  { handle: 'fail', label: 'Fail', text: 'text-destructive' },
] as const

function isConnected(handle: string) {
  return edges.value.some(e => e.source === props.id && e.sourceHandle === handle)
}

function disconnectHandle(handleId: string) {
  const ids = edges.value
    .filter(e => e.source === props.id && e.sourceHandle === handleId)
    .map(e => e.id)
  if (ids.length) removeEdges(ids)
}

function disconnectTarget() {
  const ids = edges.value.filter(e => e.target === props.id).map(e => e.id)
  if (ids.length) removeEdges(ids)
}
</script>

<template>
  <!-- Each output is its own row, so its handle and "+" centre on that row's right edge. -->
  <div class="tw-node group relative w-60 rounded-md border border-l-4 border-border bg-card text-card-foreground" :class="kindMeta.condition.border">
    <Handle type="target" :position="Position.Left" id="input"
      @contextmenu.prevent="disconnectTarget" />
    <div class="px-3 pt-2">
      <NodeHeader :meta="kindMeta.condition" />
      <div class="mt-0.5 truncate font-semibold">{{ data.label || 'Condition' }}</div>
      <div class="truncate font-mono text-small text-muted-foreground">
        <template v-if="rules.length">
          {{ conditionSummary(rules[0]) }}<span v-if="rules.length > 1"> +{{ rules.length - 1 }}</span>
        </template>
        <span v-else class="font-sans">No rules · always passes</span>
      </div>
    </div>
    <div class="mt-1 border-t border-border">
      <div v-for="out in outputs" :key="out.handle" class="relative flex h-6 items-center justify-end px-3 text-small" :class="out.text">
        {{ out.label }}
        <Handle type="source" :position="Position.Right" :id="out.handle" :class="`tw-handle-${out.handle}`"
          @contextmenu.prevent="disconnectHandle(out.handle)" />
        <AddStepButton :node-id="id" :handle="out.handle" :connected="isConnected(out.handle)" />
      </div>
    </div>
  </div>
</template>
