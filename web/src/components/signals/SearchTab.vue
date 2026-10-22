<!-- X search backfill (admin only): run a search job over recent tweets and list past jobs. -->
<script setup lang="ts">
import { computed, h, onMounted, ref } from 'vue'
import { ChevronDownIcon, ChevronRightIcon, EyeIcon, SearchIcon, TrashIcon } from '@lucide/vue'
import {
  AlertDialog, Button, Card, Combobox, DataTable, DatePicker, FormField, IconButton, Input, NumberField,
  StatusIndicator, type DataTableColumns,
} from '@/components/ui'
import { useToast } from '@/composables/useToast'
import { useSearchStore } from '../../stores/search'
import { useRulesStore } from '../../stores/rules'
import { ApiError } from '../../api/client'
import type { SearchJob } from '../../api/search'
import { formatDateTime } from '../../utils/format'

const emit = defineEmits<{ viewSignals: [] }>()

const toast      = useToast()
const store      = useSearchStore()
const rulesStore = useRulesStore()

const selectedRuleIds = ref<number[]>([])
const startMs         = ref<number | null>(null)
const endMs           = ref<number | null>(null)
const maxResults      = ref<number | null>(500)
const totalLimit      = ref<number | null>(0)
const sinceId         = ref('')
const untilId         = ref('')
const showAdvanced    = ref(false)
const errorIs503      = ref(false)

const ruleOptions = computed(() =>
  rulesStore.rules.filter(r => !r.local).map(r => ({ label: r.name, value: r.id })))

onMounted(() => {
  rulesStore.fetchRules(true)
  store.fetchJobs()
})

function buildParams() {
  const params: Record<string, any> = {}
  if (selectedRuleIds.value.length > 0)          params.rule_ids    = selectedRuleIds.value
  if (startMs.value != null)                     params.start_time  = new Date(startMs.value).toISOString()
  if (endMs.value != null)                       params.end_time    = new Date(endMs.value).toISOString()
  if (maxResults.value != null && maxResults.value > 0) params.max_results = maxResults.value
  if (totalLimit.value != null && totalLimit.value > 0) params.limit       = totalLimit.value
  if (sinceId.value.trim())                      params.since_id    = sinceId.value.trim()
  if (untilId.value.trim())                      params.until_id    = untilId.value.trim()
  return params
}

async function onRun() {
  errorIs503.value = false
  try {
    await store.run(buildParams())
    toast.success('Search completed')
  } catch (e: unknown) {
    if (e instanceof ApiError && e.status === 503) errorIs503.value = true
  }
}

// ── Delete job ───────────────────────────────────────────────────────────────

const deleteTarget = ref<SearchJob | null>(null)
const showDelete   = ref(false)
const deleting     = ref(false)

function askDelete(job: SearchJob) {
  deleteTarget.value = job
  showDelete.value = true
}

async function onConfirmDelete() {
  const target = deleteTarget.value
  if (!target) return
  deleting.value = true
  try {
    await store.removeJob(target.id)
    toast.success('Search job deleted')
    showDelete.value = false
  } catch (e: unknown) {
    toast.error(`Could not delete search job: ${e instanceof Error ? e.message : String(e)}`)
  } finally {
    deleting.value = false
  }
}

// ── Jobs table ───────────────────────────────────────────────────────────────

const statusOf = (s: SearchJob['status']) => ({
  done:    { status: 'success' as const,     label: 'Done' },
  error:   { status: 'destructive' as const, label: 'Error' },
  running: { status: 'info' as const,        label: 'Running' },
  pending: { status: 'idle' as const,        label: 'Pending' },
})[s]

