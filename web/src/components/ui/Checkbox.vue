<!-- Checkbox — wraps n-checkbox (shadcn-vue: Checkbox). The default slot is the label. -->
<script setup lang="ts">
defineProps<{ disabled?: boolean }>()

const model = defineModel<boolean>({ default: false })
</script>

<template>
  <n-checkbox v-model:checked="model" size="small" :disabled="disabled">
    <slot />
  </n-checkbox>
</template>
