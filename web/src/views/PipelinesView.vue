<script setup lang="ts">
import { ref, computed, onMounted, watch, h } from 'vue'
import {
  NFlex, NTabs, NTabPane, NTag, NSelect, NInput, NSwitch, NFormItem, NInputNumber, NDivider,
  NText, NSpace, NDataTable, NDynamicTags, NButton, NCard, NIcon, NSpin,
  NPagination, NModal, NRadioGroup, NRadio,
  useMessage,
  type DataTableColumns, type SelectOption,
} from 'naive-ui'
import {
  TrashOutline, PauseCircleOutline, CheckmarkCircleOutline, CreateOutline, ServerOutline, AddOutline,
  PlayCircleOutline, LogoTwitter, LinkOutline, UnlinkOutline,
} from '@vicons/ionicons5'
import { useRoute, useRouter } from 'vue-router'
import PageHeader from '../components/PageHeader.vue'
import FormDrawer from '../components/FormDrawer.vue'
import ActionBackendsModal from '../components/ActionBackendsModal.vue'
import { iconBtn, iconBtnConfirm, activeTag } from '../utils/renderActions'
import {
  listActions, createAction, updateAction, deleteAction, activateAction, deactivateAction,
  listActionHandlers, listAllExecutions,
  type Action, type ActionType, type ActionExecution,
} from '../api/actions'
import {
  listPipelines, createPipeline, deletePipeline, activatePipeline, deactivatePipeline,
  runPipeline, listModels, getPipelineGraph,
  type Pipeline, type ModelFile, type PipelineNode,
} from '../api/pipelines'
import {
  listTriggers, listTriggerPipelineLinks, createTriggerPipelineLink,
  deleteTriggerPipelineLink, activateTriggerPipelineLink, deactivateTriggerPipelineLink,
  type TriggerInfo, type TriggerPipelineLink,
} from '../api/trigger_links'
import { listChannels, listProviders, type NotificationChannel, type NotificationProvider } from '../api/notifications'
import { listChatTools } from '../api/chat'
import { useActionsStore } from '../stores/actions'

const route  = useRoute()
const router = useRouter()
const msg    = useMessage()

const actionsStore = useActionsStore()

// Active tab — driven by ?tab= query param
const activeTab = ref((route.query.tab as string) || 'actions')
function switchTab(tab: string) {
  activeTab.value = tab
  router.replace({ query: { tab } })
}

// ── Actions tab ───────────────────────────────────────────────────────────────

type ActionFormType = ActionType

const actionsLoading    = ref(false)
const actionsData       = ref<Action[]>([])
const filterText        = ref('')
const filterActiveOnly  = ref(false)
const showActionForm    = ref(false)
const savingAction      = ref(false)
const editingActionId   = ref<number | null>(null)
const formName          = ref('')
const formType          = ref<ActionFormType>('onnx')
const formUsecase       = ref<string>('custom')
const formModel         = ref('')
const formSystemPrompt  = ref('')
const formAllowedTools  = ref<string[]>([])
const formInputMode         = ref<string>('text_only')
const formContextActionIds  = ref<number[]>([])
const chatTools         = ref<{ name: string; description: string }[]>([])
const chatToolsLoading  = ref(false)
const formConfigText    = ref('{}')
const formHandler       = ref<string | null>(null)
const formPath          = ref('')
const formLabels        = ref<string[]>([])

// Transformers
interface TransformerSpec {
  type: string
  keys_field: string
  values_field: string
  method: string
  fields: string[]
  range_min: number | null
  range_max: number | null
  factor: number | null
  source: string
  min_score: number | null
  label_field: string
  word_field: string
  score_field: string
}
const formTransformers = ref<TransformerSpec[]>([])
const formMapCoordinateKey  = ref('')
const formMapCoordinateType = ref('array_of_pairs')
const formMapPopupTemplate  = ref('')

// entity_discovery-specific
const formDiscoveryMinConfidence      = ref(0.75)
const formDiscoveryMinFrequency       = ref(3)
const formDiscoveryAutoPromoteTypes   = ref<string[]>([])
const formDiscoveryEntityKey          = ref('entities')
const formDiscoveryStripLabelPrefixes = ref<string[]>([])

// onnx-specific
const formStripBioPrefix = ref(false)

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
  formTransformers.value.push({
    type: 'zip_arrays', keys_field: '', values_field: '',
    method: 'sum', fields: [], range_min: null, range_max: null,
    factor: null, source: '', min_score: null, label_field: '', word_field: '', score_field: '',
  })
}

// Handlers (for internal type)
const handlers = ref<string[]>([])
const handlerOptions = computed(() => handlers.value.map(h => ({ label: h, value: h })))
const chatToolOptions = computed(() =>
  chatTools.value.map(t => ({ label: t.name, value: t.name, description: t.description }))
)
const contextActionOptions = computed(() =>
  actionsData.value
    .filter(a => a.id !== editingActionId.value)
    .map(a => ({ label: `${a.name} (${a.type})`, value: a.id }))
)

// Models (for onnx type)
const models = ref<ModelFile[]>([])
const modelOptions = computed(() => models.value.map(m => ({ label: m.name, value: m.path })))

// Channels + providers (for notification type)
const channels = ref<NotificationChannel[]>([])
const providers = ref<NotificationProvider[]>([])
const channelOptions = computed(() => channels.value.map(c => ({ label: c.name, value: c.id })))
const formChannelId = ref<number | null>(null)
const formMessageTemplate = ref('')

// Pushover delivery
const formPushPriority = ref(0)
const formPushTitle    = ref<string | null>(null)
const formPushSound    = ref<string | null>(null)
const formPushUrl      = ref<string | null>(null)
const formPushUrlTitle = ref<string | null>(null)
const formPushTtl      = ref<number | null>(null)
const formPushHtml     = ref(false)
const formPushRetry    = ref<number | null>(null)
const formPushExpire   = ref<number | null>(null)

