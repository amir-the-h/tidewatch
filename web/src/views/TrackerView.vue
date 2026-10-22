<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Tabs } from '@/components/ui'
import PageHeader from '../components/PageHeader.vue'
import EntitiesTab from '../components/tracker/EntitiesTab.vue'
import CandidatesTab from '../components/tracker/CandidatesTab.vue'
import ConnectionsTab from '../components/tracker/ConnectionsTab.vue'
import { useAuthStore } from '../stores/auth'
import { useEntitiesStore } from '../stores/entities'
import { useCandidatesStore } from '../stores/candidates'

const route  = useRoute()
const router = useRouter()
const auth   = useAuthStore()
const store  = useEntitiesStore()
const candidatesStore = useCandidatesStore()

// Candidates are admin-only on the server, so viewers don't get the tab.
const tabs = computed(() => [
  { value: 'entities', label: 'Entities' },
  ...(auth.isAdmin ? [{ value: 'candidates', label: 'Candidates', count: candidatesStore.pendingCount }] : []),
  { value: 'connections', label: 'Connections' },
])

function validTab(tab: unknown): string {
  return typeof tab === 'string' && tabs.value.some(t => t.value === tab) ? tab : 'entities'
}

const entitiesTab = ref<InstanceType<typeof EntitiesTab> | null>(null)

const activeTab = ref(validTab(route.query.tab))
watch(activeTab, (tab) => {
  if (route.query.tab !== tab) router.replace({ query: { tab } })
})
watch(() => route.query.tab, (tab) => {
  if (tab && tab !== activeTab.value) activeTab.value = validTab(tab)
})

const addLabel = computed(() => auth.isAdmin && activeTab.value === 'entities' ? 'New entity' : undefined)

onMounted(() => {
  // Entities feed both the Entities table and the Connections picker.
  store.fetchEntities()
  if (auth.isAdmin) candidatesStore.fetchPendingCount()
})
</script>

<template>
  <div class="flex flex-col gap-2">
    <PageHeader
      title="Tracker"
      description="People, places and organisations matched in signals, the candidates found for review, and how they connect."
      :add-label="addLabel"
      @add="entitiesTab?.openCreate()"
    />
    <Tabs v-model="activeTab" :items="tabs">
      <template #entities><EntitiesTab ref="entitiesTab" /></template>
      <template #candidates><CandidatesTab /></template>
      <template #connections><ConnectionsTab /></template>
    </Tabs>
  </div>
</template>
