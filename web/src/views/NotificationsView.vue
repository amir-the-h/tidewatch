<!-- web/src/views/NotificationsView.vue -->
<script setup lang="ts">
import { h, onMounted, ref, computed } from 'vue'
import { useNotificationsStore } from '../stores/notifications'
import { useRulesStore } from '../stores/rules'
import { useModelsStore } from '../stores/models'
import { useMessage, NButton, NSpace } from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import type { Notification } from '../api/notifications'
import { VALID_EVENT_TYPES } from '../api/notifications'
import { FlashOutline, CreateOutline, CheckmarkCircleOutline, PauseCircleOutline, TrashOutline } from '@vicons/ionicons5'
import { iconBtn, activeTag } from '../utils/renderActions'

const store       = useNotificationsStore()
const rulesStore  = useRulesStore()
const modelsStore = useModelsStore()
const msg         = useMessage()

const filterText       = ref('')
const filterEventType  = ref<string | null>(null)
const filterProvider   = ref<string | null>(null)
const filterActiveOnly = ref(false)

const filteredNotifications = computed(() => {
  let list = store.notifications
  const q = filterText.value.trim().toLowerCase()
  if (q) list = list.filter(n => n.name.toLowerCase().includes(q))
  if (filterEventType.value) list = list.filter(n => n.event_type === filterEventType.value)
  if (filterProvider.value) list = list.filter(n => n.provider === filterProvider.value)
  if (filterActiveOnly.value) list = list.filter(n => n.active)
  return list
})

const showDrawer  = ref(false)
const editingN    = ref<Notification | null>(null)
const saving      = ref(false)

// Local form type uses `null` for optional fields (Naive UI expects `null`, not `undefined`)
interface NotificationForm {
  name: string
  event_type: string
  active: boolean
  provider: string
  filter_rule_id: number | null
  filter_model_name: string | null
  filter_model_result: unknown
  // Pushover
  pushover_priority: number
  pushover_sound: string | null
  pushover_device: string | null
  pushover_title: string | null
  pushover_url: string | null
  pushover_url_title: string | null
  pushover_ttl: number | null
  pushover_html: boolean
  pushover_retry: number | null
  pushover_expire: number | null
  // Telegram
  telegram_chat_id: string | null
  telegram_parse_mode: string | null
  telegram_disable_web_page_preview: boolean
  telegram_disable_notification: boolean
  telegram_message_thread_id: number | null
  message_template: string | null
}

const defaultForm = (): NotificationForm => ({
  name: '', event_type: 'tweet_matched', active: true,
  provider: 'pushover',
  filter_rule_id: null, filter_model_name: null, filter_model_result: null,
  pushover_priority: 0, pushover_html: false,
  pushover_sound: null, pushover_device: null, pushover_title: null,
  pushover_url: null, pushover_url_title: null, pushover_ttl: null,
  pushover_retry: null, pushover_expire: null,
  telegram_chat_id: null, telegram_parse_mode: null,
  telegram_disable_web_page_preview: false, telegram_disable_notification: false,
  telegram_message_thread_id: null,
  message_template: null,
})
const form = ref<NotificationForm>(defaultForm())

onMounted(() => {
  store.fetchNotifications()
  rulesStore.fetchRules()
  modelsStore.fetchModels()
})

function openCreate() {
  editingN.value = null
  form.value = defaultForm()
  showDrawer.value = true
}

function openEdit(n: Notification) {
  editingN.value = n
  form.value = {
    name:                  n.name,
    event_type:            n.event_type,
    active:                n.active,
    provider:              n.provider || 'pushover',
    filter_rule_id:        n.filter_rule_id        ?? null,
    filter_model_name:     n.filter_model_name     ?? null,
    filter_model_result:   n.filter_model_result   ?? null,
    pushover_priority:     n.pushover_priority,
    pushover_sound:        n.pushover_sound        ?? null,
    pushover_device:       n.pushover_device       ?? null,
    pushover_title:        n.pushover_title        ?? null,
    pushover_url:          n.pushover_url          ?? null,
    pushover_url_title:    n.pushover_url_title    ?? null,
    pushover_ttl:          n.pushover_ttl          ?? null,
    pushover_html:         n.pushover_html,
    pushover_retry:        n.pushover_retry        ?? null,
    pushover_expire:       n.pushover_expire       ?? null,
    telegram_chat_id:      n.telegram_chat_id      ?? null,
    telegram_parse_mode:   n.telegram_parse_mode   ?? null,
    telegram_disable_web_page_preview: n.telegram_disable_web_page_preview,
    telegram_disable_notification:     n.telegram_disable_notification,
    telegram_message_thread_id: n.telegram_message_thread_id ?? null,
    message_template:      n.message_template      ?? null,
  }
  showDrawer.value = true
}

