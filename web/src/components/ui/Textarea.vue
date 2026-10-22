<!-- Textarea — wraps n-input type="textarea" (shadcn-vue: Textarea). -->
<script setup lang="ts">
withDefaults(defineProps<{
  id?: string
  placeholder?: string
  rows?: number
  disabled?: boolean
}>(), { rows: 3 })

const model = defineModel<string>({ default: '' })
</script>

<template>
  <n-input
    v-model:value="model"
    type="textarea"
    size="small"
    :rows="rows"
    :placeholder="placeholder"
    :disabled="disabled"
    :input-props="id ? { id } : undefined"
  />
</template>
