<!-- web/src/views/SummarizeView.vue -->
<script setup lang="ts">
import { h, ref, computed, onMounted } from 'vue'
import { useSummariesStore } from '../stores/summaries'
import { useMessage, NButton, NSpace } from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import type { SummaryItem } from '../api/summaries'
import { listRules } from '../api/rules'
import { listSources } from '../api/sources'
import { listTrackedEntities } from '../api/entities'
import { formatDateTime } from '../utils/format'

const store = useSummariesStore()
const msg   = useMessage()

// Filter state
const filterFrom      = ref<number | null>(null)
const filterTo        = ref<number | null>(null)
const filterRuleId    = ref<number | null>(null)
const filterSourceId  = ref<number | null>(null)
const filterEntityId  = ref<number | null>(null)

// Filter options for dropdowns
const rules    = ref<{ label: string; value: number }[]>([])
const sources  = ref<{ label: string; value: number }[]>([])
const entities = ref<{ label: string; value: number }[]>([])

// Drawer state
const showDetail  = ref(false)
const selected    = ref<SummaryItem | null>(null)
const drawerWidth = computed(() => Math.min(560, window.innerWidth - 16))

function openDetail(row: SummaryItem) {
  selected.value  = row
  showDetail.value = true
}

function applyFilters() {
  store.filters = {
    from:      filterFrom.value  ? new Date(filterFrom.value).toISOString()  : undefined,
    to:        filterTo.value    ? new Date(filterTo.value).toISOString()    : undefined,
    rule_id:   filterRuleId.value   ?? undefined,
    source_id: filterSourceId.value ?? undefined,
    entity_id: filterEntityId.value ?? undefined,
  }
  store.fetch(1)
}

async function onRun() {
  try {
    applyFilters()
    const queued = await store.run()
    msg.success(`Queued ${queued} tweet${queued !== 1 ? 's' : ''} for summarization`)
  } catch (e: any) {
    msg.error(e.message)
  }
}

const columns: DataTableColumns<SummaryItem> = [
  {
    title: 'Tweet',
    key: 'tweet_text',
    ellipsis: { tooltip: true },
    render: (row) => row.tweet_text,
  },
  {
    title: 'Author',
    key: 'author_handle',
    width: 140,
    render: (row) => row.author_handle ? `@${row.author_handle}` : '—',
  },
  {
    title: 'Summary',
    key: 'summary',
    ellipsis: { tooltip: true },
    render: (row) => row.summary || '—',
  },
  {
    title: 'Model',
    key: 'model_name',
    width: 160,
    render: (row) => row.model_name,
  },
  {
    title: 'Summarized At',
    key: 'summarized_at',
    width: 180,
    render: (row) => row.summarized_at ? formatDateTime(row.summarized_at) : '—',
  },
  {
    title: '',
    key: 'actions',
    width: 80,
    render: (row) =>
      h(NButton, { size: 'small', onClick: () => openDetail(row) }, { default: () => 'Details' }),
  },
]

const pagination = computed(() => ({
  page:      store.page,
  pageSize:  store.perPage,
  itemCount: store.total,
  showSizePicker: true,
  pageSizes: [20, 50, 100],
  onChange:  (p: number) => store.fetch(p),
  onUpdatePageSize: (ps: number) => store.fetch(1, ps),
}))

onMounted(async () => {
  const [rulesData, sourcesData, entitiesData] = await Promise.allSettled([
    listRules(),
    listSources(),
    listTrackedEntities(),
  ])
  if (rulesData.status === 'fulfilled') {
    rules.value = rulesData.value.map(r => ({ label: r.name || String(r.id), value: r.id }))
  }
  if (sourcesData.status === 'fulfilled') {
    sources.value = sourcesData.value.map(s => ({ label: s.name || s.value, value: s.id }))
  }
  if (entitiesData.status === 'fulfilled') {
    entities.value = entitiesData.value.map(e => ({ label: e.name, value: e.id }))
  }
  store.fetch()
})
</script>

