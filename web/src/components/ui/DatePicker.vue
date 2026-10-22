<!-- DatePicker — wraps n-date-picker (shadcn-vue: Calendar + Popover). Value is epoch milliseconds. -->
<script setup lang="ts">
withDefaults(defineProps<{
  type?: 'date' | 'datetime'
  placeholder?: string
  clearable?: boolean
}>(), { type: 'datetime' })

const model = defineModel<number | null>({ default: null })
</script>

<template>
  <n-date-picker
    v-model:value="model"
    size="small"
    :type="type"
    :placeholder="placeholder"
    :clearable="clearable"
  />
</template>
