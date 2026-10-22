<!-- Ollama action fields: model, use case (custom / digest / chat), prompt, thinking mode,
     context injection for custom and digest, and the tool whitelist for chat. -->
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Combobox, FormField, Input, Select, Textarea } from '@/components/ui'
import { useToast } from '@/composables/useToast'
import { listChatTools } from '../../api/chat'
import type { Action } from '../../api/actions'
import FormSection from './FormSection.vue'
import type { ActionFormState } from './actionForm'

const props = defineProps<{
  form: ActionFormState
  /** Other actions, offered as context sources. */
  contextActions: Action[]
}>()

const toast = useToast()

const usecaseOptions = [
  { label: 'Custom (pipeline action)', value: 'custom' },
  { label: 'Digest',                   value: 'digest' },
  { label: 'Chat',                     value: 'chat'   },
]
const thinkOptions = [
  { label: 'Model default', value: 'null'  },
  { label: 'Enabled',       value: 'true'  },
  { label: 'Disabled',      value: 'false' },
]
const inputModeOptions = [
  { label: 'Text only',      value: 'text_only'        },
  { label: 'Context only',   value: 'context_only'     },
  { label: 'Text + context', value: 'text_and_context' },
]

const contextActionOptions = computed(() =>
  props.contextActions.map(a => ({ label: `${a.name} (${a.type})`, value: a.id }))
)

const chatTools = ref<{ name: string; description: string }[]>([])
const chatToolsLoading = ref(false)
const chatToolOptions = computed(() => chatTools.value.map(t => ({ label: t.name, value: t.name })))

async function fetchChatTools() {
  if (chatTools.value.length > 0) return
  chatToolsLoading.value = true
  try { chatTools.value = await listChatTools() }
  catch (e: unknown) { toast.error(`Could not load chat tools: ${e instanceof Error ? e.message : e}`) }
  finally { chatToolsLoading.value = false }
}

watch(() => props.form.usecase, (uc) => { if (uc === 'chat') fetchChatTools() }, { immediate: true })
</script>

<template>
  <FormField label="Model" for="action-ollama-model" required>
    <Input id="action-ollama-model" v-model="form.model" placeholder="e.g. llama3.2, mistral, gemma3" />
  </FormField>
  <FormField label="Use case" description="Custom actions run in pipelines. Only one digest action can exist; chat actions appear on the Chat page.">
    <Select v-model="form.usecase" :options="usecaseOptions" />
  </FormField>
  <FormField label="System prompt" for="action-ollama-prompt" description="Leave empty to use the default system prompt.">
    <Textarea id="action-ollama-prompt" v-model="form.systemPrompt" :rows="8" class="font-mono" />
  </FormField>
  <FormField
    label="Thinking mode"
    description="Disable for JSON extraction on thinking models (e.g. qwen3) to skip chain-of-thought and improve throughput."
  >
    <Select v-model="form.think" :options="thinkOptions" />
  </FormField>

  <template v-if="form.usecase !== 'chat'">
    <FormField label="Input" description="What the model receives for each tweet.">
      <Select v-model="form.inputMode" :options="inputModeOptions" />
    </FormField>
    <FormField
      v-if="form.inputMode !== 'text_only'"
      label="Context actions"
      description="Their latest outputs for each tweet are passed in a <context> block. tweet_id, tweet_url, tweet_author and tweet_short are always included."
    >
      <Combobox
        v-model="form.contextActionIds"
        :options="contextActionOptions"
        placeholder="Select upstream actions"
        clearable
      />
    </FormField>
  </template>

  <FormSection v-else title="Chat tools">
    <FormField
      label="Allowed tools"
      :description="chatTools.length ? `Leave empty to allow all ${chatTools.length} tools.` : 'Leave empty to allow all tools.'"
    >
      <Combobox
        v-model="form.allowedTools"
        :options="chatToolOptions"
        :placeholder="chatToolsLoading ? 'Loading tools…' : 'All tools'"
        clearable
      />
    </FormField>
  </FormSection>
</template>
