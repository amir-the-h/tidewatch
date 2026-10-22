<!-- Tweet list: filters, server-paged table, bulk actions on the selection, and the detail Sheet.
     Filters and the open tweet are mirrored to the URL (?rule_id, from, to, tweet). -->
<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { AlertDialog, Button } from '@/components/ui'
import { useToast } from '@/composables/useToast'
import { useTweetsStore } from '../../stores/tweets'
import { useRulesStore } from '../../stores/rules'
import { useAuthStore } from '../../stores/auth'
import type { Tweet, TweetAction, TweetDetail } from '../../api/tweets'
import { deleteExecutions } from '../../api/tweets'
import TweetFilters from './TweetFilters.vue'
import TweetsTable from './TweetsTable.vue'
import BulkSelectionBar from './BulkSelectionBar.vue'
import TweetDetailSheet from './TweetDetailSheet.vue'
import ClearActionsDialog from './ClearActionsDialog.vue'

const route  = useRoute()
const router = useRouter()

const store      = useTweetsStore()
const rulesStore = useRulesStore()
const auth       = useAuthStore()
const toast      = useToast()

const errorMessage = (e: unknown) => (e instanceof Error ? e.message : String(e))
const plural = (n: number, word: string) => `${n} ${word}${n === 1 ? '' : 's'}`

// ── Filters ──────────────────────────────────────────────────────────────────

const filterRuleId = ref<number | null>(null)
const filterFrom   = ref<number | null>(null)
const filterTo     = ref<number | null>(null)
const perPage      = ref(20)

const effectivePerPage = () => perPage.value === 0 ? 999999 : perPage.value

const hasFilters = computed(() => filterRuleId.value != null || filterFrom.value != null || filterTo.value != null)

function syncQueryToUrl() {
  const q: Record<string, string> = {}
  if (typeof route.query.tab === 'string') q.tab = route.query.tab
  if (filterRuleId.value != null) q.rule_id = String(filterRuleId.value)
  if (filterFrom.value != null)   q.from    = new Date(filterFrom.value).toISOString()
  if (filterTo.value != null)     q.to      = new Date(filterTo.value).toISOString()
  if (detail.value && showDetail.value) q.tweet = detail.value.tweet.id
  router.replace({ query: q })
}

function fetchFiltered() {
  return store.fetchTweets({
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
  filterRuleId.value = null
  filterFrom.value   = null
  filterTo.value     = null
  perPage.value      = 20
  selection.value    = []
  store.fetchTweets({ per_page: 20 })
  syncQueryToUrl()
}

// Changing rows-per-page re-applies the filters from page 1.
watch(perPage, onFilter)

// ── Selection ────────────────────────────────────────────────────────────────

const selection   = ref<(string | number)[]>([])
const selectedIds = computed(() => selection.value as string[])

// ── Detail ───────────────────────────────────────────────────────────────────

const showDetail    = ref(false)
const detail        = ref<TweetDetail | null>(null)
const loadingDetail = ref(false)
const reprocessing  = ref(false)

async function openDetail(id: string) {
  loadingDetail.value = true
  showDetail.value = true
  try {
    detail.value = await store.getTweet(id)
    syncQueryToUrl()
  } catch (e: unknown) {
    toast.error(`Could not load tweet: ${errorMessage(e)}`)
    showDetail.value = false
  } finally {
    loadingDetail.value = false
  }
}

// Remove tweet param from URL when the Sheet closes
watch(showDetail, (val) => {
  if (!val) {
    const q = { ...route.query }
    delete q.tweet
    router.replace({ query: q })
  }
})

async function reprocessOne(id: string) {
  reprocessing.value = true
  try {
    const { requeued } = await store.reprocess({ tweet_ids: [id] })
    if (requeued) {
      toast.success('Tweet queued for reprocessing')
      // Refresh detail after a brief delay so the worker has time to run
      setTimeout(async () => { detail.value = await store.getTweet(id) }, 2000)
    } else {
      toast.info('Nothing to run: no active action has the new_tweet trigger.')
    }
  } catch (e: unknown) {
    toast.error(`Could not reprocess tweet: ${errorMessage(e)}`)
  } finally {
    reprocessing.value = false
  }
}

async function reprocessSelected() {
  reprocessing.value = true
  try {
    const { requeued } = await store.reprocess({ tweet_ids: selectedIds.value })
    toast.success(`Queued ${plural(requeued, 'tweet')} for reprocessing`)
  } catch (e: unknown) {
    toast.error(`Could not reprocess tweets: ${errorMessage(e)}`)
  } finally {
    reprocessing.value = false
  }
}

// ── Delete tweets ────────────────────────────────────────────────────────────
// One dialog serves a row, the open tweet and the selection; deleteIds says which.

const deleteIds  = ref<string[]>([])
const showDelete = ref(false)
const deleting   = ref(false)

function askDelete(ids: string[]) {
  deleteIds.value = ids
  showDelete.value = true
}

async function onConfirmDelete() {
  const ids = deleteIds.value
  deleting.value = true
  try {
    const { deleted } = await store.remove(ids)
    if (detail.value && ids.includes(detail.value.tweet.id)) showDetail.value = false
    selection.value = selection.value.filter(k => !ids.includes(k as string))
    toast.success(`Deleted ${plural(deleted, 'tweet')}`)
    showDelete.value = false
  } catch (e: unknown) {
    toast.error(`Could not delete: ${errorMessage(e)}`)
  } finally {
    deleting.value = false
  }
}

const deleteTitle = computed(() => deleteIds.value.length === 1
  ? `Delete tweet ${deleteIds.value[0]}?`
  : `Delete ${plural(deleteIds.value.length, 'tweet')}?`)

// ── Delete noise ─────────────────────────────────────────────────────────────

const showNoise     = ref(false)
const deletingNoise = ref(false)

async function onConfirmNoise() {
  deletingNoise.value = true
  try {
    const { deleted } = await store.deleteNoise()
    selection.value = []
    toast.success(`Deleted ${plural(deleted, 'noise tweet')}`)
    showNoise.value = false
  } catch (e: unknown) {
    toast.error(`Could not delete noise tweets: ${errorMessage(e)}`)
  } finally {
    deletingNoise.value = false
  }
}

// ── Clear executions ─────────────────────────────────────────────────────────
// Scope: every action on the open tweet, one action on it, or every action on the selection.

type ClearScope = { kind: 'tweet' } | { kind: 'action'; group: TweetAction } | { kind: 'selection' }

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
  if (s.kind === 'selection') return `Clear all executions for ${plural(selectedIds.value.length, 'tweet')}?`
  if (s.kind === 'action') {
    const name = s.group.action_name || `action #${s.group.action_id}`
    return `Clear ${plural(s.group.executions.length, 'run')} of ${name}?`
  }
  const runs = detail.value!.actions.reduce((n, g) => n + g.executions.length, 0)
  return `Clear ${plural(runs, 'run')} on this tweet?`
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
      const tweetId = detail.value!.tweet.id
      ;({ deleted } = await deleteExecutions([tweetId], s.kind === 'action' ? [s.group.action_id] : undefined))
      detail.value = await store.getTweet(tweetId)
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
  const { rule_id, from, to, tweet } = route.query
  if (rule_id) filterRuleId.value = Number(rule_id)
  if (from)    filterFrom.value   = new Date(String(from)).getTime()
  if (to)      filterTo.value     = new Date(String(to)).getTime()

  await fetchFiltered()

  if (tweet) await openDetail(String(tweet))
})
</script>