// Telegram delivery
const formTgParseMode             = ref<string | null>(null)
const formTgDisablePreview        = ref(false)
const formTgDisableNotification   = ref(false)
const formTgThreadId              = ref<number | null>(null)

const selectedNotifChannel = computed(() =>
  channels.value.find(c => c.id === formChannelId.value) ?? null
)
const selectedNotifProvider = computed(() =>
  providers.value.find(p => p.id === selectedNotifChannel.value?.provider_id) ?? null
)
const notifProviderType  = computed(() => selectedNotifProvider.value?.type ?? null)
const showPushEmergency  = computed(() => notifProviderType.value === 'pushover' && formPushPriority.value === 2)

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

const typeOptions: { label: string; value: ActionFormType }[] = [
  { label: 'ONNX',         value: 'onnx'         },
  { label: 'Ollama',       value: 'ollama'        },
  { label: 'Internal',     value: 'internal'      },
  { label: 'Notification', value: 'notification'  },
]

const typeTagType: Record<ActionFormType, 'default' | 'info' | 'success' | 'warning'> = {
  onnx:         'info',
  ollama:       'success',
  internal:     'warning',
  notification: 'default',
}

const filteredActions = computed(() => {
  let list = actionsData.value
  const q = filterText.value.trim().toLowerCase()
  if (q) list = list.filter(a => a.name.toLowerCase().includes(q))
  if (filterActiveOnly.value) list = list.filter(a => a.active)
  return list
})

async function fetchActions() {
  actionsLoading.value = true
  try {
    actionsData.value = await listActions()
  } catch (e: any) {
    msg.error(e.message)
  } finally {
    actionsLoading.value = false
  }
}

async function fetchChatTools() {
  if (chatTools.value.length > 0) return // already loaded
  chatToolsLoading.value = true
  try {
    chatTools.value = await listChatTools()
  } catch (e: any) {
    msg.error('Failed to load chat tools: ' + e.message)
  } finally {
    chatToolsLoading.value = false
  }
}

function resetActionForm() {
  editingActionId.value = null
  formName.value = ''
  formType.value = 'onnx'
  formUsecase.value = 'custom'
  formModel.value = ''
  formSystemPrompt.value = ''
  formAllowedTools.value = []
  formInputMode.value = 'text_only'
  formContextActionIds.value = []
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
  formMapPopupTemplate.value = ''
  formDiscoveryMinConfidence.value      = 0.75
  formDiscoveryMinFrequency.value       = 3
  formDiscoveryAutoPromoteTypes.value   = []
  formDiscoveryEntityKey.value          = 'entities'
  formDiscoveryStripLabelPrefixes.value = []
  formStripBioPrefix.value              = false
}

function openCreateAction() {
  resetActionForm()
  showActionForm.value = true
}

