<!-- Entity candidates found by the entity_discovery pipeline, awaiting promote / merge / reject. Admin-only. -->
<script setup lang="ts">
import { computed, h, onMounted, ref, watch } from 'vue'
import {
  Badge, Button, DataTable, Dialog, FormField, Pagination, Select, StatusIndicator,
  type DataTableColumns,
} from '@/components/ui'
import { useToast } from '@/composables/useToast'
import { useEntitiesStore } from '../../stores/entities'
import { useCandidatesStore } from '../../stores/candidates'
import { listTrackedEntities, type EntityCandidate, type TrackedEntity } from '../../api/entities'
import { formatDateTime } from '../../utils/format'

const store           = useEntitiesStore()
const candidatesStore = useCandidatesStore()
const toast           = useToast()

const errorMessage = (e: unknown) => (e instanceof Error ? e.message : String(e))

const perPage = 50

// ── Filters and paging (server-side) ─────────────────────────────────────────

const statusOptions = [
  { label: 'Pending',  value: 'pending'  },
  { label: 'Promoted', value: 'promoted' },
  { label: 'Rejected', value: 'rejected' },
  { label: 'All',      value: ''         },
]

const typeOptions = ['LOC', 'PER', 'ORG', 'MISC'].map(t => ({ label: t, value: t }))

const filterStatus = ref<string | null>('pending')
const filterType   = ref<string | null>(null)
const page         = ref(1)
const loadError    = ref('')

const candidates = computed(() => candidatesStore.response?.candidates ?? [])
const total      = computed(() => candidatesStore.response?.total ?? 0)

async function loadCandidates() {
  loadError.value = ''
  try {
    await candidatesStore.fetchCandidates(
      { status: filterStatus.value || undefined, type: filterType.value || undefined },
      page.value,
      perPage,
    )
  } catch (e: unknown) {
    loadError.value = errorMessage(e)
  }
}

// Resetting the page triggers the page watcher, so load here only when already on page 1.
watch([filterStatus, filterType], () => {
  if (page.value !== 1) page.value = 1
  else loadCandidates()
})
watch(page, loadCandidates)

const hasFilters = computed(() => filterStatus.value !== 'pending' || !!filterType.value)

function clearFilters() {
  filterStatus.value = 'pending'
  filterType.value = null
}

// ── Promote / reject ─────────────────────────────────────────────────────────

async function onPromote(c: EntityCandidate) {
  try {
    await candidatesStore.promote(c.id)
    toast.success(`${c.word} promoted to a tracked entity`)
    store.fetchEntities()
    candidatesStore.fetchPendingCount()
  } catch (e: unknown) {
    toast.error(`Could not promote ${c.word}: ${errorMessage(e)}`)
  }
}

async function onReject(c: EntityCandidate) {
  try {
    await candidatesStore.reject(c.id)
    toast.success(`${c.word} rejected`)
    candidatesStore.fetchPendingCount()
  } catch (e: unknown) {
    toast.error(`Could not reject ${c.word}: ${errorMessage(e)}`)
  }
}

// ── Merge as alias ───────────────────────────────────────────────────────────

const showMerge      = ref(false)
const mergeCandidate = ref<EntityCandidate | null>(null)
const mergeTargetId  = ref<number | null>(null)
const mergeEntities  = ref<TrackedEntity[]>([])
const merging        = ref(false)

const mergeOptions = computed(() =>
  mergeEntities.value.map(e => ({ label: `${e.name} (${e.type})`, value: e.id }))
)

async function openMerge(c: EntityCandidate) {
  mergeCandidate.value = c
  mergeTargetId.value  = null
  try {
    mergeEntities.value = await listTrackedEntities()
    showMerge.value = true
  } catch (e: unknown) {
    toast.error(`Could not load tracked entities: ${errorMessage(e)}`)
  }
}

async function onConfirmMerge() {
  const c = mergeCandidate.value
  if (!c || !mergeTargetId.value) return
  merging.value = true
  try {
    await candidatesStore.merge(c.id, mergeTargetId.value)
    toast.success(`${c.word} merged as an alias`)
    showMerge.value = false
    store.fetchEntities()
    candidatesStore.fetchPendingCount()
  } catch (e: unknown) {
    toast.error(`Could not merge ${c.word}: ${errorMessage(e)}`)
  } finally {
    merging.value = false
  }
}

// ── Table columns ────────────────────────────────────────────────────────────

