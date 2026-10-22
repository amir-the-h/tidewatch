<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Tabs } from '@/components/ui'
import PageHeader from '../components/PageHeader.vue'
import TweetsTab from '../components/tweets/TweetsTab.vue'
import SearchTab from '../components/tweets/SearchTab.vue'
import { useAuthStore } from '../stores/auth'

const route  = useRoute()
const router = useRouter()
const auth   = useAuthStore()

// Search runs and lists backfill jobs through admin-only endpoints.
const tabs = computed(() => [
  { value: 'tweets', label: 'Tweets' },
  ...(auth.isAdmin ? [{ value: 'search', label: 'Search' }] : []),
])

function validTab(tab: unknown): string {
  return typeof tab === 'string' && tabs.value.some(t => t.value === tab) ? tab : 'tweets'
}

const activeTab = ref(validTab(route.query.tab))
watch(activeTab, (tab) => {
  if (route.query.tab !== tab) router.replace({ query: { tab } })
})
watch(() => route.query.tab, (tab) => {
  if (tab && tab !== activeTab.value) activeTab.value = validTab(tab)
})
</script>

<template>
  <div class="flex flex-col gap-2">
    <PageHeader
      title="Tweets"
      description="Tweets the stream and searches matched, with what each action produced for them."
    />
    <Tabs v-model="activeTab" :items="tabs">
      <template #tweets><TweetsTab /></template>
      <template #search><SearchTab @view-tweets="activeTab = 'tweets'" /></template>
    </Tabs>
  </div>
</template>
