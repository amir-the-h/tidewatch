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

export interface SearchRunResponse {
  tweets_fetched: number
  rules_queried?: number
}

export const runSearch = (body: SearchRunRequest) =>
  apiFetch<SearchRunResponse>('/api/search/run', { method: 'POST', body: JSON.stringify(body) })
