<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Tabs } from '@/components/ui'
import PageHeader from '../components/PageHeader.vue'
import SourcesTab from '../components/tabs/SourcesTab.vue'
import RulesTab from '../components/tabs/RulesTab.vue'

const route  = useRoute()
const router = useRouter()

const tabs = [
  { value: 'sources', label: 'Sources' },
  { value: 'rules',   label: 'Rules' },
]

const sourcesTab = ref<InstanceType<typeof SourcesTab> | null>(null)
const rulesTab   = ref<InstanceType<typeof RulesTab> | null>(null)

const activeTab = ref((route.query.tab as string) || 'sources')
watch(activeTab, (tab) => {
  if (route.query.tab !== tab) router.replace({ query: { tab } })
})
watch(() => route.query.tab, (tab) => {
  if (tab && tab !== activeTab.value) activeTab.value = tab as string
})

const addLabel = computed(() => activeTab.value === 'rules' ? 'New rule' : 'New source')

function onAdd() {
  if (activeTab.value === 'rules') rulesTab.value?.openCreate()
  else sourcesTab.value?.openCreate()
}
</script>

<template>
  <div class="flex flex-col gap-2">
    <PageHeader
      title="Sources"
      description="Search terms and operators, combined into the stream rules sent to X."
      :add-label="addLabel"
      @add="onAdd"
    />
    <Tabs v-model="activeTab" :items="tabs">
      <template #sources><SourcesTab ref="sourcesTab" /></template>
      <template #rules><RulesTab ref="rulesTab" /></template>
    </Tabs>
  </div>
</template>
