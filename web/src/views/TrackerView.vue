<script setup lang="ts">
import { ref, onMounted, computed, watch, h, nextTick, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { useMessage, NButton, NSpace, NBadge, NSlider, NSpin } from 'naive-ui'
import { useEntitiesStore } from '../stores/entities'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import type { TrackedEntity } from '../api/entities'
import { getEntityAliasSuggestions, searchAliasSuggestions, listEntityTracks, listTrackedEntities, type EntityCandidate } from '../api/entities'
import { estimateTokens } from '../api/chat'
import { formatDateTime } from '../utils/format'
import { TimeOutline, CreateOutline, TrashOutline, AddCircleOutline, ChatbubblesOutline, GitNetworkOutline } from '@vicons/ionicons5'
import IconButton from '../components/IconButton.vue'
import PageHeader from '../components/PageHeader.vue'
import { useCandidatesStore } from '../stores/candidates'
import { useConnectionsStore } from '../stores/connections'

const store     = useEntitiesStore()
const message   = useMessage()
const router    = useRouter()
const auth      = useAuthStore()
const chatStore = useChatStore()

const chatLoadingId = ref<number | null>(null)

async function chatWithEntity(e: TrackedEntity) {
  chatLoadingId.value = e.id
  try {
    // Fetch up to 200 tracks to collect tweet IDs (deduplicated)
    const resp = await listEntityTracks(e.id, 1, 200)
    const ids = [...new Set(resp.tracks.map(t => t.tweet_id))]
    if (ids.length === 0) {
      message.warning('No tweets found for this entity.')
      return
    }
    const est = await estimateTokens(ids)
    if (!est.ok) {
      message.error(`Too many tokens: ${est.tokens.toLocaleString()} / ${est.limit.toLocaleString()} — entity has too many tweets.`)
      return
    }
    chatStore.initSession(ids)
    chatStore.estimate = est
    router.push('/chat')
  } catch (err: any) {
    message.error('Failed to prepare chat: ' + err.message)
  } finally {
    chatLoadingId.value = null
  }
}

// ── entity list ───────────────────────────────────────────────────────────────

const filterText       = ref('')
const filterEntityType = ref<string | null>(null)
const filterActiveOnly = ref(false)
watch([filterText, filterEntityType, filterActiveOnly], () => { entityPage.value = 1 })

const entityPage    = ref(1)
const entityPerPage = ref(50)

const filteredEntities = computed(() => {
  let list = store.items
  const q = filterText.value.trim().toLowerCase()
  if (q) list = list.filter(e =>
    e.name.toLowerCase().includes(q) ||
    e.aliases.some(a => a.toLowerCase().includes(q))
  )
  if (filterEntityType.value) list = list.filter(e => e.type === filterEntityType.value)
  if (filterActiveOnly.value) list = list.filter(e => e.active)
  return list
})

const pagedEntities = computed(() => {
  const start = (entityPage.value - 1) * entityPerPage.value
  return filteredEntities.value.slice(start, start + entityPerPage.value)
})

// ── add / edit modal ──────────────────────────────────────────────────────────

const showModal  = ref(false)
const editTarget = ref<TrackedEntity | null>(null)

const form = ref({ name: '', type: '', aliases: '', notes: '' })

const entityTypeOptions = [
  { label: 'LOC — Location',     value: 'LOC'  },
  { label: 'PER — Person',       value: 'PER'  },
  { label: 'ORG — Organization', value: 'ORG'  },
  { label: 'MISC — Misc',        value: 'MISC' },
  { label: 'Custom',             value: ''     },
]

function openAdd() {
  editTarget.value = null
  form.value = { name: '', type: 'LOC', aliases: '', notes: '' }
  suggestions.value = []
  showModal.value = true
}

function openEdit(e: TrackedEntity) {
  editTarget.value = e
  form.value = {
    name:    e.name,
    type:    e.type,
    aliases: e.aliases.join(', '),
    notes:   e.notes,
  }
  suggestions.value = []
  showModal.value = true
  loadSuggestions()
}

// ── alias suggestions ─────────────────────────────────────────────────────────

const suggestions         = ref<string[]>([])
const suggestionsLoading  = ref(false)
let   nameDebounceTimer   = 0

async function loadSuggestions() {
  if (editTarget.value) {
    suggestionsLoading.value = true
    try {
      suggestions.value = await getEntityAliasSuggestions(editTarget.value.id)
    } catch { /* ignore */ } finally {
      suggestionsLoading.value = false
    }
  } else if (form.value.name.trim()) {
    suggestionsLoading.value = true
    try {
      suggestions.value = await searchAliasSuggestions(form.value.name.trim(), form.value.type)
    } catch { /* ignore */ } finally {
      suggestionsLoading.value = false
    }
  } else {
    suggestions.value = []
  }
}

// For create mode: debounce on name change
watch(() => form.value.name, () => {
  if (editTarget.value) return
  clearTimeout(nameDebounceTimer)
  nameDebounceTimer = window.setTimeout(loadSuggestions, 400)
})

function visibleSuggestions() {
  const current = new Set(
    form.value.aliases.split(',').map(s => s.trim()).filter(Boolean)
  )
  current.add(form.value.name.trim())
  return suggestions.value.filter(s => !current.has(s))
}

function addSuggestion(s: string) {
  const parts = form.value.aliases.split(',').map(s => s.trim()).filter(Boolean)
  if (!parts.includes(s)) {
    parts.push(s)
    form.value.aliases = parts.join(', ')
  }
}

const modalSaving = ref(false)

async function saveModal() {
  if (!form.value.name.trim()) {
    message.error('Name is required')
    return
  }
  modalSaving.value = true
  try {
    const aliases = form.value.aliases
      .split(',')
      .map(s => s.trim())
      .filter(Boolean)

    const body = {
      name:    form.value.name.trim(),
      type:    form.value.type,
      aliases,
      notes:   form.value.notes,
    }

    if (editTarget.value) {
      await store.update(editTarget.value.id, body)
      message.success('Entity updated')
    } else {
      await store.create(body)
      message.success('Entity added')
    }
    showModal.value = false
  } catch (e: any) {
    message.error(e?.message ?? 'Save failed')
  } finally {
    modalSaving.value = false
  }
}

// ── activate / deactivate ─────────────────────────────────────────────────────

async function toggleActive(e: TrackedEntity) {
  try {
    if (e.active) {
      await store.deactivate(e.id)
    } else {
      await store.activate(e.id)
    }
  } catch {
    message.error('Failed to toggle')
  }
}

// ── delete ────────────────────────────────────────────────────────────────────

async function remove(e: TrackedEntity) {
  try {
    await store.remove(e.id)
    message.success('Deleted')
    if (drawerEntity.value?.id === e.id) drawerOpen.value = false
  } catch {
    message.error('Delete failed')
  }
}

// ── track history drawer ──────────────────────────────────────────────────────

const drawerOpen   = ref(false)
const drawerEntity = ref<TrackedEntity | null>(null)
const trackPage    = ref(1)

async function openDrawer(e: TrackedEntity) {
  drawerEntity.value = e
  drawerOpen.value   = true
  trackPage.value    = 1
  await store.fetchTracks(e.id, 1)
}

async function onTrackPageChange(p: number) {
  trackPage.value = p
  if (drawerEntity.value) await store.fetchTracks(drawerEntity.value.id, p)
}

// ── timeline bar helpers ───────────────────────────────────────────────────────

const tracks      = computed(() => store.tracks?.tracks ?? [])
const tracksTotal = computed(() => store.tracks?.total ?? 0)
const tracksPerPage = computed(() => store.tracks?.per_page ?? 50)

// ── Candidates tab ────────────────────────────────────────────────────────────

const candidatesStore = useCandidatesStore()
const activeTab = ref<'entities' | 'candidates' | 'connections'>('entities')

// Candidates tab state
const candFilterStatus = ref<string>('pending')
const candFilterType   = ref<string | null>(null)
const candPage         = ref(1)
const candPerPage      = ref(50)
const showMergeModal   = ref(false)
const mergeCandidate   = ref<EntityCandidate | null>(null)
const mergeTargetId    = ref<number | null>(null)
const allEntities      = ref<TrackedEntity[]>([])
const mergeEntityOptions = computed(() =>
  allEntities.value.map(e => ({ label: `${e.name} (${e.type})`, value: e.id }))
)

async function loadCandidates() {
  await candidatesStore.fetchCandidates(
    { status: candFilterStatus.value || undefined, type: candFilterType.value || undefined },
    candPage.value,
    candPerPage.value,
  )
}

async function onTabChange(tab: string) {
  if (tab === 'candidates') {
    await loadCandidates()
    await candidatesStore.fetchPendingCount()
  } else if (tab === 'connections') {
    try {
      await connectionsStore.fetchConnections()
      await nextTick()
      await renderGraph()
    } catch (e: any) {
      message.error('Failed to load connections: ' + (e?.message ?? 'unknown error'))
    }
  }
}

// ── Connections tab ───────────────────────────────────────────────────────────

const connectionsStore = useConnectionsStore()
const graphContainer  = ref<HTMLElement | null>(null)
const minWeight       = ref(1)
let network: any = null
let renderInFlight = false

function destroyNetwork() {
  if (network) {
    network.destroy()
    network = null
  }
}

async function renderGraph() {
  if (!graphContainer.value || renderInFlight) return
  renderInFlight = true
  destroyNetwork()
  try {
    const { nodes, edges } = connectionsStore.graphData(minWeight.value)
    if (nodes.length === 0) return
    const { DataSet, Network } = await import('vis-network/standalone')
    if (!graphContainer.value) return
    network = new Network(
      graphContainer.value,
      { nodes: new DataSet(nodes as any[]), edges: new DataSet(edges as any[]) },
      {
        physics: { stabilization: { iterations: 100 } },
        edges: {
          scaling: { min: 1, max: 8 },
          smooth: { enabled: true, type: 'dynamic', roundness: 0.5 },
        },
        nodes: {
          shape: 'dot',
          font: { size: 12, color: '#e2e8f0' },
          color: { background: '#3b82f6', border: '#1d4ed8', highlight: { background: '#60a5fa', border: '#2563eb' } },
        },
        interaction: { hover: true, tooltipDelay: 200 },
      }
    )
  } finally {
    renderInFlight = false
  }
}

watch(minWeight, renderGraph)

onUnmounted(destroyNetwork)

async function onPromote(c: EntityCandidate) {
  try {
    await candidatesStore.promote(c.id)
    message.success(`"${c.word}" promoted to Tracker`)
    store.fetchEntities()
    candidatesStore.fetchPendingCount()
  } catch (e: any) {
    message.error('Promote failed: ' + e.message)
  }
}

async function onReject(c: EntityCandidate) {
  try {
    await candidatesStore.reject(c.id)
    message.success(`"${c.word}" rejected`)
    candidatesStore.fetchPendingCount()
  } catch (e: any) {
    message.error('Reject failed: ' + e.message)
  }
}

async function openMerge(c: EntityCandidate) {
  mergeCandidate.value = c
  mergeTargetId.value  = null
  allEntities.value    = await listTrackedEntities()
  showMergeModal.value = true
}

async function onMergeConfirm() {
  if (!mergeCandidate.value || !mergeTargetId.value) return
  try {
    await candidatesStore.merge(mergeCandidate.value.id, mergeTargetId.value)
    message.success(`"${mergeCandidate.value.word}" merged as alias`)
    showMergeModal.value = false
    store.fetchEntities()
    candidatesStore.fetchPendingCount()
  } catch (e: any) {
    message.error('Merge failed: ' + e.message)
  }
}

const candidateColumns = [
  { title: 'Word',        key: 'word' },
  { title: 'Type',        key: 'type' },
  { title: 'Count',       key: 'occurrence_count', width: 80 },
  {
    title: 'Confidence',
    key: 'avg_confidence',
    width: 110,
    render: (row: any) => (row.avg_confidence * 100).toFixed(1) + '%',
  },
  {
    title: 'First Seen',
    key: 'first_seen_at',
    render: (row: any) => formatDateTime(row.first_seen_at),
  },
  {
    title: 'Last Seen',
    key: 'last_seen_at',
    render: (row: any) => formatDateTime(row.last_seen_at),
  },
  { title: 'Status', key: 'status' },
  {
    title: 'Actions',
    key: 'actions',
    render(row: any) {
      if (row.status !== 'pending') return null
      return h(NSpace, {}, {
        default: () => [
          h(NButton, { size: 'small', type: 'primary', onClick: () => onPromote(row) }, { default: () => 'Promote' }),
          h(NButton, { size: 'small', onClick: () => openMerge(row) }, { default: () => 'Merge' }),
          h(NButton, { size: 'small', type: 'error', onClick: () => onReject(row) }, { default: () => 'Reject' }),
        ],
      })
    },
  },
]

onMounted(() => {
  store.fetchEntities()
  candidatesStore.fetchPendingCount()
})
</script>

<template>
  <div>
    <n-tabs v-model:value="activeTab" type="line" animated @update:value="onTabChange">
      <n-tab-pane name="entities" tab="Entities">
        <PageHeader title="Tracker" style="margin-bottom: 16px">
          <n-tooltip v-if="auth.isAdmin" trigger="hover">
            <template #trigger>
              <n-button type="primary" @click="openAdd">
                <template #icon><n-icon><AddCircleOutline /></n-icon></template>
              </n-button>
            </template>
            Add Entity
          </n-tooltip>
        </PageHeader>

        <n-flex align="center" :size="8" wrap style="margin-bottom: 12px">
          <n-input v-model:value="filterText" placeholder="Search by name or alias…" clearable style="width: 240px" />
          <n-select
            v-model:value="filterEntityType"
            :options="[{ label: 'All types', value: null }, ...entityTypeOptions.filter(o => o.value)]"
            clearable placeholder="All types" style="width: 180px"
          />
          <n-checkbox v-model:checked="filterActiveOnly">Active only</n-checkbox>
        </n-flex>

        <n-spin :show="store.loading">
          <n-table :bordered="false" :single-line="false" striped>
            <thead>
              <tr>
                <th>Name</th>
                <th style="width: 80px">Type</th>
                <th>Aliases</th>
                <th style="width: 80px; text-align: center">Tracks</th>
                <th style="width: 80px; text-align: center">Active</th>
                <th :style="{ width: auth.isAdmin ? '220px' : '100px' }">Actions</th>
              </tr>
            </thead>
            <tbody>
              <tr v-if="filteredEntities.length === 0">
                <td colspan="6" style="text-align: center; color: #888; padding: 32px">
                  No tracked entities. Add one to start watching.
                </td>
              </tr>
              <tr v-for="e in pagedEntities" :key="e.id">
                <td>
                  <n-text strong>{{ e.name }}</n-text>
                  <div v-if="e.notes" style="font-size: 12px; color: #888">{{ e.notes }}</div>
                </td>
                <td>
                  <n-tag v-if="e.type" size="small" :bordered="false">{{ e.type }}</n-tag>
                  <span v-else style="color: #666">—</span>
                </td>
                <td>
                  <n-space v-if="e.aliases.length">
                    <n-tag v-for="a in e.aliases" :key="a" size="small" type="info" :bordered="false">{{ a }}</n-tag>
                  </n-space>
                  <span v-else style="color: #666">—</span>
                </td>
                <td style="text-align: center">
                  <n-badge
                    :value="e.track_count ?? 0"
                    :max="9999"
                    :color="(e.track_count ?? 0) > 0 ? '#18a058' : '#555'"
                    @click="openDrawer(e)"
                    style="cursor: pointer"
                  />
                </td>
                <td style="text-align: center">
                  <n-switch
                    :value="e.active"
                    @update:value="() => toggleActive(e)"
                    size="small"
                    :disabled="!auth.isAdmin"
                  />
                </td>
                <td>
                  <n-space size="small">
                    <IconButton :icon="TimeOutline" label="View tracks" @click="openDrawer(e)" />
                    <n-tooltip trigger="hover">
                      <template #trigger>
                        <n-button
                          size="small"
                          quaternary
                          :loading="chatLoadingId === e.id"
                          :disabled="(e.track_count ?? 0) === 0"
                          @click="chatWithEntity(e)"
                        >
                          <template #icon><n-icon><ChatbubblesOutline /></n-icon></template>
                        </n-button>
                      </template>
                      Chat with entity tweets
                    </n-tooltip>
                    <template v-if="auth.isAdmin">
                      <IconButton :icon="CreateOutline" label="Edit" @click="openEdit(e)" />
                      <n-popconfirm @positive-click="remove(e)">
                        <template #trigger>
                          <IconButton :icon="TrashOutline" label="Delete" type="error" ghost />
                        </template>
                        Delete "{{ e.name }}"?
                      </n-popconfirm>
                    </template>
                  </n-space>
                </td>
              </tr>
            </tbody>
          </n-table>
        </n-spin>

        <n-pagination
          v-if="filteredEntities.length > entityPerPage"
          v-model:page="entityPage"
          :page-size="entityPerPage"
          :item-count="filteredEntities.length"
          style="margin-top:12px; justify-content:flex-end"
        />

        <!-- Add / Edit modal -->
        <n-modal v-model:show="showModal" preset="card" :title="editTarget ? 'Edit Entity' : 'Add Entity'" style="width: 480px">
          <n-form label-placement="left" label-width="80">
            <n-form-item label="Name">
              <n-input v-model:value="form.name" placeholder="e.g. Isfahan" />
            </n-form-item>
            <n-form-item label="Type">
              <n-select v-model:value="form.type" :options="entityTypeOptions" placeholder="Select type" clearable />
            </n-form-item>
            <n-form-item label="Aliases">
              <n-flex vertical style="width: 100%" :wrap="false">
                <n-input
                  v-model:value="form.aliases"
                  placeholder="Comma-separated: Esfahan, اصفهان"
                />
                <n-spin :show="suggestionsLoading" :size="'small'">
                  <n-flex v-if="visibleSuggestions().length" wrap style="margin-top: 6px" :size="[6, 6]">
                    <n-text depth="3" style="font-size: 12px; align-self: center">NER suggestions:</n-text>
                    <n-tag
                      v-for="s in visibleSuggestions()"
                      :key="s"
                      size="small"
                      type="success"
                      :bordered="false"
                      style="cursor: pointer"
                      @click="addSuggestion(s)"
                    >
                      + {{ s }}
                    </n-tag>
                  </n-flex>
                </n-spin>
              </n-flex>
            </n-form-item>
            <n-form-item label="Notes">
              <n-input v-model:value="form.notes" type="textarea" :rows="2" placeholder="Optional notes" />
            </n-form-item>
          </n-form>
          <template #footer>
            <n-flex justify="end">
              <n-button @click="showModal = false">Cancel</n-button>
              <n-button type="primary" :loading="modalSaving" @click="saveModal">Save</n-button>
            </n-flex>
          </template>
        </n-modal>

        <!-- Track history drawer -->
        <n-drawer v-model:show="drawerOpen" :width="600" placement="right">
          <n-drawer-content :title="drawerEntity ? `Tracks — ${drawerEntity.name}` : 'Tracks'" closable>
            <n-spin :show="store.tracksLoading">
              <div v-if="tracks.length === 0 && !store.tracksLoading" style="text-align: center; color: #888; padding: 32px">
                No track records yet.
              </div>

              <!-- Summary -->
              <n-statistic v-if="tracksTotal > 0" label="Total matches" :value="tracksTotal" style="margin-bottom: 16px" />

              <!-- Timeline -->
              <n-timeline v-if="tracks.length">
                <n-timeline-item
                  v-for="t in tracks"
                  :key="t.id"
                  :type="t.match_source === 'ner' ? 'success' : 'info'"
                  :title="formatDateTime(t.tweet_created_at || t.created_at)"
                >
                  <n-flex vertical gap="6">
                    <n-flex align="center" gap="8">
                      <n-tag size="small" :type="t.match_source === 'ner' ? 'success' : 'info'" :bordered="false">
                        {{ t.match_source === 'ner' ? 'NER' : 'text' }}
                      </n-tag>
                      <n-text v-if="t.tweet_author_handle" depth="3" style="font-size: 12px">
                        @{{ t.tweet_author_handle }}
                      </n-text>
                      <n-button
                        text
                        size="tiny"
                        style="font-size: 11px; color: #888"
                        @click="router.push(`/tweets?tweet=${t.tweet_id}`)"
                      >
                        {{ t.tweet_author_handle ? '↗' : `${t.tweet_id} ↗` }}
                      </n-button>
                    </n-flex>
                    <n-text v-if="t.tweet_text" style="font-size: 13px">{{ t.tweet_text }}</n-text>
                    <n-text v-else-if="t.context" depth="3" style="font-style: italic; font-size: 13px">"{{ t.context }}"</n-text>
                  </n-flex>
                </n-timeline-item>
              </n-timeline>

              <n-pagination
                v-if="tracksTotal > tracksPerPage"
                :page="trackPage"
                :page-count="Math.ceil(tracksTotal / tracksPerPage)"
                @update:page="onTrackPageChange"
                style="margin-top: 16px; justify-content: center"
              />
            </n-spin>
          </n-drawer-content>
        </n-drawer>
      </n-tab-pane>

      <n-tab-pane name="candidates">
        <template #tab>
          Candidates<n-badge v-if="candidatesStore.pendingCount > 0" :value="candidatesStore.pendingCount" style="margin-left:6px" />
        </template>

        <!-- Filters -->
        <n-space align="center" style="margin-bottom:12px">
          <n-select
            v-model:value="candFilterStatus"
            :options="[
              { label: 'Pending',  value: 'pending'  },
              { label: 'Promoted', value: 'promoted' },
              { label: 'Rejected', value: 'rejected' },
              { label: 'All',      value: ''         },
            ]"
            style="width:130px"
            @update:value="() => { candPage = 1; loadCandidates() }"
          />
          <n-select
            v-model:value="candFilterType"
            clearable
            placeholder="Type"
            :options="[
              { label: 'LOC',  value: 'LOC'  },
              { label: 'PER',  value: 'PER'  },
              { label: 'ORG',  value: 'ORG'  },
              { label: 'MISC', value: 'MISC' },
            ]"
            style="width:120px"
            @update:value="() => { candPage = 1; loadCandidates() }"
          />
        </n-space>

        <!-- Candidates table -->
        <n-data-table
          :loading="candidatesStore.loading"
          :data="candidatesStore.response?.candidates ?? []"
          :columns="candidateColumns"
          :row-key="(row: any) => row.id"
        />

        <!-- Pagination -->
        <n-pagination
          v-if="(candidatesStore.response?.total ?? 0) > candPerPage"
          v-model:page="candPage"
          :page-size="candPerPage"
          :item-count="candidatesStore.response?.total ?? 0"
          style="margin-top:12px; justify-content:flex-end"
          @update:page="loadCandidates"
        />
      </n-tab-pane>
      <n-tab-pane name="connections">
        <template #tab>
          <n-space :size="4" align="center">
            <n-icon :component="GitNetworkOutline" />
            Connections
          </n-space>
        </template>

        <div style="margin-bottom: 12px; display: flex; align-items: center; gap: 16px;">
          <span style="font-size: 13px; color: var(--n-text-color-3);">Min weight:</span>
          <n-slider
            v-model:value="minWeight"
            :min="1"
            :max="connectionsStore.connections.reduce((m, c) => c.weight > m ? c.weight : m, 1)"
            :step="1"
            style="width: 200px"
          />
          <span style="font-size: 13px; min-width: 20px;">{{ minWeight }}</span>
          <n-button
            v-if="auth.isAdmin"
            size="small"
            type="error"
            secondary
            @click="destroyNetwork(); connectionsStore.resetConnections().catch((e: any) => message.error('Reset failed: ' + e?.message))"
          >
            Reset
          </n-button>
        </div>

        <div v-if="connectionsStore.loading" style="text-align: center; padding: 40px;">
          <n-spin />
        </div>
        <div
          v-else-if="connectionsStore.connections.length === 0"
          style="text-align: center; padding: 60px; color: var(--n-text-color-3);"
        >
          No connections yet. Set up an <strong>entity_cooccurrence</strong> pipeline chained after entity_matcher to start building the graph.
        </div>
        <div
          v-else
          ref="graphContainer"
          style="width: 100%; height: calc(100vh - 200px); min-height: 400px; background: var(--n-card-color); border-radius: 8px; border: 1px solid var(--n-border-color);"
        />
      </n-tab-pane>
    </n-tabs>

    <!-- Merge modal -->
    <n-modal v-model:show="showMergeModal" preset="dialog" title="Merge as Alias" positive-text="Confirm" negative-text="Cancel" @positive-click="onMergeConfirm">
      <p style="margin-bottom:8px">Merge <strong>{{ mergeCandidate?.word }}</strong> as an alias of:</p>
      <n-select
        v-model:value="mergeTargetId"
        :options="mergeEntityOptions"
        filterable
        placeholder="Search tracked entities..."
      />
    </n-modal>
  </div>
</template>
