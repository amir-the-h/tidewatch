<!-- web/src/views/FailuresView.vue -->
<script setup lang="ts">
import { h, ref, computed, onMounted, onUnmounted } from 'vue'
import { useFailuresStore } from '../stores/failures'
import { useMessage, NButton, NSpace, NTag } from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import type { FailedTweet } from '../api/failures'
import { flushQueue } from '../api/failures'
import { apiFetch } from '../api/client'
import { formatDateTime } from '../utils/format'
import { InformationCircleOutline, RefreshOutline, TrashOutline, BanOutline } from '@vicons/ionicons5'
import { iconBtn } from '../utils/renderActions'

const store = useFailuresStore()
const msg   = useMessage()

// ── Queue / worker stats ──────────────────────────────────────────────────────

interface QueueStats  { pending: number; failed: number }
interface WorkerStats {
  total: number
  active: number
  idle: number
  active_workers?: Array<{ id: number; action: string; pipeline: string; backend: string }>
}

const queueStats  = ref<QueueStats | null>(null)
const workerStats = ref<WorkerStats | null>(null)

interface PendingSample { t: number; v: number }
const pendingHistory = ref<PendingSample[]>([])

async function fetchStats() {
  try {
    const h = await apiFetch<any>('/api/health')
    if (h.queue) {
      queueStats.value = h.queue
      const sample = { t: Date.now(), v: h.queue.pending as number }
      pendingHistory.value.push(sample)
      if (pendingHistory.value.length > 10) pendingHistory.value.shift()
    }
    if (h.workers) workerStats.value = h.workers
  } catch { /* unreachable */ }
}

const drainEta = computed<string | null>(() => {
  const hist = pendingHistory.value
  if (hist.length < 2) return null
  const first = hist[0]
  const last  = hist[hist.length - 1]
  const dt = (last.t - first.t) / 1000   // seconds elapsed
  const dv = first.v - last.v            // tasks drained
  if (dt <= 0 || dv <= 0) return null    // not draining
  const rate    = dv / dt                // tasks/sec
  const seconds = Math.ceil(last.v / rate)
  if (seconds < 60)  return `~${seconds}s`
  const m = Math.floor(seconds / 60)
  const s = seconds % 60
  if (m < 60) return s > 0 ? `~${m}m ${s}s` : `~${m}m`
  const hr = Math.floor(m / 60)
  const rm = m % 60
  return rm > 0 ? `~${hr}h ${rm}m` : `~${hr}h`
})

let pollTimer: ReturnType<typeof setInterval> | null = null

// ── Failure actions ───────────────────────────────────────────────────────────

const showDetail  = ref(false)
const selected    = ref<FailedTweet | null>(null)
const clearing    = ref(false)
const flushing    = ref(false)
const drawerWidth = computed(() => Math.min(540, window.innerWidth - 16))

function openDetail(row: FailedTweet) {
  selected.value   = row
  showDetail.value = true
}

async function onClear() {
  clearing.value = true
  try {
    await store.clear()
    msg.success('Dead-letter queue cleared')
    await fetchStats()
  } catch (e: any) {
    msg.error(e.message)
  } finally {
    clearing.value = false
  }
}

async function onFlush() {
  flushing.value = true
  try {
    await flushQueue()
    msg.success('Pending queue flushed')
    await Promise.all([store.fetch(), fetchStats()])
  } catch (e: any) {
    msg.error(e.message)
  } finally {
    flushing.value = false
  }
}

// ── Failures table ────────────────────────────────────────────────────────────

const columns: DataTableColumns<FailedTweet> = [
  {
    title: 'Tweet',
    key: 'text',
    ellipsis: { tooltip: true },
    render: (row) => row.tweet?.Tweet?.text ?? '—',
  },
  {
    title: 'Author',
    key: 'author',
    width: 140,
    render: (row) => {
      const h_ = row.tweet?.Tweet?.author_handle
      return h_ ? `@${h_}` : row.tweet?.Tweet?.author_id ?? '—'
    },
  },
  {
    title: 'Failed Models',
    key: 'failed_models',
    width: 220,
    render: (row) => {
      const models = row.failed_models ?? []
      return h(NSpace, { size: 4, wrap: true }, {
        default: () => models.map(m =>
          h(NTag, { type: 'error', size: 'small', round: true }, { default: () => m.name })
        ),
      })
    },
  },
  {
    title: 'Failed At',
    key: 'timestamp',
    width: 180,
    render: (row) => {
      const ts = row.failed_models?.[0]?.timestamp
      return ts ? formatDateTime(ts) : '—'
    },
  },
  {
    title: '',
    key: 'actions',
    width: 50,
    render: (row) => iconBtn(InformationCircleOutline, 'Details', () => openDetail(row)),
  },
]

