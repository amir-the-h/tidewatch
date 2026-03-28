<script setup lang="ts">
import { ref, computed, onMounted, h } from 'vue'
import {
  NFlex, NTag, NSelect, NInput, NSwitch, NFormItem,
  NText, NSpace, NDataTable,
  useMessage,
  type DataTableColumns,
} from 'naive-ui'
import {
  TrashOutline, PauseCircleOutline, CheckmarkCircleOutline, CreateOutline, ServerOutline,
} from '@vicons/ionicons5'
import PageHeader from '../components/PageHeader.vue'
import FormDrawer from '../components/FormDrawer.vue'
import PipelineBackendsModal from '../components/PipelineBackendsModal.vue'
import { iconBtn, iconBtnConfirm, activeTag } from '../utils/renderActions'
import {
  listPipelines, createPipeline, updatePipeline, deletePipeline, activatePipeline, deactivatePipeline,
  listPipelineHandlers, listModels,
  type Pipeline, type PipelineType, type ModelFile,
} from '../api/pipelines'

const msg = useMessage()

const pipelines = ref<Pipeline[]>([])
const loading = ref(false)

// Filter
const filterText = ref('')
const filterActiveOnly = ref(false)

const filteredPipelines = computed(() => {
  let list = pipelines.value
  const q = filterText.value.trim().toLowerCase()
  if (q) list = list.filter(p => p.name.toLowerCase().includes(q))
  if (filterActiveOnly.value) list = list.filter(p => p.active)
  return list
})

// Form state
const showForm = ref(false)
const saving = ref(false)
const editingId = ref<number | null>(null)
const formName = ref('')
const formType = ref<PipelineType>('onnx')
const formUsecase = ref<string>('custom')
const formModel = ref('')
const formSystemPrompt = ref('')
const formConfigText = ref('{}')
const formHandler = ref<string | null>(null)
const formPath = ref('')

// Handlers (for internal type)
const handlers = ref<string[]>([])
const handlerOptions = computed(() => handlers.value.map(h => ({ label: h, value: h })))

// Models (for onnx type)
const models = ref<ModelFile[]>([])
const modelOptions = computed(() => models.value.map(m => ({ label: m.name, value: m.path })))

const typeOptions: { label: string; value: PipelineType }[] = [
  { label: 'ONNX', value: 'onnx' },
  { label: 'Ollama', value: 'ollama' },
  { label: 'Internal', value: 'internal' },
]

const typeTagType: Record<PipelineType, 'default' | 'info' | 'success' | 'warning'> = {
  onnx: 'info',
  ollama: 'success',
  internal: 'warning',
}

async function fetchPipelines() {
  loading.value = true
  try {
    pipelines.value = await listPipelines()
  } catch (e: any) {
    msg.error(e.message)
  } finally {
    loading.value = false
  }
}

onMounted(async () => {
  fetchPipelines()
  try { handlers.value = await listPipelineHandlers() } catch {}
  try { models.value = await listModels() } catch {}
})

function openCreate() {
  editingId.value = null
  formName.value = ''
  formType.value = 'onnx'
  formUsecase.value = 'custom'
  formModel.value = ''
  formSystemPrompt.value = ''
  formConfigText.value = '{}'
  formHandler.value = null
  formPath.value = ''
  showForm.value = true
}

function openEdit(row: Pipeline) {
  editingId.value = row.id
  formName.value = row.name
  formType.value = row.type
  formConfigText.value = JSON.stringify(row.config ?? {}, null, 2)
  // Ollama-specific
  formModel.value = row.config?.model ?? ''
  formUsecase.value = row.config?.usecase ?? 'custom'
  formSystemPrompt.value = row.config?.system_prompt ?? ''
  // Internal-specific
  formHandler.value = row.config?.handler ?? null
  // ONNX-specific
  formPath.value = row.config?.path ?? ''
  showForm.value = true
}

async function onSave() {
  if (!formName.value.trim()) {
    msg.error('Name is required')
    return
  }
  let config: Record<string, any> = {}

  if (formType.value === 'ollama') {
    if (!formModel.value.trim()) {
      msg.error('Model name is required')
      return
    }
    config.model = formModel.value.trim()
    config.usecase = formUsecase.value
    if (formUsecase.value === 'custom' && formSystemPrompt.value.trim()) {
      config.system_prompt = formSystemPrompt.value.trim()
    }
    if (editingId.value === null && formUsecase.value !== 'custom') {
      const conflict = pipelines.value.find(p => p.config?.usecase === formUsecase.value)
      if (conflict) {
        msg.error(`A ${formUsecase.value} pipeline already exists ("${conflict.name}"). Delete it first.`)
        return
      }
    }
  } else {
    try {
      config = JSON.parse(formConfigText.value || '{}')
    } catch {
      msg.error('Config must be valid JSON')
      return
    }
    if (formType.value === 'onnx') {
      if (!formPath.value.trim()) {
        msg.error('Model path is required')
        return
      }
      config.path = formPath.value.trim()
    }
    if (formType.value === 'internal' && formHandler.value) {
      config.handler = formHandler.value
    }
  }

  saving.value = true
  try {
    if (editingId.value !== null) {
      await updatePipeline(editingId.value, { name: formName.value.trim(), config })
      msg.success('Pipeline updated')
    } else {
      await createPipeline({ name: formName.value.trim(), type: formType.value, config })
      msg.success('Pipeline created')
    }
    showForm.value = false
    await fetchPipelines()
  } catch (e: any) {
    msg.error(e.message)
  } finally {
    saving.value = false
  }
}

async function onActivate(id: number) {
  try {
    await activatePipeline(id)
    await fetchPipelines()
  } catch (e: any) {
    msg.error(e.message)
  }
}

