<!-- Signal list: filters, server-paged table, bulk actions on the selection, and the detail Sheet.
     Filters and the open signal are mirrored to the URL (?kind, rule_id, from, to, signal). -->
<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { AlertDialog, Button } from '@/components/ui'
import { useToast } from '@/composables/useToast'
import { useSignalsStore } from '../../stores/signals'
import { useRulesStore } from '../../stores/rules'
import { useAuthStore } from '../../stores/auth'
import type { Signal, SignalAction, SignalDetail, SignalKind } from '../../api/signals'
import { deleteExecutions } from '../../api/signals'
import SignalFilters from './SignalFilters.vue'
import SignalsTable from './SignalsTable.vue'
import BulkSelectionBar from './BulkSelectionBar.vue'
import SignalDetailSheet from './SignalDetailSheet.vue'
import ClearActionsDialog from './ClearActionsDialog.vue'

const route  = useRoute()
const router = useRouter()

const store      = useSignalsStore()
const rulesStore = useRulesStore()
const auth       = useAuthStore()
const toast      = useToast()

const errorMessage = (e: unknown) => (e instanceof Error ? e.message : String(e))
const plural = (n: number, word: string) => `${n} ${word}${n === 1 ? '' : 's'}`

// ── Filters ──────────────────────────────────────────────────────────────────

const filterKind   = ref<SignalKind | null>(null)
const filterRuleId = ref<number | null>(null)
const filterFrom   = ref<number | null>(null)
const filterTo     = ref<number | null>(null)
const perPage      = ref(20)

const effectivePerPage = () => perPage.value === 0 ? 999999 : perPage.value

const hasFilters = computed(() => filterKind.value != null || filterRuleId.value != null || filterFrom.value != null || filterTo.value != null)

function syncQueryToUrl() {
  const q: Record<string, string> = {}
  if (typeof route.query.tab === 'string') q.tab = route.query.tab
  if (filterKind.value != null)   q.kind    = filterKind.value
  if (filterRuleId.value != null) q.rule_id = String(filterRuleId.value)
  if (filterFrom.value != null)   q.from    = new Date(filterFrom.value).toISOString()
  if (filterTo.value != null)     q.to      = new Date(filterTo.value).toISOString()
  if (detail.value && showDetail.value) q.signal = String(detail.value.signal.id)
  router.replace({ query: q })
}

function fetchFiltered() {
  return store.fetchSignals({
    kind:     filterKind.value ?? undefined,
    rule_id:  filterRuleId.value ?? undefined,
    from:     filterFrom.value ? new Date(filterFrom.value).toISOString() : undefined,
    to:       filterTo.value   ? new Date(filterTo.value).toISOString()   : undefined,
    per_page: effectivePerPage(),
  })
}

async function onFilter() {
  selection.value = []
  await fetchFiltered()
  syncQueryToUrl()
}

function onReset() {
  filterKind.value   = null
  filterRuleId.value = null
  filterFrom.value   = null
  filterTo.value     = null
  perPage.value      = 20
  selection.value    = []
  store.fetchSignals({ per_page: 20 })
  syncQueryToUrl()
}

// Changing rows-per-page re-applies the filters from page 1.
watch(perPage, onFilter)

// ── Selection ────────────────────────────────────────────────────────────────

const selection   = ref<(string | number)[]>([])
const selectedIds = computed(() => selection.value as number[])

// ── Detail ───────────────────────────────────────────────────────────────────

const showDetail    = ref(false)
const detail        = ref<SignalDetail | null>(null)
const loadingDetail = ref(false)
const reprocessing  = ref(false)

async function openDetail(id: number) {
  loadingDetail.value = true
  showDetail.value = true
  try {
    detail.value = await store.getSignal(id)
    syncQueryToUrl()
  } catch (e: unknown) {
    toast.error(`Could not load signal: ${errorMessage(e)}`)
    showDetail.value = false
  } finally {
    loadingDetail.value = false
  }
}

// Remove signal param from URL when the Sheet closes
watch(showDetail, (val) => {
  if (!val) {
    const q = { ...route.query }
    delete q.signal
    router.replace({ query: q })
  }
})

