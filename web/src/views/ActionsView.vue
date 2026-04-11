<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NTabs, NTabPane } from 'naive-ui'
import PageHeader from '../components/PageHeader.vue'
import ActionsTab from '../components/tabs/ActionsTab.vue'
import OllamaTab from '../components/tabs/OllamaTab.vue'

const route  = useRoute()
const router = useRouter()

const activeTab = ref((route.query.tab as string) || 'actions')
function switchTab(tab: string) {
  activeTab.value = tab
  router.replace({ query: { tab } })
}

watch(() => route.query.tab, (tab) => {
  if (tab && tab !== activeTab.value) activeTab.value = tab as string
})
</script>

<template>
  <PageHeader title="Actions" />
  <n-tabs :value="activeTab" type="line" animated style="margin-top: -8px;" @update:value="switchTab">
    <n-tab-pane name="actions" tab="Actions">
      <ActionsTab />
    </n-tab-pane>
    <n-tab-pane name="ollama" tab="Ollama Backends">
      <OllamaTab />
    </n-tab-pane>
  </n-tabs>
</template>
