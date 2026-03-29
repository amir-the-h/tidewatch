<!-- web/src/views/TweetsView.vue -->
<script setup lang="ts">
import { h, onMounted, ref, computed, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useTweetsStore } from '../stores/tweets'
import { useRulesStore } from '../stores/rules'
import { useMessage, NButton, NSpace } from 'naive-ui'
import type { DataTableColumns, DataTableRowKey } from 'naive-ui'
import type { Tweet, TweetDetail } from '../api/tweets'
import { deleteExecutions } from '../api/tweets'
import { listActions } from '../api/actions'
import type { Action } from '../api/actions'
import { formatDateTime } from '../utils/format'
import { EyeOutline, TrashOutline, TrashBinOutline, FunnelOutline, CloseCircleOutline, RefreshOutline, OpenOutline, ChatbubblesOutline } from '@vicons/ionicons5'
import { iconBtn } from '../utils/renderActions'
import { useChatStore } from '../stores/chat'
import { estimateTokens } from '../api/chat'

const route  = useRoute()
const router = useRouter()

const store      = useTweetsStore()
const rulesStore = useRulesStore()
const chatStore  = useChatStore()
const msg        = useMessage()

const filterRuleId = ref<number | null>(null)
const filterFrom   = ref<number | null>(null)
const filterTo     = ref<number | null>(null)
const perPageOption = ref<number>(20)

const perPageOptions = [
  { label: '20',  value: 20  },
  { label: '50',  value: 50  },
  { label: '100', value: 100 },
  { label: 'All', value: 0   },
]

function effectivePerPage() {
  return perPageOption.value === 0 ? 999999 : perPageOption.value
}

const showDetail    = ref(false)
const detail        = ref<TweetDetail | null>(null)
const loadingDetail = ref(false)
const deleting      = ref(false)
const reprocessing  = ref(false)
const deletingNoise = ref(false)
const clearingExecutions = ref(false)
const clearingBulk           = ref(false)
const showSelectActionsModal  = ref(false)
const allActions              = ref<Action[]>([])
const selectedActionIds       = ref<number[]>([])
const loadingActions          = ref(false)

// Format a single metadata value — handles nested arrays and nulls
function fmtVal(v: unknown): string {
  if (v === null || v === undefined) return '—'
  if (Array.isArray(v)) return v.length ? v.map(x => fmtVal(x)).join(', ') : '—'
  return String(v)
}

// Return array items that are worth displaying (filter empty nested arrays / nulls)
function cleanArray(arr: unknown[]): unknown[] {
  return arr.filter(x => x !== null && x !== undefined && !(Array.isArray(x) && x.length === 0) && x !== '')
}

const checkedRowKeys    = ref<DataTableRowKey[]>([])
const hasSelection      = computed(() => checkedRowKeys.value.length > 0)
const estimatingForChat = ref(false)

function syncQueryToUrl() {
  const q: Record<string, string> = {}
  if (filterRuleId.value != null) q.rule_id = String(filterRuleId.value)
  if (filterFrom.value != null)   q.from    = new Date(filterFrom.value).toISOString()
  if (filterTo.value != null)     q.to      = new Date(filterTo.value).toISOString()
  if (detail.value && showDetail.value) q.tweet = detail.value.tweet.id
  router.replace({ query: q })
}

onMounted(async () => {
  rulesStore.fetchRules()

  // Restore filters from URL
  const { rule_id, from, to, tweet } = route.query
  if (rule_id) filterRuleId.value = Number(rule_id)
  if (from)    filterFrom.value   = new Date(String(from)).getTime()
  if (to)      filterTo.value     = new Date(String(to)).getTime()

  await store.fetchTweets({
    rule_id:  filterRuleId.value ?? undefined,
    from:     filterFrom.value ? new Date(filterFrom.value).toISOString() : undefined,
    to:       filterTo.value   ? new Date(filterTo.value).toISOString()   : undefined,
    per_page: effectivePerPage(),
  })

  // Open drawer if tweet param is present
  if (tweet) {
    await openDetail(String(tweet))
  }
})

