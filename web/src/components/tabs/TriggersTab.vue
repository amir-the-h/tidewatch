<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import {
  NFlex, NCard, NTag, NText, NSwitch, NButton, NIcon, NDivider, NSpin, NSelect, NFormItem,
  useMessage,
} from 'naive-ui'
import { AddOutline } from '@vicons/ionicons5'
import FormDrawer from '../FormDrawer.vue'
import {
  listTriggers, listTriggerPipelineLinks, createTriggerPipelineLink,
  deleteTriggerPipelineLink, activateTriggerPipelineLink, deactivateTriggerPipelineLink,
  type TriggerInfo, type TriggerPipelineLink,
} from '../../api/trigger_links'
import { listPipelines, type Pipeline } from '../../api/pipelines'

const msg = useMessage()

// ── Data ─────────────────────────────────────────────────────────────────────

const triggers        = ref<TriggerInfo[]>([])
const links           = ref<TriggerPipelineLink[]>([])
const triggersLoading = ref(false)
const pipelinesData   = ref<Pipeline[]>([])

// ── Create link form ──────────────────────────────────────────────────────────

const showLinkForm             = ref(false)
const linkFormTrigger          = ref('')
const linkFormPipelineId       = ref<number | null>(null)
const linkFormSourcePipelineId = ref<number | null>(null)
const savingLink               = ref(false)

// ── Computed ──────────────────────────────────────────────────────────────────

const linksByTrigger = computed(() => {
  const map: Record<string, TriggerPipelineLink[]> = {}
  for (const l of links.value) {
    if (!map[l.trigger_type]) map[l.trigger_type] = []
    map[l.trigger_type].push(l)
  }
  return map
})

const pipelineSelectOptions = computed(() =>
  pipelinesData.value.map(p => ({ label: p.name, value: p.id }))
)

// ── Helpers ───────────────────────────────────────────────────────────────────

function pipelineName(pipelineId: number) {
  return pipelinesData.value.find(p => p.id === pipelineId)?.name ?? `#${pipelineId}`
}

// ── Functions ─────────────────────────────────────────────────────────────────

function openAddLink(triggerType: string) {
  linkFormTrigger.value = triggerType
  linkFormPipelineId.value = null
  linkFormSourcePipelineId.value = null
  showLinkForm.value = true
}

async function onSaveLink() {
  if (!linkFormPipelineId.value) { msg.error('Pipeline is required'); return }
  if (linkFormTrigger.value === 'pipeline_finished' && !linkFormSourcePipelineId.value) {
    msg.error('Source Pipeline is required for pipeline_finished trigger'); return
  }
  savingLink.value = true
  try {
    const body: { trigger_type: string; pipeline_id: number; source_pipeline_id?: number } = {
      trigger_type: linkFormTrigger.value,
      pipeline_id: linkFormPipelineId.value,
    }
    if (linkFormTrigger.value === 'pipeline_finished' && linkFormSourcePipelineId.value) {
      body.source_pipeline_id = linkFormSourcePipelineId.value
    }
    const l = await createTriggerPipelineLink(body)
    links.value.push(l)
    msg.success('Link created')
    showLinkForm.value = false
  } catch (e: any) { msg.error(e.message) }
  finally { savingLink.value = false }
}

async function removeLink(id: number) {
  try {
    await deleteTriggerPipelineLink(id)
    links.value = links.value.filter(l => l.id !== id)
  } catch (e: any) { msg.error(e.message) }
}

async function toggleLink(link: TriggerPipelineLink) {
  try {
    if (link.active) {
      await deactivateTriggerPipelineLink(link.id)
      link.active = false
    } else {
      await activateTriggerPipelineLink(link.id)
      link.active = true
    }
  } catch (e: any) { msg.error(e.message) }
}

onMounted(async () => {
  triggersLoading.value = true
  try {
    const [t, l, pipelines] = await Promise.all([
      listTriggers(),
      listTriggerPipelineLinks(),
      listPipelines(),
    ])
    triggers.value      = t        ?? []
    links.value         = l        ?? []
    pipelinesData.value = pipelines ?? []
  } catch (e: any) { msg.error(e.message) }
  finally { triggersLoading.value = false }
})
</script>

<template>
  <n-spin v-if="triggersLoading" style="display: flex; justify-content: center; padding: 24px 0;" />
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
          Connect pipeline
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
          <span :style="{ color: link.active ? '#cdd6f4' : '#6c7086' }">
            {{ link.source_pipeline_id ? `[${pipelineName(link.source_pipeline_id)}] → ` : '' }}{{ pipelineName(link.pipeline_id) }}
          </span>
          <n-button text size="tiny" style="color: #f38ba8;" @click="removeLink(link.id)">×</n-button>
        </n-flex>
      </n-flex>
      <n-text v-else depth="3" style="font-size: 12px;">No pipelines connected.</n-text>
    </n-card>
  </div>

  <!-- Connect Pipeline to Trigger Drawer -->
  <FormDrawer
    v-model:show="showLinkForm"
    :title="`Connect pipeline to ${linkFormTrigger}`"
    save-label="Connect"
    :loading="savingLink"
    @save="onSaveLink"
  >
    <n-form-item
      v-if="linkFormTrigger === 'pipeline_finished'"
      label="Source Pipeline"
    >
      <n-select
        v-model:value="linkFormSourcePipelineId"
        :options="pipelineSelectOptions"
        placeholder="Pipeline that must finish first"
        clearable
      />
    </n-form-item>
    <n-form-item label="Pipeline">
      <n-select
        v-model:value="linkFormPipelineId"
        :options="pipelineSelectOptions"
        placeholder="Select a pipeline"
        clearable
      />
    </n-form-item>
  </FormDrawer>
</template>
