<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NTabs, NTabPane } from 'naive-ui'
import PageHeader from '../components/PageHeader.vue'
import TweetsTab from '../components/tabs/TweetsTab.vue'
import SearchTab from '../components/tabs/SearchTab.vue'

const route  = useRoute()
const router = useRouter()

const activeTab = ref((route.query.tab as string) || 'tweets')
function switchTab(tab: string) {
  activeTab.value = tab
  router.replace({ query: { tab } })
}

// Keep activeTab in sync if URL changes externally
watch(() => route.query.tab, (tab) => {
  if (tab && tab !== activeTab.value) activeTab.value = tab as string
})
</script>

<template>
  <PageHeader title="Tweets" />
  <n-tabs :value="activeTab" type="line" animated style="margin-top: -8px;" @update:value="switchTab">
    <n-tab-pane name="tweets" tab="Tweets">
      <TweetsTab />
    </n-tab-pane>
    <n-tab-pane name="search" tab="Search">
      <SearchTab />
    </n-tab-pane>
  </n-tabs>
</template>
