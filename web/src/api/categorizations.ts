// web/src/api/categorizations.ts
import { apiFetch } from './client'

export interface Categorization {
  id: number
  tweet_id: string
  model_name: string
  result: string
  score: number
  created_at: string
}

export interface CategorizationFilter {
  tweet_id?: string
  model_name?: string
}

export const listCategorizations = (filter: CategorizationFilter = {}) => {
  const params = new URLSearchParams()
  if (filter.tweet_id)   params.set('tweet_id',   filter.tweet_id)
  if (filter.model_name) params.set('model_name', filter.model_name)
  const qs = params.toString() ? `?${params}` : ''
  return apiFetch<Categorization[]>(`/api/categorizations${qs}`)
}
