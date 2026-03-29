<script setup lang="ts">
import { Handle, Position } from '@vue-flow/core'
import type { ChainCondition } from '../../api/chains'

defineProps<{
  data: { label: string; conditions?: ChainCondition[]; selected?: boolean }
}>()
</script>

<template>
  <div style="
    padding: 8px 12px;
    background: #1e1e2e; border: 1px solid #f9e2af;
    transform: rotate(45deg); width: 100px; height: 100px;
    display: flex; align-items: center; justify-content: center;
    position: relative;
  " :style="{ borderColor: data.selected ? '#cba6f7' : '#f9e2af' }">
    <Handle type="target" :position="Position.Left" id="input"
      style="background: #585b70; left: -6px; top: 50%; transform: rotate(-45deg)" />

    <div style="transform: rotate(-45deg); text-align: center; font-size: 11px; color: #f9e2af;">
      <div style="font-weight: 600">{{ data.label || 'Condition' }}</div>
      <div v-if="data.conditions?.length" style="font-size: 10px; color: #a6adc8; margin-top: 2px">
        {{ data.conditions.length }} rule{{ data.conditions.length !== 1 ? 's' : '' }}
      </div>
    </div>

    <!-- pass handle: right side -->
    <Handle type="source" :position="Position.Right" id="pass"
      style="background: #a6e3a1; right: -6px; top: 50%; transform: rotate(-45deg)" />
    <!-- fail handle: bottom -->
    <Handle type="source" :position="Position.Bottom" id="fail"
      style="background: #f38ba8; bottom: -6px; left: 50%; transform: rotate(-45deg)" />
  </div>
</template>