function validateForm(): string | null {
  if (!form.value.name) return 'Name is required'
  if (form.value.provider === 'pushover' && form.value.pushover_priority === 2) {
    if (!form.value.pushover_retry || form.value.pushover_retry < 30) return 'Retry must be ≥ 30 for emergency priority'
    if (!form.value.pushover_expire || form.value.pushover_expire < 1 || form.value.pushover_expire > 10800)
      return 'Expire must be 1–10800 for emergency priority'
  }
  if (form.value.provider === 'telegram' && !form.value.telegram_chat_id) {
    return 'Chat ID is required for Telegram'
  }
  return null
}

// Convert the form (null for empty) to the API shape (undefined for empty)
function toApiPayload() {
  const f = form.value
  return {
    name:                f.name,
    event_type:          f.event_type,
    provider:            f.provider,
    filter_rule_id:      f.filter_rule_id      ?? undefined,
    filter_model_name:   f.filter_model_name   ?? undefined,
    filter_model_result: f.filter_model_result ?? undefined,
    // Pushover
    pushover_priority:   f.pushover_priority,
    pushover_sound:      f.pushover_sound      ?? undefined,
    pushover_device:     f.pushover_device     ?? undefined,
    pushover_title:      f.pushover_title      ?? undefined,
    pushover_url:        f.pushover_url        ?? undefined,
    pushover_url_title:  f.pushover_url_title  ?? undefined,
    pushover_ttl:        f.pushover_ttl        ?? undefined,
    pushover_html:       f.pushover_html,
    pushover_retry:      f.pushover_retry      ?? undefined,
    pushover_expire:     f.pushover_expire     ?? undefined,
    // Telegram
    telegram_chat_id:    f.telegram_chat_id    ?? undefined,
    telegram_parse_mode: f.telegram_parse_mode ?? undefined,
    telegram_disable_web_page_preview: f.telegram_disable_web_page_preview,
    telegram_disable_notification:     f.telegram_disable_notification,
    telegram_message_thread_id: f.telegram_message_thread_id ?? undefined,
    message_template:    f.message_template    ?? undefined,
  }
}

async function onSave() {
  const err = validateForm()
  if (err) { msg.error(err); return }
  saving.value = true
  try {
    if (editingN.value) {
      await store.updateNotification(editingN.value.id, toApiPayload())
    } else {
      await store.createNotification(toApiPayload())
    }
    msg.success('Saved')
    showDrawer.value = false
  } catch (e: any) { msg.error(e.message) }
  finally { saving.value = false }
}

async function onTest(id: number) {
  try { await store.test(id); msg.success('Test sent') }
  catch (e: any) { msg.error(e.message) }
}

const eventTypeOptions  = VALID_EVENT_TYPES.map(t => ({ label: t, value: t }))
const providerOptions   = [
  { label: 'Pushover', value: 'pushover' },
  { label: 'Telegram', value: 'telegram' },
]
const priorityOptions   = [
  { label: '0 — Normal',    value: 0 },
  { label: '1 — High',      value: 1 },
  { label: '2 — Emergency', value: 2 },
]
const parseModeOptions  = [
  { label: '(none)',       value: null },
  { label: 'HTML',         value: 'HTML' },
  { label: 'Markdown',     value: 'Markdown' },
  { label: 'MarkdownV2',   value: 'MarkdownV2' },
]

const ruleOptions = computed(() => [
  { label: '(none)', value: null },
  ...rulesStore.rules.map(r => ({ label: r.name, value: r.id })),
])

const modelOptions = computed(() => [
  { label: '(none)', value: null },
  ...modelsStore.models.map(m => ({ label: m.name, value: m.name })),
])

const isPushover         = computed(() => form.value.provider === 'pushover')
const isTelegram         = computed(() => form.value.provider === 'telegram')
const showEmergencyFields = computed(() => isPushover.value && form.value.pushover_priority === 2)

const columns: DataTableColumns<Notification> = [
  { title: 'ID',         key: 'id',         width: 60,  sorter: 'default' },
  { title: 'Name',       key: 'name',                   sorter: 'default' },
  { title: 'Event Type', key: 'event_type',             sorter: 'default' },
  { title: 'Provider',   key: 'provider',   width: 100, sorter: 'default' },
  { title: 'Active', key: 'active', width: 80, sorter: (a, b) => Number(a.active) - Number(b.active), render: (row) => activeTag(row.active) },
  {
    title: 'Actions', key: 'actions',
    render: (row) => h(NSpace, { size: 4 }, {
      default: () => [
        iconBtn(FlashOutline,  'Send test', () => onTest(row.id)),
        iconBtn(CreateOutline, 'Edit',      () => openEdit(row)),
        row.active
          ? iconBtn(PauseCircleOutline,     'Deactivate', () => store.deactivate(row.id).catch((e: any) => msg.error(e.message)))
          : iconBtn(CheckmarkCircleOutline, 'Activate',   () => store.activate(row.id).catch((e: any) => msg.error(e.message)), { type: 'primary' }),
        iconBtn(TrashOutline, 'Delete', () => store.remove(row.id).catch((e: any) => msg.error(e.message)), { type: 'error' }),
      ]
    })
  },
]
</script>

