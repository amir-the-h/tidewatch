<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NTabs, NTabPane } from 'naive-ui'
import PageHeader from '../components/PageHeader.vue'
import SourcesTab from '../components/tabs/SourcesTab.vue'
import RulesTab from '../components/tabs/RulesTab.vue'

const route  = useRoute()
const router = useRouter()

const activeTab = ref((route.query.tab as string) || 'sources')
function switchTab(tab: string) {
  activeTab.value = tab
  router.replace({ query: { tab } })
}

watch(() => route.query.tab, (tab) => {
  if (tab && tab !== activeTab.value) activeTab.value = tab as string
})
</script>

<template>
  <PageHeader title="Sources" />
  <n-tabs :value="activeTab" type="line" animated style="margin-top: -8px;" @update:value="switchTab">
    <n-tab-pane name="sources" tab="Sources">
      <SourcesTab />
    </n-tab-pane>
    <n-tab-pane name="rules" tab="Rules">
      <RulesTab />
    </n-tab-pane>
  </n-tabs>
</template>
