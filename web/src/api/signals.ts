// web/src/api/signals.ts
import { apiFetch } from './client'

export interface SignalRule {
  id: number
  name: string
  compiled_value: string
  active: boolean
}

export type SignalKind = 'post' | 'article'

export interface Signal {
  id: number
  kind: SignalKind
  ext_ns: string
  external_id: string
  author: string
  author_id: string
  title: string
  text: string
  url: string
  lang: string
  lat?: number
  lon?: number
  attrs: Record<string, any>
  published_at: string
  fetched_at: string
  x_rules?: SignalRule[]
}

export interface SignalExecution {
  id: number
  status: 'success' | 'error'
  error?: string
  executed_at: string
  metadata: Record<string, any>
}

export interface SignalAction {
  action_id: number
  action_name: string
  executions: SignalExecution[]
}

export interface SignalDetail {
  signal: Signal
  actions: SignalAction[]
}

export interface SignalListFilter {
  rule_id?: number
  kind?: SignalKind
  from?: string
  to?: string
  page?: number
  per_page?: number
}

export interface SignalListResponse {
  signals: Signal[]
  total: number
}

/** The line shown for a signal in lists: the title for articles, the text otherwise. */
export const signalHeadline = (s: Pick<Signal, 'title' | 'text'>) => s.title || s.text

export const listSignals = (filter: SignalListFilter = {}) => {
  const params = new URLSearchParams()
  if (filter.rule_id !== undefined) params.set('rule_id', String(filter.rule_id))
  if (filter.kind)     params.set('kind', filter.kind)
  if (filter.from)     params.set('from', filter.from)
  if (filter.to)       params.set('to', filter.to)
  if (filter.page)     params.set('page', String(filter.page))
  if (filter.per_page) params.set('per_page', String(filter.per_page))
  const qs = params.toString() ? `?${params}` : ''
  return apiFetch<SignalListResponse>(`/api/signals${qs}`)
}

export const getSignal = (id: number) => apiFetch<SignalDetail>(`/api/signals/${id}`)

export const reprocessSignals = (body: { signal_ids?: number[]; action_ids?: number[] }) =>
  apiFetch<{ requeued: number }>('/api/signals/reprocess', { method: 'POST', body: JSON.stringify(body) })

export const deleteSignals = (ids: number[]) =>
  apiFetch<{ deleted: number }>('/api/signals', { method: 'DELETE', body: JSON.stringify({ ids }) })

export const deleteNoiseSignals = () =>
  apiFetch<{ deleted: number }>('/api/signals/noise', { method: 'DELETE' })

// An empty signal_ids filter means "every signal" to the API, so an empty list is refused here.
export const deleteExecutions = (signalIds: number[], actionIds?: number[]) => {
  if (signalIds.length === 0) return Promise.reject(new Error('no signals selected'))
  return apiFetch<{ deleted: number }>('/api/executions', {
    method: 'DELETE',
    body: JSON.stringify({ signal_ids: signalIds, action_ids: actionIds ?? [] }),
  })
}
