import { apiFetch } from './client'

export interface WorkerRef {
  id: number
  backend?: string
}

export interface QueueEntry {
  action_id: number
  action_name: string
  pipeline_name: string
  pipeline_type: 'onnx' | 'ollama' | 'internal' | 'notification'
  pending: number
  workers: WorkerRef[]
  throughput?: number
}

export interface QueueResponse {
  queues: QueueEntry[]
}

export const getQueue = () => apiFetch<QueueResponse>('/api/queue')

export const flushQueue = () =>
  apiFetch<{ flushed: boolean }>('/api/queue', { method: 'DELETE' })
