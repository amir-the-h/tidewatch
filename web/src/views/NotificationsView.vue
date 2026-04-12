<!-- web/src/views/NotificationsView.vue -->
<script setup lang="ts">
import { h, onMounted, ref, computed } from 'vue'
import { useNotificationsStore } from '../stores/notifications'
import { useMessage, NSpace } from 'naive-ui'
import type { DataTableColumns } from 'naive-ui'
import type { NotificationProvider, NotificationChannel } from '../api/notifications'
import { CreateOutline, CheckmarkCircleOutline, PauseCircleOutline, TrashOutline } from '@vicons/ionicons5'
import { iconBtn, activeTag } from '../utils/renderActions'

const store = useNotificationsStore()
const msg   = useMessage()

const activeTab = ref<'providers' | 'channels'>('providers')

onMounted(() => {
  store.fetchProviders()
  store.fetchChannels()
})

// ── Providers ──────────────────────────────────────────────────────────────────

const showProviderDrawer = ref(false)
const editingProvider    = ref<NotificationProvider | null>(null)
const savingProvider     = ref(false)

interface ProviderForm {
  name: string
  type: 'pushover' | 'telegram' | 'discord'
  // pushover
  pushover_app_token: string
  pushover_user_key:  string
  // telegram
  telegram_bot_token: string
  // discord
  discord_bot_token: string
}

const defaultProviderForm = (): ProviderForm => ({
  name: '', type: 'pushover',
  pushover_app_token: '', pushover_user_key: '',
  telegram_bot_token: '',
  discord_bot_token: '',
})
const providerForm = ref<ProviderForm>(defaultProviderForm())

const isPushoverProvider = computed(() => providerForm.value.type === 'pushover')
const isTelegramProvider = computed(() => providerForm.value.type === 'telegram')
const isDiscordProvider  = computed(() => providerForm.value.type === 'discord')

function openCreateProvider() {
  editingProvider.value = null
  providerForm.value    = defaultProviderForm()
  showProviderDrawer.value = true
}

function openEditProvider(p: NotificationProvider) {
  editingProvider.value = p
  providerForm.value    = {
    name: p.name,
    type: p.type,
    pushover_app_token:  p.type === 'pushover' ? (p.config.app_token   ?? '') : '',
    pushover_user_key:   p.type === 'pushover' ? (p.config.user_key    ?? '') : '',
    telegram_bot_token:  p.type === 'telegram' ? (p.config.bot_token   ?? '') : '',
    discord_bot_token: p.type === 'discord' ? (p.config.bot_token ?? '') : '',
  }
  showProviderDrawer.value = true
}

function buildProviderConfig(f: ProviderForm): Record<string, any> {
  if (f.type === 'pushover') return { app_token: f.pushover_app_token, user_key: f.pushover_user_key }
  if (f.type === 'discord')  return { bot_token: f.discord_bot_token }
  return { bot_token: f.telegram_bot_token }
}

async function onSaveProvider() {
  const f = providerForm.value
  if (!f.name) { msg.error('Name is required'); return }
  if (f.type === 'pushover' && (!f.pushover_app_token || !f.pushover_user_key)) {
    msg.error('App token and user key are required'); return
  }
  if (f.type === 'telegram' && !f.telegram_bot_token) {
    msg.error('Bot token is required'); return
  }
  if (f.type === 'discord' && !f.discord_bot_token) {
    msg.error('Bot token is required'); return
  }
  savingProvider.value = true
  try {
    const body = { name: f.name, type: f.type, config: buildProviderConfig(f) }
    if (editingProvider.value) {
      await store.updateProvider(editingProvider.value.id, body)
    } else {
      await store.createProvider(body)
    }
    msg.success('Saved')
    showProviderDrawer.value = false
  } catch (e: any) { msg.error(e.message) }
  finally { savingProvider.value = false }
}

const providerTypeOptions = [
  { label: 'Pushover', value: 'pushover' },
  { label: 'Telegram', value: 'telegram' },
  { label: 'Discord',  value: 'discord'  },
]

