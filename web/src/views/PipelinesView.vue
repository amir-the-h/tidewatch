<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NTabs, NTabPane } from 'naive-ui'
import PageHeader from '../components/PageHeader.vue'
import PipelinesTab from '../components/tabs/PipelinesTab.vue'
import TriggersTab from '../components/tabs/TriggersTab.vue'
import ExecutionsTab from '../components/tabs/ExecutionsTab.vue'

const route  = useRoute()
const router = useRouter()

const activeTab = ref((route.query.tab as string) || 'pipelines')
function switchTab(tab: string) {
  activeTab.value = tab
  router.replace({ query: { tab } })
}

watch(() => route.query.tab, (tab) => {
  if (tab && tab !== activeTab.value) activeTab.value = tab as string
})
</script>

<template>
  <PageHeader title="Pipelines" />
  <n-tabs :value="activeTab" type="line" animated style="margin-top: -8px;" @update:value="switchTab">
    <n-tab-pane name="pipelines" tab="Pipelines">
      <PipelinesTab />
    </n-tab-pane>
    <n-tab-pane name="triggers" tab="Triggers">
      <TriggersTab />
    </n-tab-pane>
    <n-tab-pane name="executions" tab="Executions">
      <ExecutionsTab />
    </n-tab-pane>
  </n-tabs>
</template>
