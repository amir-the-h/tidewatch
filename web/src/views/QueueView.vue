<!-- web/src/views/QueueView.vue -->
<script setup lang="ts">
import { h, ref, computed, onMounted } from 'vue'
import { useFailuresStore } from '../stores/failures'
import { useQueuesStore } from '../stores/queues'
import { useMessage, NButton, NSpace, NTag, NIcon, NTooltip } from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import type { FailedTweet } from '../api/failures'
import { getQueue, flushQueue } from '../api/queue'
import { apiFetch } from '../api/client'
import type { QueueEntry } from '../stores/queues'
import { formatDateTime } from '../utils/format'
import { InformationCircleOutline, RefreshOutline, TrashOutline, BanOutline } from '@vicons/ionicons5'
import { iconBtn } from '../utils/renderActions'

function fmtEta(secs: number): string {
  if (secs < 60) return `~${secs}s`
  const m = Math.floor(secs / 60), s = secs % 60
  if (m < 60) return s > 0 ? `~${m}m ${s}s` : `~${m}m`
  const hr = Math.floor(m / 60), rm = m % 60
  return rm > 0 ? `~${hr}h ${rm}m` : `~${hr}h`
}

function perActionEta(entry: QueueEntry): string | null {
  if (entry.throughput === 0 || entry.pending === 0) return null
  return fmtEta(Math.ceil(entry.pending / entry.throughput))
}

const store       = useFailuresStore()
const queuesStore = useQueuesStore()
const msg         = useMessage()

// ── Stats ─────────────────────────────────────────────────────────────────────

async function fetchStats() {
  try {
    const health = await apiFetch<any>('/api/health')
    if (health.workers) {
      queuesStore.setWorkerStats(health.workers)
    }
  } catch { /* ignore */ }
}

async function fetchQueues() {
  try {
    const data = await getQueue()
    queuesStore.setQueues(data.queues)
  } catch { /* ignore */ }
}

const drainEta = computed<string | null>(() => {
  const totalThroughput = queuesStore.queues.reduce((s, q) => s + q.throughput, 0)
  if (totalThroughput === 0 || queuesStore.totalPending === 0) return null
  return fmtEta(Math.ceil(queuesStore.totalPending / totalThroughput))
})

// ── Actions ───────────────────────────────────────────────────────────────────

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
  } catch (e: any) { msg.error(e.message) }
  finally { clearing.value = false }
}

async function onFlush() {
  flushing.value = true
  try {
    await flushQueue()
    msg.success('Pending queue flushed')
    await Promise.all([store.fetch(), fetchStats(), fetchQueues()])
  } catch (e: any) { msg.error(e.message) }
  finally { flushing.value = false }
}

// ── Queue table ───────────────────────────────────────────────────────────────

const pipelineTagType: Record<string, 'info' | 'success' | 'default' | 'warning'> = {
  onnx: 'info',
  ollama: 'success',
  internal: 'default',
  notification: 'warning',
}

const queueColumns = [
  {
    title: 'Action',
    key: 'action_name',
    render: (row: QueueEntry) =>
      h('span', { style: { color: '#cdd6f4' } }, row.action_name),
  },
  {
    title: 'Pipeline',
    key: 'pipeline_name',
    render: (row: QueueEntry) =>
      h(NSpace, { size: 6, align: 'center' }, {
        default: () => [
          h('span', { style: { color: '#a6adc8' } }, row.pipeline_name),
          h(NTag, {
            type: pipelineTagType[row.pipeline_type] ?? 'default',
            size: 'small',
            round: true,
          }, { default: () => row.pipeline_type }),
        ],
      }),
  },
  {
    title: 'Pending',
    key: 'pending',
    width: 100,
    render: (row: QueueEntry) =>
      h('span', {
        style: {
          color: row.pending > 0 ? '#f9e2af' : '#585b70',
          fontVariantNumeric: 'tabular-nums',
        },
      }, row.pending > 0 ? String(row.pending) : '—'),
  },
  {
    title: 'ETA',
    key: 'eta',
    width: 130,
    render: (row: QueueEntry) => {
      const eta = perActionEta(row)
      const rate = row.throughput > 0
        ? h('span', { style: { color: '#585b70', fontSize: '11px', marginLeft: '6px' } },
            `(${row.throughput < 1 ? row.throughput.toFixed(2) : row.throughput.toFixed(1)}/s)`)
        : null
      if (!eta) {
        return h('span', { style: { color: '#585b70' } }, '—')
      }
      return h('span', { style: { fontVariantNumeric: 'tabular-nums' } }, [
        h('span', { style: { color: '#cba6f7' } }, eta),
        rate,
      ])
    },
  },
  {
    title: 'Worker',
    key: 'workers',
    width: 220,
    render: (row: QueueEntry) => {
      const workers = row.workers ?? []
      if (workers.length === 0) return h('span', { style: { color: '#585b70' } }, '—')

      function workerLabel(w: { id: number; backend: string }) {
        if (w.backend === 'waiting') return `#${w.id} · waiting…`
        if (w.backend) return `#${w.id} · ${w.backend.replace(/^https?:\/\//, '')}`
        return `#${w.id}`
      }

      if (workers.length === 1) {
        return h('span', {
          style: { color: '#a6e3a1', fontVariantNumeric: 'tabular-nums' },
        }, workerLabel(workers[0]))
      }

      // Multiple workers: count badge with tooltip listing each one.
      return h(NTooltip, { trigger: 'hover' }, {
        trigger: () => h('span', {
          style: { color: '#a6e3a1', fontVariantNumeric: 'tabular-nums', cursor: 'default' },
        }, `${workers.length} workers`),
        default: () => h('div', {}, workers.map(w =>
          h('div', { style: { whiteSpace: 'nowrap', lineHeight: '1.6' } }, workerLabel(w))
        )),
      })
    },
  },
]

