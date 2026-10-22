<!-- Run a pipeline over historical signals starting at one node (POST /api/pipelines/{id}/run with start_node_id). -->
<script setup lang="ts">
import { ref, watch } from 'vue'
import { Button, Dialog, FormField, RadioGroup } from '@/components/ui'
import { useToast } from '@/composables/useToast'
import { runPipeline } from '../../api/pipelines'

const props = defineProps<{
  pipelineId: number
  nodeId: string | null
  nodeLabel: string
}>()

const open  = defineModel<boolean>('open', { default: false })
const toast = useToast()

const newOnly = ref(false)
const order   = ref<'asc' | 'desc'>('desc')
const running = ref(false)

const newOnlyOptions = [
  { label: 'All eligible signals', value: false },
  { label: "Only signals not yet processed by this node's action", value: true },
]
const orderOptions = [
  { label: 'Newest first (default)', value: 'desc' as const },
  { label: 'Oldest first', value: 'asc' as const },
]

// Each opening starts from the defaults.
watch(open, isOpen => {
  if (!isOpen) return
  newOnly.value = false
  order.value   = 'desc'
})

async function onRun() {
  if (!props.nodeId) return
  const numId = parseInt(props.nodeId, 10)
  if (isNaN(numId)) { toast.error('Invalid node id'); return }
  running.value = true
  try {
    const res = await runPipeline(props.pipelineId, newOnly.value, numId, order.value === 'asc' ? 'asc' : undefined)
    toast.success(res.async
      ? `Running from "${props.nodeLabel}" in the background`
      : `Queued ${res.queued} signals from "${props.nodeLabel}"`)
    open.value = false
  } catch (e: unknown) {
    toast.error(`Could not run from "${props.nodeLabel}": ${e instanceof Error ? e.message : String(e)}`)
  } finally {
    running.value = false
  }
}
</script>

<template>
  <Dialog
    v-model:open="open"
    :title="`Run from: ${nodeLabel}`"
    description="Prior detection results for each signal will be available as context."
  >
    <div class="flex flex-col gap-4">
      <FormField label="Signals">
        <RadioGroup v-model="newOnly" :options="newOnlyOptions" name="run-new-only" />
      </FormField>
      <FormField label="Order">
        <RadioGroup v-model="order" :options="orderOptions" name="run-order" />
      </FormField>
    </div>
    <template #footer>
      <Button variant="outline" size="default" @click="open = false">Cancel</Button>
      <Button size="default" :loading="running" @click="onRun">Run</Button>
    </template>
  </Dialog>
</template>
