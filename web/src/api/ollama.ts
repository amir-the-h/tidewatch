// web/src/api/ollama.ts
import { apiFetch } from './client'

export interface BackendStat {
  name: string
  url: string
  healthy: boolean
  in_use: boolean
  score: number
  request_count: number
  last_active_at?: string
}

export function listOllamaBackends(): Promise<BackendStat[]> {
  return apiFetch<BackendStat[]>('/api/ollama/backends')
}
