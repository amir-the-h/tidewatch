<!-- web/src/components/PipelineBackendsModal.vue -->
<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useMessage } from 'naive-ui'
import { listPipelineBackends, setPipelineBackends } from '../api/pipelines'
import { listOllamaBackends } from '../api/ollama'
import type { OllamaBackend } from '../api/ollama'

const props = defineProps<{
  pipelineId: number
  pipelineName: string
  show: boolean
}>()

const emit = defineEmits<{
  (e: 'update:show', value: boolean): void
  (e: 'saved'): void
}>()

const message = useMessage()
const loading = ref(false)
const saving = ref(false)
const assigned = ref<OllamaBackend[]>([])
const allBackends = ref<OllamaBackend[]>([])
const addTarget = ref<number | null>(null)

const available = computed(() =>
  allBackends.value.filter(b => !assigned.value.some(a => a.id === b.id))
)

const availableOptions = computed(() =>
  available.value.map(b => ({ label: b.name, value: b.id }))
)

watch(() => props.show, async (open) => {
  if (!open) return
  loading.value = true
  addTarget.value = null
  try {
    const [pbs, backends] = await Promise.all([
      listPipelineBackends(props.pipelineId),
      listOllamaBackends(),
    ])
    allBackends.value = backends
    const sorted = [...pbs].sort((a, b) => a.priority - b.priority)
    assigned.value = sorted
      .map(pb => backends.find(b => b.id === pb.backend_id))
      .filter((b): b is OllamaBackend => b !== undefined)
  } catch (e: any) {
    message.error(e.message ?? 'Failed to load backends')
  } finally {
    loading.value = false
  }
})

function moveUp(i: number) {
  if (i === 0) return
  const a = [...assigned.value]
  ;[a[i - 1], a[i]] = [a[i], a[i - 1]]
  assigned.value = a
}

function moveDown(i: number) {
  if (i === assigned.value.length - 1) return
  const a = [...assigned.value]
  ;[a[i], a[i + 1]] = [a[i + 1], a[i]]
  assigned.value = a
}

function remove(i: number) {
  assigned.value = assigned.value.filter((_, idx) => idx !== i)
}

function addBackend() {
  if (addTarget.value === null) return
  const b = allBackends.value.find(b => b.id === addTarget.value)
  if (!b) return
  assigned.value = [...assigned.value, b]
  addTarget.value = null
}

async function save() {
  saving.value = true
  try {
    await setPipelineBackends(
      props.pipelineId,
      assigned.value.map((b, i) => ({ backend_id: b.id, priority: i + 1 })),
    )
    message.success('Backend assignments saved')
    emit('saved')
    emit('update:show', false)
  } catch (e: any) {
    message.error(e.message ?? 'Save failed')
  } finally {
    saving.value = false
  }
}

function close() {
  if (saving.value) return
  emit('update:show', false)
}
</script>

<template>
  <n-modal
    :show="show"
    :title="`Backend Assignments — ${pipelineName}`"
    preset="card"
    style="width: 480px;"
    :mask-closable="!saving"
    @update:show="close"
  >
    <div v-if="loading" style="display: flex; justify-content: center; padding: 32px 0;">
      <n-spin size="medium" />
    </div>

    <template v-else>
      <!-- Assigned list -->
      <div v-if="assigned.length > 0" style="margin-bottom: 16px;">
        <div
          v-for="(backend, i) in assigned"
          :key="backend.id"
          style="
            display: flex;
            align-items: center;
            gap: 8px;
            padding: 6px 0;
            border-bottom: 1px solid rgba(255,255,255,0.06);
          "
        >
          <n-text depth="3" style="font-size: 12px; width: 18px; text-align: right; flex-shrink: 0;">
            {{ i + 1 }}.
          </n-text>
          <div style="flex: 1; min-width: 0;">
            <n-text style="font-size: 13px; display: block; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;">
              {{ backend.name }}
            </n-text>
            <n-text
              code
              depth="3"
              style="font-size: 11px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; display: block;"
            >
              {{ backend.url }}
            </n-text>
          </div>
          <n-button size="tiny" :disabled="i === 0" @click="moveUp(i)">↑</n-button>
          <n-button size="tiny" :disabled="i === assigned.length - 1" @click="moveDown(i)">↓</n-button>
          <n-button size="tiny" type="error" @click="remove(i)">Remove</n-button>
        </div>
      </div>

      <n-text
        v-else
        depth="3"
        style="font-size: 12px; font-style: italic; display: block; margin-bottom: 16px;"
      >
        No backends assigned — any healthy backend will be used.
      </n-text>

      <!-- Add row -->
      <n-space align="center" :size="8">
        <n-select
          v-model:value="addTarget"
          :options="availableOptions"
          placeholder="Select backend to add"
          style="width: 280px;"
          :disabled="availableOptions.length === 0"
        />
        <n-button
          size="small"
          type="primary"
          :disabled="addTarget === null"
          @click="addBackend"
        >
          + Add
        </n-button>
      </n-space>
    </template>

    <template #footer>
      <n-space justify="end">
        <n-button :disabled="saving" @click="close">Cancel</n-button>
        <n-button type="primary" :loading="saving" :disabled="loading" @click="save">Save</n-button>
      </n-space>
    </template>
  </n-modal>
</template>
