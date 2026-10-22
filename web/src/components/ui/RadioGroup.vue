<!-- RadioGroup — wraps n-radio-group + n-radio (shadcn-vue: RadioGroup/RadioGroupItem). Options stack vertically. -->
<script setup lang="ts" generic="V extends string | number | boolean">
defineProps<{
  options: { label: string; value: V }[]
  name?: string
}>()

const model = defineModel<V>({ required: true })
</script>

<template>
  <n-radio-group v-model:value="model" :name="name" size="small">
    <div class="flex flex-col gap-2">
      <n-radio v-for="opt in options" :key="String(opt.value)" :value="opt.value">{{ opt.label }}</n-radio>
    </div>
  </n-radio-group>
</template>
