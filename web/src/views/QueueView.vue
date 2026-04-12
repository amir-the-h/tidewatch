<!-- web/src/views/QueueView.vue -->
<script setup lang="ts">
import { h, ref, computed, onMounted } from 'vue'
import { useFailuresStore } from '../stores/failures'
import { useQueuesStore } from '../stores/queues'
import { useMessage, NButton, NSpace, NTag, NIcon } from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import type { FailedTweet } from '../api/failures'
import type { WorkerInfo } from '../api/queue'
import { getQueue, flushQueue, flushWorkerBuffer } from '../api/queue'
import { formatDateTime } from '../utils/format'
import { InformationCircleOutline, RefreshOutline, TrashOutline, BanOutline, StopCircleOutline } from '@vicons/ionicons5'
import { iconBtn } from '../utils/renderActions'

function formatDuration(ms: number): string {
  if (ms < 1000) return '0.0s'
  const secs = ms / 1000
  if (secs < 60) return `${secs.toFixed(1)}s`
  const m = Math.floor(secs / 60)
  const s = Math.round(secs % 60)
  return s > 0 ? `${m}m ${s}s` : `${m}m`
}

const store       = useFailuresStore()
const queuesStore = useQueuesStore()
const msg         = useMessage()

// ── Data fetching ──────────────────────────────────────────────────────────────

async function fetchQueue() {
  try {
    const data = await getQueue()
    queuesStore.setFromResponse(data.pending_tasks, data.pending_ollama, data.workers)
  } catch { /* ignore */ }
}

// ── Computed stats ─────────────────────────────────────────────────────────────

const totalWorkers  = computed(() => queuesStore.workers.length)
const activeWorkers = computed(() => queuesStore.workers.filter(w => w.status === 'active').length)
const idleWorkers   = computed(() => queuesStore.workers.filter(w => w.status === 'idle').length)

// ── Actions ───────────────────────────────────────────────────────────────────

const showDetail  = ref(false)
const selected    = ref<FailedTweet | null>(null)
const clearing    = ref(false)
const flushing    = ref(false)
const stopping    = ref(false)
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
  } catch (e: any) { msg.error(e.message) }
  finally { clearing.value = false }
}

async function onFlush() {
  flushing.value = true
  try {
    await flushQueue()
    msg.success('Pending queue flushed')
    await Promise.all([store.fetch(), fetchQueue()])
  } catch (e: any) { msg.error(e.message) }
  finally { flushing.value = false }
}

async function onStopWorkers() {
  stopping.value = true
  try {
    await flushWorkerBuffer()
    await flushQueue()
    msg.success('Workers stopped and queue flushed')
    await Promise.all([store.fetch(), fetchQueue()])
  } catch (e: any) { msg.error(e.message) }
  finally { stopping.value = false }
}

// ── Worker table ──────────────────────────────────────────────────────────────

const pipelineTagType: Record<string, 'info' | 'success' | 'default' | 'warning'> = {
  onnx: 'info',
  ollama: 'success',
  internal: 'default',
  notification: 'warning',
}

