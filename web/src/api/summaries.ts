import { apiFetch } from './client'

export interface SummaryItem {
  tweet_id: string
  author_handle: string
  tweet_text: string
  created_at: string
  summary: string
  model_name: string
  summarized_at: string
}

export interface SummaryFilters {
  from?: string       // RFC3339
  to?: string         // RFC3339
  rule_id?: number
  source_id?: number
  entity_id?: number
}

export interface SummariesResponse {
  summaries: SummaryItem[]
  total: number
}

export function listSummaries(filters: SummaryFilters, page = 1, perPage = 50) {
  const params = new URLSearchParams({ page: String(page), per_page: String(perPage) })
  if (filters.from)      params.set('from',      filters.from)
  if (filters.to)        params.set('to',        filters.to)
  if (filters.rule_id)   params.set('rule_id',   String(filters.rule_id))
  if (filters.source_id) params.set('source_id', String(filters.source_id))
  if (filters.entity_id) params.set('entity_id', String(filters.entity_id))
  return apiFetch<SummariesResponse>(`/api/summaries?${params}`)
}

export function runSummaries(filters: SummaryFilters) {
  const body: Record<string, any> = {}
  if (filters.from)      body.from      = filters.from
  if (filters.to)        body.to        = filters.to
  if (filters.rule_id)   body.rule_id   = filters.rule_id
  if (filters.source_id) body.source_id = filters.source_id
  if (filters.entity_id) body.entity_id = filters.entity_id
  return apiFetch<{ queued: number }>('/api/summaries/run', {
    method: 'POST',
    body: JSON.stringify(body),
  })
}