<template>
  <n-space vertical size="large">
    <PageHeader title="Notifications" add-label="New Notification" @add="openCreate" />

    <n-flex align="center" :size="8" wrap>
      <n-input v-model:value="filterText" placeholder="Search by name…" clearable style="width: 220px" />
      <n-select v-model:value="filterEventType" :options="[{ label: 'All events', value: null }, ...eventTypeOptions]" clearable placeholder="All events" style="width: 200px" />
      <n-select v-model:value="filterProvider" :options="[{ label: 'All providers', value: null }, ...providerOptions]" clearable placeholder="All providers" style="width: 160px" />
      <n-checkbox v-model:checked="filterActiveOnly">Active only</n-checkbox>
    </n-flex>

    <n-data-table :columns="columns" :data="filteredNotifications" :loading="store.loading" :scroll-x="660" />

    <FormDrawer
      v-model:show="showDrawer"
      :title="editingN ? 'Edit Notification' : 'New Notification'"
      width="min(560px, calc(100vw - 16px))"
      :loading="saving"
      @save="onSave"
    >
          <n-form-item label="Name">
            <n-input v-model:value="form.name" />
          </n-form-item>
          <n-form-item label="Event Type">
            <n-select v-model:value="form.event_type" :options="eventTypeOptions" />
          </n-form-item>
          <n-form-item label="Provider">
            <n-select v-model:value="form.provider" :options="providerOptions" />
          </n-form-item>
          <n-form-item label="Filter Rule (optional)">
            <n-select v-model:value="form.filter_rule_id" :options="ruleOptions" clearable />
          </n-form-item>
          <n-form-item label="Filter Model (optional)">
            <n-select v-model:value="form.filter_model_name" :options="modelOptions" clearable />
          </n-form-item>

          <!-- Pushover fields -->
          <template v-if="isPushover">
            <n-divider>Pushover</n-divider>

            <n-form-item label="Priority">
              <n-select v-model:value="form.pushover_priority" :options="priorityOptions" />
            </n-form-item>
            <n-form-item label="Sound (optional)">
              <n-input v-model:value="form.pushover_sound" clearable />
            </n-form-item>
            <n-form-item label="Device (optional)">
              <n-input v-model:value="form.pushover_device" clearable />
            </n-form-item>
            <n-form-item label="Title (optional)">
              <n-input v-model:value="form.pushover_title" clearable />
            </n-form-item>
            <n-form-item label="URL (optional)">
              <n-input v-model:value="form.pushover_url" clearable />
            </n-form-item>
            <n-form-item label="URL Title (optional)">
              <n-input v-model:value="form.pushover_url_title" clearable />
            </n-form-item>
            <n-form-item label="TTL (optional)">
              <n-input-number v-model:value="form.pushover_ttl" clearable />
            </n-form-item>
            <n-form-item label="HTML">
              <n-switch v-model:value="form.pushover_html" />
            </n-form-item>

            <!-- Emergency-only fields -->
            <template v-if="showEmergencyFields">
              <n-form-item label="Retry (≥ 30s, required for emergency)">
                <n-input-number v-model:value="form.pushover_retry" :min="30" />
              </n-form-item>
              <n-form-item label="Expire (1–10800s, required for emergency)">
                <n-input-number v-model:value="form.pushover_expire" :min="1" :max="10800" />
              </n-form-item>
            </template>
          </template>

          <!-- Telegram fields -->
          <template v-if="isTelegram">
            <n-divider>Telegram</n-divider>

            <n-form-item label="Chat ID">
              <n-input v-model:value="form.telegram_chat_id" placeholder="e.g. -1001234567890" />
            </n-form-item>
            <n-form-item label="Parse Mode (optional)">
              <n-select v-model:value="form.telegram_parse_mode" :options="parseModeOptions" />
            </n-form-item>
            <n-form-item label="Message Thread ID (optional)">
              <n-input-number v-model:value="form.telegram_message_thread_id" clearable />
            </n-form-item>
            <n-form-item label="Disable Web Page Preview">
              <n-switch v-model:value="form.telegram_disable_web_page_preview" />
            </n-form-item>
            <n-form-item label="Silent (disable notification sound)">
              <n-switch v-model:value="form.telegram_disable_notification" />
            </n-form-item>
          </template>

          <n-form-item label="Message Template (optional)">
            <n-input v-model:value="form.message_template" type="textarea" :rows="3" clearable />
          </n-form-item>
    </FormDrawer>
  </n-space>
</template>
