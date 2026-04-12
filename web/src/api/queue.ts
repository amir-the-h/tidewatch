import { apiFetch } from './client'

export interface WorkerInfo {
  id: number
  status: 'active' | 'idle'
  action_id: number
  action: string
  pipeline: string
  tweet_id: string
  started_at: string
  duration_ms: number
  backend_name: string
}

export interface QueueResponse {
  pending_tasks: number
  pending_ollama: number
  workers: WorkerInfo[]
}

export const getQueue = () => apiFetch<QueueResponse>('/api/queue')

export const flushQueue = () =>
  apiFetch<{ flushed: boolean }>('/api/queue', { method: 'DELETE' })

export const flushWorkerBuffer = () =>
  apiFetch<{ flushed: number }>('/api/workers/buffer', { method: 'DELETE' })
