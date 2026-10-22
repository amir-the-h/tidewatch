<!-- Button — shadcn-vue API over n-button. See docs/ui-standard.md → Button. -->
<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(defineProps<{
  variant?: 'default' | 'secondary' | 'outline' | 'ghost' | 'destructive' | 'link'
  size?: 'sm' | 'default' | 'lg' | 'icon'
  loading?: boolean
  disabled?: boolean
  nativeType?: 'button' | 'submit' | 'reset'
}>(), {
  variant: 'default',
  size: 'sm',
  nativeType: 'button',
})

defineEmits<{ click: [event: MouseEvent] }>()

const naive = computed(() => {
  switch (props.variant) {
    case 'default': return { type: 'primary' as const }
    case 'secondary': return { secondary: true }
    case 'ghost': return { quaternary: true }
    case 'destructive': return { type: 'error' as const }
    case 'link': return { text: true, type: 'primary' as const }
    default: return {}
  }
})

const naiveSize = computed(() => ({ sm: 'small', default: 'medium', lg: 'large', icon: 'small' } as const)[props.size])
</script>

<template>
  <n-button
    v-bind="naive"
    :size="naiveSize"
    :loading="loading"
    :disabled="disabled"
    :attr-type="nativeType"
    :class="size === 'icon' ? 'w-control-sm px-0!' : undefined"
    @click="$emit('click', $event)"
  >
    <template v-if="$slots.icon" #icon><slot name="icon" /></template>
    <slot />
  </n-button>
</template>
