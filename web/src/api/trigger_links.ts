import { apiFetch } from './client'

export interface TriggerInfo {
  type: string
  description: string
}

export interface TriggerChainLink {
  id: number
  trigger_type: string
  chain_id: number
  active: boolean
  created_at: string
}

export const listTriggers = () => apiFetch<TriggerInfo[]>('/api/triggers')
export const listTriggerChainLinks = () => apiFetch<TriggerChainLink[]>('/api/triggers/links')
export const createTriggerChainLink = (body: { trigger_type: string; chain_id: number }) =>
  apiFetch<TriggerChainLink>('/api/triggers/links', { method: 'POST', body: JSON.stringify(body) })
export const deleteTriggerChainLink = (id: number) =>
  apiFetch<void>(`/api/triggers/links/${id}`, { method: 'DELETE' })
export const activateTriggerChainLink = (id: number) =>
  apiFetch<void>(`/api/triggers/links/${id}/activate`, { method: 'POST' })
export const deactivateTriggerChainLink = (id: number) =>
  apiFetch<void>(`/api/triggers/links/${id}/deactivate`, { method: 'POST' })
