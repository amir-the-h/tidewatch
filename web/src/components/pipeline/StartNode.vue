<script setup lang="ts">
import { computed } from 'vue'
import { Handle, Position, useVueFlow } from '@vue-flow/core'
import AddStepButton from './AddStepButton.vue'
import NodeHeader from './NodeHeader.vue'
import { kindMeta, useBuilder } from './builderGraph'

const props = defineProps<{ id: string }>()

const { edges, removeEdges } = useVueFlow()
const { triggers } = useBuilder()
const connected = computed(() => edges.value.some(e => e.source === props.id))

function disconnectSource() {
  const ids = edges.value.filter(e => e.source === props.id).map(e => e.id)
  if (ids.length) removeEdges(ids)
}
</script>

<template>
  <div class="tw-node group relative w-40 rounded-md border border-l-4 border-border bg-card px-3 py-2 text-card-foreground" :class="kindMeta.start.border">
    <NodeHeader :meta="kindMeta.start" />
    <div class="mt-0.5 truncate text-small text-muted-foreground" :title="triggers.join(', ')">
      {{ triggers.length ? triggers.join(', ') : 'No trigger linked' }}
    </div>
    <Handle type="source" :position="Position.Right" id="default"
      @contextmenu.prevent="disconnectSource" />
    <AddStepButton :node-id="id" handle="default" :connected="connected" />
  </div>
</template>
