import { apiFetch } from './client'

export interface FailedModel {
  name: string
  error: string
  timestamp: string
}

/** The event of a new_signal task; other triggers carry other payloads. */
export interface FailedTaskEvent {
  signal_id?: number
  kind?: string
  source_type?: string
}

export interface FailedTaskTask {
  trigger_type: string
  action_id: number
  pipeline_id?: number
  event?: FailedTaskEvent
}

export interface FailedTask {
  task: FailedTaskTask | null
  failed_models: FailedModel[]
}

export interface FailuresResponse {
  failures: FailedTask[]
  total: number
}

export const listFailures = (offset = 0, limit = 50) =>
  apiFetch<FailuresResponse>(`/api/failures?offset=${offset}&limit=${limit}`)

export const clearFailures = () =>
  apiFetch<{ cleared: boolean }>('/api/failures', { method: 'DELETE' })
