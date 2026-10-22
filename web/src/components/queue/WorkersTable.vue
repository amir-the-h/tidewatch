<!-- Live worker slots from the worker_stats WebSocket event, active workers first. -->
<script setup lang="ts">
import { h } from 'vue'
import { Badge, DataTable, StatusIndicator, Tooltip, type DataTableColumns } from '@/components/ui'
import { useQueuesStore } from '../../stores/queues'
import type { WorkerInfo } from '../../api/queue'

defineProps<{ loading?: boolean }>()

const queues = useQueuesStore()

function formatDuration(ms: number): string {
  if (ms < 1000) return '0.0s'
  const secs = ms / 1000
  if (secs < 60) return `${secs.toFixed(1)}s`
  const m = Math.floor(secs / 60)
  const s = Math.round(secs % 60)
  return s > 0 ? `${m}m ${s}s` : `${m}m`
}

const none = () => h('span', { class: 'text-muted-foreground' }, '—')

const columns: DataTableColumns<WorkerInfo> = [
  {
    title: '#', key: 'id', width: 56,
    render: (w) => h('span', { class: 'font-mono tabular text-muted-foreground' }, String(w.id)),
  },
  {
    title: 'Status', key: 'status', width: 100,
    render: (w) => w.status === 'active'
      ? h(StatusIndicator, { status: 'success' }, () => 'Active')
      : h(StatusIndicator, { status: 'idle' }, () => 'Idle'),
  },
  {
    title: 'Action', key: 'action',
    render: (w) => w.status === 'active' ? h('span', { class: 'font-medium' }, w.action) : none(),
  },
  {
    title: 'Type', key: 'pipeline', width: 130,
    render: (w) => w.status === 'active' && w.pipeline ? h(Badge, { variant: 'secondary' }, () => w.pipeline) : none(),
  },
  {
    title: 'Tweet', key: 'tweet_id', width: 140,
    render: (w) => w.tweet_id
      ? h(Tooltip, { content: w.tweet_id }, () => h('span', { class: 'font-mono tabular text-muted-foreground' },
          w.tweet_id.slice(0, 12) + (w.tweet_id.length > 12 ? '…' : '')))
      : none(),
  },
  {
    title: 'Duration', key: 'duration_ms', width: 100,
    render: (w) => w.status === 'active' ? h('span', { class: 'font-mono tabular' }, formatDuration(w.duration_ms)) : none(),
  },
  {
    title: 'Backend', key: 'backend_name', width: 140,
    render: (w) => w.backend_name ? h('span', { class: 'truncate' }, w.backend_name) : none(),
  },
]
</script>

<template>
  <section class="flex flex-col gap-2">
    <div>
      <h2 class="m-0 text-body font-semibold">Workers</h2>
      <p class="m-0 mt-0.5 text-small text-muted-foreground">Each worker runs one task at a time; Ollama tasks wait for a free backend slot.</p>
    </div>
    <DataTable
      :columns="columns"
      :data="queues.workers"
      :loading="loading"
      :row-key="(w: WorkerInfo) => w.id"
      empty-text="No workers reported yet. They appear here once the server starts them."
    />
  </section>
</template>
