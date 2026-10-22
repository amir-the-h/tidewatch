<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Tabs } from '@/components/ui'
import PageHeader from '../components/PageHeader.vue'
import ActionsTab from '../components/tabs/ActionsTab.vue'
import OllamaTab from '../components/tabs/OllamaTab.vue'

const route  = useRoute()
const router = useRouter()

const tabs = [
  { value: 'actions', label: 'Actions' },
  { value: 'ollama',  label: 'Ollama backends' },
]

const actionsTab = ref<InstanceType<typeof ActionsTab> | null>(null)
const ollamaTab  = ref<InstanceType<typeof OllamaTab> | null>(null)

const activeTab = ref((route.query.tab as string) || 'actions')
watch(activeTab, (tab) => {
  if (route.query.tab !== tab) router.replace({ query: { tab } })
})
watch(() => route.query.tab, (tab) => {
  if (tab && tab !== activeTab.value) activeTab.value = tab as string
})

const addLabel = computed(() => activeTab.value === 'ollama' ? 'Add backend' : 'New action')

function onAdd() {
  if (activeTab.value === 'ollama') ollamaTab.value?.openCreate()
  else actionsTab.value?.openCreate()
}
</script>

<template>
  <div class="flex flex-col gap-2">
    <PageHeader
      title="Actions"
      description="The units of work pipelines run on each signal: models, LLM prompts, handlers and notifications."
      :add-label="addLabel"
      @add="onAdd"
    />
    <Tabs v-model="activeTab" :items="tabs">
      <template #actions><ActionsTab ref="actionsTab" /></template>
      <template #ollama><OllamaTab ref="ollamaTab" /></template>
    </Tabs>
  </div>
</template>
