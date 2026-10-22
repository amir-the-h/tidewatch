<!-- Dialog — centred modal over n-modal (shadcn-vue: Dialog). Footer slot holds the buttons. -->
<script setup lang="ts">
defineProps<{
  title: string
  description?: string
}>()

const open = defineModel<boolean>('open', { default: false })
</script>

<template>
  <n-modal v-model:show="open" :auto-focus="false">
    <div
      role="dialog"
      aria-modal="true"
      class="flex w-[min(480px,calc(100vw-32px))] flex-col rounded-lg border border-border bg-popover text-popover-foreground shadow-xl"
    >
      <header class="p-4 pb-0">
        <h2 class="m-0 text-title font-semibold">{{ title }}</h2>
        <p v-if="description" class="m-0 mt-0.5 text-small text-muted-foreground">{{ description }}</p>
      </header>
      <div class="p-4">
        <slot />
      </div>
      <footer v-if="$slots.footer" class="flex justify-end gap-2 p-4 pt-0">
        <slot name="footer" />
      </footer>
    </div>
  </n-modal>
</template>
