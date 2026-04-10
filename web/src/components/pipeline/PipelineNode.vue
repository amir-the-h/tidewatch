<script setup lang="ts">
import { Handle, Position } from '@vue-flow/core'
import { NTag, NText } from 'naive-ui'

const props = defineProps<{
  id: string
  data: {
    label: string
    subPipelineId?: number
    subPipelineName?: string
    onRunFromNode?: (id: string) => void
  }
  selected?: boolean
}>()
</script>

<template>
  <div :class="['pipeline-node', { selected }]">
    <Handle type="target" :position="Position.Top" />
    <div class="pipeline-node__header">
      <NTag type="info" size="small" :bordered="false">pipeline</NTag>
    </div>
    <NText class="pipeline-node__name">{{ data.subPipelineName || `Pipeline #${data.subPipelineId}` }}</NText>
    <div v-if="data.label" class="pipeline-node__label">{{ data.label }}</div>
    <button
      v-if="typeof data.onRunFromNode === 'function'"
      class="run-from-btn"
      title="Run from here"
      @mousedown.stop
      @click.stop="data.onRunFromNode(id)"
    >▶</button>
    <Handle type="source" :position="Position.Bottom" />
  </div>
</template>

<style scoped>
.pipeline-node {
  position: relative;
  background: #2d1b69;
  border: 2px solid #7c3aed;
  border-radius: 8px;
  padding: 8px 12px;
  min-width: 140px;
  cursor: pointer;
}
.pipeline-node.selected { border-color: #a78bfa; }
.pipeline-node__header { margin-bottom: 4px; }
.pipeline-node__name { font-size: 13px; font-weight: 600; display: block; }
.pipeline-node__label { font-size: 11px; color: #a0a0a0; margin-top: 2px; }
.run-from-btn {
  position: absolute;
  top: 4px;
  right: 4px;
  background: transparent;
  border: none;
  color: #a78bfa80;
  cursor: pointer;
  font-size: 10px;
  padding: 2px 4px;
  border-radius: 3px;
  opacity: 0;
  transition: opacity 0.15s, color 0.15s;
}
.pipeline-node:hover .run-from-btn {
  opacity: 1;
}
.run-from-btn:hover {
  color: #a78bfa;
  background: #3d2a7a;
}
</style>
