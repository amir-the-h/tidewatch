<!-- Combobox — searchable multi-select over n-select (shadcn-vue: Combobox with `multiple`).
     For a single value use Select with `filterable`. -->
<script setup lang="ts" generic="V extends string | number">
defineProps<{
  options: { label: string; value: V }[]
  placeholder?: string
  clearable?: boolean
  disabled?: boolean
}>()

const model = defineModel<V[]>({ default: () => [] })
</script>

<template>
  <n-select
    v-model:value="model"
    size="small"
    multiple
    filterable
    :options="options"
    :placeholder="placeholder"
    :clearable="clearable"
    :disabled="disabled"
  />
</template>
