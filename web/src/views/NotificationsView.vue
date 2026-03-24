<!-- web/src/views/NotificationsView.vue -->
<script setup lang="ts">
import { h, onMounted, ref, computed } from 'vue'
import { useNotificationsStore } from '../stores/notifications'
import { useRulesStore } from '../stores/rules'
import { useMessage, NButton, NSpace, NTag } from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import type { Notification } from '../api/notifications'
import { VALID_EVENT_TYPES } from '../api/notifications'

const store      = useNotificationsStore()
const rulesStore = useRulesStore()
const msg        = useMessage()

const showDrawer  = ref(false)
const editingN    = ref<Notification | null>(null)
const saving      = ref(false)

// Local form type uses `null` for optional fields (Naive UI expects `null`, not `undefined`)
interface NotificationForm {
  name: string
  event_type: string
  active: boolean
  filter_rule_id: number | null
  filter_model_name: string | null
  filter_model_result: unknown
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
  message_template: string | null
}

const defaultForm = (): NotificationForm => ({
  name: '', event_type: 'tweet_matched', active: true,
  filter_rule_id: null, filter_model_name: null, filter_model_result: null,
  pushover_priority: 0, pushover_html: false,
  pushover_sound: null, pushover_device: null, pushover_title: null,
  pushover_url: null, pushover_url_title: null, pushover_ttl: null,
  pushover_retry: null, pushover_expire: null, message_template: null,
})
const form = ref<NotificationForm>(defaultForm())

onMounted(() => {
  store.fetchNotifications()
  rulesStore.fetchRules()
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
    message_template:      n.message_template      ?? null,
  }
  showDrawer.value = true
}

function validateForm(): string | null {
  if (!form.value.name) return 'Name is required'
  if (form.value.pushover_priority === 2) {
    if (!form.value.pushover_retry || form.value.pushover_retry < 30) return 'Retry must be ≥ 30 for emergency priority'
    if (!form.value.pushover_expire || form.value.pushover_expire < 1 || form.value.pushover_expire > 10800)
      return 'Expire must be 1–10800 for emergency priority'
  }
  return null
}

// Convert the form (null for empty) to the API shape (undefined for empty)
function toApiPayload() {
  const f = form.value
  return {
    name:                f.name,
    event_type:          f.event_type,
    filter_rule_id:      f.filter_rule_id      ?? undefined,
    filter_model_name:   f.filter_model_name   ?? undefined,
    filter_model_result: f.filter_model_result ?? undefined,
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

const eventTypeOptions = VALID_EVENT_TYPES.map(t => ({ label: t, value: t }))
const priorityOptions  = [
  { label: '0 — Normal',    value: 0 },
  { label: '1 — High',      value: 1 },
  { label: '2 — Emergency', value: 2 },
]

const ruleOptions = computed(() => [
  { label: '(none)', value: null },
  ...rulesStore.rules.map(r => ({ label: r.name, value: r.id })),
])

const showEmergencyFields = computed(() => form.value.pushover_priority === 2)

const columns: DataTableColumns<Notification> = [
  { title: 'ID',         key: 'id',         width: 60 },
  { title: 'Name',       key: 'name' },
  { title: 'Event Type', key: 'event_type' },
  {
    title: 'Active', key: 'active', width: 80,
    render: (row) => h(NTag, { type: row.active ? 'success' : 'default', size: 'small' },
      { default: () => row.active ? 'Yes' : 'No' })
  },
  {
    title: 'Actions', key: 'actions',
    render: (row) => h(NSpace, {}, {
      default: () => [
        h(NButton, { size: 'small', onClick: () => onTest(row.id) }, { default: () => 'Test' }),
        h(NButton, { size: 'small', onClick: () => openEdit(row) }, { default: () => 'Edit' }),
        row.active
          ? h(NButton, { size: 'small', onClick: () => store.deactivate(row.id).catch((e: any) => msg.error(e.message)) }, { default: () => 'Deactivate' })
          : h(NButton, { size: 'small', type: 'primary', onClick: () => store.activate(row.id).catch((e: any) => msg.error(e.message)) }, { default: () => 'Activate' }),
        h(NButton, { size: 'small', type: 'error', onClick: () => store.remove(row.id).catch((e: any) => msg.error(e.message)) }, { default: () => 'Delete' }),
      ]
    })
  },
]
</script>

<template>
  <n-space vertical size="large">
    <n-space justify="space-between">
      <n-h2>Notifications</n-h2>
      <n-button type="primary" @click="openCreate">+ New Notification</n-button>
    </n-space>

    <n-data-table :columns="columns" :data="store.notifications" :loading="store.loading" />

    <n-drawer v-model:show="showDrawer" :width="480">
      <n-drawer-content :title="editingN ? 'Edit Notification' : 'New Notification'" closable>
        <n-form label-placement="top">
          <n-form-item label="Name">
            <n-input v-model:value="form.name" />
          </n-form-item>
          <n-form-item label="Event Type">
            <n-select v-model:value="form.event_type" :options="eventTypeOptions" />
          </n-form-item>
          <n-form-item label="Filter Rule (optional)">
            <n-select v-model:value="form.filter_rule_id" :options="ruleOptions" clearable />
          </n-form-item>
          <n-form-item label="Filter Model Name (optional)">
            <n-input v-model:value="form.filter_model_name" clearable />
          </n-form-item>

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

          <n-form-item label="Message Template (optional)">
            <n-input v-model:value="form.message_template" type="textarea" :rows="3" clearable />
          </n-form-item>
        </n-form>
        <template #footer>
          <n-button type="primary" :loading="saving" @click="onSave">Save</n-button>
        </template>
      </n-drawer-content>
    </n-drawer>
  </n-space>
</template>