<template>
  <div class="flex flex-col gap-3">
    <TweetFilters
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

    <TweetsTable
      v-model:per-page="perPage"
      v-model:selection="selection"
      @open="openDetail"
      @delete="(row: Tweet) => askDelete([row.id])"
      @retry="fetchFiltered"
    >
      <template #empty>
        <div v-if="hasFilters" class="flex flex-col items-center gap-3 py-8 text-center">
          <p class="m-0 text-muted-foreground">No tweets match these filters.</p>
          <Button variant="outline" @click="onReset">Reset filters</Button>
        </div>
        <p v-else class="m-0 py-8 text-center text-muted-foreground">
          No tweets yet. They appear here as the stream or a search matches your rules.
        </p>
      </template>
    </TweetsTable>
  </div>

  <TweetDetailSheet
    v-model:open="showDetail"
    :detail="detail"
    :loading="loadingDetail"
    :reprocessing="reprocessing"
    @reprocess="reprocessOne"
    @delete="(id: string) => askDelete([id])"
    @clear-all="askClear({ kind: 'tweet' })"
    @clear-action="(group: TweetAction) => askClear({ kind: 'action', group })"
  />

  <template v-if="auth.isAdmin">
    <AlertDialog
      v-model:open="showDelete"
      :title="deleteTitle"
      :description="`The ${deleteIds.length === 1 ? 'tweet and its rule matches are' : 'tweets and their rule matches are'} removed. This can't be undone.`"
      :action-label="deleteIds.length === 1 ? 'Delete tweet' : `Delete ${plural(deleteIds.length, 'tweet')}`"
      :loading="deleting"
      @confirm="onConfirmDelete"
    />

    <AlertDialog
      v-model:open="showNoise"
      title="Delete every noise tweet?"
      description="Deletes all stored tweets that match no rule, or only local rules, across every page, not just this one. This can't be undone."
      action-label="Delete noise tweets"
      :loading="deletingNoise"
      @confirm="onConfirmNoise"
    />

    <AlertDialog
      v-model:open="showClear"
      :title="clearTitle"
      description="The execution history and saved output are removed. The tweet is kept and can be reprocessed. This can't be undone."
      action-label="Clear executions"
      :loading="clearing"
      @confirm="onConfirmClear"
    />

    <ClearActionsDialog
      v-model:open="showClearForActions"
      :tweet-count="selection.length"
      :clearing="clearing"
      @confirm="onClearForActions"
    />
  </template>
</template>
