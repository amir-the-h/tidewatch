<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useMessage } from 'naive-ui'
import {
  listOllamaBackends,
  createOllamaBackend,
  updateOllamaBackend,
  deleteOllamaBackend,
  enableOllamaBackend,
  disableOllamaBackend,
} from '../../api/ollama'
import { useOllamaStore } from '../../stores/ollama'
import type { OllamaBackend } from '../../stores/ollama'

const message = useMessage()
const ollamaStore = useOllamaStore()
const loading     = ref(false)
const lastUpdated = ref<Date | null>(null)

// ── add / edit modal ────────────────────────────────────────────────────────
const showModal        = ref(false)
const editTarget       = ref<OllamaBackend | null>(null)
const formName         = ref('')
const formUrl          = ref('')
const formMaxConcurrent = ref(1)
const saving           = ref(false)

function openAdd() {
  editTarget.value        = null
  formName.value          = ''
  formUrl.value           = ''
  formMaxConcurrent.value = 1
  showModal.value         = true
}

function openEdit(b: OllamaBackend) {
  editTarget.value        = b
  formName.value          = b.name
  formUrl.value           = b.url
  formMaxConcurrent.value = b.max_concurrent ?? 1
  showModal.value         = true
}

async function saveModal() {
  if (!formUrl.value.trim()) {
    message.warning('URL is required')
    return
  }
  const maxConcurrent = Math.max(1, formMaxConcurrent.value || 1)
  saving.value = true
  try {
    if (editTarget.value) {
      await updateOllamaBackend(editTarget.value.id, {
        name:           formName.value.trim() || undefined,
        url:            formUrl.value.trim(),
        max_concurrent: maxConcurrent,
      })
      message.success('Backend updated')
    } else {
      await createOllamaBackend(formName.value.trim(), formUrl.value.trim(), maxConcurrent)
      message.success('Backend added')
    }
    showModal.value = false
    await refresh()
  } catch (e: any) {
    message.error(e.message ?? 'Request failed')
  } finally {
    saving.value = false
  }
}

// ── delete ──────────────────────────────────────────────────────────────────
async function handleDelete(b: OllamaBackend) {
  try {
    await deleteOllamaBackend(b.id)
    message.success(`Backend "${b.name}" deleted`)
    await refresh()
  } catch (e: any) {
    message.error(e.message ?? 'Delete failed')
  }
}

// ── enable / disable ────────────────────────────────────────────────────────
async function toggleEnabled(b: OllamaBackend) {
  try {
    if (b.enabled) {
      await disableOllamaBackend(b.id)
    } else {
      await enableOllamaBackend(b.id)
    }
    await refresh()
  } catch (e: any) {
    message.error(e.message ?? 'Toggle failed')
  }
}

// ── fetch / refresh ──────────────────────────────────────────────────────────
async function refresh() {
  try {
    ollamaStore.setBackends(await listOllamaBackends())
    lastUpdated.value = new Date()
  } catch { /* server unreachable */ }
}

onMounted(() => {
  loading.value = true
  refresh().finally(() => { loading.value = false })
})

// ── derived ─────────────────────────────────────────────────────────────────
const maxScore = computed(() => {
  let max = 0
  for (const b of ollamaStore.backends) {
    if (b.score > max) max = b.score
  }
  return max || 1
})

