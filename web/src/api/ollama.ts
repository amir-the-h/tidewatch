// web/src/api/ollama.ts
import { apiFetch } from './client'

export interface BackendStat {
  url: string
  healthy: boolean
  in_use: boolean
  score: number
  request_count: number
  last_active_at?: string  // ISO 8601, absent if never used
}

export interface PipelineBackends {
  pipeline: string
  backends: BackendStat[]
}

export function listOllamaBackends(): Promise<PipelineBackends[]> {
  return apiFetch<PipelineBackends[]>('/api/ollama/backends')
}
