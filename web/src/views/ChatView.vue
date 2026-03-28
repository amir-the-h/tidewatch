<!-- web/src/views/ChatView.vue -->
<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, nextTick, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useMessage } from 'naive-ui'
import { SendOutline, StopCircleOutline, ArrowBackOutline } from '@vicons/ionicons5'
import { useChatStore } from '../stores/chat'
import { listPipelines } from '../api/pipelines'

const router = useRouter()
const msg    = useMessage()
const store  = useChatStore()

const input          = ref('')
const messagesEl     = ref<HTMLElement | null>(null)
const inputEl        = ref<HTMLTextAreaElement | null>(null)
const estimateLoading = ref(false)
const pipelineReady  = ref(false)

// ── Scroll to bottom whenever messages change or stream appends ──────────────

function scrollToBottom() {
  nextTick(() => {
    if (messagesEl.value) {
      messagesEl.value.scrollTop = messagesEl.value.scrollHeight
    }
  })
}

// Watch last message content for streaming updates
watch(
  () => store.messages[store.messages.length - 1]?.content,
  scrollToBottom,
)
watch(() => store.messages.length, scrollToBottom)

// ── Lifecycle ────────────────────────────────────────────────────────────────

onMounted(async () => {
  try {
    const pipelines = await listPipelines({ usecase: 'chat', active: true })
    pipelineReady.value = pipelines.length > 0
  } catch {}

  if (!store.hasSession) {
    msg.warning('No tweets selected — go to Tweets and select some first.')
    return
  }
  if (!pipelineReady.value) return
  if (!store.estimate) {
    estimateLoading.value = true
    try {
      await store.loadEstimate()
    } catch (e: any) {
      msg.error('Failed to load token estimate: ' + e.message)
    } finally {
      estimateLoading.value = false
    }
  }
  focusInput()
})

onUnmounted(() => {
  store.stopStreaming()
})

function focusInput() {
  nextTick(() => inputEl.value?.focus())
}

// ── Token indicator ──────────────────────────────────────────────────────────

const tokenPercent = computed(() => {
  if (!store.estimate) return 0
  return Math.min(100, Math.round((store.estimate.tokens / store.estimate.limit) * 100))
})

const tokenBarColor = computed(() => {
  if (tokenPercent.value >= 90) return '#f38ba8'
  if (tokenPercent.value >= 70) return '#f9e2af'
  return '#a6e3a1'
})

const tokenLabel = computed(() => {
  if (!store.estimate) return ''
  const { tokens, limit, tweet_count } = store.estimate
  return `${tweet_count} tweet${tweet_count !== 1 ? 's' : ''} · ~${tokens.toLocaleString()} / ${limit.toLocaleString()} tokens (${tokenPercent.value}%)`
})

// ── Sending ──────────────────────────────────────────────────────────────────

async function send() {
  const content = input.value.trim()
  if (!content || store.isStreaming || !store.hasSession) return
  input.value = ''
  try {
    await store.sendMessage(content)
  } catch (e: any) {
    msg.error(e.message ?? 'Chat error')
  }
  focusInput()
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    send()
  }
}
</script>

