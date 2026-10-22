<!-- NumberField — wraps n-input-number (shadcn-vue: NumberField). Empty input is `null`. -->
<script setup lang="ts">
defineProps<{
  id?: string
  min?: number
  max?: number
  step?: number
  placeholder?: string
  disabled?: boolean
}>()

const model = defineModel<number | null>({ default: null })
</script>

<template>
  <n-input-number
    v-model:value="model"
    size="small"
    class="w-full"
    :min="min"
    :max="max"
    :step="step"
    :placeholder="placeholder"
    :disabled="disabled"
    :input-props="id ? { id } : undefined"
  />
</template>
