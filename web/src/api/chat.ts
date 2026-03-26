import { apiFetch } from './client'

const apiBase: string =
  (window as Window & { __ENV__?: { API_BASE_URL?: string } }).__ENV__?.API_BASE_URL ||
  (import.meta.env.VITE_API_BASE_URL as string) ||
  ''

export interface ChatMessage {
  role: 'user' | 'assistant' | 'system'
  content: string
}

export interface TokenEstimate {
  tokens: number
  limit: number
  ok: boolean
  tweet_count: number
}

export const estimateTokens = (tweetIds: string[]) =>
  apiFetch<TokenEstimate>('/api/chat/estimate', {
    method: 'POST',
    body: JSON.stringify({ tweet_ids: tweetIds }),
  })

/**
 * Streams a chat response via SSE (POST /api/chat/stream).
 * Returns an AbortController; call .abort() to cancel mid-stream.
 *
 * onChunk is called for each text fragment as it arrives.
 * onDone is called once the stream ends cleanly.
 * onError is called on network or server errors (not on abort).
 */
export function streamChat(
  tweetIds: string[],
  messages: ChatMessage[],
  onChunk: (content: string) => void,
  onDone: () => void,
  onError: (err: Error) => void,
): AbortController {
  const controller = new AbortController()

  ;(async () => {
    try {
      const token = localStorage.getItem('iran_watcher_token')
      const headers: Record<string, string> = { 'Content-Type': 'application/json' }
      if (token) headers['Authorization'] = `Bearer ${token}`

      const res = await fetch(`${apiBase}/api/chat/stream`, {
        method: 'POST',
        headers,
        body: JSON.stringify({ tweet_ids: tweetIds, messages }),
        signal: controller.signal,
      })

      if (res.status === 401) {
        localStorage.removeItem('iran_watcher_token')
        window.location.href = '/login'
        return
      }
      if (!res.ok) {
        const body = await res.json().catch(() => ({ error: res.statusText }))
        throw new Error((body as any).error ?? res.statusText)
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
          try {
            const content = JSON.parse(data) as string
            onChunk(content)
          } catch {
            // skip malformed chunk
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