async function reprocessOne(id: number) {
  reprocessing.value = true
  try {
    const { requeued } = await store.reprocess({ signal_ids: [id] })
    if (requeued) {
      toast.success('Signal queued for reprocessing')
      // Refresh detail after a brief delay so the worker has time to run, unless another signal is open by then
      setTimeout(async () => {
        try {
          const fresh = await store.getSignal(id)
          if (detail.value?.signal.id === id) detail.value = fresh
        } catch { /* the next open refetches it */ }
      }, 2000)
    } else {
      toast.info('Nothing to run: no active pipeline is linked to the new_signal trigger.')
    }
  } catch (e: unknown) {
    toast.error(`Could not reprocess signal: ${errorMessage(e)}`)
  } finally {
    reprocessing.value = false
  }
}

async function reprocessSelected() {
  reprocessing.value = true
  try {
    const { requeued } = await store.reprocess({ signal_ids: selectedIds.value })
    toast.success(`Queued ${plural(requeued, 'signal')} for reprocessing`)
  } catch (e: unknown) {
    toast.error(`Could not reprocess signals: ${errorMessage(e)}`)
  } finally {
    reprocessing.value = false
  }
}

// ── Delete signals ───────────────────────────────────────────────────────────
// One dialog serves a row, the open signal and the selection; deleteIds says which.

const deleteIds  = ref<number[]>([])
const showDelete = ref(false)
const deleting   = ref(false)

function askDelete(ids: number[]) {
  deleteIds.value = ids
  showDelete.value = true
}

async function onConfirmDelete() {
  const ids = deleteIds.value
  deleting.value = true
  try {
    const { deleted } = await store.remove(ids)
    if (detail.value && ids.includes(detail.value.signal.id)) showDetail.value = false
    selection.value = selection.value.filter(k => !ids.includes(k as number))
    toast.success(`Deleted ${plural(deleted, 'signal')}`)
    showDelete.value = false
  } catch (e: unknown) {
    toast.error(`Could not delete: ${errorMessage(e)}`)
  } finally {
    deleting.value = false
  }
}

const deleteTitle = computed(() => deleteIds.value.length === 1
  ? `Delete signal ${deleteIds.value[0]}?`
  : `Delete ${plural(deleteIds.value.length, 'signal')}?`)

// ── Delete noise ─────────────────────────────────────────────────────────────

const showNoise     = ref(false)
const deletingNoise = ref(false)

async function onConfirmNoise() {
  deletingNoise.value = true
  try {
    const { deleted } = await store.deleteNoise()
    selection.value = []
    toast.success(`Deleted ${plural(deleted, 'noise signal')}`)
    showNoise.value = false
  } catch (e: unknown) {
    toast.error(`Could not delete noise signals: ${errorMessage(e)}`)
  } finally {
    deletingNoise.value = false
  }
}

// ── Clear executions ─────────────────────────────────────────────────────────
// Scope: every action on the open signal, one action on it, or every action on the selection.

type ClearScope = { kind: 'signal' } | { kind: 'action'; group: SignalAction } | { kind: 'selection' }

const clearScope = ref<ClearScope | null>(null)
const showClear  = ref(false)
const clearing   = ref(false)

function askClear(scope: ClearScope) {
  clearScope.value = scope
  showClear.value = true
}

const clearTitle = computed(() => {
  const s = clearScope.value
  if (!s || !detail.value && s.kind !== 'selection') return 'Clear executions?'
  if (s.kind === 'selection') return `Clear all executions for ${plural(selectedIds.value.length, 'signal')}?`
  if (s.kind === 'action') {
    const name = s.group.action_name || `action #${s.group.action_id}`
    return `Clear ${plural(s.group.executions.length, 'run')} of ${name}?`
  }
  const runs = detail.value!.actions.reduce((n, g) => n + g.executions.length, 0)
  return `Clear ${plural(runs, 'run')} on this signal?`
})

async function onConfirmClear() {
  const s = clearScope.value
  if (!s) return
  clearing.value = true
  try {
    let deleted: number
    if (s.kind === 'selection') {
      ({ deleted } = await deleteExecutions(selectedIds.value))
      selection.value = []
    } else {
      const signalId = detail.value!.signal.id
      ;({ deleted } = await deleteExecutions([signalId], s.kind === 'action' ? [s.group.action_id] : undefined))
      detail.value = await store.getSignal(signalId)
    }
    toast.success(`Cleared ${plural(deleted, 'execution')}`)
    showClear.value = false
  } catch (e: unknown) {
    toast.error(`Could not clear executions: ${errorMessage(e)}`)
  } finally {
    clearing.value = false
  }
}