const workerColumns: DataTableColumns<WorkerInfo> = [
  {
    title: '#',
    key: 'id',
    width: 50,
    render: (row) => h('span', { style: { color: '#585b70', fontVariantNumeric: 'tabular-nums' } }, String(row.id)),
  },
  {
    title: 'Status',
    key: 'status',
    width: 90,
    render: (row) => {
      const isActive = row.status === 'active'
      return h('span', {
        style: { color: isActive ? '#a6e3a1' : '#585b70' },
      }, isActive ? '● active' : '○ idle')
    },
  },
  {
    title: 'Action',
    key: 'action',
    render: (row) =>
      row.status === 'active'
        ? h('span', { style: { color: '#cdd6f4' } }, row.action)
        : h('span', { style: { color: '#313244' } }, '—'),
  },
  {
    title: 'Type',
    key: 'pipeline',
    width: 130,
    render: (row) => {
      if (row.status !== 'active' || !row.pipeline) {
        return h('span', { style: { color: '#313244' } }, '—')
      }
      return h(NTag, {
        type: pipelineTagType[row.pipeline] ?? 'default',
        size: 'small',
        round: true,
      }, { default: () => row.pipeline })
    },
  },
  {
    title: 'Tweet',
    key: 'tweet_id',
    width: 130,
    render: (row) =>
      row.tweet_id
        ? h('span', { style: { color: '#a6adc8', fontVariantNumeric: 'tabular-nums', fontFamily: 'monospace' } },
            row.tweet_id.slice(0, 12) + (row.tweet_id.length > 12 ? '…' : ''))
        : h('span', { style: { color: '#313244' } }, '—'),
  },
  {
    title: 'Depth',
    key: 'depth',
    width: 70,
    render: (row) =>
      row.status === 'active'
        ? h('span', { style: { color: '#cba6f7', fontVariantNumeric: 'tabular-nums' } }, String(row.depth))
        : h('span', { style: { color: '#313244' } }, '—'),
  },
  {
    title: 'Duration',
    key: 'duration_ms',
    width: 100,
    render: (row) =>
      row.status === 'active'
        ? h('span', { style: { color: '#f9e2af', fontVariantNumeric: 'tabular-nums' } }, formatDuration(row.duration_ms))
        : h('span', { style: { color: '#313244' } }, '—'),
  },
  {
    title: 'Backend',
    key: 'backend_name',
    width: 130,
    render: (row) =>
      row.backend_name
        ? h('span', { style: { color: '#89dceb' } }, row.backend_name)
        : h('span', { style: { color: '#313244' } }, '—'),
  },
]

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
  fetchQueue()
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
            <n-button @click="() => { store.fetch(); fetchQueue() }" :loading="store.loading">
              <template #icon><n-icon><RefreshOutline /></n-icon></template>
            </n-button>
          </template>
          Refresh
        </n-tooltip>
        <n-popconfirm @positive-click="onStopWorkers">
          <template #trigger>
            <n-tooltip trigger="hover">
              <template #trigger>
                <n-button type="error" :loading="stopping">
                  <template #icon><n-icon><StopCircleOutline /></n-icon></template>
                </n-button>
              </template>
              Stop Workers &amp; Flush Queue
            </n-tooltip>
          </template>
          Cancel all active worker tasks and drop all pending queue items?
        </n-popconfirm>
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
          Drop all pending tasks from the queue?
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
    <n-grid :cols="4" :x-gap="12" :y-gap="12" responsive="screen" :item-responsive="true">
      <n-gi span="4 s:1">
        <n-card size="small" embedded>
          <n-statistic label="Pending (tasks)">
            <template #default>
              <span :style="{ color: queuesStore.pendingTasks > 0 ? '#f9e2af' : '#cdd6f4' }">
                {{ queuesStore.pendingTasks }}
              </span>
            </template>
          </n-statistic>
        </n-card>
      </n-gi>
      <n-gi span="4 s:1">
        <n-card size="small" embedded>
          <n-statistic label="Pending (Ollama)">
            <template #default>
              <span :style="{ color: queuesStore.pendingOllama > 0 ? '#f9e2af' : '#cdd6f4' }">
                {{ queuesStore.pendingOllama }}
              </span>
            </template>
          </n-statistic>
        </n-card>
      </n-gi>
      <n-gi span="4 s:1">
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
      <n-gi span="4 s:1">
        <n-card size="small" embedded>
          <n-statistic label="Workers">
            <template #default>
              <span :style="{ color: activeWorkers > 0 ? '#a6e3a1' : '#cdd6f4' }">
                {{ totalWorkers > 0 ? activeWorkers : '—' }}
              </span>
              <span v-if="totalWorkers > 0" style="color: #585b70; font-size: 14px;"> / {{ totalWorkers }}</span>
            </template>
            <template #suffix>
              <span v-if="totalWorkers > 0" style="color: #585b70; font-size: 12px; margin-left: 8px;">
                {{ idleWorkers }} idle
              </span>
            </template>
          </n-statistic>
          <div v-if="totalWorkers > 0" style="margin-top: 8px; height: 4px; border-radius: 2px; background: rgba(255,255,255,0.08); overflow: hidden;">
            <div :style="{
              height: '100%',
              width: totalWorkers > 0 ? (activeWorkers / totalWorkers * 100) + '%' : '0%',
              background: activeWorkers > 0 ? '#a6e3a1' : 'rgba(255,255,255,0.15)',
              transition: 'width 0.5s ease',
            }" />
          </div>
        </n-card>
      </n-gi>
    </n-grid>

    <!-- Workers table -->
    <n-card size="small" title="Workers">
      <n-empty
        v-if="queuesStore.workers.length === 0"
        description="No worker data"
        style="padding: 16px 0;"
      />
      <n-data-table
        v-else
        :columns="workerColumns"
        :data="queuesStore.workers"
        :pagination="false"
        size="small"
        :row-props="(row: WorkerInfo) => ({ style: row.status === 'idle' ? 'opacity: 0.4;' : '' })"
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
