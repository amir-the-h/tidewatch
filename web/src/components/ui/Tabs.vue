<!-- Tabs — wraps n-tabs (shadcn-vue: Tabs/TabsList/TabsTrigger/TabsContent). One named slot per item value.
     See docs/ui-standard.md → Tabs. -->
<script setup lang="ts">
defineProps<{
  items: { value: string; label: string }[]
}>()

const model = defineModel<string>({ required: true })
</script>

<template>
  <n-tabs v-model:value="model" type="line" size="small" :tabs-padding="0">
    <n-tab-pane v-for="item in items" :key="item.value" :name="item.value" :tab="item.label">
      <div class="pt-4">
        <slot :name="item.value" />
      </div>
    </n-tab-pane>
  </n-tabs>
</template>
