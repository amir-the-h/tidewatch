import { apiFetch } from './client'

export interface TriggerInfo {
  type: string
  description: string
}

export interface TriggerPipelineLink {
  id: number
  trigger_type: string
  pipeline_id: number
  source_pipeline_id?: number
  active: boolean
  created_at: string
}

export const listTriggers = () => apiFetch<TriggerInfo[]>('/api/triggers')
export const listTriggerPipelineLinks = () => apiFetch<TriggerPipelineLink[]>('/api/triggers/links')
export const createTriggerPipelineLink = (body: {
  trigger_type: string
  pipeline_id: number
  source_pipeline_id?: number
}) => apiFetch<TriggerPipelineLink>('/api/triggers/links', { method: 'POST', body: JSON.stringify(body) })
export const deleteTriggerPipelineLink = (id: number) =>
  apiFetch<void>(`/api/triggers/links/${id}`, { method: 'DELETE' })
export const activateTriggerPipelineLink = (id: number) =>
  apiFetch<void>(`/api/triggers/links/${id}/activate`, { method: 'POST' })
export const deactivateTriggerPipelineLink = (id: number) =>
  apiFetch<void>(`/api/triggers/links/${id}/deactivate`, { method: 'POST' })