const pagination = computed(() => ({
  page:     store.page,
  pageSize: store.perPage,
  itemCount: store.total,
  showSizePicker: true,
  pageSizes: [20, 50, 100],
  onChange:  (p: number) => store.fetch(p),
  onUpdatePageSize: (ps: number) => store.fetch(1, ps),
}))

onMounted(() => {
  store.fetch()
  fetchStats()
  pollTimer = setInterval(fetchStats, 3000)
})
onUnmounted(() => { if (pollTimer !== null) clearInterval(pollTimer) })
</script>

<template>
  <n-space vertical size="large">

    <!-- Header -->
    <n-space align="center" justify="space-between" style="flex-wrap: wrap; gap: 8px;">
      <n-h2 style="margin: 0;">Queue</n-h2>
      <n-space size="small">
        <n-tooltip trigger="hover">
          <template #trigger>
            <n-button @click="() => { store.fetch(); fetchStats() }" :loading="store.loading">
              <template #icon><n-icon><RefreshOutline /></n-icon></template>
            </n-button>
          </template>
          Refresh
        </n-tooltip>
        <n-popconfirm @positive-click="onFlush">
          <template #trigger>
            <n-tooltip trigger="hover">
              <template #trigger>
                <n-button type="warning" :loading="flushing">
                  <template #icon><n-icon><BanOutline /></n-icon></template>
                </n-button>
              </template>
              Flush Pending
            </n-tooltip>
          </template>
          Drop all pending (unprocessed) tweets from the raw queue?
        </n-popconfirm>
        <n-popconfirm @positive-click="onClear">
          <template #trigger>
            <n-tooltip trigger="hover">
              <template #trigger>
                <n-button type="error" :loading="clearing" :disabled="store.total === 0">
                  <template #icon><n-icon><TrashOutline /></n-icon></template>
                </n-button>
              </template>
              Clear Failures ({{ store.total }})
            </n-tooltip>
          </template>
          Remove all {{ store.total }} entries from the dead-letter queue?
        </n-popconfirm>
      </n-space>
    </n-space>

    <!-- Stats cards -->
    <n-grid :cols="3" :x-gap="12" :y-gap="12" responsive="screen" :item-responsive="true">
      <n-gi span="3 s:1">
        <n-card size="small" embedded>
          <n-statistic label="Redis Pending">
            <template #default>
              <span :style="{ color: (queueStats?.pending ?? 0) > 0 ? '#f9e2af' : '#cdd6f4' }">
                {{ queueStats?.pending ?? '—' }}
              </span>
            </template>
            <template #suffix>
              <span v-if="drainEta" style="color: #585b70; font-size: 12px; margin-left: 8px;">
                {{ drainEta }}
              </span>
            </template>
          </n-statistic>
        </n-card>
      </n-gi>
      <n-gi span="3 s:1">
        <n-card size="small" embedded>
          <n-statistic label="Dead-Letter">
            <template #default>
              <span :style="{ color: (queueStats?.failed ?? 0) > 0 ? '#f38ba8' : '#cdd6f4' }">
                {{ queueStats?.failed ?? '—' }}
              </span>
            </template>
          </n-statistic>
        </n-card>
      </n-gi>
      <n-gi span="3 s:1">
        <n-card size="small" embedded>
          <n-statistic label="Workers">
            <template #default>
              <span :style="{ color: (workerStats?.active ?? 0) > 0 ? '#a6e3a1' : '#cdd6f4' }">
                {{ workerStats?.active ?? '—' }}
              </span>
              <span style="color: #585b70; font-size: 14px;"> / {{ workerStats?.total ?? '—' }}</span>
            </template>
            <template #suffix>
              <span v-if="workerStats" style="color: #585b70; font-size: 12px; margin-left: 8px;">
                {{ workerStats.idle }} idle
              </span>
            </template>
          </n-statistic>
          <div v-if="workerStats" style="margin-top: 8px; height: 4px; border-radius: 2px; background: rgba(255,255,255,0.08); overflow: hidden;">
            <div :style="{
              height: '100%',
              width: workerStats.total > 0 ? (workerStats.active / workerStats.total * 100) + '%' : '0%',
              background: workerStats.active > 0 ? '#a6e3a1' : 'rgba(255,255,255,0.15)',
              transition: 'width 0.5s ease',
            }" />
          </div>
        </n-card>
      </n-gi>
    </n-grid>

    <!-- Active workers -->
    <n-card v-if="workerStats" size="small" title="Workers">
      <n-empty
        v-if="!workerStats.active_workers || workerStats.active_workers.length === 0"
        :description="`All ${workerStats.total} workers idle`"
        style="padding: 16px 0;"
      />
      <n-data-table
        v-else
        :columns="[
          { title: 'Worker', key: 'id', width: 90,
            render: (row: { id: number; action: string; pipeline: string; backend: string }) =>
              h('span', { style: { color: '#585b70', fontVariantNumeric: 'tabular-nums' } }, `#${row.id}`)
          },
          { title: 'Action', key: 'action',
            render: (row: { id: number; action: string; pipeline: string; backend: string }) =>
              h('span', { style: { color: '#a6e3a1' } }, row.action)
          },
          { title: 'Pipeline', key: 'pipeline',
            render: (row: { id: number; action: string; pipeline: string; backend: string }) =>
              h('span', { style: { color: '#cdd6f4' } }, row.pipeline || '—')
          },
          { title: 'Backend', key: 'backend', width: 180,
            render: (row: { id: number; action: string; pipeline: string; backend: string }) => {
              if (!row.backend) return h('span', { style: { color: '#585b70' } }, '—')
              if (row.backend === 'waiting') return h('span', { style: { color: '#585b70' } }, 'waiting…')
              const display = row.backend.replace(/^https?:\/\//, '')
              return h('span', { style: { color: '#cdd6f4', fontVariantNumeric: 'tabular-nums' } }, display)
            }
          },
        ]"
        :data="workerStats.active_workers"
        :pagination="false"
        size="small"
      />
    </n-card>

    <!-- Dead-letter failures -->
    <n-card size="small" title="Dead-Letter Queue">
      <n-alert v-if="store.error" type="error" style="margin-bottom: 12px;">{{ store.error }}</n-alert>

      <n-empty
        v-if="!store.loading && store.failures.length === 0"
        description="No failures — dead-letter queue is empty"
        style="padding: 32px 0;"
      />

      <n-data-table
        v-else
        :columns="columns"
        :data="store.failures"
        :pagination="pagination"
        :loading="store.loading"
        :scroll-x="640"
        :row-key="(row: FailedTweet) => row.tweet?.Tweet?.id ?? ''"
        :row-props="(row: FailedTweet) => ({ style: 'cursor: pointer;', onClick: () => openDetail(row) })"
        size="small"
      />
    </n-card>

    <!-- Detail drawer -->
    <n-drawer v-model:show="showDetail" :width="drawerWidth" placement="right">
      <n-drawer-content :native-scrollbar="false" closable>
        <template #header>Failure Details</template>

        <template v-if="selected">
          <n-space vertical size="medium">

            <n-card size="small" title="Tweet">
              <n-descriptions :column="1" size="small" label-placement="left">
                <n-descriptions-item label="ID">
                  <n-text code>{{ selected.tweet?.Tweet?.id }}</n-text>
                </n-descriptions-item>
                <n-descriptions-item label="Author">
                  @{{ selected.tweet?.Tweet?.author_handle || selected.tweet?.Tweet?.author_id }}
                </n-descriptions-item>
                <n-descriptions-item label="Created">
                  {{ selected.tweet?.Tweet?.created_at ? formatDateTime(selected.tweet.Tweet.created_at) : '—' }}
                </n-descriptions-item>
              </n-descriptions>
              <div style="margin-top: 12px; white-space: pre-wrap; color: #cdd6f4; font-size: 13px;">
                {{ selected.tweet?.Tweet?.text }}
              </div>
            </n-card>

            <n-card v-if="selected.tweet?.MatchingRules?.length" size="small" title="Matching Rules">
              <n-space size="small" wrap>
                <n-tag
                  v-for="rule in selected.tweet.MatchingRules"
                  :key="rule.id"
                  type="info"
                  size="small"
                  round
                >
                  {{ rule.tag || rule.id }}
                </n-tag>
              </n-space>
            </n-card>

            <n-card size="small" title="Failed Models">
              <n-space vertical size="small">
                <n-card v-for="(fm, i) in selected.failed_models" :key="i" size="small" embedded>
                  <n-space align="center" justify="space-between">
                    <n-tag type="error" size="small" round>{{ fm.name }}</n-tag>
                    <n-text depth="3" style="font-size: 11px;">
                      {{ fm.timestamp ? formatDateTime(fm.timestamp) : '' }}
                    </n-text>
                  </n-space>
                  <div style="
                    margin-top: 8px; padding: 8px;
                    background: rgba(243,139,168,0.08); border-radius: 4px;
                    font-family: monospace; font-size: 12px;
                    color: #f38ba8; white-space: pre-wrap; word-break: break-all;
                  ">{{ fm.error }}</div>
                </n-card>
              </n-space>
            </n-card>

          </n-space>
        </template>
      </n-drawer-content>
    </n-drawer>

  </n-space>
</template>
