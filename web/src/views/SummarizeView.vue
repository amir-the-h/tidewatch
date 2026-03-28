<!-- web/src/views/SummarizeView.vue -->
<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useDigestStore } from '../stores/summaries'
import { useMessage } from 'naive-ui'
import { listRules } from '../api/rules'
import { listSources } from '../api/sources'
import { listTrackedEntities } from '../api/entities'
import { listPipelines } from '../api/pipelines'
import { SparklesOutline, TrashOutline } from '@vicons/ionicons5'

const store = useDigestStore()
const msg   = useMessage()

const pipelineReady = ref(false)

const filterFrom     = ref<number | null>(null)
const filterTo       = ref<number | null>(null)
const filterRuleId   = ref<number | null>(null)
const filterSourceId = ref<number | null>(null)
const filterEntityId = ref<number | null>(null)
const limit          = ref(200)

const rules    = ref<{ label: string; value: number }[]>([])
const sources  = ref<{ label: string; value: number }[]>([])
const entities = ref<{ label: string; value: number }[]>([])

async function onGenerate() {
  try {
    await store.generate({
      from:      filterFrom.value  ? new Date(filterFrom.value).toISOString()  : undefined,
      to:        filterTo.value    ? new Date(filterTo.value).toISOString()    : undefined,
      rule_id:   filterRuleId.value   ?? undefined,
      source_id: filterSourceId.value ?? undefined,
      entity_id: filterEntityId.value ?? undefined,
      limit:     limit.value,
    })
  } catch {
    msg.error(store.error ?? 'Digest generation failed')
  }
}

onMounted(async () => {
  const [rulesData, sourcesData, entitiesData, pipelinesData] = await Promise.allSettled([
    listRules(),
    listSources(),
    listTrackedEntities(),
    listPipelines({ usecase: 'digest', active: true }),
  ])
  if (rulesData.status === 'fulfilled')
    rules.value = rulesData.value.map(r => ({ label: r.name || String(r.id), value: r.id }))
  if (sourcesData.status === 'fulfilled')
    sources.value = sourcesData.value.map(s => ({ label: s.name || s.value, value: s.id }))
  if (entitiesData.status === 'fulfilled')
    entities.value = entitiesData.value.map(e => ({ label: e.name, value: e.id }))
  if (pipelinesData.status === 'fulfilled')
    pipelineReady.value = pipelinesData.value.length > 0

  await store.fetchJobs()
})
</script>

<template>
  <n-space vertical size="large">
    <n-space align="center" justify="space-between">
      <n-h2 style="margin: 0;">Intelligence Digest</n-h2>
    </n-space>

    <!-- Filters -->
    <n-card size="small" title="Filters">
      <n-space wrap size="medium" align="flex-end">
        <n-form-item label="From" size="small" style="min-width: 200px; margin-bottom: 0;">
          <n-date-picker v-model:value="filterFrom" type="datetime" clearable placeholder="Start date" default-time="00:00:00" style="width: 100%;" />
        </n-form-item>

        <n-form-item label="To" size="small" style="min-width: 200px; margin-bottom: 0;">
          <n-date-picker v-model:value="filterTo" type="datetime" clearable placeholder="End date" default-time="23:59:59" style="width: 100%;" />
        </n-form-item>

        <n-form-item label="Rule" size="small" style="min-width: 180px; margin-bottom: 0;">
          <n-select v-model:value="filterRuleId" :options="rules" clearable placeholder="Any rule" style="width: 100%;" />
        </n-form-item>

        <n-form-item label="Source" size="small" style="min-width: 180px; margin-bottom: 0;">
          <n-select v-model:value="filterSourceId" :options="sources" clearable placeholder="Any source" style="width: 100%;" />
        </n-form-item>

        <n-form-item label="Entity" size="small" style="min-width: 180px; margin-bottom: 0;">
          <n-select v-model:value="filterEntityId" :options="entities" clearable placeholder="Any entity" style="width: 100%;" />
        </n-form-item>

        <n-form-item label="Tweet limit" size="small" style="min-width: 120px; margin-bottom: 0;">
          <n-input-number v-model:value="limit" :min="10" :max="1000" :step="50" style="width: 100%;" />
        </n-form-item>
      </n-space>

      <template #footer>
        <n-button type="primary" :loading="store.loading" :disabled="!pipelineReady" @click="onGenerate" style="width: 100%;">
          <template #icon><n-icon><SparklesOutline /></n-icon></template>
          {{ store.loading ? 'Generating digest…' : 'Generate Digest' }}
        </n-button>
      </template>
    </n-card>

    <n-alert v-if="!pipelineReady" type="warning">
      No active digest pipeline found. Go to <strong>Pipelines</strong> and create an Ollama pipeline with usecase <strong>digest</strong>.
    </n-alert>

    <n-alert v-if="store.error" type="error">{{ store.error }}</n-alert>

    <!-- Jobs list -->
    <template v-if="store.jobs.length > 0">
      <n-card
        v-for="job in store.jobs"
        :key="job.id"
        size="small"
      >
        <template #header>
          <n-space align="center" justify="space-between" style="width: 100%;">
            <n-space align="center">
              <span>Digest</span>
              <n-tag
                :type="job.status === 'done' ? 'success' : job.status === 'error' ? 'error' : 'warning'"
                size="small"
                round
              >{{ job.status }}</n-tag>
              <n-tag v-if="job.tweet_count > 0" type="info" size="small" round>{{ job.tweet_count }} tweets</n-tag>
              <n-text depth="3" style="font-size: 12px;">{{ new Date(job.created_at).toLocaleString() }}</n-text>
            </n-space>
            <n-tooltip trigger="hover">
              <template #trigger>
                <n-button
                  size="tiny"
                  type="error"
                  secondary
                  :disabled="job.status === 'pending' || job.status === 'running'"
                  @click="store.deleteJob(job.id).catch(() => msg.error('Failed to delete digest'))"
                >
                  <template #icon><n-icon><TrashOutline /></n-icon></template>
                </n-button>
              </template>
              Delete digest
            </n-tooltip>
          </n-space>
        </template>

        <!-- Running / pending -->
        <n-space v-if="job.status === 'pending' || job.status === 'running'" vertical>
          <n-skeleton text :repeat="4" />
          <n-skeleton text style="width: 60%" />
        </n-space>

        <!-- Error -->
        <n-alert v-else-if="job.status === 'error'" type="error">{{ job.error }}</n-alert>

        <!-- Done -->
        <div v-else style="white-space: pre-wrap; line-height: 1.7; font-size: 14px;">{{ job.result }}</div>
      </n-card>
    </template>

    <n-empty
      v-else-if="!store.loading"
      description="Set your filters and click Generate Digest"
      style="padding: 64px 0;"
    />
  </n-space>
</template>
