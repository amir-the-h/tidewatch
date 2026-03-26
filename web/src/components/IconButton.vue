<!-- Icon-only button with hover tooltip, for use in templates (counterpart to iconBtn() for render functions) -->
<script setup lang="ts">
import type { Component } from 'vue'

defineProps<{
  icon: Component
  label: string
  type?: 'default' | 'primary' | 'info' | 'success' | 'warning' | 'error'
  ghost?: boolean
  disabled?: boolean
  loading?: boolean
}>()

const emit = defineEmits<{ click: [] }>()
</script>

<template>
  <n-tooltip trigger="hover" :delay="200">
    <template #trigger>
      <n-button
        size="small"
        :type="type"
        :ghost="ghost"
        :disabled="disabled"
        :loading="loading"
        @click="emit('click')"
      >
        <template #icon><n-icon><component :is="icon" /></n-icon></template>
      </n-button>
    </template>
    {{ label }}
  </n-tooltip>
</template>
