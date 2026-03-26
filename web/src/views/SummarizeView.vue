<!-- web/src/views/SummarizeView.vue -->
<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useDigestStore } from '../stores/summaries'
import { useMessage } from 'naive-ui'
import { listRules } from '../api/rules'
import { listSources } from '../api/sources'
import { listTrackedEntities } from '../api/entities'

const store = useDigestStore()
const msg   = useMessage()

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
  const [rulesData, sourcesData, entitiesData] = await Promise.allSettled([
    listRules(),
    listSources(),
    listTrackedEntities(),
  ])
  if (rulesData.status === 'fulfilled')
    rules.value = rulesData.value.map(r => ({ label: r.name || String(r.id), value: r.id }))
  if (sourcesData.status === 'fulfilled')
    sources.value = sourcesData.value.map(s => ({ label: s.name || s.value, value: s.id }))
  if (entitiesData.status === 'fulfilled')
    entities.value = entitiesData.value.map(e => ({ label: e.name, value: e.id }))
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
          <n-date-picker v-model:value="filterFrom" type="datetime" clearable placeholder="Start date" style="width: 100%;" />
        </n-form-item>

        <n-form-item label="To" size="small" style="min-width: 200px; margin-bottom: 0;">
          <n-date-picker v-model:value="filterTo" type="datetime" clearable placeholder="End date" style="width: 100%;" />
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
        <n-button type="primary" :loading="store.loading" @click="onGenerate" style="width: 100%;">
          {{ store.loading ? 'Generating digest (may take a minute)…' : 'Generate Digest' }}
        </n-button>
      </template>
    </n-card>

    <n-alert v-if="store.error" type="error">{{ store.error }}</n-alert>

    <!-- Loading skeleton -->
    <n-card v-if="store.loading" size="small">
      <n-space vertical>
        <n-skeleton text :repeat="6" />
        <n-skeleton text style="width: 60%" />
      </n-space>
    </n-card>

    <!-- Digest output -->
    <n-card v-else-if="store.digest" size="small">
      <template #header>
        <n-space align="center">
          <span>Digest</span>
          <n-tag type="info" size="small" round>{{ store.tweetCount }} tweets</n-tag>
        </n-space>
      </template>
      <div style="white-space: pre-wrap; line-height: 1.7; font-size: 14px;">{{ store.digest }}</div>
    </n-card>

    <n-empty
      v-else
      description="Set your filters and click Generate Digest"
      style="padding: 64px 0;"
    />
  </n-space>
</template>
