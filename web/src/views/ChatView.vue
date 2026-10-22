<!-- web/src/views/ChatView.vue — Chat page (admin only): multi-turn LLM assistant over a chat action's tools. -->
<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { AlertDialog, Button, Select, Skeleton } from '@/components/ui'
import PageHeader from '../components/PageHeader.vue'
import ChatComposer from '../components/chat/ChatComposer.vue'
import ChatMessage from '../components/chat/ChatMessage.vue'
import { useChatStore } from '../stores/chat'
import { listActions, type Action } from '../api/actions'

const store = useChatStore()

// ── Chat actions ─────────────────────────────────────────────────────────────

const chatActions    = ref<Action[]>([])
const actionsLoading = ref(true)

const actionOptions = computed(() =>
  chatActions.value.map(a => ({ label: a.name, value: String(a.id) }))
)
const selectedName = computed(() =>
  chatActions.value.find(a => String(a.id) === store.selectedActionId)?.name ?? 'this action'
)

// One error block for both failures; failedOp says which one Retry repeats.
const error    = ref<string | null>(null)
const failedOp = ref<'load' | 'send' | null>(null)
let lastContent = ''

function fail(op: 'load' | 'send', e: unknown) {
  failedOp.value = op
  error.value = e instanceof Error ? e.message : String(e)
}

async function fetchChatActions() {
  actionsLoading.value = true
  error.value = null
  try {
    const all = await listActions({ active: true })
    chatActions.value = all.filter(a => a.type === 'ollama' && a.config?.usecase === 'chat')
    // If nothing is selected yet (or the stored ID is no longer valid), pick first.
    const ids = chatActions.value.map(a => String(a.id))
    if (!ids.includes(store.selectedActionId) && chatActions.value.length > 0) {
      store.selectAction(String(chatActions.value[0].id))
    }
  } catch (e: unknown) {
    fail('load', e)
  } finally {
    actionsLoading.value = false
  }
}

onMounted(fetchChatActions)

// ── Messages ─────────────────────────────────────────────────────────────────

const messagesEl = ref<HTMLElement | null>(null)
const composer   = ref<{ focus: () => void } | null>(null)

function scrollToBottom() {
  nextTick(() => {
    if (messagesEl.value) messagesEl.value.scrollTop = messagesEl.value.scrollHeight
  })
}
watch(() => store.messages[store.messages.length - 1]?.content, scrollToBottom)
watch(() => store.messages.length, scrollToBottom)

async function send(content: string) {
  lastContent = content
  error.value = null
  try {
    await store.sendMessage(content)
  } catch (e: unknown) {
    fail('send', e)
  }
  nextTick(() => composer.value?.focus())
}

function retry() {
  if (failedOp.value === 'send') {
    store.discardLastTurn()
    send(lastContent)
  } else fetchChatActions()
}

// ── Clear ────────────────────────────────────────────────────────────────────
// Conversations live only in the Pinia store (memory), so clearing can't be undone.

const showClear = ref(false)

function onConfirmClear() {
  store.clearSession()
  showClear.value = false
}
</script>

<template>
  <div class="flex h-full min-h-96 flex-col gap-4">
    <PageHeader title="Chat" description="Ask an LLM assistant about your data; it looks things up with Tidewatch's tools.">
      <div class="w-56">
        <Select
          :model-value="store.selectedActionId || null"
          :options="actionOptions"
          :loading="actionsLoading"
          :disabled="store.isStreaming"
          placeholder="Select a chat action"
          @update:model-value="(v: string | null) => v && store.selectAction(v)"
        />
      </div>
      <Button v-if="store.messages.length > 0" variant="outline" @click="showClear = true">Clear chat</Button>
    </PageHeader>

    <div
      v-if="error"
      class="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-destructive p-4 text-small"
    >
      <span class="text-destructive">
        {{ failedOp === 'send' ? 'Could not get a reply' : 'Could not load chat actions' }}: {{ error }}
      </span>
      <div class="flex items-center gap-2">
        <Button variant="outline" @click="retry">Retry</Button>
        <Button variant="ghost" @click="error = null">Dismiss</Button>
      </div>
    </div>

    <div v-if="actionsLoading" class="flex flex-col gap-3">
      <Skeleton class="h-16 w-2/3 rounded-lg" />
      <Skeleton class="ml-auto h-10 w-1/3 rounded-lg" />
    </div>

    <div
      v-else-if="chatActions.length === 0 && failedOp !== 'load'"
      class="flex flex-col items-center gap-3 rounded-lg border border-border py-8 text-center"
    >
      <p class="m-0 text-muted-foreground">
        No chat actions yet. Create an Ollama action with use case Chat on the Actions page to start a conversation.
      </p>
      <RouterLink to="/actions" custom v-slot="{ navigate }">
        <Button variant="outline" @click="navigate">Go to Actions</Button>
      </RouterLink>
    </div>

    <template v-else-if="chatActions.length > 0">
      <div ref="messagesEl" class="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto pr-1">
        <div
          v-if="store.messages.length === 0"
          class="flex flex-1 flex-col items-center justify-center gap-1 text-center text-muted-foreground"
        >
          <p class="m-0">Ask anything about your Tidewatch data.</p>
          <p class="m-0 text-small">The assistant can query signals, rules, pipelines, tracked entities and more.</p>
        </div>
        <ChatMessage
          v-for="(m, i) in store.messages"
          :key="i"
          :message="m"
          :streaming="store.isStreaming && i === store.messages.length - 1"
        />
      </div>

      <ChatComposer
        ref="composer"
        :disabled="!store.selectedActionId"
        :streaming="store.isStreaming"
        @send="send"
        @stop="store.stopStreaming()"
      />
    </template>

    <AlertDialog
      v-model:open="showClear"
      title="Clear this conversation?"
      :description="`Removes the ${store.messages.length} messages exchanged with ${selectedName}${store.isStreaming ? ' and stops the reply in progress' : ''}. Conversations are only kept in this browser tab, so they can't be recovered.`"
      action-label="Clear chat"
      @confirm="onConfirmClear"
    />
  </div>
</template>
