<!-- Tooltip — wraps n-tooltip. Default slot is the trigger; `content` is the text. -->
<script setup lang="ts">
defineProps<{
  content: string
  side?: 'top' | 'bottom' | 'left' | 'right'
}>()
</script>

<template>
  <n-tooltip trigger="hover" :delay="200" :placement="side ?? 'top'">
    <template #trigger><slot /></template>
    {{ content }}
  </n-tooltip>
</template>
