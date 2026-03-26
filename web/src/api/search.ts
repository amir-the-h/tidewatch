// web/src/api/search.ts
import { apiFetch } from './client'

export interface SearchRunRequest {
  rule_ids?: number[]
  start_time?: string
  end_time?: string
  since_id?: string
  until_id?: string
  max_results?: number
  limit?: number
}

export type SearchJobStatus = 'pending' | 'running' | 'done' | 'error'

export interface SearchJob {
  id: string
  status: SearchJobStatus
  params: SearchRunRequest
  tweets_fetched: number
  error?: string
  created_at: string
  completed_at?: string
}

export const runSearch = (body: SearchRunRequest) =>
  apiFetch<SearchJob>('/api/search/run', { method: 'POST', body: JSON.stringify(body) })

export const getSearchJob = (id: string) =>
  apiFetch<SearchJob>(`/api/search/${id}`)

export const listSearchJobs = (limit = 20) =>
  apiFetch<{ jobs: SearchJob[] }>(`/api/search?limit=${limit}`)

export const deleteSearchJob = (id: string) =>
  apiFetch<void>(`/api/search/${id}`, { method: 'DELETE' })