const columns: DataTableColumns<SearchJob> = [
  {
    title: 'Status', key: 'status', width: 110,
    render: (row) => h(StatusIndicator, { status: statusOf(row.status).status }, () => statusOf(row.status).label),
  },
  {
    title: 'Result', key: 'result', ellipsis: { tooltip: true },
    render: (row) => {
      if (row.status === 'error') return h('span', { class: 'text-destructive' }, row.error || 'Failed')
      if (row.status === 'done') return h('span', { class: 'tabular' }, `${row.tweets_fetched.toLocaleString()} tweet${row.tweets_fetched === 1 ? '' : 's'} fetched`)
      return h('span', { class: 'text-muted-foreground' }, 'Fetching tweets…')
    },
  },
  {
    title: 'Started', key: 'created_at', width: 190,
    render: (row) => h('span', { class: 'tabular text-muted-foreground' }, formatDateTime(row.created_at)),
  },
  {
    title: '', key: 'actions', width: 88, align: 'right',
    render: (row) => h('div', { class: 'flex items-center justify-end gap-1' }, [
      row.status === 'done' && row.tweets_fetched > 0
        ? h(IconButton, { icon: EyeIcon, label: 'View signals', onClick: () => emit('viewSignals') })
        : null,
      h(IconButton, { icon: TrashIcon, label: 'Delete job', onClick: () => askDelete(row) }),
    ]),
  },
]
</script>

<template>
  <div class="flex flex-col gap-6">
    <Card title="Search parameters" description="Backfills matching tweets from X's recent search into the signal list.">
      <form class="flex flex-col gap-4" @submit.prevent="onRun">
        <FormField label="Rules" description="Leave empty to search every active rule.">
          <Combobox v-model="selectedRuleIds" :options="ruleOptions" placeholder="All active rules" clearable />
        </FormField>
        <div class="grid gap-4 sm:grid-cols-2">
          <FormField label="Start time"><DatePicker v-model="startMs" placeholder="Start time" clearable /></FormField>
          <FormField label="End time"><DatePicker v-model="endMs" placeholder="End time" clearable /></FormField>
          <FormField label="Max per page" for="search-max">
            <NumberField id="search-max" v-model="maxResults" :min="1" :max="500" placeholder="500" />
          </FormField>
          <FormField label="Total limit" for="search-limit" description="0 means unlimited.">
            <NumberField id="search-limit" v-model="totalLimit" :min="0" placeholder="0" />
          </FormField>
        </div>

        <div>
          <Button variant="link" @click="showAdvanced = !showAdvanced">
            <template #icon>
              <ChevronDownIcon v-if="showAdvanced" :stroke-width="1.75" />
              <ChevronRightIcon v-else :stroke-width="1.75" />
            </template>
            Advanced options
          </Button>
        </div>
        <div v-if="showAdvanced" class="grid gap-4 sm:grid-cols-2">
          <FormField label="Since ID" for="search-since">
            <Input id="search-since" v-model="sinceId" class="font-mono" placeholder="Fetch tweets after this tweet ID" clearable />
          </FormField>
          <FormField label="Until ID" for="search-until">
            <Input id="search-until" v-model="untilId" class="font-mono" placeholder="Fetch tweets before this tweet ID" clearable />
          </FormField>
        </div>

        <div>
          <Button size="default" native-type="submit" :loading="store.loading" :disabled="store.loading">
            <template #icon><SearchIcon :stroke-width="1.75" /></template>
            Run search
          </Button>
        </div>
      </form>
    </Card>

    <div
      v-if="store.error"
      class="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-destructive p-4 text-small"
    >
      <span v-if="errorIs503" class="text-destructive">
        Search is not enabled. Set <code class="font-mono">search.enabled: true</code> in your config.
      </span>
      <span v-else class="text-destructive">Search failed: {{ store.error }}</span>
      <Button variant="outline" @click="store.error = null">Dismiss</Button>
    </div>

    <section class="flex flex-col gap-2">
      <h2 class="m-0 text-body font-semibold">Search jobs</h2>
      <DataTable
        :columns="columns"
        :data="store.jobs"
        :row-key="(row: SearchJob) => row.id"
        empty-text="No search jobs yet. Set the parameters above and run a search."
      />
    </section>
  </div>

  <AlertDialog
    v-model:open="showDelete"
    title="Delete this search job?"
    description="Only the job record is removed. Signals it fetched stay in the signal list."
    action-label="Delete job"
    :loading="deleting"
    @confirm="onConfirmDelete"
  />
</template>
