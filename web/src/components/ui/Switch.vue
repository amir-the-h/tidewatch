<!-- Switch — wraps n-switch (shadcn-vue: Switch). -->
<script setup lang="ts">
defineProps<{ disabled?: boolean; loading?: boolean }>()

const model = defineModel<boolean>({ default: false })
</script>

<template>
  <n-switch v-model:value="model" size="small" :disabled="disabled" :loading="loading" />
</template>
