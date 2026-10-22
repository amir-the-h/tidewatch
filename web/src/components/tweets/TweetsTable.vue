<!-- Server-paged tweet list. Admins get row selection and a row Delete; everyone can open the detail. -->
<script setup lang="ts">
import { computed, h } from 'vue'
import { InfoIcon, TrashIcon } from '@lucide/vue'
import { Button, DataTable, IconButton, Pagination, Select, type DataTableColumns } from '@/components/ui'
import { useTweetsStore } from '../../stores/tweets'
import { useAuthStore } from '../../stores/auth'
import type { Tweet } from '../../api/tweets'
import { formatDateTime } from '../../utils/format'

const emit = defineEmits<{ open: [id: string]; delete: [row: Tweet]; retry: [] }>()

/** 0 means every tweet on one page. */
const perPage   = defineModel<number>('perPage', { required: true })
const selection = defineModel<(string | number)[]>('selection', { required: true })

const store = useTweetsStore()
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

const columns = computed<DataTableColumns<Tweet>>(() => [
  {
    title: 'ID', key: 'id', width: 190,
    render: (row) => h('span', { class: 'font-mono tabular text-muted-foreground' }, row.id),
  },
  { title: 'Text', key: 'text', ellipsis: { tooltip: true } },
  {
    title: 'Author', key: 'author_handle', width: 160, ellipsis: { tooltip: true },
    render: (row) => row.author_handle
      ? `@${row.author_handle}`
      : h('span', { class: 'font-mono tabular text-muted-foreground' }, row.author_id),
  },
  {
    title: 'Created', key: 'created_at', width: 190,
    render: (row) => h('span', { class: 'tabular text-muted-foreground' }, formatDateTime(row.created_at)),
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
    <span class="text-destructive">Could not load tweets: {{ store.error }}</span>
    <Button variant="outline" @click="emit('retry')">Retry</Button>
  </div>

  <template v-else>
    <!-- Selection only feeds admin bulk actions, so viewers get no checkbox column. -->
    <DataTable
      :selection="auth.isAdmin ? selection : undefined"
      :columns="columns"
      :data="store.tweets"
      :loading="store.loading"
      :row-key="(row: Tweet) => row.id"
      @update:selection="selection = $event ?? []"
    >
      <template #empty><slot name="empty" /></template>
    </DataTable>

    <div v-if="store.total > pageSizes[0].value" class="flex flex-wrap items-center justify-between gap-3">
      <span class="text-small tabular text-muted-foreground">{{ store.total.toLocaleString() }} tweets</span>
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