// Remove tweet param from URL when drawer is closed
watch(showDetail, (val) => {
  if (!val) {
    const q = { ...route.query }
    delete q.tweet
    router.replace({ query: q })
  }
})

async function onFilter() {
  checkedRowKeys.value = []
  await store.fetchTweets({
    rule_id:  filterRuleId.value ?? undefined,
    from:     filterFrom.value ? new Date(filterFrom.value).toISOString() : undefined,
    to:       filterTo.value   ? new Date(filterTo.value).toISOString()   : undefined,
    per_page: effectivePerPage(),
  })
  syncQueryToUrl()
}

async function openDetail(id: string) {
  loadingDetail.value = true
  showDetail.value = true
  try {
    detail.value = await store.getTweet(id)
    syncQueryToUrl()
  }
  catch (e: any) { msg.error(e.message); showDetail.value = false }
  finally { loadingDetail.value = false }
}

async function deleteOne(id: string) {
  deleting.value = true
  try {
    await store.remove([id])
    if (detail.value?.tweet.id === id) showDetail.value = false
    msg.success('Deleted')
  } catch (e: any) {
    msg.error(e.message)
  } finally {
    deleting.value = false
  }
}

async function deleteSelected() {
  const ids = checkedRowKeys.value as string[]
  deleting.value = true
  try {
    const { deleted } = await store.remove(ids)
    checkedRowKeys.value = []
    msg.success(`Deleted ${deleted} tweet${deleted !== 1 ? 's' : ''}`)
  } catch (e: any) {
    msg.error(e.message)
  } finally {
    deleting.value = false
  }
}

async function reprocessOne(id: string) {
  reprocessing.value = true
  try {
    const { requeued } = await store.reprocess({ tweet_ids: [id] })
    if (requeued) {
      msg.success('Queued for reprocessing')
      // Refresh detail after a brief delay so the worker has time to run
      setTimeout(async () => {
        detail.value = await store.getTweet(id)
      }, 2000)
    } else {
      msg.warning('No active new-tweet actions — configure an action with trigger type "new_tweet" first')
    }
  } catch (e: any) {
    msg.error(e.message)
  } finally {
    reprocessing.value = false
  }
}

async function clearAllExecutions(tweetId: string) {
  clearingExecutions.value = true
  try {
    const { deleted } = await deleteExecutions([tweetId])
    msg.success(`Cleared ${deleted} execution${deleted !== 1 ? 's' : ''}`)
    detail.value = await store.getTweet(tweetId)
  } catch (e: any) {
    msg.error(e.message)
  } finally {
    clearingExecutions.value = false
  }
}

async function clearActionExecutions(tweetId: string, actionId: number) {
  clearingExecutions.value = true
  try {
    const { deleted } = await deleteExecutions([tweetId], [actionId])
    msg.success(`Cleared ${deleted} execution${deleted !== 1 ? 's' : ''}`)
    detail.value = await store.getTweet(tweetId)
  } catch (e: any) {
    msg.error(e.message)
  } finally {
    clearingExecutions.value = false
  }
}

async function bulkClearAll() {
  const ids = checkedRowKeys.value as string[]
  clearingBulk.value = true
  try {
    const { deleted } = await deleteExecutions(ids)
    checkedRowKeys.value = []
    msg.success(`Cleared ${deleted} execution${deleted !== 1 ? 's' : ''}`)
  } catch (e: any) {
    msg.error(e.message)
  } finally {
    clearingBulk.value = false
  }
}

async function openSelectActionsModal() {
  showSelectActionsModal.value = true
  loadingActions.value = true
  try {
    allActions.value = await listActions()
  } catch (e: any) {
    msg.error('Failed to load actions: ' + e.message)
  } finally {
    loadingActions.value = false
  }
}