<template>
  <n-space vertical size="large">
    <n-space align="center" justify="space-between" style="flex-wrap: wrap; gap: 8px;">
      <n-h2 style="margin: 0;">Summarize Tweets</n-h2>
      <n-space>
        <n-button @click="store.fetch()" :loading="store.loading">Refresh</n-button>
        <n-button type="primary" @click="onRun" :loading="store.running">
          Run Summarization
        </n-button>
      </n-space>
    </n-space>

    <!-- Filters -->
    <n-card size="small">
      <n-space wrap size="medium" align="flex-end">
        <n-form-item label="From" size="small" style="min-width: 200px; margin-bottom: 0;">
          <n-date-picker
            v-model:value="filterFrom"
            type="datetime"
            clearable
            placeholder="Start date"
            style="width: 100%;"
          />
        </n-form-item>

        <n-form-item label="To" size="small" style="min-width: 200px; margin-bottom: 0;">
          <n-date-picker
            v-model:value="filterTo"
            type="datetime"
            clearable
            placeholder="End date"
            style="width: 100%;"
          />
        </n-form-item>

        <n-form-item label="Rule" size="small" style="min-width: 180px; margin-bottom: 0;">
          <n-select
            v-model:value="filterRuleId"
            :options="rules"
            clearable
            placeholder="Any rule"
            style="width: 100%;"
          />
        </n-form-item>

        <n-form-item label="Source" size="small" style="min-width: 180px; margin-bottom: 0;">
          <n-select
            v-model:value="filterSourceId"
            :options="sources"
            clearable
            placeholder="Any source"
            style="width: 100%;"
          />
        </n-form-item>

        <n-form-item label="Entity" size="small" style="min-width: 180px; margin-bottom: 0;">
          <n-select
            v-model:value="filterEntityId"
            :options="entities"
            clearable
            placeholder="Any entity"
            style="width: 100%;"
          />
        </n-form-item>

        <n-button size="small" @click="applyFilters">Apply Filters</n-button>
      </n-space>
    </n-card>

    <n-alert v-if="store.error" type="error">{{ store.error }}</n-alert>

    <n-empty
      v-if="!store.loading && store.summaries.length === 0"
      description="No summaries yet — use Run Summarization to generate them"
      style="padding: 48px 0;"
    />

    <n-data-table
      v-else
      :columns="columns"
      :data="store.summaries"
      :pagination="pagination"
      :loading="store.loading"
      :scroll-x="800"
      :row-key="(row: SummaryItem) => `${row.tweet_id}-${row.model_name}`"
      :row-props="(row: SummaryItem) => ({ style: 'cursor: pointer;', onClick: () => openDetail(row) })"
      size="small"
    />

    <!-- Detail drawer -->
    <n-drawer v-model:show="showDetail" :width="drawerWidth" placement="right">
      <n-drawer-content :native-scrollbar="false" closable>
        <template #header>Summary Details</template>

        <template v-if="selected">
          <n-space vertical size="medium">

            <n-card size="small" title="Tweet">
              <n-descriptions :column="1" size="small" label-placement="left">
                <n-descriptions-item label="ID">
                  <n-text code>{{ selected.tweet_id }}</n-text>
                </n-descriptions-item>
                <n-descriptions-item label="Author">
                  @{{ selected.author_handle || '—' }}
                </n-descriptions-item>
                <n-descriptions-item label="Created">
                  {{ selected.created_at ? formatDateTime(selected.created_at) : '—' }}
                </n-descriptions-item>
              </n-descriptions>
              <div style="margin-top: 12px; white-space: pre-wrap; color: #cdd6f4; font-size: 13px;">
                {{ selected.tweet_text }}
              </div>
            </n-card>

            <n-card size="small" title="Summary">
              <n-descriptions :column="1" size="small" label-placement="left" style="margin-bottom: 12px;">
                <n-descriptions-item label="Model">
                  <n-tag type="info" size="small" round>{{ selected.model_name }}</n-tag>
                </n-descriptions-item>
                <n-descriptions-item label="Generated">
                  {{ selected.summarized_at ? formatDateTime(selected.summarized_at) : '—' }}
                </n-descriptions-item>
              </n-descriptions>
              <div style="white-space: pre-wrap; color: #a6e3a1; font-size: 13px; line-height: 1.6;">
                {{ selected.summary || 'No summary available.' }}
              </div>
            </n-card>

          </n-space>
        </template>
      </n-drawer-content>
    </n-drawer>
  </n-space>
</template>