function openEditAction(row: Action) {
  resetActionForm()
  editingActionId.value = row.id
  formName.value = row.name
  formType.value = row.type
  const configWithoutMap = { ...(row.config ?? {}) }
  delete configWithoutMap.map_config
  formConfigText.value = JSON.stringify(configWithoutMap, null, 2)
  // Ollama-specific
  formModel.value = row.config?.model ?? ''
  formUsecase.value = row.config?.usecase ?? 'custom'
  formSystemPrompt.value = row.config?.system_prompt ?? ''
  formAllowedTools.value = Array.isArray(row.config?.allowed_tools) ? [...row.config.allowed_tools] : []
  formInputMode.value = row.config?.input_mode ?? 'text_only'
  formContextActionIds.value = Array.isArray(row.config?.context_action_ids)
    ? [...row.config.context_action_ids]
    : []
  if (row.type === 'ollama' && row.config?.usecase === 'chat') {
    fetchChatTools()
  }
  // Internal-specific
  formHandler.value = row.config?.handler ?? null
  formDiscoveryMinConfidence.value      = row.config?.min_confidence      ?? 0.75
  formDiscoveryMinFrequency.value       = row.config?.min_frequency       ?? 3
  formDiscoveryAutoPromoteTypes.value   = Array.isArray(row.config?.auto_promote_types)   ? [...row.config.auto_promote_types]   : []
  formDiscoveryEntityKey.value          = row.config?.entity_key          ?? 'entities'
  formDiscoveryStripLabelPrefixes.value = Array.isArray(row.config?.strip_label_prefixes) ? [...row.config.strip_label_prefixes] : []
  // ONNX-specific
  formPath.value = row.config?.path ?? ''
  formLabels.value = Array.isArray(row.config?.labels) ? [...row.config.labels] : []
  formStripBioPrefix.value = row.config?.strip_bio_prefix ?? false
  // Transformers
  formTransformers.value = Array.isArray(row.config?.transformers)
    ? row.config.transformers.map((t: any) => ({
        type: t.type ?? 'zip_arrays', keys_field: t.keys_field ?? '',
        values_field: t.values_field ?? '', method: t.method ?? 'sum',
        fields: Array.isArray(t.fields) ? t.fields : [],
        range_min: t.range_min ?? null, range_max: t.range_max ?? null,
        factor: t.factor ?? null, source: t.source ?? '',
        min_score: t.min_score ?? null, label_field: t.label_field ?? '',
        word_field: t.word_field ?? '', score_field: t.score_field ?? '',
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
  formMapPopupTemplate.value  = row.config?.map_config?.popup_template  ?? ''
  showActionForm.value = true
}

async function onSaveAction() {
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
    if (formSystemPrompt.value.trim()) {
      config.system_prompt = formSystemPrompt.value.trim()
    }
    if (formUsecase.value === 'chat' && formAllowedTools.value.length > 0) {
      config.allowed_tools = formAllowedTools.value
    }
    if (formUsecase.value !== 'chat' && formInputMode.value && formInputMode.value !== 'text_only') {
      config.input_mode = formInputMode.value
      if (formContextActionIds.value.length > 0) {
        config.context_action_ids = formContextActionIds.value
      }
    }
    // Only enforce uniqueness for digest (allow multiple chat actions)
    if (editingActionId.value === null && formUsecase.value === 'digest') {
      const conflict = actionsData.value.find(a => a.config?.usecase === 'digest')
      if (conflict) {
        msg.error(`A digest action already exists ("${conflict.name}"). Delete it first.`)
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
      if (formTgDisablePreview.value)       config.disable_web_page_preview = true
      if (formTgDisableNotification.value)  config.disable_notification     = true
      if (formTgThreadId.value)             config.message_thread_id        = formTgThreadId.value
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
      if (formLabels.value.length > 0) config.labels = formLabels.value
      if (formStripBioPrefix.value) config.strip_bio_prefix = true
    }
    if (formType.value === 'internal' && formHandler.value) {
      config.handler = formHandler.value
    }
    if (formHandler.value === 'entity_discovery') {
      config.min_confidence     = formDiscoveryMinConfidence.value
      config.min_frequency      = formDiscoveryMinFrequency.value
      config.auto_promote_types = formDiscoveryAutoPromoteTypes.value
      config.entity_key         = formDiscoveryEntityKey.value
      if (formDiscoveryStripLabelPrefixes.value.length > 0)
        config.strip_label_prefixes = formDiscoveryStripLabelPrefixes.value
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
          if (t.factor !== null)   spec.factor = t.factor
          if (t.fields.length > 0) spec.fields = t.fields
        } else if (t.type === 'group_entities') {
          if (t.source)            spec.source      = t.source
          if (t.min_score !== null) spec.min_score  = t.min_score
          if (t.label_field)       spec.label_field = t.label_field
          if (t.word_field)        spec.word_field  = t.word_field
          if (t.score_field)       spec.score_field = t.score_field
        }
        return spec
      })
  }

  if (formMapCoordinateKey.value.trim()) {
    config.map_config = {
      coordinate_key:  formMapCoordinateKey.value.trim(),
      coordinate_type: formMapCoordinateType.value,
      ...(formMapPopupTemplate.value.trim() ? { popup_template: formMapPopupTemplate.value.trim() } : {}),
    }
  }

  savingAction.value = true
  try {
    if (editingActionId.value !== null) {
      await updateAction(editingActionId.value, { name: formName.value.trim(), type: formType.value, config })
      msg.success('Action updated')
    } else {
      await createAction({ name: formName.value.trim(), type: formType.value, config })
      msg.success('Action created')
    }
    showActionForm.value = false
    await fetchActions()
  } catch (e: any) {
    msg.error(e.message)
  } finally {
    savingAction.value = false
  }
}

async function onActivateAction(id: number) {
  try { await activateAction(id); await fetchActions() } catch (e: any) { msg.error(e.message) }
}

async function onDeactivateAction(id: number) {
  try { await deactivateAction(id); await fetchActions() } catch (e: any) { msg.error(e.message) }
}

async function onDeleteAction(id: number) {
  try {
    await deleteAction(id)
    actionsData.value = actionsData.value.filter(a => a.id !== id)
  } catch (e: any) { msg.error(e.message) }
}

// Backend assignment modal
const backendModalAction = ref<Action | null>(null)
const showBackendModal   = ref(false)

function openBackendsModal(row: Action) {
  backendModalAction.value = row
  showBackendModal.value = true
}

const actionColumns: DataTableColumns<Action> = [
  { title: 'ID',   key: 'id',   width: 60,  sorter: 'default' },
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
        iconBtn(CreateOutline, 'Edit', () => openEditAction(row)),
        row.active
          ? iconBtn(PauseCircleOutline, 'Deactivate', () => onDeactivateAction(row.id))
          : iconBtn(CheckmarkCircleOutline, 'Activate', () => onActivateAction(row.id), { type: 'primary' }),
        iconBtnConfirm(TrashOutline, 'Delete', 'Delete this action?', () =>
          onDeleteAction(row.id), { type: 'error' }),
      ],
    }),
  },
]

// ── Pipelines tab ─────────────────────────────────────────────────────────────

const pipelinesData   = ref<Pipeline[]>([])
const pipelinesLoading = ref(false)

const showPipelineForm  = ref(false)
const savingPipeline    = ref(false)
const pipelineFormName  = ref('')
const pipelineFormDesc  = ref('')

const showRunModal      = ref(false)
const runPipelineRow    = ref<Pipeline | null>(null)
const runNewOnly        = ref(false)
const runOrder          = ref<'asc' | 'desc'>('desc')
const runningPipeline   = ref(false)
const runStartNodeId    = ref<number | null>(null)
const runGraphNodes     = ref<PipelineNode[]>([])
const runGraphLoading   = ref(false)

async function fetchPipelinesData() {
  pipelinesLoading.value = true
  try { pipelinesData.value = (await listPipelines()) ?? [] }
  catch (e: any) { msg.error(e.message) }
  finally { pipelinesLoading.value = false }
}

function openCreatePipeline() {
  pipelineFormName.value = ''
  pipelineFormDesc.value = ''
  showPipelineForm.value = true
}

async function onSavePipeline() {
  if (!pipelineFormName.value.trim()) { msg.error('Name is required'); return }
  savingPipeline.value = true
  try {
    const p = await createPipeline({ name: pipelineFormName.value, description: pipelineFormDesc.value })
    msg.success('Pipeline created')
    showPipelineForm.value = false
    router.push(`/pipelines/${p.id}/build`)
  } catch (e: any) { msg.error(e.message) }
  finally { savingPipeline.value = false }
}