async function bulkClearSelected() {
  if (!selectedActionIds.value.length) {
    msg.warning('Select at least one action')
    return
  }
  const ids = checkedRowKeys.value as string[]
  clearingBulk.value = true
  try {
    const { deleted } = await deleteExecutions(ids, selectedActionIds.value)
    checkedRowKeys.value = []
    selectedActionIds.value = []
    showSelectActionsModal.value = false
    msg.success(`Cleared ${deleted} execution${deleted !== 1 ? 's' : ''}`)
  } catch (e: any) {
    msg.error(e.message)
  } finally {
    clearingBulk.value = false
  }
}

async function deleteNoise() {
  deletingNoise.value = true
  try {
    const { deleted } = await store.deleteNoise()
    checkedRowKeys.value = []
    msg.success(`Deleted ${deleted} noise tweet${deleted !== 1 ? 's' : ''}`)
  } catch (e: any) {
    msg.error(e.message)
  } finally {
    deletingNoise.value = false
  }
}

async function reprocessSelected() {
  const ids = checkedRowKeys.value as string[]
  reprocessing.value = true
  try {
    const { requeued } = await store.reprocess({ tweet_ids: ids })
    msg.success(`Queued ${requeued} tweet${requeued !== 1 ? 's' : ''} for reprocessing`)
  } catch (e: any) {
    msg.error(e.message)
  } finally {
    reprocessing.value = false
  }
}

async function chatWithSelected() {
  const ids = checkedRowKeys.value as string[]
  estimatingForChat.value = true
  try {
    const est = await estimateTokens(ids)
    if (!est.ok) {
      msg.error(`Too many tokens: ${est.tokens.toLocaleString()} / ${est.limit.toLocaleString()} — select fewer tweets.`)
      return
    }
    chatStore.initSession(ids)
    chatStore.estimate = est
    router.push('/chat')
  } catch (e: any) {
    msg.error('Token estimate failed: ' + e.message)
  } finally {
    estimatingForChat.value = false
  }
}

const columns: DataTableColumns<Tweet> = [
  { type: 'selection', multiple: true },
  { title: 'ID',      key: 'id', width: 160, ellipsis: true },
  { title: 'Text',    key: 'text', ellipsis: true },
  {
    title: 'Author', key: 'author_handle', width: 160,
    render: (row) => row.author_handle ? `@${row.author_handle}` : row.author_id,
  },
  { title: 'Created', key: 'created_at', width: 200, render: (row) => formatDateTime(row.created_at) },
  {
    title: '', key: 'actions', width: 120,
    render: (row) => h(NSpace, { size: 4 }, {
      default: () => [
        iconBtn(EyeOutline,   'View detail', () => openDetail(row.id)),
        iconBtn(TrashOutline, 'Delete',      () => deleteOne(row.id), { type: 'error' }),
      ]
    })
  },
]
</script>

