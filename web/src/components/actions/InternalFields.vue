<!-- Internal action fields: the handler, plus the typed settings of handlers that have them
     (discord_tag_thread, entity_discovery). Other handlers are configured through the JSON field. -->
<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Combobox, FormField, Input, NumberField, Select, TagsInput } from '@/components/ui'
import { listActionHandlers } from '../../api/actions'
import type { NotificationChannel, NotificationProvider } from '../../api/notifications'
import type { ActionFormState } from './actionForm'

const props = defineProps<{
  form: ActionFormState
  channels: NotificationChannel[]
  providers: NotificationProvider[]
}>()

const handlers = ref<string[]>([])
const handlersLoading = ref(false)
const handlerOptions = computed(() => handlers.value.map(h => ({ label: h, value: h })))

const discordChannelOptions = computed(() => {
  const discordProviderIds = new Set(props.providers.filter(p => p.type === 'discord').map(p => p.id))
  return props.channels
    .filter(c => discordProviderIds.has(c.provider_id))
    .map(c => ({ label: c.name, value: c.id }))
})

const entityTypeOptions = [
  { label: 'LOC — Location',     value: 'LOC'  },
  { label: 'PER — Person',       value: 'PER'  },
  { label: 'ORG — Organization', value: 'ORG'  },
  { label: 'MISC — Misc',        value: 'MISC' },
]

onMounted(async () => {
  handlersLoading.value = true
  try { handlers.value = await listActionHandlers() } catch { /* picker stays empty */ }
  finally { handlersLoading.value = false }
})
</script>

<template>
  <FormField label="Handler">
    <Select
      v-model="form.handler"
      :options="handlerOptions"
      :loading="handlersLoading"
      placeholder="Select handler"
      filterable
      clearable
    />
  </FormField>

  <template v-if="form.handler === 'discord_tag_thread'">
    <FormField label="Discord channel" required description="A notification channel on a Discord provider.">
      <Select
        v-model="form.discordTagChannelId"
        :options="discordChannelOptions"
        placeholder="Select a Discord channel"
        filterable
        clearable
      />
    </FormField>
    <FormField
      label="Tag names key"
      for="action-tag-names-key"
      description="Pipeline context key holding the tag names to apply. Defaults to discord_tag_names."
    >
      <Input id="action-tag-names-key" v-model="form.discordTagNamesKey" placeholder="discord_tag_names" class="font-mono" />
    </FormField>
  </template>

  <template v-if="form.handler === 'entity_discovery'">
    <div class="grid grid-cols-2 gap-3">
      <FormField label="Min confidence" for="action-discovery-confidence" description="Average score, 0 to 1.">
        <NumberField id="action-discovery-confidence" v-model="form.discoveryMinConfidence" :min="0" :max="1" :step="0.05" placeholder="0.75" />
      </FormField>
      <FormField label="Min frequency" for="action-discovery-frequency" description="Occurrences before auto-promotion.">
        <NumberField id="action-discovery-frequency" v-model="form.discoveryMinFrequency" :min="1" :step="1" placeholder="3" />
      </FormField>
    </div>
    <FormField label="Auto-promote types" description="Candidates of these types are promoted automatically; the rest queue for review.">
      <Combobox v-model="form.discoveryAutoPromoteTypes" :options="entityTypeOptions" placeholder="None" clearable />
    </FormField>
    <FormField label="Entity key" for="action-discovery-entity-key" description="Context key holding the NER output.">
      <Input id="action-discovery-entity-key" v-model="form.discoveryEntityKey" placeholder="entities" class="font-mono" />
    </FormField>
    <FormField
      label="Strip label prefixes"
      description="Removed from NER labels before storing entity types. Use B- and I- for BIO-tagged models (I-LOC becomes LOC)."
    >
      <TagsInput v-model="form.discoveryStripLabelPrefixes" placeholder="Add prefix" />
    </FormField>
  </template>
</template>
