<!-- Slider — single-thumb slider over n-slider (shadcn-vue: Slider). shadcn's model is `number[]`; this
     wrapper takes one number, and converts to/from `[value]` internally once it is swapped. -->
<script setup lang="ts">
withDefaults(defineProps<{
  min?: number
  max?: number
  step?: number
  disabled?: boolean
}>(), { min: 0, max: 100, step: 1 })

const model = defineModel<number>({ default: 0 })
</script>

<template>
  <n-slider v-model:value="model" :min="min" :max="max" :step="step" :disabled="disabled" :tooltip="false" />
</template>
