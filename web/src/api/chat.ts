const apiBase: string =
  (window as Window & { __ENV__?: { API_BASE_URL?: string } }).__ENV__?.API_BASE_URL ||
  (import.meta.env.VITE_API_BASE_URL as string) ||
  ''

export interface ChatMessage {
  role: 'user' | 'assistant' | 'system'
  content: string
}

export interface ChatToolInfo {
  name: string
  description: string
}

export type ChatEvent =
  | { type: 'tool_call'; name: string; args: Record<string, unknown> }
  | { type: 'tool_result'; name: string; result: string }
  | { type: 'text_chunk'; content: string }
  | { type: 'error'; message: string }

/** Returns all available MCP tool names and descriptions. */
export async function listChatTools(): Promise<ChatToolInfo[]> {
  const token = localStorage.getItem('iran_watcher_token')
  const headers: Record<string, string> = {}
  if (token) headers['Authorization'] = `Bearer ${token}`
  const res = await fetch(`${apiBase}/api/chat/tools`, { headers })
  if (!res.ok) throw new Error(`Failed to load tools: ${res.statusText}`)
  return res.json()
}

/**
 * Streams an agentic chat response via SSE (POST /api/chat/stream).
 * Returns an AbortController; call .abort() to cancel mid-stream.
 *
 * actionId: ID of the chat action to use. Empty string = server fallback.
 * onEvent is called for each parsed ChatEvent.
 * onDone is called once the [DONE] sentinel is received.
 * onError is called on network or server errors (not on abort).
 */
export function streamChat(
  actionId: string,
  messages: ChatMessage[],
  onEvent: (event: ChatEvent) => void,
  onDone: () => void,
  onError: (err: Error) => void,
): AbortController {
  const controller = new AbortController()

  ;(async () => {
    try {
      const token = localStorage.getItem('iran_watcher_token')
      const headers: Record<string, string> = { 'Content-Type': 'application/json' }
      if (token) headers['Authorization'] = `Bearer ${token}`

      const body: Record<string, unknown> = { messages }
      if (actionId) body['action_id'] = actionId

      const res = await fetch(`${apiBase}/api/chat/stream`, {
        method: 'POST',
        headers,
        body: JSON.stringify(body),
        signal: controller.signal,
      })

      if (res.status === 401) {
        localStorage.removeItem('iran_watcher_token')
        window.location.href = '/login'
        return
      }
      if (!res.ok) {
        const b = await res.json().catch(() => ({ error: res.statusText }))
        throw new Error((b as any).error ?? res.statusText)
      }

      const reader = res.body!.getReader()
      const decoder = new TextDecoder()
      let buffer = ''

      while (true) {
        const { done, value } = await reader.read()
        if (done) break

        buffer += decoder.decode(value, { stream: true })
        const lines = buffer.split('\n')
        buffer = lines.pop() ?? ''

        for (const line of lines) {
          if (!line.startsWith('data: ')) continue
          const data = line.slice(6).trim()
          if (data === '[DONE]') {
            onDone()
            return
          }
          if (data.startsWith(':')) continue // SSE comment / keep-alive
          try {
            const event = JSON.parse(data) as ChatEvent
            onEvent(event)
          } catch {
            // skip malformed event
          }
        }
      }
      onDone()
    } catch (err) {
      if ((err as any).name !== 'AbortError') {
        onError(err as Error)
      }
    }
  })()

  return controller
}