async function openRunPipeline(row: Pipeline) {
  runPipelineRow.value = row
  runNewOnly.value = false
  runOrder.value = 'desc'
  runStartNodeId.value = null
  runGraphNodes.value = []
  showRunModal.value = true
  runGraphLoading.value = true
  const fetchFor = row.id
  try {
    const graph = await getPipelineGraph(row.id)
    if (runPipelineRow.value?.id !== fetchFor) return
    runGraphNodes.value = graph.nodes
      .filter(n => n.type === 'action' || n.type === 'pipeline')
      .sort((a, b) => a.pos_y !== b.pos_y ? a.pos_y - b.pos_y : a.pos_x - b.pos_x)
  } catch { /* non-fatal — picker stays empty */ }
  finally { runGraphLoading.value = false }
}

async function onConfirmRunPipeline(): Promise<false | void> {
  if (!runPipelineRow.value) return false
  runningPipeline.value = true
  try {
    const res = await runPipeline(
      runPipelineRow.value.id,
      runNewOnly.value,
      runStartNodeId.value ?? undefined,
      runOrder.value === 'asc' ? 'asc' : undefined,
    )
    msg.success(res.async
      ? `Pipeline "${runPipelineRow.value.name}" is running in the background`
      : `Queued ${res.queued} tweets for "${runPipelineRow.value.name}"`)
    showRunModal.value = false
  } catch (e: any) { msg.error(e.message) }
  finally { runningPipeline.value = false }
  return false
}

async function onActivatePipeline(id: number) {
  try {
    await activatePipeline(id)
    const p = pipelinesData.value.find(p => p.id === id)
    if (p) p.active = true
  } catch (e: any) { msg.error(e.message) }
}

async function onDeactivatePipeline(id: number) {
  try {
    await deactivatePipeline(id)
    const p = pipelinesData.value.find(p => p.id === id)
    if (p) p.active = false
  } catch (e: any) { msg.error(e.message) }
}

async function onDeletePipeline(id: number) {
  try {
    await deletePipeline(id)
    pipelinesData.value = pipelinesData.value.filter(p => p.id !== id)
  } catch (e: any) { msg.error(e.message) }
}

const pipelineColumns: DataTableColumns<Pipeline> = [
  { title: 'ID', key: 'id', width: 60, sorter: 'default' },
  { title: 'Name', key: 'name', sorter: 'default', ellipsis: { tooltip: true } },
  {
    title: 'Active', key: 'active', width: 80,
    render: (row) => activeTag(row.active),
  },
  {
    title: 'Nodes', key: 'node_count', width: 80,
    render: (row) => h(NTag, { size: 'small', round: true }, { default: () => String(row.node_count) }),
  },
  {
    title: 'Created', key: 'created_at', width: 160, sorter: 'default',
    render: (row) => new Date(row.created_at).toLocaleString(),
  },
  {
    title: 'Actions', key: 'actions', width: 240,
    render: (row) => h(NSpace, { size: 4 }, {
      default: () => [
        row.active
          ? iconBtn(PlayCircleOutline, 'Run', () => openRunPipeline(row), { type: 'primary' })
          : null,
        h(NButton, { size: 'tiny', onClick: () => router.push(`/pipelines/${row.id}/build`) }, {
          icon: () => h(NIcon, null, { default: () => h(CreateOutline) }),
          default: () => 'Build',
        }),
        row.active
          ? iconBtn(PauseCircleOutline, 'Deactivate', () => onDeactivatePipeline(row.id))
          : iconBtn(CheckmarkCircleOutline, 'Activate', () => onActivatePipeline(row.id), { type: 'primary' }),
        iconBtnConfirm(TrashOutline, 'Delete', 'Delete this pipeline?', () =>
          onDeletePipeline(row.id), { type: 'error' }),
      ],
    }),
  },
]

// Trigger-pipeline links
const triggers       = ref<TriggerInfo[]>([])
const links          = ref<TriggerPipelineLink[]>([])
const triggersLoading = ref(false)

const showLinkForm    = ref(false)
const linkFormTrigger = ref('')
const linkFormPipelineId = ref<number | null>(null)
const linkFormSourcePipelineId = ref<number | null>(null)
const savingLink      = ref(false)

const linksByTrigger = computed(() => {
  const map: Record<string, TriggerPipelineLink[]> = {}
  for (const l of links.value) {
    if (!map[l.trigger_type]) map[l.trigger_type] = []
    map[l.trigger_type].push(l)
  }
  return map
})

const pipelineSelectOptions = computed(() =>
  pipelinesData.value.map(p => ({ label: p.name, value: p.id }))
)

function openAddLink(triggerType: string) {
  linkFormTrigger.value = triggerType
  linkFormPipelineId.value = null
  linkFormSourcePipelineId.value = null
  showLinkForm.value = true
}

async function onSaveLink() {
  if (!linkFormPipelineId.value) { msg.error('Pipeline is required'); return }
  if (linkFormTrigger.value === 'pipeline_finished' && !linkFormSourcePipelineId.value) {
    msg.error('Source Pipeline is required for pipeline_finished trigger'); return
  }
  savingLink.value = true
  try {
    const body: { trigger_type: string; pipeline_id: number; source_pipeline_id?: number } = {
      trigger_type: linkFormTrigger.value,
      pipeline_id: linkFormPipelineId.value,
    }
    if (linkFormTrigger.value === 'pipeline_finished' && linkFormSourcePipelineId.value) {
      body.source_pipeline_id = linkFormSourcePipelineId.value
    }
    const l = await createTriggerPipelineLink(body)
    links.value.push(l)
    msg.success('Link created')
    showLinkForm.value = false
  } catch (e: any) { msg.error(e.message) }
  finally { savingLink.value = false }
}

