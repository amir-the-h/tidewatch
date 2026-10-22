<!-- Sheet — side panel over n-drawer (shadcn-vue: Sheet). See docs/ui-standard.md → Sheet. -->
<script setup lang="ts">
withDefaults(defineProps<{
  title?: string
  description?: string
  side?: 'left' | 'right'
  width?: string
}>(), {
  side: 'right',
  width: 'min(480px, 100vw)',
})

const open = defineModel<boolean>('open', { default: false })
</script>

<template>
  <n-drawer v-model:show="open" :placement="side" :width="width">
    <div class="flex h-full flex-col bg-card text-card-foreground">
      <header v-if="title || $slots.header" class="border-b border-border p-4">
        <slot name="header">
          <h2 class="m-0 text-title font-semibold">{{ title }}</h2>
          <p v-if="description" class="m-0 mt-0.5 text-small text-muted-foreground">{{ description }}</p>
        </slot>
      </header>
      <div class="flex min-h-0 flex-1 flex-col overflow-y-auto">
        <slot />
      </div>
      <footer v-if="$slots.footer" class="flex justify-end gap-2 border-t border-border p-4">
        <slot name="footer" />
      </footer>
    </div>
  </n-drawer>
</template>
