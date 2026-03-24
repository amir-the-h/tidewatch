// web/src/api/sources.ts
import { apiFetch } from './client'

export interface Source {
  id: number
  name: string
  type: string
  class: string
  operator: string
  value: string
  active: boolean
  created_at: string
  updated_at: string
}

export interface CreateSourceRequest {
  name: string
  type: string
  value: string
}

export interface UpdateSourceRequest {
  name?: string
  type?: string
  value?: string
}

export const listSources    = () => apiFetch<Source[]>('/api/sources')
export const createSource   = (body: CreateSourceRequest) =>
  apiFetch<Source>('/api/sources', { method: 'POST', body: JSON.stringify(body) })
export const updateSource   = (id: number, body: UpdateSourceRequest) =>
  apiFetch<Source>(`/api/sources/${id}`, { method: 'PUT', body: JSON.stringify(body) })
export const deleteSource   = (id: number) =>
  apiFetch<void>(`/api/sources/${id}`, { method: 'DELETE' })
export const deactivateSource = (id: number) =>
  apiFetch<void>(`/api/sources/${id}/deactivate`, { method: 'POST' })
