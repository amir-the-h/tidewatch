<script setup lang="ts">
import { computed } from 'vue'
import { Handle, Position } from '@vue-flow/core'
import AddStepButton from './AddStepButton.vue'
import NodeHeader from './NodeHeader.vue'
import { actionDetail, kindMeta, useBuilder, useNodeEdges } from './builderGraph'

const props = defineProps<{
  id: string
  data: { label: string; actionId?: number | null }
}>()

const { isConnected, disconnectSource, disconnectTarget } = useNodeEdges(props.id)
const { actions } = useBuilder()

const action    = computed(() => (props.data.actionId ? actions.value.get(props.data.actionId) : undefined))
const meta      = computed(() => kindMeta[action.value?.type ?? 'internal'])
const title     = computed(() => props.data.label || action.value?.name || 'Action')
const detail    = computed(() => {
  const a = action.value
  if (!a) return ''
  return a.name !== title.value ? a.name : actionDetail(a)
})
</script>

<template>
  <div
    class="tw-node group relative w-60 rounded-md border border-l-4 border-border bg-card px-3 py-2 text-card-foreground"
    :class="[meta.border, { 'opacity-60': action && !action.active }]"
  >
    <Handle type="target" :position="Position.Left" id="input"
      @contextmenu.prevent="disconnectTarget()" />
    <NodeHeader :meta="meta" :inactive="action && !action.active" />
    <div class="mt-0.5 truncate font-semibold" :title="title">{{ title }}</div>
    <div v-if="!action" class="truncate text-small text-destructive">No action selected</div>
    <div v-else-if="detail" class="truncate font-mono text-small text-muted-foreground" :title="detail">{{ detail }}</div>
    <Handle type="source" :position="Position.Right" id="default"
      @contextmenu.prevent="disconnectSource()" />
    <AddStepButton :node-id="id" handle="default" :connected="isConnected()" />
  </div>
</template>
