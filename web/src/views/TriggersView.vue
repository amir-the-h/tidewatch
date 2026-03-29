<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import {
  NFlex, NText, NTag, NButton, NIcon, NSelect, NFormItem, NSwitch,
  NSpin, NCard, useMessage,
} from 'naive-ui'
import { AddOutline } from '@vicons/ionicons5'
import PageHeader from '../components/PageHeader.vue'
import FormDrawer from '../components/FormDrawer.vue'
import {
  listTriggers, listTriggerChainLinks, createTriggerChainLink,
  deleteTriggerChainLink, activateTriggerChainLink, deactivateTriggerChainLink,
  type TriggerInfo, type TriggerChainLink,
} from '../api/trigger_links'
import { listChains, type Chain } from '../api/chains'

const msg = useMessage()

const triggers   = ref<TriggerInfo[]>([])
const links      = ref<TriggerChainLink[]>([])
const chains     = ref<Chain[]>([])
const loading    = ref(true)

// Add-link form
const showForm     = ref(false)
const formTrigger  = ref<string>('')
const formChainId  = ref<number | null>(null)
const savingLink   = ref(false)

onMounted(async () => {
  try {
    const [t, l, c] = await Promise.all([listTriggers(), listTriggerChainLinks(), listChains()])
    triggers.value = t ?? []
    links.value    = l ?? []
    chains.value   = c ?? []
  } catch (e: any) { msg.error(e.message) }
  finally { loading.value = false }
})

// Group links by trigger type
const linksByTrigger = computed(() => {
  const map: Record<string, TriggerChainLink[]> = {}
  for (const l of links.value) {
    if (!map[l.trigger_type]) map[l.trigger_type] = []
    map[l.trigger_type].push(l)
  }
  return map
})

const chainOptions = computed(() =>
  chains.value.map(c => ({ label: c.name, value: c.id }))
)

function openAddLink(triggerType: string) {
  formTrigger.value = triggerType
  formChainId.value = null
  showForm.value = true
}

async function onSaveLink() {
  if (!formChainId.value) { msg.error('Chain is required'); return }
  savingLink.value = true
  try {
    const l = await createTriggerChainLink({ trigger_type: formTrigger.value, chain_id: formChainId.value })
    links.value.push(l)
    msg.success('Link created')
    showForm.value = false
  } catch (e: any) { msg.error(e.message) }
  finally { savingLink.value = false }
}

async function removeLink(id: number) {
  try {
    await deleteTriggerChainLink(id)
    links.value = links.value.filter(l => l.id !== id)
  } catch (e: any) { msg.error(e.message) }
}

async function toggleLink(link: TriggerChainLink) {
  try {
    if (link.active) {
      await deactivateTriggerChainLink(link.id)
      link.active = false
    } else {
      await activateTriggerChainLink(link.id)
      link.active = true
    }
  } catch (e: any) { msg.error(e.message) }
}

function chainName(chainId: number) {
  return chains.value.find(c => c.id === chainId)?.name ?? `#${chainId}`
}
</script>

<template>
  <PageHeader title="Triggers" />

  <n-spin v-if="loading" />

  <div v-else>
    <n-card
      v-for="trigger in triggers"
      :key="trigger.type"
      size="small"
      style="margin-bottom: 12px;"
    >
      <n-flex align="center" justify="space-between" style="margin-bottom: 8px;">
        <div>
          <n-tag type="info" size="small" style="font-family: monospace; margin-right: 8px;">
            {{ trigger.type }}
          </n-tag>
          <n-text depth="3" style="font-size: 12px;">{{ trigger.description }}</n-text>
        </div>
        <n-button size="tiny" ghost type="primary" @click="openAddLink(trigger.type)">
          <template #icon><n-icon><AddOutline /></n-icon></template>
          Connect chain
        </n-button>
      </n-flex>

      <n-flex v-if="linksByTrigger[trigger.type]?.length" :size="6" wrap>
        <n-flex
          v-for="link in linksByTrigger[trigger.type]"
          :key="link.id"
          align="center" :size="4"
          style="
            background: #313244; border-radius: 4px; padding: 2px 6px;
            font-size: 12px;
          "
        >
          <n-switch :value="link.active" size="small" @update:value="toggleLink(link)" />
          <span :style="{ color: link.active ? '#cdd6f4' : '#6c7086' }">{{ chainName(link.chain_id) }}</span>
          <n-button text size="tiny" style="color: #f38ba8;" @click="removeLink(link.id)">×</n-button>
        </n-flex>
      </n-flex>
      <n-text v-else depth="3" style="font-size: 12px;">No chains connected.</n-text>
    </n-card>
  </div>

  <FormDrawer
    v-model:show="showForm"
    :title="`Connect chain to ${formTrigger}`"
    :saveLabel="`Connect`"
    :loading="savingLink"
    @save="onSaveLink"
  >
    <n-form-item label="Chain">
      <n-select
        v-model:value="formChainId"
        :options="chainOptions"
        placeholder="Select a chain"
        clearable
      />
    </n-form-item>
  </FormDrawer>
</template>
