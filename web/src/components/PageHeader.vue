<!-- Page header: title, one-line description and the page's primary action; optional toolbar row below.
     See docs/ui-standard.md → PageHeader. -->
<script setup lang="ts">
import { PlusIcon } from '@lucide/vue'
import { Button } from '@/components/ui'

defineProps<{
  title: string
  description?: string
  /** Label for the default primary "add" button. Omit to hide it and use the default slot instead. */
  addLabel?: string
}>()

const emit = defineEmits<{ add: [] }>()
</script>

<template>
  <div class="flex flex-col gap-4">
    <div class="flex flex-wrap items-start justify-between gap-4">
      <div class="min-w-0">
        <h1 class="m-0 text-page-title font-semibold">{{ title }}</h1>
        <p v-if="description" class="m-0 mt-0.5 text-muted-foreground">{{ description }}</p>
      </div>
      <div class="flex items-center gap-2">
        <slot>
          <Button v-if="addLabel" @click="emit('add')">
            <template #icon><PlusIcon :stroke-width="1.75" /></template>
            {{ addLabel }}
          </Button>
        </slot>
      </div>
    </div>
    <div v-if="$slots.toolbar" class="flex flex-wrap items-center gap-2">
      <slot name="toolbar" />
    </div>
  </div>
</template>
