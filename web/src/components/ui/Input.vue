<!-- Input — wraps n-input (shadcn-vue: Input). -->
<script setup lang="ts">
defineProps<{
  id?: string
  placeholder?: string
  clearable?: boolean
  disabled?: boolean
}>()

const model = defineModel<string>({ default: '' })
defineEmits<{ enter: [] }>()
</script>

<template>
  <n-input
    v-model:value="model"
    size="small"
    :placeholder="placeholder"
    :clearable="clearable"
    :disabled="disabled"
    :input-props="id ? { id } : undefined"
    @keyup.enter="$emit('enter')"
  />
</template>
