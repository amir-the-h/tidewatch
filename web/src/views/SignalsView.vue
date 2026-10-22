<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Tabs } from '@/components/ui'
import PageHeader from '../components/PageHeader.vue'
import SignalsTab from '../components/signals/SignalsTab.vue'
import SearchTab from '../components/signals/SearchTab.vue'
import { useAuthStore } from '../stores/auth'

const route  = useRoute()
const router = useRouter()
const auth   = useAuthStore()

// Search runs and lists backfill jobs through admin-only endpoints.
const tabs = computed(() => [
  { value: 'signals', label: 'Signals' },
  ...(auth.isAdmin ? [{ value: 'search', label: 'Search' }] : []),
])

function validTab(tab: unknown): string {
  return typeof tab === 'string' && tabs.value.some(t => t.value === tab) ? tab : 'signals'
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
      title="Signals"
      description="Everything the sources delivered (X posts today), with what each action produced for it."
    />
    <Tabs v-model="activeTab" :items="tabs">
      <template #signals><SignalsTab /></template>
      <template #search><SearchTab @view-signals="activeTab = 'signals'" /></template>
    </Tabs>
  </div>
</template>
