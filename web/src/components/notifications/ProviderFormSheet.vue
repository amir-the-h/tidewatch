<!-- Create / edit a notification provider: name, type, and the credentials that type needs. -->
<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
import { Button, FormField, Input, Select, Sheet } from '@/components/ui'
import { useToast } from '@/composables/useToast'
import type { NotificationProvider } from '../../api/notifications'
import { useNotificationsStore } from '../../stores/notifications'
import FormSection from '../actions/FormSection.vue'
import { providerTypeLabel, providerTypeOptions, type ProviderType } from './providerTypes'

const props = defineProps<{
  /** The provider to edit, or null to create one. */
  provider: NotificationProvider | null
}>()

const open = defineModel<boolean>('open', { default: false })

const store  = useNotificationsStore()
const toast  = useToast()
const saving = ref(false)

interface ProviderForm {
  name: string
  type: ProviderType
  pushoverAppToken: string
  pushoverUserKey: string
  telegramBotToken: string
  discordBotToken: string
}

const emptyForm = (): ProviderForm => ({
  name: '', type: 'pushover',
  pushoverAppToken: '', pushoverUserKey: '',
  telegramBotToken: '',
  discordBotToken: '',
})

function formFrom(p: NotificationProvider): ProviderForm {
  return {
    name: p.name,
    type: p.type,
    pushoverAppToken: p.type === 'pushover' ? (p.config.app_token ?? '') : '',
    pushoverUserKey:  p.type === 'pushover' ? (p.config.user_key  ?? '') : '',
    telegramBotToken: p.type === 'telegram' ? (p.config.bot_token ?? '') : '',
    discordBotToken:  p.type === 'discord'  ? (p.config.bot_token ?? '') : '',
  }
}

const form = reactive(emptyForm())

watch(open, (isOpen) => {
  if (isOpen) Object.assign(form, props.provider ? formFrom(props.provider) : emptyForm())
})

function buildConfig(f: ProviderForm): Record<string, string> {
  if (f.type === 'pushover') return { app_token: f.pushoverAppToken, user_key: f.pushoverUserKey }
  if (f.type === 'discord')  return { bot_token: f.discordBotToken }
  return { bot_token: f.telegramBotToken }
}

function validate(f: ProviderForm): string | null {
  if (!f.name) return 'Enter a name for the provider'
  if (f.type === 'pushover' && (!f.pushoverAppToken || !f.pushoverUserKey)) return 'Enter the app token and user key'
  if (f.type === 'telegram' && !f.telegramBotToken) return 'Enter the bot token'
  if (f.type === 'discord'  && !f.discordBotToken)  return 'Enter the bot token'
  return null
}

async function onSave() {
  const invalid = validate(form)
  if (invalid) { toast.error(invalid); return }
  saving.value = true
  try {
    const body = { name: form.name, type: form.type, config: buildConfig(form) }
    if (props.provider) {
      await store.updateProvider(props.provider.id, body)
      toast.success('Provider updated')
    } else {
      await store.createProvider(body)
      toast.success('Provider created')
    }
    open.value = false
  } catch (e: unknown) {
    toast.error(`Could not ${props.provider ? 'update' : 'create'} provider: ${e instanceof Error ? e.message : e}`)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <Sheet
    v-model:open="open"
    :title="provider ? `Edit ${provider.name}` : 'New provider'"
    description="The account Tidewatch sends through. Channels on this provider say where messages go."
  >
    <form class="flex flex-col gap-4 p-4" @submit.prevent="onSave">
      <FormField label="Name" for="provider-name" required>
        <Input id="provider-name" v-model="form.name" placeholder="e.g. Ops Telegram bot" />
      </FormField>
      <FormField
        label="Type"
        required
        :description="provider ? 'The type can\'t be changed after the provider is created.' : undefined"
      >
        <Select v-model="form.type" :options="providerTypeOptions" :disabled="!!provider" />
      </FormField>

      <FormSection :title="`${providerTypeLabel[form.type]} credentials`">
        <template v-if="form.type === 'pushover'">
          <FormField label="App token" for="provider-app-token" required description="The API token of your Pushover application.">
            <Input id="provider-app-token" v-model="form.pushoverAppToken" type="password" autocomplete="new-password" />
          </FormField>
          <FormField label="User key" for="provider-user-key" required description="The user or group key to deliver to.">
            <Input id="provider-user-key" v-model="form.pushoverUserKey" type="password" autocomplete="new-password" />
          </FormField>
        </template>

        <FormField v-else-if="form.type === 'telegram'" label="Bot token" for="provider-tg-token" required description="From @BotFather.">
          <Input id="provider-tg-token" v-model="form.telegramBotToken" type="password" autocomplete="new-password" />
        </FormField>

        <FormField
          v-else
          label="Bot token"
          for="provider-discord-token"
          required
          description="From the Discord developer portal, without the &quot;Bot &quot; prefix. Tidewatch adds it."
        >
          <Input id="provider-discord-token" v-model="form.discordBotToken" type="password" autocomplete="new-password" />
        </FormField>
      </FormSection>
    </form>
    <template #footer>
      <Button variant="outline" size="default" @click="open = false">Cancel</Button>
      <Button size="default" :loading="saving" @click="onSave">{{ provider ? 'Save provider' : 'Create provider' }}</Button>
    </template>
  </Sheet>
</template>
