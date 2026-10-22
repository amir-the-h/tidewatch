<!-- Create / edit a notification channel: name, provider, and where that provider delivers. -->
<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { Button, FormField, Input, Select, Sheet } from '@/components/ui'
import { useToast } from '@/composables/useToast'
import type { NotificationChannel } from '../../api/notifications'
import { useNotificationsStore } from '../../stores/notifications'
import { providerTypeLabel } from './providerTypes'

const props = defineProps<{
  /** The channel to edit, or null to create one. */
  channel: NotificationChannel | null
}>()

const open = defineModel<boolean>('open', { default: false })

const store  = useNotificationsStore()
const toast  = useToast()
const saving = ref(false)

interface ChannelForm {
  name: string
  providerId: number | null
  telegramChatId: string
  /** Optional: target one Pushover device instead of all of the user's devices. */
  pushoverDevice: string
  discordForumChannelId: string
}

const emptyForm = (): ChannelForm => ({
  name: '', providerId: null,
  telegramChatId: '', pushoverDevice: '', discordForumChannelId: '',
})

function formFrom(c: NotificationChannel): ChannelForm {
  const cfg = c.config ?? {}
  return {
    name: c.name,
    providerId: c.provider_id,
    telegramChatId: cfg.chat_id ?? '',
    pushoverDevice: cfg.device ?? '',
    discordForumChannelId: cfg.forum_channel_id ?? '',
  }
}

const form = reactive(emptyForm())

watch(open, (isOpen) => {
  if (isOpen) Object.assign(form, props.channel ? formFrom(props.channel) : emptyForm())
})

const providerOptions = computed(() =>
  store.providers.map(p => ({ label: `${p.name} (${providerTypeLabel[p.type] ?? p.type})`, value: p.id })),
)

const providerType = computed(() => store.providers.find(p => p.id === form.providerId)?.type ?? null)

function buildConfig(f: ChannelForm): Record<string, string> {
  if (providerType.value === 'telegram') return { chat_id: f.telegramChatId }
  if (providerType.value === 'discord')  return { forum_channel_id: f.discordForumChannelId }
  if (providerType.value === 'pushover') return f.pushoverDevice ? { device: f.pushoverDevice } : {}
  return {}
}

function validate(f: ChannelForm): string | null {
  if (!f.name)       return 'Enter a name for the channel'
  if (!f.providerId) return 'Choose a provider'
  if (providerType.value === 'telegram' && !f.telegramChatId)        return 'Enter the chat ID'
  if (providerType.value === 'discord'  && !f.discordForumChannelId) return 'Enter the forum channel ID'
  return null
}

async function onSave() {
  const invalid = validate(form)
  if (invalid) { toast.error(invalid); return }
  saving.value = true
  try {
    const body = { name: form.name, provider_id: form.providerId!, config: buildConfig(form) }
    if (props.channel) {
      await store.updateChannel(props.channel.id, body)
      toast.success('Channel updated')
    } else {
      await store.createChannel(body)
      toast.success('Channel created')
    }
    open.value = false
  } catch (e: unknown) {
    toast.error(`Could not ${props.channel ? 'update' : 'create'} channel: ${e instanceof Error ? e.message : e}`)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <Sheet
    v-model:open="open"
    :title="channel ? `Edit ${channel.name}` : 'New channel'"
    description="Where a provider delivers. The message template and delivery options live on the notification action."
  >
    <form class="flex flex-col gap-4 p-4" @submit.prevent="onSave">
      <FormField label="Name" for="channel-name" required>
        <Input id="channel-name" v-model="form.name" placeholder="e.g. Alerts forum" />
      </FormField>
      <FormField
        label="Provider"
        required
        :description="channel ? 'The provider can\'t be changed after the channel is created.' : undefined"
      >
        <Select
          v-model="form.providerId"
          :options="providerOptions"
          :disabled="!!channel"
          placeholder="Select a provider"
        />
      </FormField>

      <FormField
        v-if="providerType === 'telegram'"
        label="Chat ID"
        for="channel-chat-id"
        required
        description="The user, group or channel ID the bot posts to. Group and channel IDs start with -100."
      >
        <Input id="channel-chat-id" v-model="form.telegramChatId" placeholder="-1001234567890" class="font-mono" />
      </FormField>

      <FormField
        v-else-if="providerType === 'discord'"
        label="Forum channel ID"
        for="channel-forum-id"
        required
        description="The Discord Forum channel the bot opens threads in."
      >
        <Input id="channel-forum-id" v-model="form.discordForumChannelId" placeholder="1234567890123456789" class="font-mono" />
      </FormField>

      <FormField
        v-else-if="providerType === 'pushover'"
        label="Device"
        for="channel-device"
        description="Leave blank to send to all of the user's devices."
      >
        <Input id="channel-device" v-model="form.pushoverDevice" clearable placeholder="All devices" class="font-mono" />
      </FormField>
    </form>
    <template #footer>
      <Button variant="outline" size="default" @click="open = false">Cancel</Button>
      <Button size="default" :loading="saving" @click="onSave">{{ channel ? 'Save channel' : 'Create channel' }}</Button>
    </template>
  </Sheet>
</template>