// Split active (pending or workers) from idle rows.
const activeQueues   = computed(() => queuesStore.queues.filter(q => q.pending > 0 || q.workers.length > 0))
const inactiveQueues = computed(() => queuesStore.queues.filter(q => q.pending === 0 && q.workers.length === 0))

// ── Failures table ────────────────────────────────────────────────────────────

const failureColumns: DataTableColumns<FailedTweet> = [
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
      const handle = row.tweet?.Tweet?.author_handle
      return handle ? `@${handle}` : row.tweet?.Tweet?.author_id ?? '—'
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
  page:      store.page,
  pageSize:  store.perPage,
  itemCount: store.total,
  showSizePicker: true,
  pageSizes: [20, 50, 100],
  onChange:          (p: number)  => store.fetch(p),
  onUpdatePageSize:  (ps: number) => store.fetch(1, ps),
}))

// ── Lifecycle ─────────────────────────────────────────────────────────────────

onMounted(() => {
  store.fetch()
  fetchStats()
  fetchQueues()
})
</script>

<template>
  <n-space vertical size="large">

    <!-- Header -->
    <n-space align="center" justify="space-between" style="flex-wrap: wrap; gap: 8px;">
      <n-h2 style="margin: 0;">Queue</n-h2>
      <n-space size="small">
        <n-tooltip trigger="hover">
          <template #trigger>
            <n-button @click="() => { store.fetch(); fetchStats(); fetchQueues() }" :loading="store.loading">
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
          Drop all pending tasks from all action queues?
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
              <span :style="{ color: queuesStore.totalPending > 0 ? '#f9e2af' : '#cdd6f4' }">
                {{ queuesStore.totalPending }}
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
              <span :style="{ color: store.total > 0 ? '#f38ba8' : '#cdd6f4' }">
                {{ store.total > 0 ? store.total : '—' }}
              </span>
            </template>
          </n-statistic>
        </n-card>
      </n-gi>
      <n-gi span="3 s:1">
        <n-card size="small" embedded>
          <n-statistic label="Workers">
            <template #default>
              <span :style="{ color: (queuesStore.workerStats?.active ?? 0) > 0 ? '#a6e3a1' : '#cdd6f4' }">
                {{ queuesStore.workerStats?.active ?? '—' }}
              </span>
              <span style="color: #585b70; font-size: 14px;"> / {{ queuesStore.workerStats?.total ?? '—' }}</span>
            </template>
            <template #suffix>
              <span v-if="queuesStore.workerStats" style="color: #585b70; font-size: 12px; margin-left: 8px;">
                {{ queuesStore.workerStats.idle }} idle
              </span>
            </template>
          </n-statistic>
          <div v-if="queuesStore.workerStats" style="margin-top: 8px; height: 4px; border-radius: 2px; background: rgba(255,255,255,0.08); overflow: hidden;">
            <div :style="{
              height: '100%',
              width: queuesStore.workerStats.total > 0 ? (queuesStore.workerStats.active / queuesStore.workerStats.total * 100) + '%' : '0%',
              background: queuesStore.workerStats.active > 0 ? '#a6e3a1' : 'rgba(255,255,255,0.15)',
              transition: 'width 0.5s ease',
            }" />
          </div>
        </n-card>
      </n-gi>
    </n-grid>

    <!-- Per-action queues table -->
    <n-card size="small" title="Action Queues">
      <n-empty
        v-if="queuesStore.queues.length === 0"
        description="No active actions"
        style="padding: 16px 0;"
      />
      <template v-else>
        <!-- Active rows (pending > 0 or workers assigned) -->
        <n-empty
          v-if="activeQueues.length === 0"
          description="All queues idle"
          style="padding: 12px 0 4px;"
        />
        <n-data-table
          v-else
          :columns="queueColumns"
          :data="activeQueues"
          :pagination="false"
          size="small"
        />

        <!-- Inactive rows — collapsed by default -->
        <n-collapse v-if="inactiveQueues.length > 0" style="margin-top: 8px;">
          <n-collapse-item :title="`${inactiveQueues.length} inactive`" name="inactive">
            <n-data-table
              :columns="queueColumns"
              :data="inactiveQueues"
              :pagination="false"
              size="small"
              :row-props="() => ({ style: 'opacity: 0.5;' })"
            />
          </n-collapse-item>
        </n-collapse>
      </template>
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
        :columns="failureColumns"
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
                  type="info" size="small" round
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
