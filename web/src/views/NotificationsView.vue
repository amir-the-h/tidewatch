<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Tabs } from '@/components/ui'
import PageHeader from '../components/PageHeader.vue'
import ProvidersTab from '../components/notifications/ProvidersTab.vue'
import ChannelsTab from '../components/notifications/ChannelsTab.vue'
import { useNotificationsStore } from '../stores/notifications'

const route  = useRoute()
const router = useRouter()
const store  = useNotificationsStore()

const tabs = [
  { value: 'providers', label: 'Providers' },
  { value: 'channels',  label: 'Channels'  },
]

function validTab(tab: unknown): string {
  return typeof tab === 'string' && tabs.some(t => t.value === tab) ? tab : 'providers'
}

const providersTab = ref<InstanceType<typeof ProvidersTab> | null>(null)
const channelsTab  = ref<InstanceType<typeof ChannelsTab> | null>(null)

const activeTab = ref(validTab(route.query.tab))
watch(activeTab, (tab) => {
  if (route.query.tab !== tab) router.replace({ query: { tab } })
})
watch(() => route.query.tab, (tab) => {
  if (tab && tab !== activeTab.value) activeTab.value = validTab(tab)
})

const addLabel = computed(() => activeTab.value === 'channels' ? 'New channel' : 'New provider')

function onAdd() {
  if (activeTab.value === 'channels') channelsTab.value?.openCreate()
  else providersTab.value?.openCreate()
}

// Both tabs need both lists (provider names on channels, channel counts on providers),
// so they load here rather than in each tab.
onMounted(() => {
  store.fetchProviders()
  store.fetchChannels()
})
</script>

<template>
  <div class="flex flex-col gap-2">
    <PageHeader
      title="Notifications"
      description="Providers hold the credentials for Pushover, Telegram and Discord; channels say where each one delivers. Notification actions send to a channel."
      :add-label="addLabel"
      @add="onAdd"
    />
    <Tabs v-model="activeTab" :items="tabs">
      <template #providers><ProvidersTab ref="providersTab" /></template>
      <template #channels><ChannelsTab ref="channelsTab" @add-provider="activeTab = 'providers'" /></template>
    </Tabs>
  </div>
</template>
