<script setup lang="ts">
import { ref, computed, onMounted, h } from 'vue'
import {
  NFlex, NTag, NSelect, NInput, NSwitch, NFormItem, NInputNumber, NDivider,
  NText, NSpace, NDataTable, NDynamicTags, NButton, NCard, NIcon,
  useMessage,
  type DataTableColumns, type SelectOption,
} from 'naive-ui'
import {
  TrashOutline, PauseCircleOutline, CheckmarkCircleOutline, CreateOutline, ServerOutline, AddOutline,
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
import { listChannels, listProviders, type NotificationChannel, type NotificationProvider } from '../api/notifications'

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
const formLabels = ref<string[]>([])

// Transformers (all pipeline types)
interface TransformerSpec {
  type: string
  // zip_arrays
  keys_field: string
  values_field: string
  // normalize
  method: string
  fields: string[]
  range_min: number | null
  range_max: number | null
  // multiply
  factor: number | null
  // group_entities
  source: string
  min_score: number | null
  label_field: string
  word_field: string
  score_field: string
}
const formTransformers = ref<TransformerSpec[]>([])
const formMapCoordinateKey  = ref('')
const formMapCoordinateType = ref('array_of_pairs')
const transformerTypeOptions = [
  { label: 'Zip Arrays',     value: 'zip_arrays'     },
  { label: 'Normalize',      value: 'normalize'      },
  { label: 'Multiply',       value: 'multiply'       },
  { label: 'Group Entities', value: 'group_entities' },
]
const coordinateTypeOptions = [
  { label: 'Array of pairs  [[lat,lon],...]',           value: 'array_of_pairs'   },
  { label: 'Single pair  [lat,lon]',                    value: 'single_pair'      },
  { label: 'Object  {"lat":x,"lon":x}',                 value: 'object'           },
  { label: 'Array of objects  [{"lat":x,"lon":x},...]', value: 'array_of_objects' },
  { label: 'GeoJSON Point',                             value: 'geojson_point'    },
]
const normalizeMethodOptions = [
  { label: 'Sum (probabilities)',  value: 'sum'     },
  { label: 'Min-Max ([0, 1])',     value: 'minmax'  },
  { label: 'Softmax',              value: 'softmax' },
]
function addTransformer() {
  formTransformers.value.push({ type: 'zip_arrays', keys_field: '', values_field: '', method: 'sum', fields: [], range_min: null, range_max: null, factor: null, source: '', min_score: null, label_field: '', word_field: '', score_field: '' })
}

// Handlers (for internal type)
const handlers = ref<string[]>([])
const handlerOptions = computed(() => handlers.value.map(h => ({ label: h, value: h })))

// Models (for onnx type)
const models = ref<ModelFile[]>([])
const modelOptions = computed(() => models.value.map(m => ({ label: m.name, value: m.path })))

// Channels + providers (for notification type)
const channels = ref<NotificationChannel[]>([])
const providers = ref<NotificationProvider[]>([])
const channelOptions = computed(() => channels.value.map(c => ({ label: c.name, value: c.id })))
const formChannelId = ref<number | null>(null)
const formMessageTemplate = ref('')

// Notification delivery options — Pushover
const formPushPriority = ref(0)
const formPushTitle = ref<string | null>(null)
const formPushSound = ref<string | null>(null)
const formPushUrl = ref<string | null>(null)
const formPushUrlTitle = ref<string | null>(null)
const formPushTtl = ref<number | null>(null)
const formPushHtml = ref(false)
const formPushRetry = ref<number | null>(null)
const formPushExpire = ref<number | null>(null)

// Notification delivery options — Telegram
const formTgParseMode = ref<string | null>(null)
const formTgDisablePreview = ref(false)
const formTgDisableNotification = ref(false)
const formTgThreadId = ref<number | null>(null)

const selectedNotifChannel = computed(() =>
  channels.value.find(c => c.id === formChannelId.value) ?? null
)
const selectedNotifProvider = computed(() =>
  providers.value.find(p => p.id === selectedNotifChannel.value?.provider_id) ?? null
)
const notifProviderType = computed(() => selectedNotifProvider.value?.type ?? null)
const showPushEmergency = computed(() => notifProviderType.value === 'pushover' && formPushPriority.value === 2)

const pushPriorityOptions = [
  { label: '0 — Normal',    value: 0 },
  { label: '1 — High',      value: 1 },
  { label: '2 — Emergency', value: 2 },
]
const tgParseModeOptions: SelectOption[] = [
  { label: '(none)',     value: '' },
  { label: 'HTML',       value: 'HTML' },
  { label: 'Markdown',   value: 'Markdown' },
  { label: 'MarkdownV2', value: 'MarkdownV2' },
]

const typeOptions: { label: string; value: PipelineType }[] = [
  { label: 'ONNX', value: 'onnx' },
  { label: 'Ollama', value: 'ollama' },
  { label: 'Internal', value: 'internal' },
  { label: 'Notification', value: 'notification' },
]

const typeTagType: Record<PipelineType, 'default' | 'info' | 'success' | 'warning'> = {
  onnx: 'info',
  ollama: 'success',
  internal: 'warning',
  notification: 'default',
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
  try { channels.value = await listChannels() } catch {}
  try { providers.value = await listProviders() } catch {}
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
  formLabels.value = []
  formTransformers.value = []
  formChannelId.value = null
  formMessageTemplate.value = '${tweet}'
  formPushPriority.value = 0; formPushTitle.value = null; formPushSound.value = null
  formPushUrl.value = null; formPushUrlTitle.value = null; formPushTtl.value = null
  formPushHtml.value = false; formPushRetry.value = null; formPushExpire.value = null
  formTgParseMode.value = null; formTgDisablePreview.value = false
  formTgDisableNotification.value = false; formTgThreadId.value = null
  formMapCoordinateKey.value = ''
  formMapCoordinateType.value = 'array_of_pairs'
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
  formLabels.value = Array.isArray(row.config?.labels) ? [...row.config.labels] : []
  // Transformers (all types)
  formTransformers.value = Array.isArray(row.config?.transformers)
    ? row.config.transformers.map((t: any) => ({
        type: t.type ?? 'zip_arrays',
        keys_field: t.keys_field ?? '',
        values_field: t.values_field ?? '',
        method: t.method ?? 'sum',
        fields: Array.isArray(t.fields) ? t.fields : [],
        range_min: t.range_min ?? null,
        range_max: t.range_max ?? null,
        factor: t.factor ?? null,
        source: t.source ?? '',
        min_score: t.min_score ?? null,
        label_field: t.label_field ?? '',
        word_field: t.word_field ?? '',
        score_field: t.score_field ?? '',
      }))
    : []
  // Notification-specific
  formChannelId.value = row.config?.channel_id ?? null
  formMessageTemplate.value = row.config?.message_template ?? '${tweet}'
  formPushPriority.value = row.config?.priority ?? 0
  formPushTitle.value = row.config?.title ?? null
  formPushSound.value = row.config?.sound ?? null
  formPushUrl.value = row.config?.url ?? null
  formPushUrlTitle.value = row.config?.url_title ?? null
  formPushTtl.value = row.config?.ttl ?? null
  formPushHtml.value = row.config?.html ?? false
  formPushRetry.value = row.config?.retry ?? null
  formPushExpire.value = row.config?.expire ?? null
  formTgParseMode.value = row.config?.parse_mode ?? null
  formTgDisablePreview.value = row.config?.disable_web_page_preview ?? false
  formTgDisableNotification.value = row.config?.disable_notification ?? false
  formTgThreadId.value = row.config?.message_thread_id ?? null
  formMapCoordinateKey.value  = row.config?.map_config?.coordinate_key  ?? ''
  formMapCoordinateType.value = row.config?.map_config?.coordinate_type ?? 'array_of_pairs'
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
  } else if (formType.value === 'notification') {
    if (formChannelId.value == null) {
      msg.error('Channel is required')
      return
    }
    if (!formMessageTemplate.value.trim()) {
      msg.error('Message template is required')
      return
    }
    config.channel_id = formChannelId.value
    config.message_template = formMessageTemplate.value.trim()
    if (notifProviderType.value === 'pushover') {
      config.priority = formPushPriority.value
      config.html = formPushHtml.value
      if (formPushTitle.value)    config.title     = formPushTitle.value
      if (formPushSound.value)    config.sound     = formPushSound.value
      if (formPushUrl.value)      config.url       = formPushUrl.value
      if (formPushUrlTitle.value) config.url_title = formPushUrlTitle.value
      if (formPushTtl.value)      config.ttl       = formPushTtl.value
      if (formPushPriority.value === 2) {
        if (!formPushRetry.value || formPushRetry.value < 30) {
          msg.error('Retry must be ≥ 30 for emergency priority'); return
        }
        if (!formPushExpire.value || formPushExpire.value < 1) {
          msg.error('Expire is required for emergency priority'); return
        }
        config.retry  = formPushRetry.value
        config.expire = formPushExpire.value
      }
    } else if (notifProviderType.value === 'telegram') {
      if (formTgParseMode.value && formTgParseMode.value !== '')
        config.parse_mode = formTgParseMode.value
      if (formTgDisablePreview.value)    config.disable_web_page_preview  = true
      if (formTgDisableNotification.value) config.disable_notification    = true
      if (formTgThreadId.value)          config.message_thread_id         = formTgThreadId.value
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
      if (formLabels.value.length > 0) {
        config.labels = formLabels.value
      }
    }
    if (formType.value === 'internal' && formHandler.value) {
      config.handler = formHandler.value
    }
  }

  if (formTransformers.value.length > 0) {
    config.transformers = formTransformers.value
      .filter(t => t.type)
      .map(t => {
        const spec: Record<string, any> = { type: t.type }
        if (t.type === 'zip_arrays') {
          if (t.keys_field)   spec.keys_field   = t.keys_field
          if (t.values_field) spec.values_field = t.values_field
        } else if (t.type === 'normalize') {
          if (t.method && t.method !== 'sum') spec.method = t.method
          if (t.fields.length > 0)           spec.fields = t.fields
          if (t.method === 'minmax') {
            if (t.range_min !== null) spec.range_min = t.range_min
            if (t.range_max !== null) spec.range_max = t.range_max
          }
        } else if (t.type === 'multiply') {
          if (t.factor !== null) spec.factor = t.factor
          if (t.fields.length > 0) spec.fields = t.fields
        } else if (t.type === 'group_entities') {
          if (t.source)      spec.source      = t.source
          if (t.min_score !== null) spec.min_score = t.min_score
          if (t.label_field) spec.label_field = t.label_field
          if (t.word_field)  spec.word_field  = t.word_field
          if (t.score_field) spec.score_field = t.score_field
        }
        return spec
      })
  }

  if (formMapCoordinateKey.value.trim()) {
    config.map_config = {
      coordinate_key:  formMapCoordinateKey.value.trim(),
      coordinate_type: formMapCoordinateType.value,
    }
  }

  saving.value = true
  try {
    if (editingId.value !== null) {
      await updatePipeline(editingId.value, { name: formName.value.trim(), type: formType.value, config })
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
    title: 'Actions', key: 'actions', width: 200,
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

      <!-- Notification pipeline fields -->
      <template v-else-if="formType === 'notification'">
        <n-form-item label="Channel">
          <n-select
            v-model:value="formChannelId"
            :options="channelOptions"
            placeholder="Select a notification channel"
            filterable
            clearable
          />
        </n-form-item>
        <n-form-item label="Message Template">
          <n-input
            v-model:value="formMessageTemplate"
            type="textarea"
            :rows="3"
            placeholder="e.g. ${tweet} — by @${author}"
            style="font-family: monospace; font-size: 12px"
          />
        </n-form-item>
        <n-text depth="3" style="font-size:12px; margin-top:-8px; display:block">
          Available vars: ${tweet}, ${author}, ${tweet_id}, ${tweet_url}, plus any action execution key.
        </n-text>

        <!-- Pushover delivery options -->
        <template v-if="notifProviderType === 'pushover'">
          <n-divider>Pushover delivery options</n-divider>
          <n-form-item label="Priority">
            <n-select v-model:value="formPushPriority" :options="pushPriorityOptions" />
          </n-form-item>
          <n-form-item label="Title (optional)">
            <n-input v-model:value="formPushTitle" clearable />
          </n-form-item>
          <n-form-item label="Sound (optional)">
            <n-input v-model:value="formPushSound" clearable />
          </n-form-item>
          <n-form-item label="URL (optional)">
            <n-input v-model:value="formPushUrl" clearable />
          </n-form-item>
          <n-form-item label="URL Title (optional)">
            <n-input v-model:value="formPushUrlTitle" clearable />
          </n-form-item>
          <n-form-item label="TTL seconds (optional)">
            <n-input-number v-model:value="formPushTtl" clearable />
          </n-form-item>
          <n-form-item label="HTML">
            <n-switch v-model:value="formPushHtml" />
          </n-form-item>
          <template v-if="showPushEmergency">
            <n-form-item label="Retry (≥ 30s)">
              <n-input-number v-model:value="formPushRetry" :min="30" />
            </n-form-item>
            <n-form-item label="Expire (1–10800s)">
              <n-input-number v-model:value="formPushExpire" :min="1" :max="10800" />
            </n-form-item>
          </template>
        </template>

        <!-- Telegram delivery options -->
        <template v-if="notifProviderType === 'telegram'">
          <n-divider>Telegram delivery options</n-divider>
          <n-form-item label="Parse Mode (optional)">
            <n-select v-model:value="formTgParseMode" :options="tgParseModeOptions" />
          </n-form-item>
          <n-form-item label="Message Thread ID (optional)">
            <n-input-number v-model:value="formTgThreadId" clearable />
          </n-form-item>
          <n-form-item label="Disable Web Page Preview">
            <n-switch v-model:value="formTgDisablePreview" />
          </n-form-item>
          <n-form-item label="Silent (no sound)">
            <n-switch v-model:value="formTgDisableNotification" />
          </n-form-item>
        </template>
      </template>

      <!-- Config JSON for non-Ollama, non-notification types -->
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
        <n-form-item v-if="formType === 'onnx'" label="Labels">
          <n-dynamic-tags v-model:value="formLabels" />
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

      <!-- Transformers — available for all pipeline types -->
      <n-divider style="margin: 4px 0" />
      <n-form-item label="Transformers">
        <n-flex vertical style="width: 100%; gap: 8px">
          <n-card
            v-for="(t, i) in formTransformers"
            :key="i"
            size="small"
            style="background: var(--n-color-embedded)"
          >
            <n-flex align="center" style="margin-bottom: 8px">
              <n-select
                v-model:value="t.type"
                :options="transformerTypeOptions"
                style="flex: 1"
              />
              <n-button text type="error" @click="formTransformers.splice(i, 1)">
                <template #icon><n-icon><TrashOutline /></n-icon></template>
              </n-button>
            </n-flex>
            <template v-if="t.type === 'zip_arrays'">
              <n-flex>
                <n-form-item label="Keys field" style="flex: 1; margin-bottom: 0">
                  <n-input v-model:value="t.keys_field" placeholder="labels" />
                </n-form-item>
                <n-form-item label="Values field" style="flex: 1; margin-bottom: 0">
                  <n-input v-model:value="t.values_field" placeholder="scores" />
                </n-form-item>
              </n-flex>
            </template>
            <template v-else-if="t.type === 'multiply'">
              <n-flex>
                <n-form-item label="Factor" style="flex: 1; margin-bottom: 0">
                  <n-input-number v-model:value="t.factor" placeholder="e.g. 100" style="width: 100%" />
                </n-form-item>
                <n-form-item label="Fields (empty = all numeric)" style="flex: 2; margin-bottom: 0">
                  <n-dynamic-tags v-model:value="t.fields" />
                </n-form-item>
              </n-flex>
            </template>
            <template v-else-if="t.type === 'normalize'">
              <n-flex>
                <n-form-item label="Method" style="flex: 1; margin-bottom: 0">
                  <n-select v-model:value="t.method" :options="normalizeMethodOptions" />
                </n-form-item>
                <n-form-item label="Fields (empty = all numeric)" style="flex: 2; margin-bottom: 0">
                  <n-dynamic-tags v-model:value="t.fields" />
                </n-form-item>
              </n-flex>
              <n-flex v-if="t.method === 'minmax'" style="margin-top: 8px">
                <n-form-item label="Range min" style="flex: 1; margin-bottom: 0">
                  <n-input-number v-model:value="t.range_min" placeholder="0" style="width: 100%" />
                </n-form-item>
                <n-form-item label="Range max" style="flex: 1; margin-bottom: 0">
                  <n-input-number v-model:value="t.range_max" placeholder="1" style="width: 100%" />
                </n-form-item>
              </n-flex>
            </template>
            <template v-else-if="t.type === 'group_entities'">
              <n-flex>
                <n-form-item label="Source field" style="flex: 1; margin-bottom: 0">
                  <n-input v-model:value="t.source" placeholder="entities" />
                </n-form-item>
                <n-form-item label="Min score" style="flex: 1; margin-bottom: 0">
                  <n-input-number v-model:value="t.min_score" :min="0" :max="1" :step="0.05" placeholder="0.8" style="width: 100%" />
                </n-form-item>
              </n-flex>
              <n-flex style="margin-top: 8px">
                <n-form-item label="Label field" style="flex: 1; margin-bottom: 0">
                  <n-input v-model:value="t.label_field" placeholder="label" />
                </n-form-item>
                <n-form-item label="Word field" style="flex: 1; margin-bottom: 0">
                  <n-input v-model:value="t.word_field" placeholder="word" />
                </n-form-item>
                <n-form-item label="Score field" style="flex: 1; margin-bottom: 0">
                  <n-input v-model:value="t.score_field" placeholder="score" />
                </n-form-item>
              </n-flex>
            </template>
          </n-card>
          <n-button dashed style="width: 100%" @click="addTransformer">
            <template #icon><n-icon><AddOutline /></n-icon></template>
            Add Transformer
          </n-button>
        </n-flex>
      </n-form-item>
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
