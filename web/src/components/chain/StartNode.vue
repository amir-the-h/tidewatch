<script setup lang="ts">
import { Handle, Position, useVueFlow } from '@vue-flow/core'

const props = defineProps<{ id: string }>()

const { edges, removeEdges } = useVueFlow()

function disconnectSource() {
  const ids = edges.value.filter(e => e.source === props.id).map(e => e.id)
  if (ids.length) removeEdges(ids)
}
</script>

<template>
  <div style="
    padding: 8px 16px; border-radius: 20px;
    background: #313244; border: 2px solid #89dceb;
    font-size: 12px; color: #89dceb; font-weight: 600;
    min-width: 80px; text-align: center;
  ">
    Start
    <Handle type="source" :position="Position.Right" id="default" style="background: #89dceb"
      @contextmenu.prevent="disconnectSource" />
  </div>
</template>
