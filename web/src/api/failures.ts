// web/src/api/failures.ts
import { apiFetch } from './client'

export interface FailedModel {
  name: string
  error: string
  timestamp: string
}

export interface MatchingRule {
  id: string
  tag: string
}

export interface FailedTweetInner {
  id: string
  author_id: string
  author_handle: string
  text: string
  created_at: string
  fetched_at: string
}

export interface StreamTweet {
  Tweet: FailedTweetInner
  MatchingRules: MatchingRule[] | null
}

export interface FailedTweet {
  tweet: StreamTweet
  failed_models: FailedModel[]
}

export interface FailuresResponse {
  failures: FailedTweet[]
  total: number
}

export const listFailures = (offset = 0, limit = 50) =>
  apiFetch<FailuresResponse>(`/api/failures?offset=${offset}&limit=${limit}`)

export const clearFailures = () =>
  apiFetch<{ cleared: boolean }>('/api/failures', { method: 'DELETE' })

export const flushQueue = () =>
  apiFetch<{ flushed: boolean }>('/api/queue', { method: 'DELETE' })
