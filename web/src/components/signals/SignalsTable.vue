<!-- Server-paged signal list. Admins get row selection and a row Delete; everyone can open the detail. -->
<script setup lang="ts">
import { computed, h } from 'vue'
import { InfoIcon, TrashIcon } from '@lucide/vue'
import { Badge, Button, DataTable, IconButton, Pagination, Select, type DataTableColumns } from '@/components/ui'
import { useSignalsStore } from '../../stores/signals'
import { useAuthStore } from '../../stores/auth'
import { signalHeadline, type Signal } from '../../api/signals'
import { formatDateTime } from '../../utils/format'

const emit = defineEmits<{ open: [id: number]; delete: [row: Signal]; retry: [] }>()

/** 0 means every signal on one page. */
const perPage   = defineModel<number>('perPage', { required: true })
const selection = defineModel<(string | number)[]>('selection', { required: true })

const store = useSignalsStore()
const auth  = useAuthStore()

const ALL = 999999
const effectivePerPage = computed(() => perPage.value === 0 ? ALL : perPage.value)

const pageSizes = [
  { label: '20',  value: 20 },
  { label: '50',  value: 50 },
  { label: '100', value: 100 },
  { label: 'All', value: 0 },
]

const page = computed({
  get: () => store.page,
  set: (p: number) => { store.goToPage(p, effectivePerPage.value) },
})

const perPageModel = computed({
  get: () => perPage.value,
  set: (pp: number | null) => { if (pp != null) perPage.value = pp },
})

const columns = computed<DataTableColumns<Signal>>(() => [
  {
    title: 'ID', key: 'id', width: 90,
    render: (row) => h('span', { class: 'font-mono tabular text-muted-foreground' }, String(row.id)),
  },
  {
    title: 'Kind', key: 'kind', width: 90,
    render: (row) => h(Badge, { variant: 'outline' }, () => row.kind),
  },
  {
    title: 'Text', key: 'text', ellipsis: { tooltip: true },
    render: (row) => signalHeadline(row),
  },
  {
    title: 'Author', key: 'author', width: 160, ellipsis: { tooltip: true },
    render: (row) => row.author
      ? (row.ext_ns === 'x' ? `@${row.author}` : row.author)
      : h('span', { class: 'font-mono tabular text-muted-foreground' }, row.author_id),
  },
  {
    title: 'Published', key: 'published_at', width: 190,
    render: (row) => h('span', { class: 'tabular text-muted-foreground' }, formatDateTime(row.published_at)),
  },
  {
    title: '', key: 'actions', width: auth.isAdmin ? 88 : 56, align: 'right',
    render: (row) => h('div', { class: 'flex items-center justify-end gap-1' }, [
      h(IconButton, { icon: InfoIcon, label: 'Details', onClick: () => emit('open', row.id) }),
      auth.isAdmin ? h(IconButton, { icon: TrashIcon, label: 'Delete', onClick: () => emit('delete', row) }) : null,
    ]),
  },
])
</script>

<template>
  <div
    v-if="store.error"
    class="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-destructive p-4 text-small"
  >
    <span class="text-destructive">Could not load signals: {{ store.error }}</span>
    <Button variant="outline" @click="emit('retry')">Retry</Button>
  </div>

  <template v-else>
    <!-- Selection only feeds admin bulk actions, so viewers get no checkbox column. -->
    <DataTable
      :selection="auth.isAdmin ? selection : undefined"
      :columns="columns"
      :data="store.signals"
      :loading="store.loading"
      :row-key="(row: Signal) => row.id"
      @update:selection="selection = $event ?? []"
    >
      <template #empty><slot name="empty" /></template>
    </DataTable>

    <div v-if="store.total > pageSizes[0].value" class="flex flex-wrap items-center justify-between gap-3">
      <span class="text-small tabular text-muted-foreground">{{ store.total.toLocaleString() }} signals</span>
      <div class="flex flex-wrap items-center gap-3">
        <div class="flex items-center gap-2 text-small text-muted-foreground">
          Rows per page
          <div class="w-20"><Select v-model="perPageModel" :options="pageSizes" /></div>
        </div>
        <Pagination v-model:page="page" :total="store.total" :items-per-page="effectivePerPage" />
      </div>
    </div>
  </template>
</template>
