<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Tabs } from '@/components/ui'
import PageHeader from '../components/PageHeader.vue'
import PipelinesTab from '../components/tabs/PipelinesTab.vue'
import TriggersTab from '../components/tabs/TriggersTab.vue'
import ExecutionsTab from '../components/tabs/ExecutionsTab.vue'

const route  = useRoute()
const router = useRouter()

const tabs = [
  { value: 'pipelines',  label: 'Pipelines' },
  { value: 'triggers',   label: 'Triggers' },
  { value: 'executions', label: 'Executions' },
]

const pipelinesTab = ref<InstanceType<typeof PipelinesTab> | null>(null)

const activeTab = ref((route.query.tab as string) || 'pipelines')
watch(activeTab, (tab) => {
  if (route.query.tab !== tab) router.replace({ query: { tab } })
})
watch(() => route.query.tab, (tab) => {
  if (tab && tab !== activeTab.value) activeTab.value = tab as string
})
</script>

<template>
  <div class="flex flex-col gap-2">
    <PageHeader
      title="Pipelines"
      description="DAG workflows of actions, the triggers that start them, and every action execution."
      :add-label="activeTab === 'pipelines' ? 'New pipeline' : undefined"
      @add="pipelinesTab?.openCreate()"
    />
    <Tabs v-model="activeTab" :items="tabs">
      <template #pipelines><PipelinesTab ref="pipelinesTab" /></template>
      <template #triggers><TriggersTab /></template>
      <template #executions><ExecutionsTab /></template>
    </Tabs>
  </div>
</template>