function timeAgo(iso: string): string {
  const diff = Math.floor((Date.now() - new Date(iso).getTime()) / 1000)
  if (diff < 60) return 'just now'
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`
  return `${Math.floor(diff / 86400)}d ago`
}

function formatLastUpdated(d: Date): string {
  return d.toLocaleTimeString()
}
</script>

<template>
  <n-space vertical size="large">

    <!-- Header -->
    <n-space align="center" justify="space-between" style="flex-wrap: wrap; gap: 8px;">
      <n-h2 style="margin: 0;">Ollama Backends</n-h2>
      <n-space align="center" :size="12">
        <n-text v-if="lastUpdated" depth="3" style="font-size: 12px;">
          Updated {{ formatLastUpdated(lastUpdated) }}
        </n-text>
        <n-button size="small" type="primary" @click="openAdd">+ Add Backend</n-button>
      </n-space>
    </n-space>

    <!-- Loading spinner on first load -->
    <div v-if="loading" style="display: flex; justify-content: center; padding: 48px 0;">
      <n-spin size="large" />
    </div>

    <!-- Empty state -->
    <n-empty
      v-else-if="ollamaStore.backends.length === 0"
      description="No Ollama backends configured"
      style="padding: 48px 0;"
    />

    <!-- Backend cards -->
    <template v-else>
      <n-grid :cols="3" :x-gap="12" :y-gap="12" responsive="screen" :item-responsive="true">
        <n-gi
          v-for="backend in ollamaStore.backends"
          :key="backend.id"
          span="3 s:3 m:1"
        >
          <n-card
            size="small"
            embedded
            style="height: 100%;"
            :style="{ opacity: backend.enabled ? 1 : 0.55 }"
          >
            <!-- Status dot + name/url row -->
            <n-space align="center" :size="8" style="margin-bottom: 12px; flex-wrap: nowrap;">
              <!-- Status indicator -->
              <n-tooltip trigger="hover">
                <template #trigger>
                  <span
                    :style="{
                      display: 'inline-block',
                      width: '10px',
                      height: '10px',
                      borderRadius: '50%',
                      flexShrink: '0',
                      background: !backend.enabled
                        ? '#585b70'
                        : !backend.healthy
                          ? '#f38ba8'
                          : backend.active_count >= (backend.max_concurrent || 1)
                            ? '#f9e2af'
                            : '#a6e3a1',
                      boxShadow: !backend.enabled
                        ? 'none'
                        : !backend.healthy
                          ? '0 0 0 2px rgba(243,139,168,0.2)'
                          : backend.active_count >= (backend.max_concurrent || 1)
                            ? '0 0 0 2px rgba(249,226,175,0.3)'
                            : '0 0 0 2px rgba(166,227,161,0.2)',
                    }"
                  />
                </template>
                {{
                  !backend.enabled
                    ? 'Disabled'
                    : !backend.healthy
                      ? 'Unhealthy'
                      : backend.active_count >= (backend.max_concurrent || 1)
                        ? `At capacity (${backend.active_count}/${backend.max_concurrent})`
                        : backend.active_count > 0
                          ? `Active (${backend.active_count}/${backend.max_concurrent})`
                          : 'Healthy (idle)'
                }}
              </n-tooltip>

              <!-- Name + URL stack -->
              <div style="overflow: hidden; min-width: 0; flex: 1;">
                <n-text style="font-size: 13px; display: block; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;">
                  {{ backend.name }}
                </n-text>
                <n-tooltip trigger="hover" :delay="300">
                  <template #trigger>
                    <n-text
                      code
                      depth="3"
                      style="
                        font-size: 11px;
                        overflow: hidden;
                        text-overflow: ellipsis;
                        white-space: nowrap;
                        display: block;
                        max-width: 100%;
                      "
                    >
                      {{ backend.url }}
                    </n-text>
                  </template>
                  {{ backend.url }}
                </n-tooltip>
              </div>
            </n-space>

            <!-- Score bar -->
            <div style="margin-bottom: 10px;">
              <div v-if="backend.score > 0">
                <div style="
                  display: flex; justify-content: space-between; align-items: center;
                  margin-bottom: 4px;
                ">
                  <n-text depth="3" style="font-size: 11px;">Speed</n-text>
                  <n-text style="font-size: 11px; color: #cdd6f4; font-variant-numeric: tabular-nums;">
                    {{ backend.score.toFixed(1) }} tok/s
                  </n-text>
                </div>
                <div style="height: 4px; border-radius: 2px; background: rgba(255,255,255,0.08); overflow: hidden;">
                  <div
                    :style="{
                      height: '100%',
                      width: (backend.score / maxScore * 100) + '%',
                      background: '#89b4fa',
                      borderRadius: '2px',
                      transition: 'width 0.5s ease',
                    }"
                  />
                </div>
              </div>
              <n-text v-else depth="3" style="font-size: 11px; font-style: italic;">
                Not yet measured
              </n-text>
            </div>

            <!-- Request count + active/max + last active -->
            <n-space justify="space-between" align="center" style="flex-wrap: wrap; gap: 4px; margin-bottom: 10px;">
              <n-text depth="3" style="font-size: 11px; font-variant-numeric: tabular-nums;">
                {{ backend.request_count }} requests
              </n-text>
              <n-text depth="3" style="font-size: 11px; font-variant-numeric: tabular-nums;">
                {{ backend.active_count }}/{{ backend.max_concurrent }} active
              </n-text>
              <n-text depth="3" style="font-size: 11px;">
                {{ backend.last_active_at ? timeAgo(backend.last_active_at) : 'Never' }}
              </n-text>
            </n-space>

            <!-- Action buttons -->
            <n-space :size="6">
              <n-button
                size="tiny"
                :type="backend.enabled ? 'default' : 'primary'"
                @click="toggleEnabled(backend)"
              >
                {{ backend.enabled ? 'Disable' : 'Enable' }}
              </n-button>
              <n-button size="tiny" @click="openEdit(backend)">Edit</n-button>
              <n-popconfirm @positive-click="handleDelete(backend)">
                <template #trigger>
                  <n-button size="tiny" type="error">Delete</n-button>
                </template>
                Delete backend "{{ backend.name }}"?
              </n-popconfirm>
            </n-space>
          </n-card>
        </n-gi>
      </n-grid>
    </template>

  </n-space>

  <!-- Add / Edit modal -->
  <n-modal
    v-model:show="showModal"
    :title="editTarget ? 'Edit Backend' : 'Add Backend'"
    preset="card"
    style="width: 400px;"
    :mask-closable="!saving"
  >
    <n-form label-placement="left" label-width="90px">
      <n-form-item label="Name">
        <n-input
          v-model:value="formName"
          placeholder="Defaults to URL"
          :disabled="saving"
        />
      </n-form-item>
      <n-form-item label="URL" required>
        <n-input
          v-model:value="formUrl"
          placeholder="http://localhost:11434"
          :disabled="saving"
        />
      </n-form-item>
      <n-form-item label="Max Slots">
        <n-input-number
          v-model:value="formMaxConcurrent"
          :min="1"
          :max="32"
          :disabled="saving"
          style="width: 100%"
        />
        <template #feedback>
          Set to match <n-text code>OLLAMA_NUM_PARALLEL</n-text> on the server
        </template>
      </n-form-item>
    </n-form>
    <template #footer>
      <n-space justify="end">
        <n-button :disabled="saving" @click="showModal = false">Cancel</n-button>
        <n-button type="primary" :loading="saving" @click="saveModal">
          {{ editTarget ? 'Save' : 'Add' }}
        </n-button>
      </n-space>
    </template>
  </n-modal>
</template>
