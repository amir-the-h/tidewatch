<script setup lang="ts">
import { ref, computed, onMounted, h } from 'vue'
import {
  NFlex, NTag, NSelect, NInput, NSwitch, NFormItem,
  NText, NSpace, NDataTable,
  useMessage,
  type DataTableColumns,
} from 'naive-ui'
import {
  TrashOutline, PauseCircleOutline, CheckmarkCircleOutline,
} from '@vicons/ionicons5'
import PageHeader from '../components/PageHeader.vue'
import FormDrawer from '../components/FormDrawer.vue'
import { iconBtn, iconBtnConfirm, activeTag } from '../utils/renderActions'
import {
  listPipelines, createPipeline, deletePipeline, activatePipeline, deactivatePipeline,
  listPipelineHandlers,
  type Pipeline, type PipelineType,
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
const formName = ref('')
const formType = ref<PipelineType>('onnx')
const formConfigText = ref('{}')
const formHandler = ref<string | null>(null)

// Handlers (for internal type)
const handlers = ref<string[]>([])
const handlerOptions = computed(() => handlers.value.map(h => ({ label: h, value: h })))

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
})

function openCreate() {
  formName.value = ''
  formType.value = 'onnx'
  formConfigText.value = '{}'
  formHandler.value = null
  showForm.value = true
}

async function onSave() {
  if (!formName.value.trim()) {
    msg.error('Name is required')
    return
  }
  let config: Record<string, any> = {}
  try {
    config = JSON.parse(formConfigText.value || '{}')
  } catch {
    msg.error('Config must be valid JSON')
    return
  }
  if (formType.value === 'internal' && formHandler.value) {
    config.handler = formHandler.value
  }
  saving.value = true
  try {
    await createPipeline({ name: formName.value.trim(), type: formType.value, config })
    msg.success('Pipeline created')
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

const columns: DataTableColumns<Pipeline> = [
  { title: 'ID', key: 'id', width: 60, sorter: 'default' },
  { title: 'Name', key: 'name', sorter: 'default', ellipsis: { tooltip: true } },
  {
    title: 'Type', key: 'type', width: 100, sorter: 'default',
    render: (row) => h(NTag, { size: 'small', type: typeTagType[row.type] ?? 'default' },
      { default: () => row.type }),
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
    title: 'Actions', key: 'actions', width: 140,
    render: (row) => h(NSpace, { size: 4 }, {
      default: () => [
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

  <!-- Create Drawer -->
  <FormDrawer
    v-model:show="showForm"
    title="New Pipeline"
    save-label="Create"
    :loading="saving"
    @save="onSave"
  >
    <n-form-item label="Name">
      <n-input v-model:value="formName" placeholder="Pipeline name" />
    </n-form-item>
    <n-form-item label="Type">
      <n-select v-model:value="formType" :options="typeOptions" />
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

    <n-form-item label="Config (JSON)">
      <n-input
        v-model:value="formConfigText"
        type="textarea"
        :rows="5"
        placeholder="{}"
        style="font-family: monospace; font-size: 12px"
      />
    </n-form-item>
  </FormDrawer>
</template>