const showClearForActions = ref(false)

async function onClearForActions(actionIds: number[]) {
  clearing.value = true
  try {
    const { deleted } = await deleteExecutions(selectedIds.value, actionIds)
    selection.value = []
    showClearForActions.value = false
    toast.success(`Cleared ${plural(deleted, 'execution')}`)
  } catch (e: unknown) {
    toast.error(`Could not clear executions: ${errorMessage(e)}`)
  } finally {
    clearing.value = false
  }
}

// ── Load ─────────────────────────────────────────────────────────────────────

onMounted(async () => {
  rulesStore.fetchRules()

  // Restore filters from URL
  const { kind, rule_id, from, to, signal } = route.query
  if (kind === 'post' || kind === 'article') filterKind.value = kind
  if (rule_id) filterRuleId.value = Number(rule_id)
  if (from)    filterFrom.value   = new Date(String(from)).getTime()
  if (to)      filterTo.value     = new Date(String(to)).getTime()

  await fetchFiltered()

  if (signal && Number.isInteger(Number(signal))) await openDetail(Number(signal))
})
</script>

<template>
  <div class="flex flex-col gap-3">
    <SignalFilters
      v-model:kind="filterKind"
      v-model:rule-id="filterRuleId"
      v-model:from="filterFrom"
      v-model:to="filterTo"
      @apply="onFilter"
      @reset="onReset"
      @delete-noise="showNoise = true"
    />

    <BulkSelectionBar
      v-if="auth.isAdmin && selection.length"
      :count="selection.length"
      :reprocessing="reprocessing"
      @reprocess="reprocessSelected"
      @delete="askDelete(selectedIds)"
      @clear-all="askClear({ kind: 'selection' })"
      @clear-for-actions="showClearForActions = true"
      @deselect="selection = []"
    />

    <SignalsTable
      v-model:per-page="perPage"
      v-model:selection="selection"
      @open="openDetail"
      @delete="(row: Signal) => askDelete([row.id])"
      @retry="fetchFiltered"
    >
      <template #empty>
        <div v-if="hasFilters" class="flex flex-col items-center gap-3 py-8 text-center">
          <p class="m-0 text-muted-foreground">No signals match these filters.</p>
          <Button variant="outline" @click="onReset">Reset filters</Button>
        </div>
        <p v-else class="m-0 py-8 text-center text-muted-foreground">
          No signals yet. They appear here as the stream or a search matches your rules.
        </p>
      </template>
    </SignalsTable>
  </div>

  <SignalDetailSheet
    v-model:open="showDetail"
    :detail="detail"
    :loading="loadingDetail"
    :reprocessing="reprocessing"
    @reprocess="reprocessOne"
    @delete="(id: number) => askDelete([id])"
    @clear-all="askClear({ kind: 'signal' })"
    @clear-action="(group: SignalAction) => askClear({ kind: 'action', group })"
  />

  <template v-if="auth.isAdmin">
    <AlertDialog
      v-model:open="showDelete"
      :title="deleteTitle"
      :description="`The ${deleteIds.length === 1 ? 'signal and its executions, rule matches and entity tracks are' : 'signals and their executions, rule matches and entity tracks are'} removed. This can't be undone.`"
      :action-label="deleteIds.length === 1 ? 'Delete signal' : `Delete ${plural(deleteIds.length, 'signal')}`"
      :loading="deleting"
      @confirm="onConfirmDelete"
    />

    <AlertDialog
      v-model:open="showNoise"
      title="Delete every noise signal?"
      description="Deletes all stored X signals that match no rule, or only local rules, across every page, not just this one. This can't be undone."
      action-label="Delete noise signals"
      :loading="deletingNoise"
      @confirm="onConfirmNoise"
    />

    <AlertDialog
      v-model:open="showClear"
      :title="clearTitle"
      description="The execution history and saved output are removed. The signal is kept and can be reprocessed. This can't be undone."
      action-label="Clear executions"
      :loading="clearing"
      @confirm="onConfirmClear"
    />

    <ClearActionsDialog
      v-model:open="showClearForActions"
      :signal-count="selection.length"
      :clearing="clearing"
      @confirm="onClearForActions"
    />
  </template>
</template>
