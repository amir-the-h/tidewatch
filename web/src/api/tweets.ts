// web/src/api/tweets.ts
import { apiFetch } from './client'

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

export interface TweetExecution {
  id: number
  status: 'success' | 'error'
  error?: string
  executed_at: string
  metadata: Record<string, any>
}

export interface TweetAction {
  action_id: number
  action_name: string
  executions: TweetExecution[]
}

export interface TweetDetail {
  tweet: Tweet
  actions: TweetAction[]
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

export const reprocessTweets = (body: { tweet_ids?: string[]; action_ids?: number[] }) =>
  apiFetch<{ requeued: number }>('/api/tweets/reprocess', { method: 'POST', body: JSON.stringify(body) })

export const deleteTweets = (ids: string[]) =>
  apiFetch<{ deleted: number }>('/api/tweets', { method: 'DELETE', body: JSON.stringify({ ids }) })

export const deleteNoiseTweets = () =>
  apiFetch<{ deleted: number }>('/api/tweets/noise', { method: 'DELETE' })

// An empty tweet_ids filter means "every tweet" to the API, so an empty list is refused here.
export const deleteExecutions = (tweetIds: string[], actionIds?: number[]) => {
  if (tweetIds.length === 0) return Promise.reject(new Error('no tweets selected'))
  return apiFetch<{ deleted: number }>('/api/executions', {
    method: 'DELETE',
    body: JSON.stringify({ tweet_ids: tweetIds, action_ids: actionIds ?? [] }),
  })
}