async function removeLink(id: number) {
  try {
    await deleteTriggerPipelineLink(id)
    links.value = links.value.filter(l => l.id !== id)
  } catch (e: any) { msg.error(e.message) }
}

async function toggleLink(link: TriggerPipelineLink) {
  try {
    if (link.active) {
      await deactivateTriggerPipelineLink(link.id)
      link.active = false
    } else {
      await activateTriggerPipelineLink(link.id)
      link.active = true
    }
  } catch (e: any) { msg.error(e.message) }
}

function pipelineName(pipelineId: number) {
  return pipelinesData.value.find(p => p.id === pipelineId)?.name ?? `#${pipelineId}`
}

// ── Executions tab ────────────────────────────────────────────────────────────

const executions      = ref<ActionExecution[]>([])
const execsLoading    = ref(false)
const execsPage       = ref(1)
const execFilterFrom  = ref('')
const execFilterTo    = ref('')
const execFilterTweet = ref('')
const execFilterStatus = ref('')

async function loadExecutions() {
  execsLoading.value = true
  try {
    executions.value = await listAllExecutions({
      page: execsPage.value,
      per_page: 50,
      from:     execFilterFrom.value  || undefined,
      to:       execFilterTo.value    || undefined,
      tweet_id: execFilterTweet.value || undefined,
      status:   execFilterStatus.value || undefined,
    })
  } catch (e: any) { msg.error(e.message) }
  finally { execsLoading.value = false }
}

function onExecsPageChange(page: number) {
  execsPage.value = page
  loadExecutions()
}

const execColumns: DataTableColumns<ActionExecution> = [
  { title: 'ID',      key: 'id',        width: 60 },
  { title: 'Action',  key: 'action_id', width: 80 },
  { title: 'Tweet ID', key: 'tweet_id', width: 200, ellipsis: { tooltip: true } },
  {
    title: 'Status', key: 'status', width: 90,
    render: (row) => h(NTag, { size: 'small', type: row.status === 'success' ? 'success' : 'error' },
      { default: () => row.status }),
  },
  { title: 'Error', key: 'error', ellipsis: { tooltip: true } },
  {
    title: 'Executed', key: 'executed_at', width: 160,
    render: (row) => new Date(row.executed_at).toLocaleString(),
  },
]

// ── Mount ─────────────────────────────────────────────────────────────────────

onMounted(async () => {
  fetchActions()
  actionsStore.fetchTwitterStatus().catch(() => {})
  try { handlers.value = await listActionHandlers() } catch {}
  try { models.value = await listModels() } catch {}
  try { channels.value = await listChannels() } catch {}
  try { providers.value = await listProviders() } catch {}

  fetchPipelinesData()

  triggersLoading.value = true
  try {
    const [t, l] = await Promise.all([listTriggers(), listTriggerPipelineLinks()])
    triggers.value = t ?? []
    links.value    = l ?? []
  } catch (e: any) { msg.error(e.message) }
  finally { triggersLoading.value = false }

  if (activeTab.value === 'executions') {
    loadExecutions()
  }
})

watch(formUsecase, (val) => {
  if (val === 'chat' && formType.value === 'ollama') {
    fetchChatTools()
  }
})
</script>

