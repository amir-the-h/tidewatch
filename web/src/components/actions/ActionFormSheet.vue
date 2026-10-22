<!-- Create / edit an action. Common fields here; per-type fields in the *Fields components beside it. -->
<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { Button, FormField, Input, Select, Sheet, Textarea } from '@/components/ui'
import { useToast } from '@/composables/useToast'
import { createAction, updateAction, type Action, type ActionType } from '../../api/actions'
import {
  listChannels, listProviders, type NotificationChannel, type NotificationProvider,
} from '../../api/notifications'
import InternalFields from './InternalFields.vue'
import MapOutputFields from './MapOutputFields.vue'
import NotificationFields from './NotificationFields.vue'
import OllamaFields from './OllamaFields.vue'
import OnnxFields from './OnnxFields.vue'
import TransformersField from './TransformersField.vue'
import { actionFormFrom, buildActionConfig, emptyActionForm } from './actionForm'

const props = defineProps<{
  /** The action to edit, or null to create one. */
  action: Action | null
  /** All actions: context sources for Ollama, and the digest uniqueness check. */
  actions: Action[]
}>()

const open = defineModel<boolean>('open', { default: false })
const emit = defineEmits<{ saved: [] }>()

const toast = useToast()
const saving = ref(false)
const form = reactive(emptyActionForm())

const typeOptions: { label: string; value: ActionType }[] = [
  { label: 'ONNX',         value: 'onnx'         },
  { label: 'Ollama',       value: 'ollama'       },
  { label: 'Internal',     value: 'internal'     },
  { label: 'Notification', value: 'notification' },
]

const typeDescriptions: Record<ActionType, string> = {
  onnx:         'Runs a local ONNX model on the tweet text.',
  ollama:       'Prompts an LLM on an Ollama backend.',
  internal:     'Runs a built-in handler.',
  notification: 'Sends a message to a notification channel.',
}

const channels  = ref<NotificationChannel[]>([])
const providers = ref<NotificationProvider[]>([])

const providerType = computed(() => {
  const channel = channels.value.find(c => c.id === form.channelId)
  return providers.value.find(p => p.id === channel?.provider_id)?.type ?? null
})

const contextActions = computed(() => props.actions.filter(a => a.id !== props.action?.id))

watch(open, (isOpen) => {
  if (isOpen) Object.assign(form, props.action ? actionFormFrom(props.action) : emptyActionForm())
})

async function onSave() {
  if (!form.name.trim()) { toast.error('Enter a name for the action'); return }
  const built = buildActionConfig(form, providerType.value)
  if ('error' in built) { toast.error(built.error); return }
  if (!props.action && form.type === 'ollama' && form.usecase === 'digest') {
    const conflict = props.actions.find(a => a.config?.usecase === 'digest')
    if (conflict) { toast.error(`A digest action already exists ("${conflict.name}"). Delete it first.`); return }
  }

  saving.value = true
  try {
    const body = { name: form.name.trim(), type: form.type, config: built.config }
    if (props.action) {
      await updateAction(props.action.id, body)
      toast.success('Action updated')
    } else {
      await createAction(body)
      toast.success('Action created')
    }
    open.value = false
    emit('saved')
  } catch (e: unknown) {
    toast.error(`Could not save action: ${e instanceof Error ? e.message : e}`)
  } finally {
    saving.value = false
  }
}

onMounted(async () => {
  try { channels.value = await listChannels() } catch { /* notification pickers stay empty */ }
  try { providers.value = await listProviders() } catch { /* provider options hidden */ }
})
</script>

<template>
  <Sheet
    v-model:open="open"
    :title="action ? 'Edit action' : 'New action'"
    :description="typeDescriptions[form.type]"
    width="min(560px, 100vw)"
  >
    <form class="flex flex-col gap-4 p-4" @submit.prevent="onSave">
      <FormField label="Name" for="action-name" required>
        <Input id="action-name" v-model="form.name" placeholder="e.g. Location extractor" />
      </FormField>
      <FormField label="Type">
        <Select v-model="form.type" :options="typeOptions" />
      </FormField>

      <OnnxFields v-if="form.type === 'onnx'" :form="form" />
      <OllamaFields v-else-if="form.type === 'ollama'" :form="form" :context-actions="contextActions" />
      <InternalFields v-else-if="form.type === 'internal'" :form="form" :channels="channels" :providers="providers" />
      <NotificationFields v-else :form="form" :channels="channels" :provider-type="providerType" />

      <FormField
        v-if="form.type === 'onnx' || form.type === 'internal'"
        label="Config (JSON)"
        for="action-config"
        description="Other config keys. The fields above take precedence when set."
      >
        <Textarea id="action-config" v-model="form.configText" :rows="6" placeholder="{}" class="font-mono" />
      </FormField>

      <template v-if="form.type !== 'notification'">
        <MapOutputFields :form="form" />
        <TransformersField v-model="form.transformers" />
      </template>
    </form>
    <template #footer>
      <Button variant="outline" size="default" @click="open = false">Cancel</Button>
      <Button size="default" :loading="saving" @click="onSave">{{ action ? 'Save action' : 'Create action' }}</Button>
    </template>
  </Sheet>
</template>
