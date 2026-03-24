<!-- web/src/views/TweetsView.vue -->
<script setup lang="ts">
import { h, onMounted, ref, computed } from 'vue'
import { useTweetsStore } from '../stores/tweets'
import { useRulesStore } from '../stores/rules'
import { useMessage, NButton, NSpace } from 'naive-ui'
import type { DataTableColumns, DataTableRowKey } from 'naive-ui'
import type { Tweet, TweetDetail } from '../api/tweets'

const store      = useTweetsStore()
const rulesStore = useRulesStore()
const msg        = useMessage()

const filterRuleId = ref<number | null>(null)
const filterFrom   = ref<number | null>(null)
const filterTo     = ref<number | null>(null)

const showDetail    = ref(false)
const detail        = ref<TweetDetail | null>(null)
const loadingDetail = ref(false)
const deleting      = ref(false)
const reprocessing  = ref(false)

const checkedRowKeys = ref<DataTableRowKey[]>([])
const hasSelection   = computed(() => checkedRowKeys.value.length > 0)

onMounted(() => {
  store.fetchTweets()
  rulesStore.fetchRules()
})

async function onFilter() {
  checkedRowKeys.value = []
  await store.fetchTweets({
    rule_id: filterRuleId.value ?? undefined,
    from:    filterFrom.value ? new Date(filterFrom.value).toISOString() : undefined,
    to:      filterTo.value   ? new Date(filterTo.value).toISOString()   : undefined,
  })
}

async function openDetail(id: string) {
  loadingDetail.value = true
  showDetail.value = true
  try { detail.value = await store.getTweet(id) }
  catch (e: any) { msg.error(e.message); showDetail.value = false }
  finally { loadingDetail.value = false }
}

async function deleteOne(id: string) {
  deleting.value = true
  try {
    await store.remove([id])
    if (detail.value?.tweet.id === id) showDetail.value = false
    msg.success('Deleted')
  } catch (e: any) {
    msg.error(e.message)
  } finally {
    deleting.value = false
  }
}

async function deleteSelected() {
  const ids = checkedRowKeys.value as string[]
  deleting.value = true
  try {
    const { deleted } = await store.remove(ids)
    checkedRowKeys.value = []
    msg.success(`Deleted ${deleted} tweet${deleted !== 1 ? 's' : ''}`)
  } catch (e: any) {
    msg.error(e.message)
  } finally {
    deleting.value = false
  }
}

async function reprocessOne(id: string) {
  reprocessing.value = true
  try {
    const { requeued } = await store.reprocess({ tweet_ids: [id] })
    if (requeued) {
      msg.success('Queued for reprocessing')
      // Refresh detail after a brief delay so the worker has time to run
      setTimeout(async () => {
        detail.value = await store.getTweet(id)
      }, 2000)
    } else {
      msg.warning('Tweet not found')
    }
  } catch (e: any) {
    msg.error(e.message)
  } finally {
    reprocessing.value = false
  }
}

async function reprocessSelected() {
  const ids = checkedRowKeys.value as string[]
  reprocessing.value = true
  try {
    const { requeued } = await store.reprocess({ tweet_ids: ids })
    msg.success(`Queued ${requeued} tweet${requeued !== 1 ? 's' : ''} for reprocessing`)
  } catch (e: any) {
    msg.error(e.message)
  } finally {
    reprocessing.value = false
  }
}

const columns: DataTableColumns<Tweet> = [
  { type: 'selection', multiple: true },
  { title: 'ID',      key: 'id', width: 160, ellipsis: true },
  { title: 'Text',    key: 'text', ellipsis: true },
  {
    title: 'Author', key: 'author_handle', width: 160,
    render: (row) => row.author_handle ? `@${row.author_handle}` : row.author_id,
  },
  { title: 'Created', key: 'created_at', width: 180 },
  {
    title: '', key: 'actions', width: 120,
    render: (row) => h(NSpace, { size: 'small' }, {
      default: () => [
        h(NButton, { size: 'small', onClick: () => openDetail(row.id) }, { default: () => 'Detail' }),
        h(NButton, { size: 'small', type: 'error', onClick: () => deleteOne(row.id) }, { default: () => 'Delete' }),
      ]
    })
  },
]
</script>

