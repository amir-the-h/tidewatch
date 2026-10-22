<!-- "+" beside a node output: opens the node picker to add a step after it. Sits just right of the output
     handle; once the output is connected it shows only on hover or when the node is selected. -->
<script setup lang="ts">
import { PlusIcon } from '@lucide/vue'
import { useBuilder } from './builderGraph'

const props = defineProps<{ nodeId: string; handle: string; connected: boolean }>()
const { addAfter } = useBuilder()
</script>

<template>
  <button
    type="button"
    class="tw-add-step nodrag nopan absolute top-1/2 -right-9 flex size-5 -translate-y-1/2 cursor-pointer items-center justify-center rounded-full border border-border bg-card p-0 text-muted-foreground transition-opacity hover:border-primary hover:text-primary"
    :class="{ 'tw-add-step-connected': props.connected }"
    :aria-label="`Add step after ${handle === 'default' ? 'this node' : handle}`"
    :title="`Add step${handle === 'default' ? '' : ` on ${handle}`}`"
    @click.stop="addAfter(nodeId, handle)"
  >
    <PlusIcon class="size-3.5" :stroke-width="2" />
  </button>
</template>
