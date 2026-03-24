import { apiFetch } from './client'

export interface TrackedEntity {
  id: number
  name: string
  type: string
  aliases: string[]
  notes: string
  active: boolean
  created_at: string
  updated_at: string
  track_count?: number
}

export interface EntityTrack {
  id: number
  entity_id: number
  entity_name?: string
  tweet_id: string
  match_source: 'text' | 'ner'
  context: string
  summary?: string
  tweet_text?: string
  tweet_author_handle?: string
  created_at: string
}

export interface EntityTracksResponse {
  tracks: EntityTrack[]
  total: number
  page: number
  per_page: number
}

export interface TrackedEntityFilter {
  type?: string
  active?: boolean
}

export const listTrackedEntities = (filter: TrackedEntityFilter = {}) => {
  const params = new URLSearchParams()
  if (filter.type)           params.set('type',   filter.type)
  if (filter.active != null) params.set('active', String(filter.active))
  const qs = params.toString() ? `?${params}` : ''
  return apiFetch<TrackedEntity[]>(`/api/tracked-entities${qs}`)
}

export const getTrackedEntity = (id: number) =>
  apiFetch<TrackedEntity>(`/api/tracked-entities/${id}`)

export const createTrackedEntity = (body: Partial<TrackedEntity>) =>
  apiFetch<TrackedEntity>('/api/tracked-entities', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })

export const updateTrackedEntity = (id: number, body: Partial<TrackedEntity>) =>
  apiFetch<TrackedEntity>(`/api/tracked-entities/${id}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })

export const deleteTrackedEntity = (id: number) =>
  apiFetch<void>(`/api/tracked-entities/${id}`, { method: 'DELETE' })

export const activateTrackedEntity = (id: number) =>
  apiFetch<void>(`/api/tracked-entities/${id}/activate`, { method: 'POST' })

export const deactivateTrackedEntity = (id: number) =>
  apiFetch<void>(`/api/tracked-entities/${id}/deactivate`, { method: 'POST' })

export const listEntityTracks = (id: number, page = 1, perPage = 50) => {
  const params = new URLSearchParams({ page: String(page), per_page: String(perPage) })
  return apiFetch<EntityTracksResponse>(`/api/tracked-entities/${id}/tracks?${params}`)
}
