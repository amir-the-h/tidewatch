<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { PlusIcon, XIcon } from '@lucide/vue'
import {
  AlertDialog, Button, FormField, IconButton, Select, Sheet, Skeleton, Switch,
} from '@/components/ui'
import { useToast } from '@/composables/useToast'
import {
  listTriggers, listTriggerPipelineLinks, createTriggerPipelineLink,
  deleteTriggerPipelineLink, activateTriggerPipelineLink, deactivateTriggerPipelineLink,
  type TriggerInfo, type TriggerPipelineLink,
} from '../../api/trigger_links'
import { listPipelines, type Pipeline } from '../../api/pipelines'

const toast = useToast()

// ── Data ─────────────────────────────────────────────────────────────────────

const triggers        = ref<TriggerInfo[]>([])
const links           = ref<TriggerPipelineLink[]>([])
const triggersLoading = ref(false)
const loadError       = ref('')
const pipelinesData   = ref<Pipeline[]>([])

// ── Create link form ──────────────────────────────────────────────────────────

const showLinkForm             = ref(false)
const linkFormTrigger          = ref('')
const linkFormPipelineId       = ref<number | null>(null)
const linkFormSourcePipelineId = ref<number | null>(null)
const savingLink               = ref(false)

// ── Remove confirm ────────────────────────────────────────────────────────────

const removeTarget = ref<TriggerPipelineLink | null>(null)
const showRemove   = ref(false)
const removing     = ref(false)

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

function linkLabel(link: TriggerPipelineLink) {
  const target = pipelineName(link.pipeline_id)
  return link.source_pipeline_id ? `${pipelineName(link.source_pipeline_id)} → ${target}` : target
}

// ── Functions ─────────────────────────────────────────────────────────────────

async function load() {
  triggersLoading.value = true
  loadError.value = ''
  try {
    const [t, l, pipelines] = await Promise.all([
      listTriggers(),
      listTriggerPipelineLinks(),
      listPipelines(),
    ])
    triggers.value      = t        ?? []
    links.value         = l        ?? []
    pipelinesData.value = pipelines ?? []
  } catch (e: any) { loadError.value = e.message }
  finally { triggersLoading.value = false }
}

function openAddLink(triggerType: string) {
  linkFormTrigger.value = triggerType
  linkFormPipelineId.value = null
  linkFormSourcePipelineId.value = null
  showLinkForm.value = true
}

async function onSaveLink() {
  if (!linkFormPipelineId.value) { toast.error('Select the pipeline to start'); return }
  if (linkFormTrigger.value === 'pipeline_finished' && !linkFormSourcePipelineId.value) {
    toast.error('Select the source pipeline that must finish first'); return
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
    toast.success('Pipeline connected')
    showLinkForm.value = false
  } catch (e: any) { toast.error(`Could not connect pipeline: ${e.message}`) }
  finally { savingLink.value = false }
}

function askRemove(link: TriggerPipelineLink) {
  removeTarget.value = link
  showRemove.value = true
}

async function onConfirmRemove() {
  const target = removeTarget.value
  if (!target) return
  removing.value = true
  try {
    await deleteTriggerPipelineLink(target.id)
    links.value = links.value.filter(l => l.id !== target.id)
    toast.success('Pipeline disconnected')
    showRemove.value = false
  } catch (e: any) { toast.error(`Could not disconnect pipeline: ${e.message}`) }
  finally { removing.value = false }
}

async function toggleLink(link: TriggerPipelineLink, active: boolean) {
  try {
    if (active) await activateTriggerPipelineLink(link.id)
    else await deactivateTriggerPipelineLink(link.id)
    link.active = active
  } catch (e: any) { toast.error(e.message) }
}

onMounted(load)
</script>

<template>
  <div v-if="triggersLoading" class="flex flex-col gap-3">
    <Skeleton v-for="i in 3" :key="i" class="h-24" />
  </div>

  <div
    v-else-if="loadError"
    class="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-destructive p-4 text-small"
  >
    <span class="text-destructive">Could not load triggers: {{ loadError }}</span>
    <Button variant="outline" @click="load">Retry</Button>
  </div>

  <div v-else class="flex flex-col gap-3">
    <section
      v-for="trigger in triggers"
      :key="trigger.type"
      class="flex flex-col gap-3 rounded-lg border border-border bg-card p-4"
    >
      <div class="flex flex-wrap items-start justify-between gap-2">
        <div class="min-w-0">
          <h2 class="m-0 font-mono text-body font-medium">{{ trigger.type }}</h2>
          <p class="m-0 mt-0.5 text-small text-muted-foreground">{{ trigger.description }}</p>
        </div>
        <Button variant="outline" @click="openAddLink(trigger.type)">
          <template #icon><PlusIcon :stroke-width="1.75" /></template>
          Connect pipeline
        </Button>
      </div>

      <ul v-if="linksByTrigger[trigger.type]?.length" class="m-0 flex list-none flex-wrap gap-2 p-0">
        <li
          v-for="link in linksByTrigger[trigger.type]"
          :key="link.id"
          class="flex items-center gap-2 rounded-md border border-border bg-muted py-0.5 pr-0.5 pl-2 text-small"
        >
          <Switch
            :model-value="link.active"
            :aria-label="`${link.active ? 'Pause' : 'Resume'} ${linkLabel(link)}`"
            @update:model-value="toggleLink(link, $event)"
          />
          <span :class="link.active ? 'text-foreground' : 'text-muted-foreground'">{{ linkLabel(link) }}</span>
          <IconButton :icon="XIcon" :label="`Disconnect ${linkLabel(link)}`" @click="askRemove(link)" />
        </li>
      </ul>
      <p v-else class="m-0 text-small text-muted-foreground">No pipelines connected. Connect one to run it on this trigger.</p>
    </section>
  </div>

  <!-- Connect pipeline to trigger -->
  <Sheet
    v-model:open="showLinkForm"
    title="Connect pipeline"
    :description="`Runs the pipeline whenever ${linkFormTrigger} fires.`"
  >
    <form class="flex flex-col gap-3 p-4" @submit.prevent="onSaveLink">
      <FormField
        v-if="linkFormTrigger === 'pipeline_finished'"
        label="Source pipeline"
        description="The pipeline that must finish first."
        required
      >
        <Select
          v-model="linkFormSourcePipelineId"
          :options="pipelineSelectOptions"
          placeholder="Select a pipeline"
          filterable
          clearable
        />
      </FormField>
      <FormField label="Pipeline" required>
        <Select
          v-model="linkFormPipelineId"
          :options="pipelineSelectOptions"
          placeholder="Select a pipeline"
          filterable
          clearable
        />
      </FormField>
    </form>
    <template #footer>
      <Button variant="outline" size="default" @click="showLinkForm = false">Cancel</Button>
      <Button size="default" :loading="savingLink" @click="onSaveLink">Connect</Button>
    </template>
  </Sheet>

  <!-- Disconnect confirm -->
  <AlertDialog
    v-model:open="showRemove"
    title="Disconnect pipeline?"
    :description="removeTarget ? `${linkLabel(removeTarget)} will no longer run on ${removeTarget.trigger_type}.` : undefined"
    action-label="Disconnect"
    :loading="removing"
    @confirm="onConfirmRemove"
  />
</template>
