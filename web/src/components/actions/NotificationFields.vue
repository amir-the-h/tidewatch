<!-- Notification action fields: channel and message template, then the delivery options of the
     channel's provider (Discord thread name, Pushover or Telegram settings). -->
<script setup lang="ts">
import { computed } from 'vue'
import { FormField, Input, NumberField, Select, Textarea } from '@/components/ui'
import type { NotificationChannel } from '../../api/notifications'
import FormSection from './FormSection.vue'
import SwitchField from './SwitchField.vue'
import type { ActionFormState, NotificationProviderType } from './actionForm'

const props = defineProps<{
  form: ActionFormState
  channels: NotificationChannel[]
  providerType: NotificationProviderType
}>()

const channelOptions = computed(() => props.channels.map(c => ({ label: c.name, value: c.id })))

const pushPriorityOptions = [
  { label: '0 — Normal',    value: 0 },
  { label: '1 — High',      value: 1 },
  { label: '2 — Emergency', value: 2 },
]
const tgParseModeOptions = [
  { label: 'None',       value: '' },
  { label: 'HTML',       value: 'HTML' },
  { label: 'Markdown',   value: 'Markdown' },
  { label: 'MarkdownV2', value: 'MarkdownV2' },
]

const templateVars = '${text_short}, ${text}, ${title}, ${author}, ${url}, ${signal_id}'
</script>

<template>
  <FormField label="Channel" required>
    <Select
      v-model="form.channelId"
      :options="channelOptions"
      placeholder="Select a notification channel"
      filterable
      clearable
    />
  </FormField>
  <FormField
    label="Message template"
    for="action-message-template"
    required
    :description="`Variables: ${templateVars}, plus any action execution key.`"
  >
    <Textarea
      id="action-message-template"
      v-model="form.messageTemplate"
      :rows="4"
      placeholder="${text_short} — by ${author}"
      class="font-mono"
    />
  </FormField>

  <FormField
    v-if="providerType === 'discord'"
    label="Thread name template"
    for="action-thread-name"
    required
    description="Names the Forum thread. Same variables as the message; cut to 100 characters."
  >
    <Input id="action-thread-name" v-model="form.threadNameTemplate" placeholder="[${category}] ${author}" class="font-mono" />
  </FormField>

  <FormSection v-if="providerType === 'pushover'" title="Pushover delivery">
    <FormField label="Priority">
      <Select v-model="form.pushPriority" :options="pushPriorityOptions" />
    </FormField>
    <template v-if="form.pushPriority === 2">
      <div class="grid grid-cols-2 gap-3">
        <FormField label="Retry" for="action-push-retry" required description="Seconds, at least 30.">
          <NumberField id="action-push-retry" v-model="form.pushRetry" :min="30" />
        </FormField>
        <FormField label="Expire" for="action-push-expire" required description="Seconds, 1 to 10800.">
          <NumberField id="action-push-expire" v-model="form.pushExpire" :min="1" :max="10800" />
        </FormField>
      </div>
    </template>
    <div class="grid grid-cols-2 gap-3">
      <FormField label="Title" for="action-push-title">
        <Input id="action-push-title" v-model="form.pushTitle" placeholder="Optional" clearable />
      </FormField>
      <FormField label="Sound" for="action-push-sound">
        <Input id="action-push-sound" v-model="form.pushSound" placeholder="Optional" clearable />
      </FormField>
    </div>
    <FormField label="URL" for="action-push-url" description="Supplementary link button. Accepts the same variables as the message.">
      <Input
        id="action-push-url"
        v-model="form.pushUrl"
        placeholder="https://tidewatch.example.com/signals?signal=${signal_id}"
        clearable
        class="font-mono"
      />
    </FormField>
    <div class="grid grid-cols-2 gap-3">
      <FormField label="URL title" for="action-push-url-title">
        <Input id="action-push-url-title" v-model="form.pushUrlTitle" placeholder="Optional" clearable />
      </FormField>
      <FormField label="TTL" for="action-push-ttl" description="Seconds.">
        <NumberField id="action-push-ttl" v-model="form.pushTtl" placeholder="Optional" />
      </FormField>
    </div>
    <SwitchField v-model="form.pushHtml" label="HTML" description="Render the message as Pushover HTML." />
  </FormSection>

  <FormSection v-if="providerType === 'telegram'" title="Telegram delivery">
    <div class="grid grid-cols-2 gap-3">
      <FormField label="Parse mode">
        <Select v-model="form.tgParseMode" :options="tgParseModeOptions" />
      </FormField>
      <FormField label="Message thread ID" for="action-tg-thread">
        <NumberField id="action-tg-thread" v-model="form.tgThreadId" placeholder="Optional" />
      </FormField>
    </div>
    <SwitchField v-model="form.tgDisablePreview" label="Disable link previews" />
    <SwitchField v-model="form.tgDisableNotification" label="Silent" description="Deliver without a notification sound." />
  </FormSection>
</template>
