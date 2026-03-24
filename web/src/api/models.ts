// web/src/api/models.ts
import { apiFetch } from './client'

export type ModelType = 'classification' | 'ner' | 'sentiment' | 'summarizer' | 'extraction'

export interface Model {
  id: number
  name: string
  version: string
  path: string
  type: ModelType
  active: boolean
  rule_ids?: number[]
}

export const listModels     = () => apiFetch<Model[]>('/api/models')
export const createModel    = (body: Omit<Model, 'id' | 'active'>) =>
  apiFetch<Model>('/api/models', { method: 'POST', body: JSON.stringify(body) })
export const deleteModel    = (id: number) => apiFetch<void>(`/api/models/${id}`, { method: 'DELETE' })
export const activateModel  = (id: number) => apiFetch<Model>(`/api/models/${id}/activate`,   { method: 'POST' })
export const deactivateModel = (id: number) => apiFetch<Model>(`/api/models/${id}/deactivate`, { method: 'POST' })
export const downloadModel  = (repo_id: string) =>
  apiFetch<{ repo_id: string; local_dir: string; status: string }>(
    '/api/models/download', { method: 'POST', body: JSON.stringify({ repo_id }) })

export const listModelLabels   = (id: number) => apiFetch<string[]>(`/api/models/${id}/labels`)
export const addModelLabel     = (id: number, label: string) =>
  apiFetch<string[]>(`/api/models/${id}/labels`, { method: 'POST', body: JSON.stringify({ label }) })
export const removeModelLabel  = (id: number, label: string) =>
  apiFetch<string[]>(`/api/models/${id}/labels/${encodeURIComponent(label)}`, { method: 'DELETE' })

export const addModelRule    = (id: number, rule_id: number) =>
  apiFetch<Model>(`/api/models/${id}/rules`, { method: 'POST', body: JSON.stringify({ rule_id }) })
export const removeModelRule = (id: number, rule_id: number) =>
  apiFetch<Model>(`/api/models/${id}/rules/${rule_id}`, { method: 'DELETE' })
