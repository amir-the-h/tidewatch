// web/src/api/tweets.ts
import { apiFetch } from './client'
import type { CategorizationResults } from './categorizations'

export interface TweetRule {
  id: number
  name: string
  compiled_value: string
  active: boolean
}

export interface Tweet {
  id: string
  text: string
  author_id: string
  author_handle: string
  created_at: string
  rules?: TweetRule[]
}

export interface TweetDetail {
  tweet: Tweet
  categorizations: Categorization[]
}

export interface Categorization {
  id: number
  tweet_id: string
  model_name: string
  model_version: string
  results: CategorizationResults
  created_at: string
}

export interface TweetListFilter {
  rule_id?: number
  from?: string
  to?: string
  page?: number
  per_page?: number
}

export interface TweetListResponse {
  tweets: Tweet[]
  total: number
}

export const listTweets = (filter: TweetListFilter = {}) => {
  const params = new URLSearchParams()
  if (filter.rule_id !== undefined) params.set('rule_id', String(filter.rule_id))
  if (filter.from)     params.set('from', filter.from)
  if (filter.to)       params.set('to', filter.to)
  if (filter.page)     params.set('page', String(filter.page))
  if (filter.per_page) params.set('per_page', String(filter.per_page))
  const qs = params.toString() ? `?${params}` : ''
  return apiFetch<TweetListResponse>(`/api/tweets${qs}`)
}

export const getTweet = (id: string) => apiFetch<TweetDetail>(`/api/tweets/${id}`)

export const reprocessTweets = (body: { tweet_ids?: string[]; model_names?: string[] }) =>
  apiFetch<{ requeued: number }>('/api/tweets/reprocess', { method: 'POST', body: JSON.stringify(body) })

export const deleteTweets = (ids: string[]) =>
  apiFetch<{ deleted: number }>('/api/tweets', { method: 'DELETE', body: JSON.stringify({ ids }) })

export const deleteNoiseTweets = () =>
  apiFetch<{ deleted: number }>('/api/tweets/noise', { method: 'DELETE' })
