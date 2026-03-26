<!-- Reusable drawer shell for create/edit forms -->
<script setup lang="ts">
defineProps<{
  title: string
  width?: string
  loading?: boolean
  saveLabel?: string
}>()

const show = defineModel<boolean>('show')
const emit = defineEmits<{ save: [] }>()
</script>

<template>
  <n-drawer v-model:show="show" :width="width ?? 'min(420px, calc(100vw - 16px))'">
    <n-drawer-content :title="title" closable>
      <n-form label-placement="top">
        <slot />
      </n-form>
      <template #footer>
        <n-button type="primary" :loading="loading" @click="emit('save')">
          {{ saveLabel ?? 'Save' }}
        </n-button>
      </template>
    </n-drawer-content>
  </n-drawer>
</template>
