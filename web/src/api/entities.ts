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
  signal_id: number
  match_source: 'text' | 'ner'
  context: string
  summary?: string
  signal_text?: string
  signal_author?: string
  signal_published_at?: string
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

export const getEntityAliasSuggestions = (id: number) =>
  apiFetch<string[]>(`/api/tracked-entities/${id}/alias-suggestions`)

export const searchAliasSuggestions = (name: string, entityType: string) => {
  const params = new URLSearchParams({ name })
  if (entityType) params.set('type', entityType)
  return apiFetch<string[]>(`/api/alias-suggestions?${params}`)
}

export interface EntityCandidate {
  id: number
  word: string
  type: string
  occurrence_count: number
  avg_confidence: number
  sources: string[]
  status: 'pending' | 'promoted' | 'rejected'
  promoted_entity_id?: number
  auto_promoted: boolean
  first_seen_at: string
  last_seen_at: string
}

export interface EntityCandidatesResponse {
  candidates: EntityCandidate[]
  total: number
  page: number
  per_page: number
}

export interface EntityCandidateFilter {
  status?: string
  type?: string
}

export const listEntityCandidates = (filter: EntityCandidateFilter = {}, page = 1, perPage = 50) => {
  const params = new URLSearchParams({ page: String(page), per_page: String(perPage) })
  if (filter.status) params.set('status', filter.status)
  if (filter.type)   params.set('type',   filter.type)
  return apiFetch<EntityCandidatesResponse>(`/api/entity-candidates?${params}`)
}

export const promoteEntityCandidate = (id: number) =>
  apiFetch<TrackedEntity>(`/api/entity-candidates/${id}/promote`, { method: 'POST' })

export const rejectEntityCandidate = (id: number) =>
  apiFetch<void>(`/api/entity-candidates/${id}/reject`, { method: 'POST' })

export const mergeEntityCandidate = (id: number, entityId: number) =>
  apiFetch<TrackedEntity>(`/api/entity-candidates/${id}/merge`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ entity_id: entityId }),
  })

export const deleteEntityCandidate = (id: number) =>
  apiFetch<void>(`/api/entity-candidates/${id}`, { method: 'DELETE' })
