<script setup lang="ts">
import { Handle, Position } from '@vue-flow/core'
import AddStepButton from './AddStepButton.vue'
import NodeHeader from './NodeHeader.vue'
import { kindMeta, useBuilder, useNodeEdges } from './builderGraph'

const props = defineProps<{ id: string }>()

const { isConnected, disconnectSource } = useNodeEdges(props.id)
const { triggers } = useBuilder()
</script>

<template>
  <div class="tw-node group relative w-40 rounded-md border border-l-4 border-border bg-card px-3 py-2 text-card-foreground" :class="kindMeta.start.border">
    <NodeHeader :meta="kindMeta.start" />
    <div class="mt-0.5 truncate text-small text-muted-foreground" :title="triggers.join(', ')">
      {{ triggers.length ? triggers.join(', ') : 'No trigger linked' }}
    </div>
    <Handle type="source" :position="Position.Right" id="default"
      @contextmenu.prevent="disconnectSource()" />
    <AddStepButton :node-id="id" handle="default" :connected="isConnected()" />
  </div>
</template>