<template>
  <n-space vertical size="large">
    <n-h2>Tweets</n-h2>

    <!-- Filters -->
    <n-space wrap>
      <n-select
        v-model:value="filterRuleId"
        :options="rulesStore.rules.filter(r => !r.local).map(r => ({ label: r.name, value: r.id }))"
        placeholder="Filter by rule"
        clearable
        style="width: 200px"
      />
      <n-date-picker v-model:value="filterFrom" type="datetime" placeholder="From" clearable style="max-width: 220px" />
      <n-date-picker v-model:value="filterTo"   type="datetime" placeholder="To"   clearable style="max-width: 220px" />
      <n-select
        v-model:value="perPageOption"
        :options="perPageOptions"
        style="width: 90px"
        @update:value="onFilter"
      />
      <n-tooltip trigger="hover">
        <template #trigger>
          <n-button @click="onFilter">
            <template #icon><n-icon><FunnelOutline /></n-icon></template>
          </n-button>
        </template>
        Apply filters
      </n-tooltip>
      <n-tooltip trigger="hover">
        <template #trigger>
          <n-button secondary @click="filterRuleId.value = null; filterFrom.value = null; filterTo.value = null; perPageOption.value = 20; store.fetchTweets({ per_page: 20 }); checkedRowKeys.value = []; router.replace({ query: {} })">
            <template #icon><n-icon><CloseCircleOutline /></n-icon></template>
          </n-button>
        </template>
        Reset filters
      </n-tooltip>
      <n-popconfirm @positive-click="deleteNoise">
        <template #trigger>
          <n-tooltip trigger="hover">
            <template #trigger>
              <n-button type="warning" :loading="deletingNoise">
                <template #icon><n-icon><TrashBinOutline /></n-icon></template>
              </n-button>
            </template>
            Delete noise tweets
          </n-tooltip>
        </template>
        Delete all tweets with no rules or only local rules?
      </n-popconfirm>
    </n-space>

    <!-- Bulk action toolbar -->
    <n-space v-if="hasSelection" align="center">
      <span>{{ checkedRowKeys.length }} selected</span>
      <n-tooltip trigger="hover">
        <template #trigger>
          <n-button type="primary" size="small" :loading="reprocessing" @click="reprocessSelected">
            <template #icon><n-icon><RefreshOutline /></n-icon></template>
          </n-button>
        </template>
        Reprocess selected
      </n-tooltip>
      <n-tooltip trigger="hover">
        <template #trigger>
          <n-button type="error" size="small" :loading="deleting" @click="deleteSelected">
            <template #icon><n-icon><TrashOutline /></n-icon></template>
          </n-button>
        </template>
        Delete selected
      </n-tooltip>
      <n-tooltip trigger="hover">
        <template #trigger>
          <n-button size="small" :loading="estimatingForChat" @click="chatWithSelected">
            <template #icon><n-icon><ChatbubblesOutline /></n-icon></template>
          </n-button>
        </template>
        Chat with selected tweets
      </n-tooltip>
      <n-button-group size="small">
        <n-popconfirm @positive-click="bulkClearAll">
          <template #trigger>
            <n-tooltip trigger="hover">
              <template #trigger>
                <n-button size="small" :loading="clearingBulk">
                  <template #icon><n-icon><TrashBinOutline /></n-icon></template>
                  Clear All
                </n-button>
              </template>
              Clear all executions for selected tweets
            </n-tooltip>
          </template>
          Clear all execution history for {{ checkedRowKeys.length }} tweet{{ checkedRowKeys.length !== 1 ? 's' : '' }}?
        </n-popconfirm>
        <n-tooltip trigger="hover">
          <template #trigger>
            <n-button size="small" @click="openSelectActionsModal">
              Select Actions…
            </n-button>
          </template>
          Clear executions for specific actions
        </n-tooltip>
      </n-button-group>
    </n-space>

    <n-data-table
      :columns="columns"
      :data="store.tweets"
      :loading="store.loading"
      :scroll-x="760"
      :pagination="{
        page: store.page,
        pageSize: effectivePerPage(),
        itemCount: store.total,
        onChange: (p: number) => store.goToPage(p, effectivePerPage()),
      }"
      :remote="true"
      :row-key="(row: Tweet) => row.id"
      v-model:checked-row-keys="checkedRowKeys"
    />

    <!-- Detail drawer -->
    <n-drawer v-model:show="showDetail" :width="'min(540px, calc(100vw - 16px))'">
      <n-drawer-content title="Tweet Detail" closable>
        <n-spin v-if="loadingDetail" />
        <div v-else-if="detail">
          <n-descriptions :column="1" bordered>
            <n-descriptions-item label="ID">{{ detail.tweet.id }}</n-descriptions-item>
            <n-descriptions-item label="Author">
              <a
                v-if="detail.tweet.author_handle"
                :href="`https://x.com/${detail.tweet.author_handle}`"
                target="_blank"
                rel="noopener noreferrer"
              >@{{ detail.tweet.author_handle }}</a>
              <span v-else>{{ detail.tweet.author_id }}</span>
            </n-descriptions-item>
            <n-descriptions-item label="Created">{{ formatDateTime(detail.tweet.created_at) }}</n-descriptions-item>
            <n-descriptions-item label="Text">{{ detail.tweet.text }}</n-descriptions-item>
          </n-descriptions>

          <n-h3 style="margin-top: 16px">Matched Rules</n-h3>
          <n-empty v-if="!detail.tweet.rules?.length" description="None" />
          <n-space v-else wrap>
            <n-tag
              v-for="rule in detail.tweet.rules"
              :key="rule.id"
              size="small"
              :type="rule.active ? 'info' : 'default'"
            >
              {{ rule.name }}
            </n-tag>
          </n-space>

          <!-- Pipeline Executions — grouped by action -->
          <template v-if="detail.actions?.length">
            <div style="display: flex; align-items: center; gap: 8px; margin-top: 16px">
              <n-h3 style="margin: 0">Pipeline Runs</n-h3>
              <n-popconfirm @positive-click="clearAllExecutions(detail.tweet.id)">
                <template #trigger>
                  <n-button size="tiny" type="error" secondary :loading="clearingExecutions">
                    Clear All
                  </n-button>
                </template>
                Clear all execution history for this tweet?
              </n-popconfirm>
            </div>
            <div v-for="group in detail.actions" :key="group.action_id" class="action-group">
              <!-- Action header -->
              <div class="action-group-header">
                <span class="action-group-name">{{ group.action_name || `action #${group.action_id}` }}</span>
                <span class="action-group-count">{{ group.executions.length }} run{{ group.executions.length !== 1 ? 's' : '' }}</span>
                <n-popconfirm @positive-click="clearActionExecutions(detail.tweet.id, group.action_id)" style="margin-left: auto">
                  <template #trigger>
                    <n-button size="tiny" type="error" secondary :loading="clearingExecutions">
                      Clear
                    </n-button>
                  </template>
                  Clear execution history for {{ group.action_name || `action #${group.action_id}` }}?
                </n-popconfirm>
              </div>

              <!-- Individual runs for this action -->
              <div
                v-for="exec in group.executions"
                :key="exec.id"
                class="exec-run"
              >
                <!-- Run timestamp + status -->
                <div class="exec-run-header">
                  <n-tag size="small" :type="exec.status === 'success' ? 'success' : 'error'" :bordered="false">
                    {{ exec.status }}
                  </n-tag>
                  <span style="font-size:11px; color: var(--n-text-color-disabled)">{{ formatDateTime(exec.executed_at) }}</span>
                </div>

                <!-- Error state -->
                <span v-if="exec.status === 'error'" style="color: var(--n-error-color); font-size: 12px">{{ exec.error }}</span>

                <!-- No metadata -->
                <span v-else-if="!exec.metadata || !Object.keys(exec.metadata).length" style="color: var(--n-text-color-disabled); font-size: 12px">(no output)</span>

                <!-- Structured extractor output -->
                <template v-else-if="'casualties' in exec.metadata || 'attackers' in exec.metadata || 'locations' in exec.metadata">
                  <div class="exec-grid">
                    <template v-if="cleanArray(exec.metadata.locations ?? []).length">
                      <span class="exec-label">Locations</span>
                      <n-space wrap size="small">
                        <n-tag v-for="(loc, i) in cleanArray(exec.metadata.locations)" :key="i" size="small" type="info">{{ fmtVal(loc) }}</n-tag>
                      </n-space>
                    </template>
                    <template v-if="cleanArray(exec.metadata.attackers ?? []).length">
                      <span class="exec-label">Attackers</span>
                      <n-space wrap size="small">
                        <n-tag v-for="(a, i) in cleanArray(exec.metadata.attackers)" :key="i" size="small" type="error">{{ fmtVal(a) }}</n-tag>
                      </n-space>
                    </template>
                    <template v-if="cleanArray(exec.metadata.defenders ?? []).length">
                      <span class="exec-label">Defenders</span>
                      <n-space wrap size="small">
                        <n-tag v-for="(d, i) in cleanArray(exec.metadata.defenders)" :key="i" size="small" type="warning">{{ fmtVal(d) }}</n-tag>
                      </n-space>
                    </template>
                    <template v-if="cleanArray(exec.metadata.targets ?? []).length">
                      <span class="exec-label">Targets</span>
                      <n-space wrap size="small">
                        <n-tag v-for="(t, i) in cleanArray(exec.metadata.targets)" :key="i" size="small">{{ fmtVal(t) }}</n-tag>
                      </n-space>
                    </template>
                    <template v-if="cleanArray(exec.metadata.attack_types ?? []).length">
                      <span class="exec-label">Attack types</span>
                      <n-space wrap size="small">
                        <n-tag v-for="(at, i) in cleanArray(exec.metadata.attack_types)" :key="i" size="small" type="warning">{{ fmtVal(at) }}</n-tag>
                      </n-space>
                    </template>
                    <template v-if="cleanArray(exec.metadata.weapons ?? []).length">
                      <span class="exec-label">Weapons</span>
                      <n-space wrap size="small">
                        <n-tag v-for="(w, i) in cleanArray(exec.metadata.weapons)" :key="i" size="small">{{ fmtVal(w) }}</n-tag>
                      </n-space>
                    </template>
                    <template v-if="cleanArray(exec.metadata.coordinates ?? []).length">
                      <span class="exec-label">Coordinates</span>
                      <n-space wrap size="small">
                        <n-tag v-for="(c, i) in cleanArray(exec.metadata.coordinates)" :key="i" size="small" type="info">{{ fmtVal(c) }}</n-tag>
                      </n-space>
                    </template>
                    <!-- Boolean flags -->
                    <template v-if="exec.metadata.casualties || exec.metadata.confirmed || exec.metadata.interception || exec.metadata.interception_partial || exec.metadata.air_defense_active || exec.metadata.damage_reported || exec.metadata.alert || exec.metadata.nuclear || exec.metadata.escalation">
                      <span class="exec-label">Flags</span>
                      <n-space wrap size="small">
                        <n-tag v-if="exec.metadata.casualties" size="small" type="error">casualties</n-tag>
                        <n-tag v-if="exec.metadata.confirmed" size="small" type="success">confirmed</n-tag>
                        <n-tag v-if="exec.metadata.interception" size="small" type="info">interception</n-tag>
                        <n-tag v-if="exec.metadata.interception_partial" size="small" type="info">interception (partial)</n-tag>
                        <n-tag v-if="exec.metadata.air_defense_active" size="small" type="warning">air defense active</n-tag>
                        <n-tag v-if="exec.metadata.damage_reported" size="small">damage reported</n-tag>
                        <n-tag v-if="exec.metadata.alert" size="small" type="error">alert</n-tag>
                        <n-tag v-if="exec.metadata.nuclear" size="small" type="error">nuclear</n-tag>
                        <n-tag v-if="exec.metadata.escalation" size="small" type="warning">escalation</n-tag>
                      </n-space>
                    </template>
                    <template v-if="exec.metadata.casualty_count != null">
                      <span class="exec-label">Casualty count</span>
                      <span style="font-size:13px">{{ exec.metadata.casualty_count }}</span>
                    </template>
                  </div>
                </template>

                <!-- Generic key-value metadata fallback -->
                <div v-else class="exec-grid">
                  <template v-for="(val, key) in exec.metadata" :key="key">
                    <span class="exec-label">{{ key }}</span>
                    <span v-if="!Array.isArray(val)" style="font-size:13px; word-break:break-word">{{ val === null ? '—' : String(val) }}</span>
                    <n-space v-else-if="cleanArray(val).length" wrap size="small">
                      <n-tag v-for="(item, i) in cleanArray(val)" :key="i" size="small">{{ fmtVal(item) }}</n-tag>
                    </n-space>
                    <span v-else style="font-size:13px; color: var(--n-text-color-disabled)">—</span>
                  </template>
                </div>
              </div>
            </div>
          </template>

          <div style="margin-top: 16px">
            <n-space size="small">
              <n-tooltip trigger="hover">
                <template #trigger>
                  <n-button
                    tag="a"
                    :href="`https://x.com/i/web/status/${detail.tweet.id}`"
                    target="_blank"
                    rel="noopener noreferrer"
                    size="small"
                  >
                    <template #icon><n-icon><OpenOutline /></n-icon></template>
                  </n-button>
                </template>
                Open on X
              </n-tooltip>
              <n-tooltip trigger="hover">
                <template #trigger>
                  <n-button type="primary" size="small" :loading="reprocessing" @click="reprocessOne(detail.tweet.id)">
                    <template #icon><n-icon><RefreshOutline /></n-icon></template>
                  </n-button>
                </template>
                Reprocess tweet
              </n-tooltip>
              <n-tooltip trigger="hover">
                <template #trigger>
                  <n-button type="error" size="small" :loading="deleting" @click="deleteOne(detail.tweet.id)">
                    <template #icon><n-icon><TrashOutline /></n-icon></template>
                  </n-button>
                </template>
                Delete tweet
              </n-tooltip>
            </n-space>
          </div>
        </div>
      </n-drawer-content>
    </n-drawer>

    <!-- Select Actions modal for bulk clear -->
    <n-modal v-model:show="showSelectActionsModal" preset="card" title="Clear Executions — Select Actions" style="max-width: 480px">
      <n-spin v-if="loadingActions" />
      <template v-else>
        <p style="margin: 0 0 12px; font-size: 13px; color: var(--n-text-color-2)">
          Select which actions to clear for {{ checkedRowKeys.length }} selected tweet{{ checkedRowKeys.length !== 1 ? 's' : '' }}.
        </p>
        <n-checkbox-group v-model:value="selectedActionIds">
          <n-space vertical size="small">
            <n-checkbox
              v-for="action in allActions"
              :key="action.id"
              :value="action.id"
              :label="action.name"
            />
          </n-space>
        </n-checkbox-group>
      </template>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showSelectActionsModal = false; selectedActionIds.value = []">Cancel</n-button>
          <n-popconfirm @positive-click="bulkClearSelected">
            <template #trigger>
              <n-button type="error" :disabled="!selectedActionIds.length" :loading="clearingBulk">
                Clear Executions
              </n-button>
            </template>
            Clear executions for {{ selectedActionIds.length }} action{{ selectedActionIds.length !== 1 ? 's' : '' }} across {{ checkedRowKeys.length }} tweet{{ checkedRowKeys.length !== 1 ? 's' : '' }}?
          </n-popconfirm>
        </n-space>
      </template>
    </n-modal>
  </n-space>
</template>

<style scoped>
.exec-grid {
  display: grid;
  grid-template-columns: 90px 1fr;
  gap: 4px 8px;
  align-items: start;
}
.exec-label {
  font-size: 11px;
  color: var(--n-text-color-disabled);
  padding-top: 3px;
  text-transform: uppercase;
  letter-spacing: 0.04em;
}
.action-group {
  margin-top: 12px;
  border: 1px solid var(--n-border-color);
  border-radius: 6px;
  overflow: hidden;
}
.action-group-header {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 10px;
  background: var(--n-table-header-color);
  border-bottom: 1px solid var(--n-border-color);
  flex: 1;
}
.action-group-name {
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.03em;
}
.action-group-count {
  font-size: 11px;
  color: var(--n-text-color-disabled);
}
.exec-run {
  padding: 8px 10px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.exec-run + .exec-run {
  border-top: 1px solid var(--n-border-color);
}
.exec-run-header {
  display: flex;
  align-items: center;
  gap: 6px;
}
</style>
