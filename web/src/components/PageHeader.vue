<!-- Page-level header: title on the left, action buttons on the right -->
<script setup lang="ts">
import { AddCircleOutline } from '@vicons/ionicons5'

defineProps<{
  title: string
  /** Tooltip label for the default add button. Omit to hide it and use the slot instead. */
  addLabel?: string
}>()

const emit = defineEmits<{ add: [] }>()
</script>

<template>
  <n-flex justify="space-between" align="center">
    <n-h2 style="margin: 0">{{ title }}</n-h2>
    <slot>
      <n-tooltip v-if="addLabel" trigger="hover">
        <template #trigger>
          <n-button type="primary" @click="emit('add')">
            <template #icon><n-icon><AddCircleOutline /></n-icon></template>
          </n-button>
        </template>
        {{ addLabel }}
      </n-tooltip>
    </slot>
  </n-flex>
</template>
