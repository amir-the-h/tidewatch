import { defineStore } from 'pinia'
import { ref } from 'vue'
import { streamChat } from '../api/chat'
import type { ChatMessage, ChatEvent } from '../api/chat'

export type { ChatMessage }

export interface ToolStep {
  name: string
  args: Record<string, unknown>
  result: string
}

export interface DisplayMessage {
  role: 'user' | 'assistant'
  content: string
  toolSteps: ToolStep[]
}

export const useChatStore = defineStore('chat', () => {
  const messages   = ref<DisplayMessage[]>([])
  const isStreaming = ref(false)

  let abortController: AbortController | null = null
  // Pending tool step: tool_call received but tool_result not yet
  let pendingStep: { name: string; args: Record<string, unknown> } | null = null

  /**
   * Append a user message and stream the assistant reply.
   * Resolves when the stream ends cleanly; rejects on error.
   */
  async function sendMessage(content: string): Promise<void> {
    if (isStreaming.value) return

    // Build history to send: only role+content (no toolSteps)
    const history: ChatMessage[] = [
      ...messages.value.map(m => ({ role: m.role, content: m.content })),
      { role: 'user' as const, content },
    ]

    messages.value.push({ role: 'user', content, toolSteps: [] })
    const assistantMsg: DisplayMessage = { role: 'assistant', content: '', toolSteps: [] }
    messages.value.push(assistantMsg)
    const assistantIdx = messages.value.length - 1

    isStreaming.value = true
    pendingStep = null

    return new Promise((resolve, reject) => {
      abortController = streamChat(
        history,
        (event: ChatEvent) => {
          const msg = messages.value[assistantIdx]
          if (event.type === 'tool_call') {
            pendingStep = { name: event.name, args: event.args }
          } else if (event.type === 'tool_result' && pendingStep) {
            msg.toolSteps = [
              ...msg.toolSteps,
              { name: pendingStep.name, args: pendingStep.args, result: event.result },
            ]
            messages.value[assistantIdx] = { ...msg }
            pendingStep = null
          } else if (event.type === 'text_chunk') {
            messages.value[assistantIdx] = {
              ...msg,
              content: msg.content + event.content,
            }
          }
          // error events: log but don't crash the UI
        },
        () => {
          isStreaming.value = false
          abortController = null
          pendingStep = null
          resolve()
        },
        (err) => {
          isStreaming.value = false
          abortController = null
          pendingStep = null
          reject(err)
        },
      )
    })
  }

  /** Cancel an in-progress stream. */
  function stopStreaming() {
    abortController?.abort()
    abortController = null
    isStreaming.value = false
    pendingStep = null
  }

  /** Clear the conversation. */
  function clearSession() {
    stopStreaming()
    messages.value = []
  }

  return {
    messages,
    isStreaming,
    sendMessage,
    stopStreaming,
    clearSession,
  }
})
