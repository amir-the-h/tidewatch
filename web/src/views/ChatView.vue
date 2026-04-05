<!-- web/src/views/ChatView.vue -->
<script setup lang="ts">
import { ref, nextTick, watch } from 'vue'
import { useMessage } from 'naive-ui'
import { SendOutline, StopCircleOutline } from '@vicons/ionicons5'
import { useChatStore } from '../stores/chat'

const msg   = useMessage()
const store = useChatStore()

const input      = ref('')
const messagesEl = ref<HTMLElement | null>(null)
const inputEl    = ref<HTMLTextAreaElement | null>(null)

// ── Scroll to bottom on new messages or streaming content ──────────────────

function scrollToBottom() {
  nextTick(() => {
    if (messagesEl.value) {
      messagesEl.value.scrollTop = messagesEl.value.scrollHeight
    }
  })
}
watch(() => store.messages[store.messages.length - 1]?.content, scrollToBottom)
watch(() => store.messages.length, scrollToBottom)

// ── Send ──────────────────────────────────────────────────────────────────

async function send() {
  const content = input.value.trim()
  if (!content || store.isStreaming) return
  input.value = ''
  try {
    await store.sendMessage(content)
  } catch (e: any) {
    msg.error(e.message ?? 'Chat error')
  }
  nextTick(() => inputEl.value?.focus())
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    send()
  }
}

function formatArgs(args: Record<string, unknown>): string {
  return JSON.stringify(args, null, 2)
}

function truncateResult(result: string): string {
  const max = 500
  return result.length > max ? result.slice(0, max) + '…' : result
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
      <n-text style="font-size: 16px; font-weight: 600; color: #cdd6f4;">Chat</n-text>
      <n-button
        v-if="store.messages.length > 0"
        size="small"
        quaternary
        style="margin-left: auto; color: #6c7086;"
        @click="store.clearSession()"
      >
        Clear chat
      </n-button>
    </div>

    <!-- Messages area -->
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
        <n-text depth="3" style="font-size: 13px;">Ask anything about your iran-watcher data…</n-text>
        <n-text depth="3" style="font-size: 12px;">The assistant can query tweets, rules, pipelines, entities, and more.</n-text>
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
          maxWidth: '80%',
          display: 'flex',
          flexDirection: 'column',
          gap: '6px',
        }">
          <!-- Tool steps (assistant only) -->
          <template v-if="m.role === 'assistant'">
            <details
              v-for="(step, si) in m.toolSteps"
              :key="si"
              style="
                font-size: 12px;
                border: 1px solid rgba(255,255,255,0.08);
                border-radius: 8px;
                padding: 6px 10px;
                background: rgba(255,255,255,0.03);
                color: #a6adc8;
                cursor: pointer;
              "
            >
              <summary style="list-style: none; display: flex; align-items: center; gap: 6px; user-select: none;">
                <span style="color: #89b4fa;">⚙</span>
                <span>Called <code style="color: #cba6f7;">{{ step.name }}</code></span>
              </summary>
              <div style="margin-top: 8px; display: flex; flex-direction: column; gap: 6px;">
                <div>
                  <n-text depth="3" style="font-size: 11px; text-transform: uppercase; letter-spacing: 0.05em;">Arguments</n-text>
                  <pre style="margin: 4px 0 0; font-size: 11px; white-space: pre-wrap; word-break: break-all; color: #cdd6f4;">{{ formatArgs(step.args) }}</pre>
                </div>
                <div>
                  <n-text depth="3" style="font-size: 11px; text-transform: uppercase; letter-spacing: 0.05em;">Result</n-text>
                  <pre style="margin: 4px 0 0; font-size: 11px; white-space: pre-wrap; word-break: break-all; color: #cdd6f4;">{{ truncateResult(step.result) }}</pre>
                </div>
              </div>
            </details>
          </template>

          <!-- Message bubble -->
          <div
            v-if="m.role === 'user' || m.content || (store.isStreaming && i === store.messages.length - 1)"
            :style="{
              padding: '10px 14px',
              borderRadius: m.role === 'user' ? '14px 14px 4px 14px' : '14px 14px 14px 4px',
              background: m.role === 'user' ? 'rgba(137,180,250,0.18)' : 'rgba(255,255,255,0.05)',
              border: m.role === 'user' ? '1px solid rgba(137,180,250,0.25)' : '1px solid rgba(255,255,255,0.08)',
              fontSize: '13px',
              lineHeight: '1.6',
              color: '#cdd6f4',
              whiteSpace: 'pre-wrap',
              wordBreak: 'break-word',
            }"
          >
            <span v-if="m.content">{{ m.content }}</span>
            <!-- Spinner while waiting for first chunk (no content yet) -->
            <n-spin
              v-if="!m.content && m.role === 'assistant' && store.isStreaming && i === store.messages.length - 1"
              size="small"
            />
            <!-- Streaming cursor (shown after first token arrives) -->
            <span
              v-else-if="m.content && store.isStreaming && i === store.messages.length - 1 && m.role === 'assistant'"
              style="
                display: inline-block;
                width: 2px; height: 14px;
                background: #89b4fa;
                margin-left: 2px;
                vertical-align: text-bottom;
                animation: blink 1s step-end infinite;
              "
            />
          </div>
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
        placeholder="Ask anything… (Enter to send, Shift+Enter for newline)"
        style="flex: 1;"
        @keydown="onKeydown"
      />
      <n-tooltip v-if="!store.isStreaming" trigger="hover">
        <template #trigger>
          <n-button
            type="primary"
            :disabled="!input.trim()"
            @click="send"
          >
            <template #icon><n-icon><SendOutline /></n-icon></template>
          </n-button>
        </template>
        Send (Enter)
      </n-tooltip>
      <n-tooltip v-else trigger="hover">
        <template #trigger>
          <n-button type="error" @click="store.stopStreaming()">
            <template #icon><n-icon><StopCircleOutline /></n-icon></template>
          </n-button>
        </template>
        Stop generating
      </n-tooltip>
    </div>
  </div>
</template>

<style scoped>
@keyframes blink {
  0%, 100% { opacity: 1; }
  50%       { opacity: 0; }
}

details > summary::-webkit-details-marker { display: none; }
</style>