<template>
  <div style="
    display: flex;
    flex-direction: column;
    height: calc(100vh - 48px - 48px);
    min-height: 400px;
  ">
    <!-- Header bar -->
    <div style="
      display: flex;
      align-items: center;
      gap: 12px;
      padding: 0 0 12px 0;
      flex-shrink: 0;
    ">
      <n-tooltip trigger="hover">
        <template #trigger>
          <n-button size="small" quaternary @click="router.back()">
            <template #icon><n-icon><ArrowBackOutline /></n-icon></template>
          </n-button>
        </template>
        Back
      </n-tooltip>

      <n-text style="font-size: 16px; font-weight: 600; color: #cdd6f4;">Chat</n-text>

      <div v-if="store.hasSession" style="flex: 1; max-width: 400px;">
        <!-- Token bar -->
        <div v-if="store.estimate" style="display: flex; flex-direction: column; gap: 3px;">
          <n-text depth="3" style="font-size: 11px;">{{ tokenLabel }}</n-text>
          <div style="height: 3px; border-radius: 2px; background: rgba(255,255,255,0.08); overflow: hidden;">
            <div :style="{
              height: '100%',
              width: tokenPercent + '%',
              background: tokenBarColor,
              transition: 'width 0.3s ease',
            }" />
          </div>
        </div>
        <n-spin v-else-if="estimateLoading" size="small" />
      </div>

      <n-button
        v-if="store.messages.length > 0"
        size="small"
        quaternary
        style="margin-left: auto; color: #6c7086;"
        @click="store.messages = []"
      >
        Clear chat
      </n-button>
    </div>

    <!-- No pipeline state -->
    <div
      v-if="!pipelineReady"
      style="flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 16px;"
    >
      <n-alert type="warning" style="max-width: 480px;">
        No active chat pipeline found. Go to <strong>Pipelines</strong> and create an Ollama pipeline with usecase <strong>chat</strong>.
      </n-alert>
      <n-button type="primary" @click="router.push('/pipelines')">Go to Pipelines</n-button>
    </div>

    <!-- No session state -->
    <div
      v-else-if="!store.hasSession"
      style="flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 16px;"
    >
      <n-text depth="3">No tweets selected as context.</n-text>
      <n-button type="primary" @click="router.push('/tweets')">Go to Tweets</n-button>
    </div>

    <!-- Chat area -->
    <template v-else>
      <!-- Messages -->
      <div
        ref="messagesEl"
        style="
          flex: 1;
          overflow-y: auto;
          display: flex;
          flex-direction: column;
          gap: 12px;
          padding-right: 4px;
          margin-bottom: 12px;
        "
      >
        <!-- Empty state -->
        <div
          v-if="store.messages.length === 0"
          style="
            flex: 1;
            display: flex;
            flex-direction: column;
            align-items: center;
            justify-content: center;
            gap: 8px;
            color: #6c7086;
          "
        >
          <n-text depth="3" style="font-size: 13px;">
            {{ store.estimate?.tweet_count ?? store.tweetIds.length }} tweet{{ (store.estimate?.tweet_count ?? store.tweetIds.length) !== 1 ? 's' : '' }} loaded as context
          </n-text>
          <n-text depth="3" style="font-size: 12px;">Ask anything about them…</n-text>
        </div>

        <!-- Message bubbles -->
        <div
          v-for="(m, i) in store.messages"
          :key="i"
          :style="{
            display: 'flex',
            justifyContent: m.role === 'user' ? 'flex-end' : 'flex-start',
          }"
        >
          <div :style="{
            maxWidth: '75%',
            padding: '10px 14px',
            borderRadius: m.role === 'user' ? '14px 14px 4px 14px' : '14px 14px 14px 4px',
            background: m.role === 'user' ? 'rgba(137,180,250,0.18)' : 'rgba(255,255,255,0.05)',
            border: m.role === 'user' ? '1px solid rgba(137,180,250,0.25)' : '1px solid rgba(255,255,255,0.08)',
            fontSize: '13px',
            lineHeight: '1.6',
            color: '#cdd6f4',
            whiteSpace: 'pre-wrap',
            wordBreak: 'break-word',
          }">
            <!-- Streaming cursor -->
            <span v-if="m.content">{{ m.content }}</span>
            <span
              v-if="store.isStreaming && i === store.messages.length - 1 && m.role === 'assistant'"
              style="
                display: inline-block;
                width: 2px; height: 14px;
                background: #89b4fa;
                margin-left: 2px;
                vertical-align: text-bottom;
                animation: blink 1s step-end infinite;
              "
            />
            <n-spin v-else-if="!m.content && m.role === 'assistant'" size="small" />
          </div>
        </div>
      </div>

      <!-- Input row -->
      <div style="
        flex-shrink: 0;
        display: flex;
        gap: 8px;
        align-items: flex-end;
        border-top: 1px solid rgba(255,255,255,0.08);
        padding-top: 12px;
      ">
        <n-input
          ref="inputEl"
          v-model:value="input"
          type="textarea"
          :autosize="{ minRows: 1, maxRows: 5 }"
          placeholder="Ask about the selected tweets… (Enter to send, Shift+Enter for newline)"
          :disabled="!store.hasSession"
          style="flex: 1;"
          @keydown="onKeydown"
        />
        <n-tooltip v-if="!store.isStreaming" trigger="hover">
          <template #trigger>
            <n-button
              type="primary"
              :disabled="!input.trim() || !store.hasSession"
              @click="send"
            >
              <template #icon><n-icon><SendOutline /></n-icon></template>
            </n-button>
          </template>
          Send (Enter)
        </n-tooltip>
        <n-tooltip v-else trigger="hover">
          <template #trigger>
            <n-button type="error" @click="store.stopStreaming">
              <template #icon><n-icon><StopCircleOutline /></n-icon></template>
            </n-button>
          </template>
          Stop generating
        </n-tooltip>
      </div>
    </template>
  </div>
</template>

<style scoped>
@keyframes blink {
  0%, 100% { opacity: 1; }
  50%       { opacity: 0; }
}
</style>
