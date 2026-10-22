<!-- TagsInput — free-text list of short values over n-dynamic-tags (shadcn-vue: TagsInput). -->
<script setup lang="ts">
defineProps<{
  placeholder?: string
  disabled?: boolean
}>()

const model = defineModel<string[]>({ default: () => [] })
</script>

<template>
  <n-dynamic-tags
    v-model:value="model"
    size="small"
    :disabled="disabled"
    :input-props="placeholder ? { placeholder } : undefined"
  />
</template>
