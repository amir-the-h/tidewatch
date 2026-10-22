<!-- Textarea — wraps n-input type="textarea" (shadcn-vue: Textarea).
     `autosize` grows the field with its content from `rows` up to `maxRows` (shadcn: field-sizing-content).
     Native listeners such as @keydown fall through to the field; `focus()` is exposed like a native textarea. -->
<script setup lang="ts">
import { ref } from 'vue'

withDefaults(defineProps<{
  id?: string
  placeholder?: string
  /** Visible rows; the minimum when `autosize` is set. */
  rows?: number
  autosize?: boolean
  /** Row limit for `autosize`; the field scrolls beyond it. */
  maxRows?: number
  disabled?: boolean
}>(), { rows: 3 })

const model = defineModel<string>({ default: '' })

const input = ref<{ focus: () => void } | null>(null)
defineExpose({ focus: () => input.value?.focus() })
</script>

<template>
  <n-input
    ref="input"
    v-model:value="model"
    type="textarea"
    size="small"
    :rows="rows"
    :autosize="autosize ? { minRows: rows, maxRows } : false"
    :placeholder="placeholder"
    :disabled="disabled"
    :input-props="id ? { id } : undefined"
  />
</template>
