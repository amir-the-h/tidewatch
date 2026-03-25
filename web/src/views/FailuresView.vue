<!-- web/src/views/FailuresView.vue -->
<script setup lang="ts">
import { h, ref, computed, onMounted } from 'vue'
import { useFailuresStore } from '../stores/failures'
import { useMessage, NButton, NSpace, NTag } from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import type { FailedTweet } from '../api/failures'
import { flushQueue } from '../api/failures'
import { formatDateTime } from '../utils/format'

const store   = useFailuresStore()
const msg     = useMessage()

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
    store.fetch()
  } catch (e: any) {
    msg.error(e.message)
  } finally {
    flushing.value = false
  }
}

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
    width: 80,
    render: (row) =>
      h(NButton, { size: 'small', onClick: () => openDetail(row) }, { default: () => 'Details' }),
  },
]

const pagination = computed(() => ({
  page:       store.page,
  pageSize:   store.perPage,
  itemCount:  store.total,
  showSizePicker: true,
  pageSizes:  [20, 50, 100],
  onChange:   (p: number) => store.fetch(p),
  onUpdatePageSize: (ps: number) => store.fetch(1, ps),
}))

onMounted(() => store.fetch())
</script>

<template>
  <n-space vertical size="large">
    <n-space align="center" justify="space-between" style="flex-wrap: wrap; gap: 8px;">
      <n-h2 style="margin: 0;">Queue Failures</n-h2>
      <n-space>
        <n-button @click="store.fetch()" :loading="store.loading">Refresh</n-button>
        <n-popconfirm @positive-click="onFlush">
          <template #trigger>
            <n-button type="warning" :loading="flushing">Flush Pending Queue</n-button>
          </template>
          Drop all pending (unprocessed) tweets from the raw queue?
        </n-popconfirm>
        <n-popconfirm @positive-click="onClear">
          <template #trigger>
            <n-button type="error" :loading="clearing" :disabled="store.total === 0">
              Clear Failures ({{ store.total }})
            </n-button>
          </template>
          Remove all {{ store.total }} entries from the dead-letter queue?
        </n-popconfirm>
      </n-space>
    </n-space>

    <n-alert v-if="store.error" type="error">{{ store.error }}</n-alert>

    <n-empty
      v-if="!store.loading && store.failures.length === 0"
      description="No failures — queue is clean"
      style="padding: 48px 0;"
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

    <!-- Detail drawer -->
    <n-drawer
      v-model:show="showDetail"
      :width="drawerWidth"
      placement="right"
    >
      <n-drawer-content :native-scrollbar="false" closable>
        <template #header>Failure Details</template>

        <template v-if="selected">
          <n-space vertical size="medium">

            <!-- Tweet info -->
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

            <!-- Matching rules -->
            <n-card
              v-if="selected.tweet?.MatchingRules?.length"
              size="small"
              title="Matching Rules"
            >
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

            <!-- Failed models -->
            <n-card size="small" title="Failed Models">
              <n-space vertical size="small">
                <n-card
                  v-for="(fm, i) in selected.failed_models"
                  :key="i"
                  size="small"
                  embedded
                >
                  <n-space align="center" justify="space-between">
                    <n-tag type="error" size="small" round>{{ fm.name }}</n-tag>
                    <n-text depth="3" style="font-size: 11px;">
                      {{ fm.timestamp ? formatDateTime(fm.timestamp) : '' }}
                    </n-text>
                  </n-space>
                  <div style="
                    margin-top: 8px;
                    padding: 8px;
                    background: rgba(243,139,168,0.08);
                    border-radius: 4px;
                    font-family: monospace;
                    font-size: 12px;
                    color: #f38ba8;
                    white-space: pre-wrap;
                    word-break: break-all;
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
