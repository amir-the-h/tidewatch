<script setup lang="ts">
import { computed } from 'vue'
import { Handle, Position, useVueFlow } from '@vue-flow/core'
import AddStepButton from './AddStepButton.vue'
import NodeHeader from './NodeHeader.vue'
import { kindMeta, useBuilder } from './builderGraph'

const props = defineProps<{
  id: string
  data: { label: string; subPipelineId?: number | null }
}>()

const { edges, removeEdges } = useVueFlow()
const { pipelines } = useBuilder()

const sub       = computed(() => (props.data.subPipelineId ? pipelines.value.get(props.data.subPipelineId) : undefined))
const title     = computed(() => props.data.label || sub.value?.name || 'Sub-pipeline')
const connected = computed(() => edges.value.some(e => e.source === props.id))

function disconnectSource() {
  const ids = edges.value.filter(e => e.source === props.id).map(e => e.id)
  if (ids.length) removeEdges(ids)
}

function disconnectTarget() {
  const ids = edges.value.filter(e => e.target === props.id).map(e => e.id)
  if (ids.length) removeEdges(ids)
}
</script>

<template>
  <div
    class="tw-node group relative w-60 rounded-md border border-l-4 border-border bg-card px-3 py-2 text-card-foreground"
    :class="[kindMeta.pipeline.border, { 'opacity-60': sub && !sub.active }]"
  >
    <Handle type="target" :position="Position.Left" id="input"
      @contextmenu.prevent="disconnectTarget" />
    <NodeHeader :meta="kindMeta.pipeline" :inactive="sub && !sub.active" />
    <div class="mt-0.5 truncate font-semibold" :title="title">{{ title }}</div>
    <div v-if="!sub" class="truncate text-small text-destructive">No pipeline selected</div>
    <div v-else-if="sub.name !== title" class="truncate text-small text-muted-foreground">{{ sub.name }}</div>
    <Handle type="source" :position="Position.Right" id="default"
      @contextmenu.prevent="disconnectSource" />
    <AddStepButton :node-id="id" handle="default" :connected="connected" />
  </div>
</template>