const providerColumns: DataTableColumns<NotificationProvider> = [
  { title: 'ID',   key: 'id',   width: 60,  sorter: 'default' },
  { title: 'Name', key: 'name',             sorter: 'default' },
  { title: 'Type', key: 'type', width: 110, sorter: 'default' },
  { title: 'Active', key: 'active', width: 80, sorter: (a, b) => Number(a.active) - Number(b.active), render: row => activeTag(row.active) },
  {
    title: 'Actions', key: 'actions',
    render: row => h(NSpace, { size: 4 }, { default: () => [
      iconBtn(CreateOutline, 'Edit', () => openEditProvider(row)),
      row.active
        ? iconBtn(PauseCircleOutline, 'Deactivate', () => store.deactivateProvider(row.id).catch((e: any) => msg.error(e.message)))
        : iconBtn(CheckmarkCircleOutline, 'Activate', () => store.activateProvider(row.id).catch((e: any) => msg.error(e.message)), { type: 'primary' }),
      iconBtn(TrashOutline, 'Delete', () => store.removeProvider(row.id).catch((e: any) => msg.error(e.message)), { type: 'error' }),
    ]})
  },
]

// ── Channels ──────────────────────────────────────────────────────────────────

const showChannelDrawer = ref(false)
const editingChannel    = ref<NotificationChannel | null>(null)
const savingChannel     = ref(false)

interface ChannelForm {
  name:        string
  provider_id: number | null
  // Telegram routing
  tg_chat_id:  string
  // Pushover routing (optional: target a specific device)
  push_device: string | null
  // Discord routing
  discord_forum_channel_id: string
}

const defaultChannelForm = (): ChannelForm => ({
  name: '', provider_id: null,
  tg_chat_id: '', push_device: null,
  discord_forum_channel_id: '',
})
const channelForm = ref<ChannelForm>(defaultChannelForm())

const selectedChannelProvider = computed(() =>
  store.providers.find(p => p.id === channelForm.value.provider_id) ?? null
)
const isPushoverChannel = computed(() => selectedChannelProvider.value?.type === 'pushover')
const isTelegramChannel = computed(() => selectedChannelProvider.value?.type === 'telegram')
const isDiscordChannel  = computed(() => selectedChannelProvider.value?.type === 'discord')

function openCreateChannel() {
  editingChannel.value  = null
  channelForm.value     = defaultChannelForm()
  showChannelDrawer.value = true
}

function openEditChannel(c: NotificationChannel) {
  editingChannel.value = c
  const cfg = c.config ?? {}
  channelForm.value = {
    name:                    c.name,
    provider_id:             c.provider_id,
    tg_chat_id:              cfg.chat_id          ?? '',
    push_device:             cfg.device           ?? null,
    discord_forum_channel_id: cfg.forum_channel_id ?? '',
  }
  showChannelDrawer.value = true
}

function buildChannelConfig(f: ChannelForm): Record<string, any> {
  const provider = store.providers.find(p => p.id === f.provider_id)
  if (!provider) return {}
  if (provider.type === 'telegram') return { chat_id: f.tg_chat_id }
  if (provider.type === 'discord')  return { forum_channel_id: f.discord_forum_channel_id }
  // pushover: device is optional routing
  const cfg: Record<string, any> = {}
  if (f.push_device) cfg.device = f.push_device
  return cfg
}

async function onSaveChannel() {
  const f = channelForm.value
  if (!f.name)        { msg.error('Name is required');     return }
  if (!f.provider_id) { msg.error('Provider is required'); return }
  if (isTelegramChannel.value && !f.tg_chat_id) {
    msg.error('Chat ID is required'); return
  }
  if (isDiscordChannel.value && !f.discord_forum_channel_id) {
    msg.error('Forum Channel ID is required'); return
  }
  savingChannel.value = true
  try {
    const body = { name: f.name, provider_id: f.provider_id!, config: buildChannelConfig(f) }
    if (editingChannel.value) {
      await store.updateChannel(editingChannel.value.id, body)
    } else {
      await store.createChannel(body)
    }
    msg.success('Saved')
    showChannelDrawer.value = false
  } catch (e: any) { msg.error(e.message) }
  finally { savingChannel.value = false }
}

const providerSelectOptions = computed(() =>
  store.providers.map(p => ({ label: `${p.name} (${p.type})`, value: p.id }))
)
const providerNameById = computed(() => {
  const m: Record<number, string> = {}
  store.providers.forEach(p => { m[p.id] = p.name })
  return m
})

const channelColumns: DataTableColumns<NotificationChannel> = [
  { title: 'ID',       key: 'id',          width: 60,  sorter: 'default' },
  { title: 'Name',     key: 'name',                    sorter: 'default' },
  { title: 'Provider', key: 'provider_id', width: 160, render: row => providerNameById.value[row.provider_id] ?? String(row.provider_id) },
  { title: 'Active',   key: 'active',      width: 80,  sorter: (a, b) => Number(a.active) - Number(b.active), render: row => activeTag(row.active) },
  {
    title: 'Actions', key: 'actions',
    render: row => h(NSpace, { size: 4 }, { default: () => [
      iconBtn(CreateOutline, 'Edit', () => openEditChannel(row)),
      row.active
        ? iconBtn(PauseCircleOutline, 'Deactivate', () => store.deactivateChannel(row.id).catch((e: any) => msg.error(e.message)))
        : iconBtn(CheckmarkCircleOutline, 'Activate', () => store.activateChannel(row.id).catch((e: any) => msg.error(e.message)), { type: 'primary' }),
      iconBtn(TrashOutline, 'Delete', () => store.removeChannel(row.id).catch((e: any) => msg.error(e.message)), { type: 'error' }),
    ]})
  },
]
</script>

