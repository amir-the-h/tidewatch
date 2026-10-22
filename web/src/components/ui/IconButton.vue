<!-- IconButton — icon-only Button with the tooltip and aria-label the standard requires. Composed from Button + Tooltip. -->
<script setup lang="ts">
import type { Component } from 'vue'
import Button from './Button.vue'
import Tooltip from './Tooltip.vue'

withDefaults(defineProps<{
  icon: Component
  label: string
  variant?: 'ghost' | 'outline' | 'secondary' | 'default' | 'destructive'
  disabled?: boolean
  loading?: boolean
}>(), { variant: 'ghost' })

defineEmits<{ click: [event: MouseEvent] }>()
</script>

<template>
  <Tooltip :content="label">
    <Button
      :variant="variant"
      size="icon"
      :disabled="disabled"
      :loading="loading"
      :aria-label="label"
      @click="$emit('click', $event)"
    >
      <template #icon><component :is="icon" :stroke-width="1.75" /></template>
    </Button>
  </Tooltip>
</template>
