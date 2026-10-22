<!-- Badge — a short static label (type, role, entity type). See docs/ui-standard.md → Badge. -->
<script setup lang="ts">
withDefaults(defineProps<{
  variant?: 'secondary' | 'outline' | 'default' | 'destructive'
}>(), { variant: 'secondary' })

const variants = {
  secondary: 'bg-secondary text-secondary-foreground',
  outline: 'border border-border text-foreground',
  default: 'bg-primary text-primary-foreground',
  destructive: 'bg-destructive text-destructive-foreground',
}
</script>

<template>
  <span
    class="inline-flex h-5 items-center gap-1 rounded-sm px-1.5 text-small font-medium whitespace-nowrap"
    :class="variants[variant]"
  >
    <slot />
  </span>
</template>
