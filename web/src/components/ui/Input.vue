<!-- Input — wraps n-input (shadcn-vue: Input). `type="password"` adds a show/hide toggle. -->
<script setup lang="ts">
defineProps<{
  id?: string
  type?: 'text' | 'password'
  placeholder?: string
  clearable?: boolean
  disabled?: boolean
  autocomplete?: string
}>()

const model = defineModel<string>({ default: '' })
defineEmits<{ enter: [] }>()
</script>

<template>
  <n-input
    v-model:value="model"
    size="small"
    :type="type ?? 'text'"
    :show-password-on="type === 'password' ? 'click' : undefined"
    :placeholder="placeholder"
    :clearable="clearable"
    :disabled="disabled"
    :input-props="id || autocomplete ? { id, autocomplete } : undefined"
    @keyup.enter="$emit('enter')"
  />
</template>
