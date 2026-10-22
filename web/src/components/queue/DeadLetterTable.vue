<!-- Dead-letter queue: tasks whose pipeline action errored, server-paged. -->
<script setup lang="ts">
import { computed, h } from 'vue'
import { InfoIcon } from '@lucide/vue'
import { Badge, Button, DataTable, IconButton, Pagination, Select, type DataTableColumns } from '@/components/ui'
import { useFailuresStore } from '../../stores/failures'
import type { FailedTweet } from '../../api/failures'
import { formatDateTime } from '../../utils/format'

const emit = defineEmits<{ open: [row: FailedTweet] }>()

const store = useFailuresStore()

const pageSizes = [20, 50, 100].map(n => ({ label: String(n), value: n }))

const page = computed({
  get: () => store.page,
  set: (p: number) => { store.fetch(p) },
})

const perPage = computed({
  get: () => store.perPage,
  set: (pp: number | null) => { if (pp) store.fetch(1, pp) },
})

const none = () => h('span', { class: 'text-muted-foreground' }, '—')

const columns: DataTableColumns<FailedTweet> = [
  {
    title: 'Tweet', key: 'text', ellipsis: { tooltip: true },
    render: (row) => row.tweet?.Tweet?.text ?? none(),
  },
  {
    title: 'Author', key: 'author', width: 140,
    render: (row) => {
      const handle = row.tweet?.Tweet?.author_handle
      const id = row.tweet?.Tweet?.author_id
      if (handle) return `@${handle}`
      return id ? h('span', { class: 'font-mono tabular text-muted-foreground' }, id) : none()
    },
  },
  {
    title: 'Failed actions', key: 'failed_models', width: 220,
    render: (row) => h('div', { class: 'flex flex-wrap gap-1' },
      (row.failed_models ?? []).map(m => h(Badge, { variant: 'destructive' }, () => m.name))),
  },
  {
    title: 'Failed at', key: 'timestamp', width: 180,
    render: (row) => {
      const ts = row.failed_models?.[0]?.timestamp
      return ts ? h('span', { class: 'tabular text-muted-foreground' }, formatDateTime(ts)) : none()
    },
  },
  {
    title: '', key: 'actions', width: 56, align: 'right',
    render: (row) => h(IconButton, { icon: InfoIcon, label: 'Details', onClick: () => emit('open', row) }),
  },
]
</script>

<template>
  <section class="flex flex-col gap-2">
    <div>
      <h2 class="m-0 text-body font-semibold">Dead-letter queue</h2>
      <p class="m-0 mt-0.5 text-small text-muted-foreground">Tasks whose action returned an error. They are not retried.</p>
    </div>

    <div
      v-if="store.error"
      class="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-destructive p-4 text-small"
    >
      <span class="text-destructive">Could not load failures: {{ store.error }}</span>
      <Button variant="outline" @click="store.fetch(store.page)">Retry</Button>
    </div>

    <template v-else>
      <DataTable
        :columns="columns"
        :data="store.failures"
        :loading="store.loading"
        empty-text="No failed tasks. Tasks whose action errors land here."
      />
      <div v-if="store.total > pageSizes[0].value" class="flex flex-wrap items-center justify-end gap-3">
        <div class="flex items-center gap-2 text-small text-muted-foreground">
          Rows per page
          <div class="w-20"><Select v-model="perPage" :options="pageSizes" /></div>
        </div>
        <Pagination v-model:page="page" :total="store.total" :items-per-page="store.perPage" />
      </div>
    </template>
  </section>
</template>