async function onDeactivate(id: number) {
  try {
    await deactivatePipeline(id)
    await fetchPipelines()
  } catch (e: any) {
    msg.error(e.message)
  }
}

async function onDelete(id: number) {
  try {
    await deletePipeline(id)
    pipelines.value = pipelines.value.filter(p => p.id !== id)
  } catch (e: any) {
    msg.error(e.message)
  }
}

// ── Backend assignment modal ─────────────────────────────────────────────────
const backendModalPipeline = ref<Pipeline | null>(null)
const showBackendModal = ref(false)

function openBackendsModal(row: Pipeline) {
  backendModalPipeline.value = row
  showBackendModal.value = true
}

const columns: DataTableColumns<Pipeline> = [
  { title: 'ID', key: 'id', width: 60, sorter: 'default' },
  { title: 'Name', key: 'name', sorter: 'default', ellipsis: { tooltip: true } },
  {
    title: 'Type', key: 'type', width: 100, sorter: 'default',
    render: (row) => h(NTag, { size: 'small', type: typeTagType[row.type] ?? 'default' },
      { default: () => row.type }),
  },
  {
    title: 'Use Case', key: 'usecase', width: 100,
    render: (row) => {
      const uc = row.type === 'ollama' ? (row.config?.usecase ?? 'custom') : null
      if (!uc || uc === 'custom') return null
      return h(NTag, { size: 'small', type: 'warning' }, { default: () => uc })
    },
  },
  {
    title: 'Active', key: 'active', width: 80,
    render: (row) => activeTag(row.active),
  },
  {
    title: 'Created', key: 'created_at', width: 160, sorter: 'default',
    render: (row) => new Date(row.created_at).toLocaleString(),
  },
  {
    title: 'Actions', key: 'actions', width: 160,
    render: (row) => h(NSpace, { size: 4 }, {
      default: () => [
        ...(row.type === 'ollama'
          ? [iconBtn(ServerOutline, 'Backends', () => openBackendsModal(row))]
          : []),
        iconBtn(CreateOutline, 'Edit', () => openEdit(row)),
        row.active
          ? iconBtn(PauseCircleOutline, 'Deactivate', () => onDeactivate(row.id))
          : iconBtn(CheckmarkCircleOutline, 'Activate', () => onActivate(row.id), { type: 'primary' }),
        iconBtnConfirm(TrashOutline, 'Delete', 'Delete this pipeline?', () =>
          onDelete(row.id), { type: 'error' }),
      ],
    }),
  },
]
</script>

<template>
  <div>
    <PageHeader title="Pipelines" add-label="New Pipeline" @add="openCreate" />

    <!-- Filters -->
    <n-flex style="margin-bottom: 12px" :size="12" align="center">
      <n-input v-model:value="filterText" placeholder="Filter by name…" clearable style="width: 200px" />
      <n-switch v-model:value="filterActiveOnly" size="small" />
      <n-text depth="3" style="font-size: 13px">Active only</n-text>
    </n-flex>

    <!-- Table -->
    <n-data-table
      :columns="columns"
      :data="filteredPipelines"
      :loading="loading"
      :bordered="false"
      :row-key="(row: Pipeline) => row.id"
      size="small"
    />

    <!-- Create / Edit Drawer -->
    <FormDrawer
      v-model:show="showForm"
      :title="editingId !== null ? 'Edit Pipeline' : 'New Pipeline'"
      :save-label="editingId !== null ? 'Save' : 'Create'"
      :loading="saving"
      @save="onSave"
    >
      <n-form-item label="Name">
        <n-input v-model:value="formName" placeholder="Pipeline name" />
      </n-form-item>
      <n-form-item label="Type">
        <n-select v-model:value="formType" :options="typeOptions" :disabled="editingId !== null" />
      </n-form-item>

      <!-- Handler dropdown for internal type -->
      <n-form-item v-if="formType === 'internal'" label="Handler">
        <n-select
          v-model:value="formHandler"
          :options="handlerOptions"
          placeholder="Select handler"
          clearable
        />
      </n-form-item>

      <!-- Ollama-specific fields -->
      <template v-if="formType === 'ollama'">
        <n-form-item label="Model">
          <n-input v-model:value="formModel" placeholder="e.g. llama3.2, mistral, gemma3" />
        </n-form-item>
        <n-form-item label="Use Case">
          <n-select
            v-model:value="formUsecase"
            :options="[
              { label: 'Custom (action pipeline)', value: 'custom' },
              { label: 'Digest', value: 'digest' },
              { label: 'Chat', value: 'chat' },
            ]"
          />
        </n-form-item>
        <n-form-item v-if="formUsecase === 'custom'" label="System Prompt">
          <n-input
            v-model:value="formSystemPrompt"
            type="textarea"
            :rows="4"
            placeholder="Leave empty to use the default generic extraction prompt"
            style="font-family: monospace; font-size: 12px"
          />
        </n-form-item>
      </template>

      <!-- Config JSON for non-Ollama types -->
      <template v-else>
        <n-form-item v-if="formType === 'onnx'" label="Model">
          <n-select
            v-model:value="formPath"
            :options="modelOptions"
            placeholder="Select a model file"
            filterable
            clearable
          />
        </n-form-item>
        <n-form-item label="Config (JSON)">
          <n-input
            v-model:value="formConfigText"
            type="textarea"
            :rows="5"
            placeholder="{}"
            style="font-family: monospace; font-size: 12px"
          />
        </n-form-item>
      </template>
    </FormDrawer>

    <PipelineBackendsModal
      v-if="backendModalPipeline"
      v-model:show="showBackendModal"
      :pipeline-id="backendModalPipeline.id"
      :pipeline-name="backendModalPipeline.name"
      @saved="fetchPipelines"
    />
  </div>
</template>
