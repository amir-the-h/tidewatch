<!-- Actions on the selected signals (admin only): reprocess, delete, and clear their execution history. -->
<script setup lang="ts">
import { EraserIcon, ListChecksIcon, RefreshCwIcon, TrashIcon, XIcon } from '@lucide/vue'
import { Button, IconButton } from '@/components/ui'

defineProps<{ count: number; reprocessing?: boolean }>()

defineEmits<{
  reprocess: []
  delete: []
  clearAll: []
  clearForActions: []
  deselect: []
}>()
</script>

<template>
  <div class="flex flex-wrap items-center gap-2 rounded-lg border border-border bg-muted px-3 py-2">
    <span class="text-small font-medium tabular">{{ count }} selected</span>
    <IconButton :icon="XIcon" label="Clear selection" @click="$emit('deselect')" />
    <div class="ml-auto flex flex-wrap items-center gap-2">
      <Button variant="outline" :loading="reprocessing" @click="$emit('reprocess')">
        <template #icon><RefreshCwIcon :stroke-width="1.75" /></template>
        Reprocess
      </Button>
      <Button variant="outline" @click="$emit('clearAll')">
        <template #icon><EraserIcon :stroke-width="1.75" /></template>
        Clear executions
      </Button>
      <Button variant="outline" @click="$emit('clearForActions')">
        <template #icon><ListChecksIcon :stroke-width="1.75" /></template>
        Clear for actions…
      </Button>
      <Button variant="destructive" @click="$emit('delete')">
        <template #icon><TrashIcon :stroke-width="1.75" /></template>
        Delete
      </Button>
    </div>
  </div>
</template>
