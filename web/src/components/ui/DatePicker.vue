<!-- DatePicker — wraps n-date-picker (shadcn-vue: Calendar + Popover). Value is epoch milliseconds.
     `default-time` ("HH:mm:ss") is the time filled in when a datetime pick has only a date. -->
<script setup lang="ts">
withDefaults(defineProps<{
  type?: 'date' | 'datetime'
  placeholder?: string
  clearable?: boolean
  defaultTime?: string
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
    :default-time="defaultTime"
  />
</template>