<template>
  <n-space vertical size="large">
    <n-h2>Tweets</n-h2>

    <!-- Filters -->
    <n-space wrap>
      <n-select
        v-model:value="filterRuleId"
        :options="rulesStore.rules.map(r => ({ label: r.name, value: r.id }))"
        placeholder="Filter by rule"
        clearable
        style="width: 200px"
      />
      <n-date-picker v-model:value="filterFrom" type="datetime" placeholder="From" clearable />
      <n-date-picker v-model:value="filterTo"   type="datetime" placeholder="To"   clearable />
      <n-button @click="onFilter">Apply</n-button>
      <n-button secondary @click="filterRuleId = null; filterFrom = null; filterTo = null; store.fetchTweets(); checkedRowKeys = []">Reset</n-button>
    </n-space>

    <!-- Bulk action toolbar -->
    <n-space v-if="hasSelection" align="center">
      <span>{{ checkedRowKeys.length }} selected</span>
      <n-button type="primary" size="small" :loading="reprocessing" @click="reprocessSelected">
        Reprocess selected
      </n-button>
      <n-button type="error" size="small" :loading="deleting" @click="deleteSelected">
        Delete selected
      </n-button>
    </n-space>

    <n-data-table
      :columns="columns"
      :data="store.tweets"
      :loading="store.loading"
      :pagination="{
        page: store.page,
        pageSize: store.perPage,
        itemCount: store.total,
        onChange: (p: number) => store.goToPage(p),
      }"
      :remote="true"
      :row-key="(row: Tweet) => row.id"
      v-model:checked-row-keys="checkedRowKeys"
    />

    <!-- Detail drawer -->
    <n-drawer v-model:show="showDetail" :width="540">
      <n-drawer-content title="Tweet Detail" closable>
        <n-spin v-if="loadingDetail" />
        <div v-else-if="detail">
          <n-descriptions :column="1" bordered>
            <n-descriptions-item label="ID">{{ detail.tweet.id }}</n-descriptions-item>
            <n-descriptions-item label="Author">
              <a
                v-if="detail.tweet.author_handle"
                :href="`https://x.com/${detail.tweet.author_handle}`"
                target="_blank"
                rel="noopener noreferrer"
              >@{{ detail.tweet.author_handle }}</a>
              <span v-else>{{ detail.tweet.author_id }}</span>
            </n-descriptions-item>
            <n-descriptions-item label="Created">{{ detail.tweet.created_at }}</n-descriptions-item>
            <n-descriptions-item label="Text">{{ detail.tweet.text }}</n-descriptions-item>
          </n-descriptions>

          <n-h3 style="margin-top: 16px">Matched Rules</n-h3>
          <n-empty v-if="!detail.tweet.rules?.length" description="None" />
          <n-space v-else wrap>
            <n-tag
              v-for="rule in detail.tweet.rules"
              :key="rule.id"
              size="small"
              :type="rule.active ? 'info' : 'default'"
            >
              {{ rule.name }}
            </n-tag>
          </n-space>

          <n-h3 style="margin-top: 16px">Categorizations</n-h3>
          <n-empty v-if="!detail.categorizations.length" description="None" />
          <n-list v-else>
            <n-list-item v-for="cat in detail.categorizations" :key="cat.id">
              <n-space vertical size="small">
                <n-tag size="small">{{ cat.model_name }}</n-tag>
                <!-- Summarizer -->
                <span v-if="cat.results.summary != null">{{ cat.results.summary || '(empty summary)' }}</span>
                <!-- Extraction (geo-event): truncated retweet -->
                <span v-else-if="cat.results.truncated" style="color: var(--n-text-color-disabled)">Truncated retweet — full text unavailable</span>
                <!-- Extraction (geo-event) -->
                <n-space v-else-if="cat.results.description != null" vertical size="small">
                  <span v-if="cat.results.description">{{ cat.results.description }}</span>
                  <n-space v-if="cat.results.locations?.length" wrap>
                    <n-tag
                      v-for="(loc, i) in cat.results.locations"
                      :key="i"
                      size="small"
                      type="info"
                    >
                      {{ loc.label ? `${loc.label}: ` : '' }}{{ loc.coordinates[0].toFixed(5) }}, {{ loc.coordinates[1].toFixed(5) }}
                      <template v-if="loc.place"> · {{ loc.place }}</template>
                    </n-tag>
                  </n-space>
                  <n-space v-if="cat.results.geolocated_by?.length" wrap>
                    <n-tag v-for="h in cat.results.geolocated_by" :key="h" size="small">{{ h }}</n-tag>
                  </n-space>
                  <n-space v-if="cat.results.location_url" wrap>
                    <a :href="cat.results.location_url" target="_blank" rel="noopener noreferrer" style="font-size:12px">
                      GeoConfirmed ↗
                    </a>
                  </n-space>
                </n-space>
                <!-- NER: word spans -->
                <n-space v-else-if="cat.results.entities?.length" wrap>
                  <n-tag v-for="(e, i) in cat.results.entities" :key="i" size="small" type="success">
                    {{ e.word ?? `token[${e.token_index}]` }} · {{ e.label }} ({{ (e.score * 100).toFixed(0) }}%)
                  </n-tag>
                </n-space>
                <!-- Zero-shot: ranked label list -->
                <n-space v-else-if="cat.results.labels?.length" wrap>
                  <n-tag
                    v-for="(lbl, i) in cat.results.labels"
                    :key="lbl"
                    size="small"
                    :type="i === 0 ? 'primary' : 'default'"
                  >
                    {{ lbl }} ({{ ((cat.results.scores?.[i] ?? 0) * 100).toFixed(1) }}%)
                  </n-tag>
                </n-space>
                <!-- Zero-shot with no confident labels -->
                <span v-else-if="cat.results.labels != null" style="color: var(--n-text-color-disabled)">(no label above threshold)</span>
                <!-- Standard classification (label + score) -->
                <n-space v-else>
                  <span>{{ cat.results.label ?? '—' }}</span>
                  <n-tag v-if="cat.results.score != null" size="small" type="info">
                    {{ (cat.results.score * 100).toFixed(1) }}%
                  </n-tag>
                </n-space>
              </n-space>
            </n-list-item>
          </n-list>

          <div style="margin-top: 16px">
            <n-space>
              <n-button
                tag="a"
                :href="`https://x.com/i/web/status/${detail.tweet.id}`"
                target="_blank"
                rel="noopener noreferrer"
                size="small"
              >
                Open on X
              </n-button>
              <n-button type="primary" size="small" :loading="reprocessing" @click="reprocessOne(detail.tweet.id)">
                Reprocess
              </n-button>
              <n-button type="error" size="small" :loading="deleting" @click="deleteOne(detail.tweet.id)">
                Delete tweet
              </n-button>
            </n-space>
          </div>
        </div>
      </n-drawer-content>
    </n-drawer>
  </n-space>
</template>
