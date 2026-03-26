<script setup lang="ts">
import { ref, onMounted, computed, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useMessage } from 'naive-ui'
import { useEntitiesStore } from '../stores/entities'
import type { TrackedEntity } from '../api/entities'
import { getEntityAliasSuggestions, searchAliasSuggestions } from '../api/entities'
import { formatDateTime } from '../utils/format'

const store   = useEntitiesStore()
const message = useMessage()
const router  = useRouter()

// ── entity list ───────────────────────────────────────────────────────────────

onMounted(() => store.fetchEntities())

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


</script>

<template>
  <div>
    <n-flex justify="space-between" align="center" style="margin-bottom: 16px">
      <n-h2 style="margin: 0">Tracked Entities</n-h2>
      <n-button type="primary" @click="openAdd">+ Add Entity</n-button>
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
            <th style="width: 220px">Actions</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="store.items.length === 0">
            <td colspan="6" style="text-align: center; color: #888; padding: 32px">
              No tracked entities. Add one to start watching.
            </td>
          </tr>
          <tr v-for="e in store.items" :key="e.id">
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
              <n-switch :value="e.active" @update:value="() => toggleActive(e)" size="small" />
            </td>
            <td>
              <n-space>
                <n-button size="small" @click="openDrawer(e)">Tracks</n-button>
                <n-button size="small" @click="openEdit(e)">Edit</n-button>
                <n-popconfirm @positive-click="remove(e)">
                  <template #trigger>
                    <n-button size="small" type="error" ghost>Delete</n-button>
                  </template>
                  Delete "{{ e.name }}"?
                </n-popconfirm>
              </n-space>
            </td>
          </tr>
        </tbody>
      </n-table>
    </n-spin>

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
              :title="formatDateTime(t.created_at)"
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
  </div>
</template>
