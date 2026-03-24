// web/src/api/categorizations.ts
import { apiFetch } from './client'

export interface CategorizationResults {
  // classification / sentiment / emotion
  label?: string
  score?: number
  // zero-shot: sorted arrays of labels + scores
  labels?: string[]
  scores?: number[]
  // NER: word spans (new) or legacy token indices (old)
  entities?: { word?: string; token_index?: number; label: string; score: number }[]
}

export interface Categorization {
  id: number
  tweet_id: string
  model_name: string
  model_version: string
  results: CategorizationResults
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