<template>
  <PageHeader title="Pipelines" />

  <n-tabs :value="activeTab" type="line" animated style="margin-top: -8px;" @update:value="switchTab">

    <!-- ── Actions ────────────────────────────────────────────────────────── -->
    <n-tab-pane name="actions" tab="Actions">

      <!-- Twitter Connection -->
      <n-card size="small" style="margin-bottom: 12px;">
        <n-flex align="center" justify="space-between">
          <n-flex align="center" :size="8">
            <n-icon size="20" :component="LogoTwitter" />
            <template v-if="actionsStore.twitterLoading">
              <n-spin size="small" />
            </template>
            <template v-else-if="actionsStore.twitterStatus">
              <n-text>Connected as <n-text strong>@{{ actionsStore.twitterStatus.username }}</n-text></n-text>
            </template>
            <template v-else>
              <n-text depth="3">Twitter account not connected</n-text>
            </template>
          </n-flex>
          <n-button
            v-if="!actionsStore.twitterLoading && actionsStore.twitterStatus"
            size="small" type="error" ghost
            @click="actionsStore.disconnectTwitter().then(() => msg.success('Disconnected')).catch((e: any) => msg.error(e.message))"
          >
            <template #icon><n-icon><UnlinkOutline /></n-icon></template>
            Disconnect
          </n-button>
          <n-button
            v-else-if="!actionsStore.twitterLoading"
            size="small" type="primary"
            @click="actionsStore.startTwitterAuth().catch((e: any) => msg.error(e.message))"
          >
            <template #icon><n-icon><LinkOutline /></n-icon></template>
            Connect Twitter
          </n-button>
        </n-flex>
      </n-card>

      <!-- Toolbar -->
      <n-flex style="margin-bottom: 12px;" :size="12" align="center" justify="space-between">
        <n-flex :size="12" align="center">
          <n-input v-model:value="filterText" placeholder="Filter by name…" clearable style="width: 200px" />
          <n-switch v-model:value="filterActiveOnly" size="small" />
          <n-text depth="3" style="font-size: 13px">Active only</n-text>
        </n-flex>
        <n-button type="primary" size="small" @click="openCreateAction">New Action</n-button>
      </n-flex>

      <n-data-table
        :columns="actionColumns"
        :data="filteredActions"
        :loading="actionsLoading"
        :bordered="false"
        :row-key="(row: Action) => row.id"
        size="small"
      />
    </n-tab-pane>

    <!-- ── Pipelines ─────────────────────────────────────────────────────── -->
    <n-tab-pane name="pipelines" tab="Pipelines">
      <n-flex justify="end" style="margin-bottom: 12px;">
        <n-button type="primary" size="small" @click="openCreatePipeline">New Pipeline</n-button>
      </n-flex>

      <n-data-table
        :columns="pipelineColumns"
        :data="pipelinesData"
        :loading="pipelinesLoading"
        :bordered="false"
        :row-key="(row: Pipeline) => row.id"
        size="small"
        style="margin-bottom: 24px;"
      />

      <!-- Trigger links section -->
      <n-divider style="margin: 16px 0">Trigger → Pipeline Links</n-divider>
      <n-spin v-if="triggersLoading" />
      <div v-else>
        <n-card
          v-for="trigger in triggers"
          :key="trigger.type"
          size="small"
          style="margin-bottom: 12px;"
        >
          <n-flex align="center" justify="space-between" style="margin-bottom: 8px;">
            <div>
              <n-tag type="info" size="small" style="font-family: monospace; margin-right: 8px;">
                {{ trigger.type }}
              </n-tag>
              <n-text depth="3" style="font-size: 12px;">{{ trigger.description }}</n-text>
            </div>
            <n-button size="tiny" ghost type="primary" @click="openAddLink(trigger.type)">
              <template #icon><n-icon><AddOutline /></n-icon></template>
              Connect pipeline
            </n-button>
          </n-flex>

          <n-flex v-if="linksByTrigger[trigger.type]?.length" :size="6" wrap>
            <n-flex
              v-for="link in linksByTrigger[trigger.type]"
              :key="link.id"
              align="center" :size="4"
              style="
                background: #313244; border-radius: 4px; padding: 2px 6px;
                font-size: 12px;
              "
            >
              <n-switch :value="link.active" size="small" @update:value="toggleLink(link)" />
              <span :style="{ color: link.active ? '#cdd6f4' : '#6c7086' }">{{ link.source_pipeline_id ? `[${pipelineName(link.source_pipeline_id)}] → ` : '' }}{{ pipelineName(link.pipeline_id) }}</span>
              <n-button text size="tiny" style="color: #f38ba8;" @click="removeLink(link.id)">×</n-button>
            </n-flex>
          </n-flex>
          <n-text v-else depth="3" style="font-size: 12px;">No pipelines connected.</n-text>
        </n-card>
      </div>
    </n-tab-pane>

    <!-- ── Executions ────────────────────────────────────────────────────── -->
    <n-tab-pane name="executions" tab="Executions" @vue:mounted="loadExecutions">
      <n-flex style="margin-bottom: 12px;" :size="12" align="center" wrap>
        <n-input v-model:value="execFilterTweet" placeholder="Tweet ID…" clearable style="width: 180px" />
        <n-input v-model:value="execFilterFrom"  placeholder="From (ISO)…" clearable style="width: 180px" />
        <n-input v-model:value="execFilterTo"    placeholder="To (ISO)…"   clearable style="width: 180px" />
        <n-select
          v-model:value="execFilterStatus"
          :options="[{ label: 'All', value: '' }, { label: 'Success', value: 'success' }, { label: 'Error', value: 'error' }]"
          style="width: 120px"
        />
        <n-button size="small" type="primary" @click="() => { execsPage = 1; loadExecutions() }">Search</n-button>
      </n-flex>

      <n-data-table
        :columns="execColumns"
        :data="executions"
        :loading="execsLoading"
        :bordered="false"
        size="small"
      />
      <n-flex justify="end" style="margin-top: 12px">
        <n-pagination
          :page="execsPage"
          :page-size="50"
          :item-count="executions.length === 50 ? execsPage * 50 + 1 : (execsPage - 1) * 50 + executions.length"
          @update:page="onExecsPageChange"
        />
      </n-flex>
    </n-tab-pane>

  </n-tabs>

  <!-- Create / Edit Action Drawer -->
  <FormDrawer
    v-model:show="showActionForm"
    :title="editingActionId !== null ? 'Edit Action' : 'New Action'"
    :save-label="editingActionId !== null ? 'Save' : 'Create'"
    :loading="savingAction"
    @save="onSaveAction"
  >
    <n-form-item label="Name">
      <n-input v-model:value="formName" placeholder="Action name" />
    </n-form-item>
    <n-form-item label="Type">
      <n-select v-model:value="formType" :options="typeOptions" />
    </n-form-item>

    <!-- Handler dropdown for internal type -->
    <n-form-item v-if="formType === 'internal'" label="Handler">
      <n-select v-model:value="formHandler" :options="handlerOptions" placeholder="Select handler" clearable />
    </n-form-item>

    <!-- entity_discovery config -->
    <template v-if="formType === 'internal' && formHandler === 'entity_discovery'">
      <n-form-item label="Min Confidence (0–1)">
        <n-input-number v-model:value="formDiscoveryMinConfidence" :min="0" :max="1" :step="0.05" placeholder="0.75" style="width:160px" />
      </n-form-item>
      <n-form-item label="Min Frequency">
        <n-input-number v-model:value="formDiscoveryMinFrequency" :min="1" :step="1" placeholder="3" style="width:120px" />
      </n-form-item>
      <n-form-item label="Auto-Promote Types">
        <n-select
          v-model:value="formDiscoveryAutoPromoteTypes"
          multiple clearable
          :options="[
            { label: 'LOC — Location',     value: 'LOC'  },
            { label: 'PER — Person',       value: 'PER'  },
            { label: 'ORG — Organization', value: 'ORG'  },
            { label: 'MISC — Misc',        value: 'MISC' },
          ]"
          placeholder="None (all queue for review)"
        />
      </n-form-item>
      <n-form-item label="Entity Key">
        <n-input v-model:value="formDiscoveryEntityKey" placeholder="entities" />
      </n-form-item>
      <n-form-item label="Strip Label Prefixes">
        <n-flex vertical :size="4">
          <n-dynamic-tags v-model:value="formDiscoveryStripLabelPrefixes" />
          <n-text depth="3" style="font-size: 11px">
            Strip these prefixes from NER labels before storing entity types.
            Use <code>B-</code> and <code>I-</code> for BIO-tagged models (e.g. "I-LOC" → "LOC").
          </n-text>
        </n-flex>
      </n-form-item>
    </template>

    <!-- Ollama-specific -->
    <template v-if="formType === 'ollama'">
      <n-form-item label="Model">
        <n-input v-model:value="formModel" placeholder="e.g. llama3.2, mistral, gemma3" />
      </n-form-item>
      <n-form-item label="Use Case">
        <n-select v-model:value="formUsecase" :options="[
          { label: 'Custom (action)', value: 'custom' },
          { label: 'Digest',          value: 'digest' },
          { label: 'Chat',            value: 'chat'   },
        ]" />
      </n-form-item>
      <n-form-item label="System Prompt">
        <n-input
          v-model:value="formSystemPrompt"
          type="textarea" :rows="4"
          placeholder="Leave empty to use the default system prompt"
          style="font-family: monospace; font-size: 12px"
        />
      </n-form-item>
      <!-- Context injection (custom/digest usecase only) -->
      <template v-if="formUsecase !== 'chat'">
        <n-form-item label="Input Mode">
          <n-select
            v-model:value="formInputMode"
            :options="[
              { label: 'Text only (default)',  value: 'text_only'        },
              { label: 'Context only',         value: 'context_only'     },
              { label: 'Text + Context',       value: 'text_and_context' },
            ]"
          />
        </n-form-item>
        <n-form-item v-if="formInputMode !== 'text_only'" label="Context Actions">
          <n-select
            v-model:value="formContextActionIds"
            multiple
            filterable
            clearable
            :options="contextActionOptions"
            placeholder="Select upstream actions whose outputs to include"
          />
        </n-form-item>
        <n-text v-if="formInputMode !== 'text_only'" depth="3" style="font-size: 12px; margin-top: -8px; display: block">
          The selected actions' latest outputs for each tweet will be passed to the LLM in a &lt;context&gt; block.
          Built-in tweet variables (tweet_id, tweet_url, tweet_author, tweet_short) are always included.
        </n-text>
      </template>
      <!-- Chat tools selector (chat usecase only) -->
      <template v-if="formUsecase === 'chat'">
        <n-divider>Chat Tools</n-divider>
        <n-form-item label="Allowed Tools">
          <n-select
            v-model:value="formAllowedTools"
            multiple
            filterable
            clearable
            :options="chatToolOptions"
            :loading="chatToolsLoading"
            placeholder="Leave empty to allow all tools"
          />
        </n-form-item>
        <n-text depth="3" style="font-size: 12px; margin-top: -8px; display: block">
          Restrict the tools available to this chat assistant. Leave empty to allow all {{ chatTools.length }} tools.
        </n-text>
      </template>
    </template>

    <!-- Notification pipeline fields -->
    <template v-else-if="formType === 'notification'">
      <n-form-item label="Channel">
        <n-select v-model:value="formChannelId" :options="channelOptions" placeholder="Select a notification channel" filterable clearable />
      </n-form-item>
      <n-form-item label="Message Template">
        <n-input
          v-model:value="formMessageTemplate"
          type="textarea" :rows="3"
          placeholder="e.g. ${tweet} — by @${author}"
          style="font-family: monospace; font-size: 12px"
        />
      </n-form-item>
      <n-text depth="3" style="font-size:12px; margin-top:-8px; display:block">
        Available vars: ${tweet}, ${author}, ${tweet_id}, ${tweet_url}, plus any action execution key.
      </n-text>

      <template v-if="notifProviderType === 'pushover'">
        <n-divider>Pushover delivery options</n-divider>
        <n-form-item label="Priority">
          <n-select v-model:value="formPushPriority" :options="pushPriorityOptions" />
        </n-form-item>
        <n-form-item label="Title (optional)"><n-input v-model:value="formPushTitle" clearable /></n-form-item>
        <n-form-item label="Sound (optional)"><n-input v-model:value="formPushSound" clearable /></n-form-item>
        <n-form-item label="URL (optional)"><n-input v-model:value="formPushUrl" clearable /></n-form-item>
        <n-form-item label="URL Title (optional)"><n-input v-model:value="formPushUrlTitle" clearable /></n-form-item>
        <n-form-item label="TTL seconds (optional)"><n-input-number v-model:value="formPushTtl" clearable /></n-form-item>
        <n-form-item label="HTML"><n-switch v-model:value="formPushHtml" /></n-form-item>
        <template v-if="showPushEmergency">
          <n-form-item label="Retry (≥ 30s)"><n-input-number v-model:value="formPushRetry" :min="30" /></n-form-item>
          <n-form-item label="Expire (1–10800s)"><n-input-number v-model:value="formPushExpire" :min="1" :max="10800" /></n-form-item>
        </template>
      </template>

      <template v-if="notifProviderType === 'telegram'">
        <n-divider>Telegram delivery options</n-divider>
        <n-form-item label="Parse Mode (optional)">
          <n-select v-model:value="formTgParseMode" :options="tgParseModeOptions" />
        </n-form-item>
        <n-form-item label="Message Thread ID (optional)"><n-input-number v-model:value="formTgThreadId" clearable /></n-form-item>
        <n-form-item label="Disable Web Page Preview"><n-switch v-model:value="formTgDisablePreview" /></n-form-item>
        <n-form-item label="Silent (no sound)"><n-switch v-model:value="formTgDisableNotification" /></n-form-item>
      </template>
    </template>

    <!-- Config JSON for non-Ollama, non-notification types -->
    <template v-else>
      <n-form-item v-if="formType === 'onnx'" label="Model">
        <n-select v-model:value="formPath" :options="modelOptions" placeholder="Select a model file" filterable clearable />
      </n-form-item>
      <n-form-item v-if="formType === 'onnx'" label="Labels">
        <n-dynamic-tags v-model:value="formLabels" />
      </n-form-item>
      <n-form-item v-if="formType === 'onnx'" label="BIO Label Stripping">
        <n-checkbox v-model:checked="formStripBioPrefix">
          Strip B-/I- prefixes from NER labels (for models outputting "I-LOC", "B-PER" style labels)
        </n-checkbox>
      </n-form-item>
      <n-form-item label="Config (JSON)">
        <n-input
          v-model:value="formConfigText"
          type="textarea" :rows="5"
          placeholder="{}"
          style="font-family: monospace; font-size: 12px"
        />
      </n-form-item>
    </template>

    <!-- Map Output (hidden for notification type) -->
    <template v-if="formType !== 'notification'">
      <n-divider style="margin: 4px 0">Map Output <span style="font-size:11px;color:#6c7086">(optional)</span></n-divider>
      <n-form-item label="Coordinate key">
        <n-input v-model:value="formMapCoordinateKey" placeholder="e.g. geo_coordinates" clearable />
      </n-form-item>
      <n-form-item v-if="formMapCoordinateKey.trim()" label="Coordinate type">
        <n-select v-model:value="formMapCoordinateType" :options="coordinateTypeOptions" />
      </n-form-item>
      <n-form-item v-if="formMapCoordinateKey.trim()" label="Popup template (optional)">
        <n-input
          v-model:value="formMapPopupTemplate"
          type="textarea" :rows="3"
          placeholder="${description}&#10;Confidence: ${confidence}"
          style="font-family: monospace; font-size: 12px"
        />
      </n-form-item>
      <n-text v-if="formMapCoordinateKey.trim()" depth="3" style="font-size:12px; margin-top:-8px; display:block">
        Use <code>${key}</code> for any execution metadata key. Built-ins: ${tweet_id}, ${tweet_created_at}, ${model_name}. Leave blank for default layout.
      </n-text>
    </template>

    <!-- Transformers (hidden for notification type) -->
    <template v-if="formType !== 'notification'">
      <n-divider style="margin: 4px 0" />
      <n-form-item label="Transformers">
        <n-flex vertical style="width: 100%; gap: 8px">
          <n-card v-for="(t, i) in formTransformers" :key="i" size="small" style="background: var(--n-color-embedded)">
            <n-flex align="center" style="margin-bottom: 8px">
              <n-select v-model:value="t.type" :options="transformerTypeOptions" style="flex: 1" />
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
    </template>
  </FormDrawer>

  <!-- Action Backend Assignments Modal -->
  <ActionBackendsModal
    v-if="backendModalAction"
    v-model:show="showBackendModal"
    :action-id="backendModalAction.id"
    :action-name="backendModalAction.name"
    @saved="fetchActions"
  />

  <!-- New Pipeline Drawer -->
  <FormDrawer v-model:show="showPipelineForm" title="New Pipeline" :loading="savingPipeline" @save="onSavePipeline">
    <n-form-item label="Name">
      <n-input v-model:value="pipelineFormName" placeholder="Pipeline name" />
    </n-form-item>
    <n-form-item label="Description">
      <n-input v-model:value="pipelineFormDesc" placeholder="Optional description" type="textarea" :rows="2" />
    </n-form-item>
  </FormDrawer>

  <!-- Run Pipeline Modal -->
  <n-modal
    v-model:show="showRunModal"
    preset="dialog"
    :title="`Run pipeline — ${runPipelineRow?.name}`"
    positive-text="Run"
    negative-text="Cancel"
    :loading="runningPipeline"
    @positive-click="onConfirmRunPipeline"
  >
    <n-space vertical :size="12" style="margin-top: 8px;">
      <n-form-item label="Start from node" :show-feedback="false">
        <n-select
          v-model:value="runStartNodeId"
          :loading="runGraphLoading"
          :options="runGraphNodes.map(n => ({
            label: n.label || `Node #${n.id}`,
            value: n.id,
          }))"
          placeholder="Start of pipeline (default)"
          clearable
        />
      </n-form-item>
      <n-text v-if="runStartNodeId" depth="3" style="font-size: 12px;">
        Prior detection results for each tweet will be available as context.
      </n-text>
      <n-radio-group v-model:value="runNewOnly">
        <n-space vertical :size="8">
          <n-radio :value="false">All eligible tweets</n-radio>
          <n-radio :value="true">
            {{ runStartNodeId
              ? "Only tweets not yet processed by the selected node's action"
              : "Only tweets without existing executions for this pipeline's actions" }}
          </n-radio>
        </n-space>
      </n-radio-group>
      <n-radio-group v-model:value="runOrder">
        <n-space vertical :size="8">
          <n-radio value="desc">Newest first (default)</n-radio>
          <n-radio value="asc">Oldest first</n-radio>
        </n-space>
      </n-radio-group>
    </n-space>
  </n-modal>

  <!-- Connect Pipeline to Trigger Drawer -->
  <FormDrawer
    v-model:show="showLinkForm"
    :title="`Connect pipeline to ${linkFormTrigger}`"
    save-label="Connect"
    :loading="savingLink"
    @save="onSaveLink"
  >
    <n-form-item
      v-if="linkFormTrigger === 'pipeline_finished'"
      label="Source Pipeline"
    >
      <n-select
        v-model:value="linkFormSourcePipelineId"
        :options="pipelineSelectOptions"
        placeholder="Pipeline that must finish first"
        clearable
      />
    </n-form-item>
    <n-form-item label="Pipeline">
      <n-select
        v-model:value="linkFormPipelineId"
        :options="pipelineSelectOptions"
        placeholder="Select a pipeline"
        clearable
      />
    </n-form-item>
  </FormDrawer>
</template>
