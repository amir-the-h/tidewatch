<!-- Digest request form: time window, optional rule/entity filters and signal limit. Emits the request. -->
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { SparklesIcon } from '@lucide/vue'
import { Button, Card, DatePicker, FormField, NumberField, Select } from '@/components/ui'
import { listRules } from '../../api/rules'
import { listTrackedEntities } from '../../api/entities'
import { listActions } from '../../api/actions'
import type { DigestRequest } from '../../api/summaries'

defineProps<{ loading: boolean }>()
const emit = defineEmits<{ generate: [req: DigestRequest] }>()

const filterFrom     = ref<number | null>(null)
const filterTo       = ref<number | null>(null)
const filterRuleId   = ref<number | null>(null)
const filterEntityId = ref<number | null>(null)
const limit          = ref<number | null>(200)

const rules    = ref<{ label: string; value: number }[]>([])
const entities = ref<{ label: string; value: number }[]>([])

const checking      = ref(true)
const pipelineReady = ref(false)

function onSubmit() {
  emit('generate', {
    from:      filterFrom.value ? new Date(filterFrom.value).toISOString() : undefined,
    to:        filterTo.value   ? new Date(filterTo.value).toISOString()   : undefined,
    rule_id:   filterRuleId.value   ?? undefined,
    entity_id: filterEntityId.value ?? undefined,
    limit:     limit.value ?? undefined,
  })
}

onMounted(async () => {
  const [rulesData, entitiesData, actionsData] = await Promise.allSettled([
    listRules(),
    listTrackedEntities(),
    listActions({ type: 'ollama', active: true }),
  ])
  if (rulesData.status === 'fulfilled')
    rules.value = rulesData.value.map(r => ({ label: r.name || String(r.id), value: r.id }))
  if (entitiesData.status === 'fulfilled')
    entities.value = entitiesData.value.map(e => ({ label: e.name, value: e.id }))
  if (actionsData.status === 'fulfilled')
    pipelineReady.value = actionsData.value.length > 0
  checking.value = false
})
</script>

<template>
  <Card title="Digest window" description="Which signals the digest summarises. Every filter is optional.">
    <form class="flex flex-col gap-4" @submit.prevent="onSubmit">
      <div class="grid gap-4 sm:grid-cols-2">
        <FormField label="From">
          <DatePicker v-model="filterFrom" placeholder="Start date" default-time="00:00:00" clearable />
        </FormField>
        <FormField label="To">
          <DatePicker v-model="filterTo" placeholder="End date" default-time="23:59:59" clearable />
        </FormField>
      </div>
      <div class="grid gap-4 sm:grid-cols-3">
        <FormField label="Rule">
          <Select v-model="filterRuleId" :options="rules" placeholder="Any rule" clearable filterable />
        </FormField>
        <FormField label="Entity">
          <Select v-model="filterEntityId" :options="entities" placeholder="Any entity" clearable filterable />
        </FormField>
        <FormField label="Signal limit" for="digest-limit">
          <NumberField id="digest-limit" v-model="limit" :min="10" :max="1000" :step="50" />
        </FormField>
      </div>

      <p v-if="!checking && !pipelineReady" class="m-0 text-small text-warning">
        No active Ollama action found. Digests run through a pipeline linked to the
        <code class="font-mono">digest_requested</code> trigger; set one up on the Pipelines page.
      </p>

      <div>
        <Button size="default" native-type="submit" :loading="loading" :disabled="!pipelineReady || loading">
          <template #icon><SparklesIcon :stroke-width="1.75" /></template>
          {{ loading ? 'Generating digest…' : 'Generate digest' }}
        </Button>
      </div>
    </form>
  </Card>
</template>