<template>
  <n-space vertical size="large">
    <n-tabs v-model:value="activeTab" type="line" animated>

      <!-- ── Providers ──────────────────────────────────────────────────── -->
      <n-tab-pane name="providers" tab="Providers">
        <n-space vertical size="large">
          <PageHeader title="Notification Providers" add-label="New Provider" @add="openCreateProvider" />
          <n-data-table :columns="providerColumns" :data="store.providers" :loading="store.loading" :scroll-x="560" />
        </n-space>

        <FormDrawer
          v-model:show="showProviderDrawer"
          :title="editingProvider ? 'Edit Provider' : 'New Provider'"
          width="min(480px, calc(100vw - 16px))"
          :loading="savingProvider"
          @save="onSaveProvider"
        >
          <n-form-item label="Name">
            <n-input v-model:value="providerForm.name" />
          </n-form-item>
          <n-form-item label="Type">
            <n-select v-model:value="providerForm.type" :options="providerTypeOptions" :disabled="!!editingProvider" />
          </n-form-item>

          <template v-if="isPushoverProvider">
            <n-divider>Pushover credentials</n-divider>
            <n-form-item label="App Token">
              <n-input v-model:value="providerForm.pushover_app_token" />
            </n-form-item>
            <n-form-item label="User Key">
              <n-input v-model:value="providerForm.pushover_user_key" />
            </n-form-item>
          </template>

          <template v-else-if="isTelegramProvider">
            <n-divider>Telegram credentials</n-divider>
            <n-form-item label="Bot Token">
              <n-input v-model:value="providerForm.telegram_bot_token" />
            </n-form-item>
          </template>

          <template v-else-if="isDiscordProvider">
            <n-divider>Discord Bot credentials</n-divider>
            <n-form-item label="Bot Token">
              <n-input v-model:value="providerForm.discord_bot_token" placeholder="Bot MTxxxxx…" />
            </n-form-item>
          </template>
        </FormDrawer>
      </n-tab-pane>

      <!-- ── Channels ───────────────────────────────────────────────────── -->
      <n-tab-pane name="channels" tab="Channels">
        <n-space vertical size="large">
          <PageHeader title="Notification Channels" add-label="New Channel" @add="openCreateChannel" />
          <n-data-table :columns="channelColumns" :data="store.channels" :loading="store.loading" :scroll-x="580" />
        </n-space>

        <FormDrawer
          v-model:show="showChannelDrawer"
          :title="editingChannel ? 'Edit Channel' : 'New Channel'"
          width="min(560px, calc(100vw - 16px))"
          :loading="savingChannel"
          @save="onSaveChannel"
        >
          <n-form-item label="Name">
            <n-input v-model:value="channelForm.name" />
          </n-form-item>
          <n-form-item label="Provider">
            <n-select
              v-model:value="channelForm.provider_id"
              :options="providerSelectOptions"
              :disabled="!!editingChannel"
              placeholder="Select a provider…"
            />
          </n-form-item>

          <!-- Telegram: required routing field -->
          <n-form-item v-if="isTelegramChannel" label="Chat ID">
            <n-input v-model:value="channelForm.tg_chat_id" placeholder="-1001234567890" />
          </n-form-item>

          <!-- Pushover: optional device routing -->
          <n-form-item v-if="isPushoverChannel" label="Device (optional)">
            <n-input v-model:value="channelForm.push_device" clearable placeholder="Leave blank to send to all devices" />
          </n-form-item>

          <!-- Discord: required forum channel ID -->
          <n-form-item v-if="isDiscordChannel" label="Forum Channel ID" required>
            <n-input v-model:value="channelForm.discord_forum_channel_id" placeholder="1234567890123456789" />
          </n-form-item>

          <n-text v-if="selectedChannelProvider" depth="3" style="font-size:12px; display:block">
            Message template and delivery options are configured on the notification pipeline.
          </n-text>
        </FormDrawer>
      </n-tab-pane>

    </n-tabs>
  </n-space>
</template>
