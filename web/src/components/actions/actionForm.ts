// Create/edit form state for an Action, and the mapping between it and the saved `config` JSON.
// The per-type field components in this folder edit an ActionFormState; buildActionConfig turns it back
// into the config the API stores.
import type { Action, ActionType } from '../../api/actions'

export interface TransformerSpec {
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

export interface ActionFormState {
  name: string
  type: ActionType
  /** Raw JSON for onnx/internal actions; the typed fields below take precedence on save. */
  configText: string
  transformers: TransformerSpec[]
  mapCoordinateKey: string
  mapCoordinateType: string
  mapPopupTemplate: string

  // onnx
  path: string | null
  labels: string[]
  stripBioPrefix: boolean

  // ollama
  model: string
  usecase: string
  systemPrompt: string
  /** 'null' (model default), 'true' or 'false'. */
  think: string
  allowedTools: string[]
  inputMode: string
  contextActionIds: number[]

  // internal
  handler: string | null
  discordTagChannelId: number | null
  discordTagNamesKey: string
  discoveryMinConfidence: number | null
  discoveryMinFrequency: number | null
  discoveryAutoPromoteTypes: string[]
  discoveryEntityKey: string
  discoveryStripLabelPrefixes: string[]

  // notification
  channelId: number | null
  messageTemplate: string
  threadNameTemplate: string
  pushPriority: number
  pushTitle: string
  pushSound: string
  pushUrl: string
  pushUrlTitle: string
  pushTtl: number | null
  pushHtml: boolean
  pushRetry: number | null
  pushExpire: number | null
  tgParseMode: string
  tgDisablePreview: boolean
  tgDisableNotification: boolean
  tgThreadId: number | null
}

export type NotificationProviderType = 'pushover' | 'telegram' | 'discord' | string | null

export function emptyTransformer(): TransformerSpec {
  return {
    type: 'zip_arrays', keys_field: '', values_field: '',
    method: 'sum', fields: [], range_min: null, range_max: null,
    factor: null, source: '', min_score: null, label_field: '', word_field: '', score_field: '',
  }
}

export function emptyActionForm(): ActionFormState {
  return {
    name: '',
    type: 'onnx',
    configText: '{}',
    transformers: [],
    mapCoordinateKey: '',
    mapCoordinateType: 'array_of_pairs',
    mapPopupTemplate: '',
    path: null,
    labels: [],
    stripBioPrefix: false,
    model: '',
    usecase: 'custom',
    systemPrompt: '',
    think: 'null',
    allowedTools: [],
    inputMode: 'text_only',
    contextActionIds: [],
    handler: null,
    discordTagChannelId: null,
    discordTagNamesKey: 'discord_tag_names',
    discoveryMinConfidence: 0.75,
    discoveryMinFrequency: 3,
    discoveryAutoPromoteTypes: [],
    discoveryEntityKey: 'entities',
    discoveryStripLabelPrefixes: [],
    channelId: null,
    messageTemplate: '${tweet}',
    threadNameTemplate: '',
    pushPriority: 0,
    pushTitle: '',
    pushSound: '',
    pushUrl: '',
    pushUrlTitle: '',
    pushTtl: null,
    pushHtml: false,
    pushRetry: null,
    pushExpire: null,
    tgParseMode: '',
    tgDisablePreview: false,
    tgDisableNotification: false,
    tgThreadId: null,
  }
}

export function actionFormFrom(row: Action): ActionFormState {
  const c = row.config ?? {}
  const configWithoutMap = { ...c }
  delete configWithoutMap.map_config
  return {
    ...emptyActionForm(),
    name: row.name,
    type: row.type,
    configText: JSON.stringify(configWithoutMap, null, 2),
    transformers: Array.isArray(c.transformers)
      ? c.transformers.map((t: Partial<TransformerSpec>) => ({
          type: t.type ?? 'zip_arrays', keys_field: t.keys_field ?? '',
          values_field: t.values_field ?? '', method: t.method ?? 'sum',
          fields: Array.isArray(t.fields) ? t.fields : [],
          range_min: t.range_min ?? null, range_max: t.range_max ?? null,
          factor: t.factor ?? null, source: t.source ?? '',
          min_score: t.min_score ?? null, label_field: t.label_field ?? '',
          word_field: t.word_field ?? '', score_field: t.score_field ?? '',
        }))
      : [],
    mapCoordinateKey: c.map_config?.coordinate_key ?? '',
    mapCoordinateType: c.map_config?.coordinate_type ?? 'array_of_pairs',
    mapPopupTemplate: c.map_config?.popup_template ?? '',
    path: c.path ?? null,
    labels: Array.isArray(c.labels) ? [...c.labels] : [],
    stripBioPrefix: c.strip_bio_prefix ?? false,
    model: c.model ?? '',
    usecase: c.usecase ?? 'custom',
    systemPrompt: c.system_prompt ?? '',
    think: typeof c.think === 'boolean' ? String(c.think) : 'null',
    allowedTools: Array.isArray(c.allowed_tools) ? [...c.allowed_tools] : [],
    inputMode: c.input_mode ?? 'text_only',
    contextActionIds: Array.isArray(c.context_action_ids) ? [...c.context_action_ids] : [],
    handler: c.handler ?? null,
    discordTagChannelId: c.channel_id ?? null,
    discordTagNamesKey: c.tag_names_key ?? 'discord_tag_names',
    discoveryMinConfidence: c.min_confidence ?? 0.75,
    discoveryMinFrequency: c.min_frequency ?? 3,
    discoveryAutoPromoteTypes: Array.isArray(c.auto_promote_types) ? [...c.auto_promote_types] : [],
    discoveryEntityKey: c.entity_key ?? 'entities',
    discoveryStripLabelPrefixes: Array.isArray(c.strip_label_prefixes) ? [...c.strip_label_prefixes] : [],
    channelId: c.channel_id ?? null,
    messageTemplate: c.message_template ?? '${tweet}',
    threadNameTemplate: c.thread_name_template ?? '',
    pushPriority: c.priority ?? 0,
    pushTitle: c.title ?? '',
    pushSound: c.sound ?? '',
    pushUrl: c.url ?? '',
    pushUrlTitle: c.url_title ?? '',
    pushTtl: c.ttl ?? null,
    pushHtml: c.html ?? false,
    pushRetry: c.retry ?? null,
    pushExpire: c.expire ?? null,
    tgParseMode: c.parse_mode ?? '',
    tgDisablePreview: c.disable_web_page_preview ?? false,
    tgDisableNotification: c.disable_notification ?? false,
    tgThreadId: c.message_thread_id ?? null,
  }
}

/** Builds the config JSON to save, or returns the first validation error. */
export function buildActionConfig(
  f: ActionFormState,
  providerType: NotificationProviderType,
): { config: Record<string, unknown> } | { error: string } {
  let config: Record<string, unknown> = {}

  if (f.type === 'ollama') {
    if (!f.model.trim()) return { error: 'Enter a model name' }
    config.model = f.model.trim()
    config.usecase = f.usecase
    if (f.systemPrompt.trim()) config.system_prompt = f.systemPrompt.trim()
    if (f.usecase === 'chat' && f.allowedTools.length > 0) config.allowed_tools = f.allowedTools
    if (f.think !== 'null') config.think = f.think === 'true'
    if (f.usecase !== 'chat' && f.inputMode && f.inputMode !== 'text_only') {
      config.input_mode = f.inputMode
      if (f.contextActionIds.length > 0) config.context_action_ids = f.contextActionIds
    }
  } else if (f.type === 'notification') {
    if (f.channelId == null) return { error: 'Choose a channel' }
    if (!f.messageTemplate.trim()) return { error: 'Enter a message template' }
    config.channel_id = f.channelId
    config.message_template = f.messageTemplate.trim()
    if (providerType === 'pushover') {
      config.priority = f.pushPriority
      config.html = f.pushHtml
      if (f.pushTitle)    config.title     = f.pushTitle
      if (f.pushSound)    config.sound     = f.pushSound
      if (f.pushUrl)      config.url       = f.pushUrl
      if (f.pushUrlTitle) config.url_title = f.pushUrlTitle
      if (f.pushTtl)      config.ttl       = f.pushTtl
      if (f.pushPriority === 2) {
        if (!f.pushRetry || f.pushRetry < 30) return { error: 'Retry must be at least 30 seconds for emergency priority' }
        if (!f.pushExpire || f.pushExpire < 1) return { error: 'Enter an expiry for emergency priority' }
        config.retry  = f.pushRetry
        config.expire = f.pushExpire
      }
    } else if (providerType === 'telegram') {
      if (f.tgParseMode)           config.parse_mode               = f.tgParseMode
      if (f.tgDisablePreview)      config.disable_web_page_preview = true
      if (f.tgDisableNotification) config.disable_notification     = true
      if (f.tgThreadId)            config.message_thread_id        = f.tgThreadId
    } else if (providerType === 'discord') {
      if (!f.threadNameTemplate.trim()) return { error: 'Enter a thread name template for Discord' }
      config.thread_name_template = f.threadNameTemplate.trim()
    }
  } else {
    let parsed: unknown
    try {
      parsed = JSON.parse(f.configText || '{}')
    } catch {
      return { error: 'Config must be valid JSON' }
    }
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return { error: 'Config must be a JSON object' }
    config = parsed as Record<string, unknown>
    // Keys the typed fields own are dropped from the JSON first, so clearing a field removes its key.
    delete config.transformers
    delete config.map_config
    if (f.type === 'onnx') {
      delete config.labels
      delete config.strip_bio_prefix
    }
    if (f.type === 'internal' && f.handler === 'discord_tag_thread') delete config.tag_names_key
    if (f.type === 'internal' && f.handler === 'entity_discovery') delete config.strip_label_prefixes
    if (f.type === 'onnx') {
      if (!f.path?.trim()) return { error: 'Choose a model file' }
      config.path = f.path.trim()
      if (f.labels.length > 0) config.labels = f.labels
      if (f.stripBioPrefix) config.strip_bio_prefix = true
    }
    if (f.type === 'internal' && f.handler) config.handler = f.handler
    if (f.type === 'internal' && f.handler === 'discord_tag_thread') {
      if (!f.discordTagChannelId) return { error: 'Choose a Discord channel' }
      config.channel_id = f.discordTagChannelId
      if (f.discordTagNamesKey && f.discordTagNamesKey !== 'discord_tag_names')
        config.tag_names_key = f.discordTagNamesKey
    }
    if (f.type === 'internal' && f.handler === 'entity_discovery') {
      config.min_confidence     = f.discoveryMinConfidence
      config.min_frequency      = f.discoveryMinFrequency
      config.auto_promote_types = f.discoveryAutoPromoteTypes
      config.entity_key         = f.discoveryEntityKey
      if (f.discoveryStripLabelPrefixes.length > 0)
        config.strip_label_prefixes = f.discoveryStripLabelPrefixes
    }
  }

  if (f.type !== 'notification' && f.transformers.length > 0) {
    config.transformers = f.transformers
      .filter(t => t.type)
      .map(t => {
        const spec: Record<string, unknown> = { type: t.type }
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
          if (t.source)             spec.source      = t.source
          if (t.min_score !== null) spec.min_score   = t.min_score
          if (t.label_field)        spec.label_field = t.label_field
          if (t.word_field)         spec.word_field  = t.word_field
          if (t.score_field)        spec.score_field = t.score_field
        }
        return spec
      })
  }

  if (f.type !== 'notification' && f.mapCoordinateKey.trim()) {
    config.map_config = {
      coordinate_key:  f.mapCoordinateKey.trim(),
      coordinate_type: f.mapCoordinateType,
      ...(f.mapPopupTemplate.trim() ? { popup_template: f.mapPopupTemplate.trim() } : {}),
    }
  }

  return { config }
}
