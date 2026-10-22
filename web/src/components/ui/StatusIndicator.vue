<!-- StatusIndicator — dot plus label for live state. Never colour alone. See docs/ui-standard.md → StatusIndicator. -->
<script setup lang="ts">
withDefaults(defineProps<{
  status: 'success' | 'info' | 'warning' | 'destructive' | 'idle'
}>(), {})

const dot = {
  success: 'bg-success',
  info: 'bg-info',
  warning: 'bg-warning',
  destructive: 'bg-destructive',
  idle: 'ring-[1.5px] ring-inset ring-muted-foreground',
}

const label = {
  success: 'text-foreground',
  info: 'text-foreground',
  warning: 'text-warning',
  destructive: 'text-destructive',
  idle: 'text-muted-foreground',
}
</script>

<template>
  <span class="inline-flex items-center gap-1.5 text-small font-medium" :class="label[status]">
    <span class="size-2 shrink-0 rounded-full" :class="dot[status]" aria-hidden="true" />
    <slot />
  </span>
</template>
