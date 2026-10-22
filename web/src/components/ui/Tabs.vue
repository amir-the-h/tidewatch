<!-- Tabs — wraps n-tabs (shadcn-vue: Tabs/TabsList/TabsTrigger/TabsContent). One named slot per item value.
     An item's `count` (e.g. items awaiting review) shows next to its label when above zero.
     See docs/ui-standard.md → Tabs. -->
<script setup lang="ts">
defineProps<{
  items: { value: string; label: string; count?: number }[]
}>()

const model = defineModel<string>({ required: true })
</script>

<template>
  <n-tabs v-model:value="model" type="line" size="small" :tabs-padding="0">
    <n-tab-pane v-for="item in items" :key="item.value" :name="item.value">
      <template #tab>
        <span class="inline-flex items-center gap-1.5">
          {{ item.label }}
          <span
            v-if="item.count"
            class="inline-flex h-4 min-w-4 items-center justify-center rounded-sm bg-secondary px-1 text-label font-medium text-secondary-foreground tabular"
          >{{ item.count }}</span>
        </span>
      </template>
      <div class="pt-4">
        <slot :name="item.value" />
      </div>
    </n-tab-pane>
  </n-tabs>
</template>
