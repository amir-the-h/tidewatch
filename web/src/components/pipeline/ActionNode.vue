<script setup lang="ts">
import { Handle, Position, useVueFlow } from '@vue-flow/core'

const props = defineProps<{
  id: string
  data: {
    label: string
    pipelineType?: string
    selected?: boolean
    onRunFromNode?: (id: string) => void
  }
}>()

const { edges, removeEdges } = useVueFlow()

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
  <div class="action-node" :style="{ borderColor: data.selected ? '#cba6f7' : '#585b70' }">
    <Handle type="target" :position="Position.Left" id="input" style="background: #585b70"
      @contextmenu.prevent="disconnectTarget" />
    <div style="font-weight: 600; margin-bottom: 2px">{{ data.label }}</div>
    <div v-if="data.pipelineType" style="font-size: 10px; color: #6c7086">{{ data.pipelineType }}</div>
    <button
      v-if="typeof data.onRunFromNode === 'function'"
      class="run-from-btn"
      title="Run from here"
      @mousedown.stop
      @click.stop="data.onRunFromNode(id)"
    >▶</button>
    <Handle type="source" :position="Position.Right" id="default" style="background: #a6e3a1"
      @contextmenu.prevent="disconnectSource" />
  </div>
</template>

<style scoped>
.action-node {
  position: relative;
  padding: 8px 12px;
  border-radius: 6px;
  background: #1e1e2e;
  border: 1px solid #585b70;
  min-width: 120px;
  font-size: 12px;
  color: #cdd6f4;
}
.run-from-btn {
  position: absolute;
  top: 4px;
  right: 4px;
  background: transparent;
  border: none;
  color: #6c7086;
  cursor: pointer;
  font-size: 10px;
  padding: 2px 4px;
  border-radius: 3px;
  opacity: 0;
  transition: opacity 0.15s, color 0.15s;
}
.action-node:hover .run-from-btn {
  opacity: 1;
}
.run-from-btn:hover {
  color: #a6e3a1;
  background: #313244;
}
</style>
