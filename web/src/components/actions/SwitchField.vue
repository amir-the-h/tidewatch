<!-- A boolean setting: label and help text on the left, Switch on the right (as in RulesTab's "Local rule"). -->
<script setup lang="ts">
import { Switch } from '@/components/ui'

defineProps<{
  label: string
  description?: string
}>()

const model = defineModel<boolean>({ default: false })
</script>

<template>
  <div class="flex items-start justify-between gap-4">
    <div class="min-w-0">
      <p class="m-0 text-small font-medium text-foreground">{{ label }}</p>
      <p v-if="description" class="m-0 mt-0.5 text-small text-muted-foreground">{{ description }}</p>
    </div>
    <Switch v-model="model" :aria-label="label" />
  </div>
</template>
