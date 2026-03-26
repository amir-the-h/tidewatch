import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { estimateTokens, streamChat } from '../api/chat'
import type { ChatMessage, TokenEstimate } from '../api/chat'

export type { ChatMessage }

export const useChatStore = defineStore('chat', () => {
  const tweetIds   = ref<string[]>([])
  const messages   = ref<ChatMessage[]>([])
  const isStreaming = ref(false)
  const estimate   = ref<TokenEstimate | null>(null)

  let abortController: AbortController | null = null

  /** Initialise a new chat session with the given tweet IDs, clearing history. */
  function initSession(ids: string[]) {
    stopStreaming()
    tweetIds.value  = ids
    messages.value  = []
    estimate.value  = null
    isStreaming.value = false
  }

  /** Fetch and cache the token estimate for the current tweet set. */
  async function loadEstimate() {
    if (tweetIds.value.length === 0) return
    estimate.value = await estimateTokens(tweetIds.value)
  }

  /**
   * Append a user message and stream the assistant reply.
   * Resolves when the stream ends cleanly; rejects on error.
   */
  async function sendMessage(content: string): Promise<void> {
    if (isStreaming.value) return

    // Snapshot the conversation history to send (excluding pending reply)
    const history: ChatMessage[] = [...messages.value, { role: 'user', content }]

    messages.value.push({ role: 'user', content })
    messages.value.push({ role: 'assistant', content: '' })
    const assistantIdx = messages.value.length - 1

    isStreaming.value = true

    return new Promise((resolve, reject) => {
      abortController = streamChat(
        tweetIds.value,
        // Send only user/assistant turns — backend adds the system+context message
        history.filter(m => m.role !== 'system'),
        (chunk) => {
          messages.value[assistantIdx] = {
            ...messages.value[assistantIdx],
            content: messages.value[assistantIdx].content + chunk,
          }
        },
        () => {
          isStreaming.value = false
          abortController  = null
          resolve()
        },
        (err) => {
          isStreaming.value = false
          abortController  = null
          reject(err)
        },
      )
    })
  }

  /** Cancel an in-progress stream. */
  function stopStreaming() {
    abortController?.abort()
    abortController  = null
    isStreaming.value = false
  }

  /** Discard the current session entirely. */
  function clearSession() {
    stopStreaming()
    tweetIds.value  = []
    messages.value  = []
    estimate.value  = null
  }

  const hasSession = computed(() => tweetIds.value.length > 0)

  return {
    tweetIds,
    messages,
    isStreaming,
    estimate,
    hasSession,
    initSession,
    loadEstimate,
    sendMessage,
    stopStreaming,
    clearSession,
  }
})
