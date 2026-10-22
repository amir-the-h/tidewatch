import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
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

const STORAGE_KEY = 'chat_action_id'

export const useChatStore = defineStore('chat', () => {
  // Per-action conversation history keyed by action ID string.
  const conversations  = ref<Record<string, DisplayMessage[]>>({})
  const selectedActionId = ref<string>(localStorage.getItem(STORAGE_KEY) ?? '')
  const isStreaming    = ref(false)

  let abortController: AbortController | null = null
  let pendingStep: { name: string; args: Record<string, unknown> } | null = null

  /** Messages for the currently selected action. */
  const messages = computed<DisplayMessage[]>(
    () => conversations.value[selectedActionId.value] ?? []
  )

  /** Switch to a different chat action; preserves that action's history. */
  function selectAction(id: string) {
    selectedActionId.value = id
    localStorage.setItem(STORAGE_KEY, id)
  }

  function ensureConversation(id: string) {
    if (!conversations.value[id]) {
      conversations.value[id] = []
    }
  }

  /**
   * Append a user message and stream the assistant reply.
   * Resolves when the stream ends cleanly; rejects on error.
   */
  async function sendMessage(content: string): Promise<void> {
    if (isStreaming.value) return
    const actionId = selectedActionId.value
    ensureConversation(actionId)

    const conv = conversations.value[actionId]

    // Build history to send: only role+content (no toolSteps)
    const history: ChatMessage[] = [
      ...conv.map(m => ({ role: m.role, content: m.content })),
      { role: 'user' as const, content },
    ]

    conv.push({ role: 'user', content, toolSteps: [] })
    const assistantMsg: DisplayMessage = { role: 'assistant', content: '', toolSteps: [] }
    conv.push(assistantMsg)
    const assistantIdx = conv.length - 1

    isStreaming.value = true
    pendingStep = null

    return new Promise((resolve, reject) => {
      abortController = streamChat(
        actionId,
        history,
        (event: ChatEvent) => {
          const msg = conversations.value[actionId][assistantIdx]
          if (event.type === 'tool_call') {
            pendingStep = { name: event.name, args: event.args }
          } else if (event.type === 'tool_result' && pendingStep) {
            conversations.value[actionId][assistantIdx] = {
              ...msg,
              toolSteps: [
                ...msg.toolSteps,
                { name: pendingStep.name, args: pendingStep.args, result: event.result },
              ],
            }
            pendingStep = null
          } else if (event.type === 'text_chunk') {
            conversations.value[actionId][assistantIdx] = {
              ...msg,
              content: msg.content + event.content,
            }
          }
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

  /** Drop the last user + assistant pair (a failed send) so it can be re-sent without duplicating it. */
  function discardLastTurn() {
    const conv = conversations.value[selectedActionId.value]
    if (conv && conv.length >= 2 && conv[conv.length - 1].role === 'assistant' && conv[conv.length - 2].role === 'user') {
      conv.splice(-2)
    }
  }

  /** Clear the conversation for the currently selected action. */
  function clearSession() {
    stopStreaming()
    if (selectedActionId.value) {
      conversations.value[selectedActionId.value] = []
    }
  }

  return {
    messages,
    conversations,
    selectedActionId,
    isStreaming,
    selectAction,
    sendMessage,
    stopStreaming,
    discardLastTurn,
    clearSession,
  }
})
