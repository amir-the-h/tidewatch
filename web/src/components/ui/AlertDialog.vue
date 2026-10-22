<!-- AlertDialog — confirms a destructive action (shadcn-vue: AlertDialog). The confirm button names the action.
     See docs/ui-standard.md → Interaction. -->
<script setup lang="ts">
import Button from './Button.vue'
import Dialog from './Dialog.vue'

withDefaults(defineProps<{
  title: string
  description?: string
  /** Confirm button label, naming the action ("Delete pipeline"). */
  actionLabel: string
  variant?: 'destructive' | 'default'
  loading?: boolean
}>(), { variant: 'destructive' })

const open = defineModel<boolean>('open', { default: false })
defineEmits<{ confirm: [] }>()
</script>

<template>
  <Dialog v-model:open="open" :title="title" :description="description">
    <slot />
    <template #footer>
      <Button variant="outline" :disabled="loading" @click="open = false">Cancel</Button>
      <Button :variant="variant" :loading="loading" @click="$emit('confirm')">{{ actionLabel }}</Button>
    </template>
  </Dialog>
</template>