const statusIndicator: Record<EntityCandidate['status'], 'warning' | 'success' | 'idle'> = {
  pending: 'warning', promoted: 'success', rejected: 'idle',
}
const statusLabel: Record<EntityCandidate['status'], string> = {
  pending: 'Pending', promoted: 'Promoted', rejected: 'Rejected',
}

const columns: DataTableColumns<EntityCandidate> = [
  {
    title: 'Word', key: 'word', ellipsis: { tooltip: true },
    render: (row) => h('span', { class: 'font-medium' }, row.word),
  },
  {
    title: 'Type', key: 'type', width: 80,
    render: (row) => row.type ? h(Badge, { variant: 'secondary' }, () => row.type) : null,
  },
  {
    title: 'Count', key: 'occurrence_count', width: 80, align: 'right',
    render: (row) => h('span', { class: 'tabular' }, String(row.occurrence_count)),
  },
  {
    title: 'Confidence', key: 'avg_confidence', width: 100, align: 'right',
    render: (row) => h('span', { class: 'tabular' }, (row.avg_confidence * 100).toFixed(1) + '%'),
  },
  {
    title: 'First seen', key: 'first_seen_at', width: 180,
    render: (row) => h('span', { class: 'tabular text-muted-foreground' }, formatDateTime(row.first_seen_at)),
  },
  {
    title: 'Last seen', key: 'last_seen_at', width: 180,
    render: (row) => h('span', { class: 'tabular text-muted-foreground' }, formatDateTime(row.last_seen_at)),
  },
  {
    title: 'Status', key: 'status', width: 110,
    render: (row) => h(StatusIndicator, { status: statusIndicator[row.status] ?? 'idle' },
      () => statusLabel[row.status] ?? row.status),
  },
  {
    title: '', key: 'actions', width: 220, align: 'right',
    render: (row) => row.status !== 'pending' ? null : h('div', { class: 'flex items-center justify-end gap-1' }, [
      h(Button, { variant: 'outline', onClick: () => onPromote(row) }, () => 'Promote'),
      h(Button, { variant: 'outline', onClick: () => openMerge(row) }, () => 'Merge'),
      h(Button, { variant: 'ghost', onClick: () => onReject(row) }, () => 'Reject'),
    ]),
  },
]

onMounted(() => {
  loadCandidates()
  candidatesStore.fetchPendingCount()
})
</script>

<template>
  <div class="flex flex-col gap-3">
    <div class="flex flex-wrap items-center gap-2">
      <div class="w-44">
        <Select v-model="filterStatus" :options="statusOptions" />
      </div>
      <div class="w-44">
        <Select v-model="filterType" :options="typeOptions" placeholder="All types" clearable />
      </div>
    </div>

    <div
      v-if="loadError"
      class="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-destructive p-4 text-small"
    >
      <span class="text-destructive">Could not load candidates: {{ loadError }}</span>
      <Button variant="outline" @click="loadCandidates">Retry</Button>
    </div>

    <template v-else>
      <DataTable
        :columns="columns"
        :data="candidates"
        :loading="candidatesStore.loading"
        :row-key="(row: EntityCandidate) => row.id"
      >
        <template #empty>
          <div v-if="hasFilters" class="flex flex-col items-center gap-3 py-8 text-center">
            <p class="m-0 text-muted-foreground">No candidates match these filters.</p>
            <Button variant="outline" @click="clearFilters">Show pending</Button>
          </div>
          <p v-else class="m-0 py-8 text-center text-muted-foreground">
            No candidates to review. They appear here as the entity_discovery pipeline finds new names in signals.
          </p>
        </template>
      </DataTable>
      <div class="flex justify-end">
        <Pagination v-model:page="page" :total="total" :items-per-page="perPage" />
      </div>
    </template>
  </div>

  <Dialog
    v-model:open="showMerge"
    :title="`Merge ${mergeCandidate?.word ?? 'candidate'} as an alias`"
    description="The word is added to the chosen entity's aliases and the candidate is marked promoted."
  >
    <FormField label="Tracked entity" required>
      <Select v-model="mergeTargetId" :options="mergeOptions" filterable placeholder="Search tracked entities" />
    </FormField>
    <template #footer>
      <Button variant="outline" :disabled="merging" @click="showMerge = false">Cancel</Button>
      <Button :disabled="!mergeTargetId" :loading="merging" @click="onConfirmMerge">Merge as alias</Button>
    </template>
  </Dialog>
</template>
