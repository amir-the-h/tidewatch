<!-- Select — wraps n-select (shadcn-vue: Select, or Combobox when `filterable`). -->
<script setup lang="ts" generic="V extends string | number">
defineProps<{
  options: { label: string; value: V }[]
  placeholder?: string
  clearable?: boolean
  filterable?: boolean
  loading?: boolean
  disabled?: boolean
}>()

const model = defineModel<V | null>({ default: null })
</script>

<template>
  <n-select
    v-model:value="model"
    size="small"
    :options="options"
    :placeholder="placeholder"
    :clearable="clearable"
    :filterable="filterable"
    :loading="loading"
    :disabled="disabled"
  />
</template>
