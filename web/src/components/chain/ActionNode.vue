<script setup lang="ts">
import { Handle, Position } from '@vue-flow/core'

defineProps<{
  data: { label: string; pipelineType?: string; selected?: boolean }
}>()
</script>

<template>
  <div style="
    padding: 8px 12px; border-radius: 6px;
    background: #1e1e2e; border: 1px solid #585b70;
    min-width: 120px; font-size: 12px; color: #cdd6f4;
  " :style="{ borderColor: data.selected ? '#cba6f7' : '#585b70' }">
    <Handle type="target" :position="Position.Left" id="input" style="background: #585b70" />
    <div style="font-weight: 600; margin-bottom: 2px">{{ data.label }}</div>
    <div v-if="data.pipelineType" style="font-size: 10px; color: #6c7086">{{ data.pipelineType }}</div>
    <Handle type="source" :position="Position.Right" id="default" style="background: #a6e3a1" />
  </div>
</template>
