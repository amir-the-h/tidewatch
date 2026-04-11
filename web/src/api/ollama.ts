// web/src/api/ollama.ts
import { apiFetch } from './client'

export interface OllamaBackend {
  id: number
  name: string
  url: string
  enabled: boolean
  max_concurrent: number
  created_at: string
  healthy: boolean
  active_count: number
  score: number
  request_count: number
  last_active_at?: string
}

export function listOllamaBackends(): Promise<OllamaBackend[]> {
  return apiFetch<OllamaBackend[]>('/api/ollama/backends')
}

export function createOllamaBackend(name: string, url: string, maxConcurrent: number): Promise<OllamaBackend> {
  return apiFetch<OllamaBackend>('/api/ollama/backends', {
    method: 'POST',
    body: JSON.stringify({ name, url, max_concurrent: maxConcurrent }),
  })
}

export function updateOllamaBackend(id: number, patch: { name?: string; url?: string; max_concurrent?: number }): Promise<OllamaBackend> {
  return apiFetch<OllamaBackend>(`/api/ollama/backends/${id}`, {
    method: 'PUT',
    body: JSON.stringify(patch),
  })
}

export function deleteOllamaBackend(id: number): Promise<void> {
  return apiFetch<void>(`/api/ollama/backends/${id}`, { method: 'DELETE' })
}

export function enableOllamaBackend(id: number): Promise<void> {
  return apiFetch<void>(`/api/ollama/backends/${id}/enable`, { method: 'POST' })
}

export function disableOllamaBackend(id: number): Promise<void> {
  return apiFetch<void>(`/api/ollama/backends/${id}/disable`, { method: 'POST' })
}
