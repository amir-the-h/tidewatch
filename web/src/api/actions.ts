import { apiFetch } from './client'

export type ActionType = 'onnx' | 'ollama' | 'internal' | 'notification'

export interface Action {
  id: number
  name: string
  type: ActionType
  config: Record<string, any>
  active: boolean
  created_at: string
  updated_at: string
}

export interface ActionBackend {
  action_id: number
  backend_id: number
  priority: number
}

export interface ActionExecution {
  id: number
  action_id: number
  tweet_id: string
  status: 'success' | 'error'
  error?: string
  executed_at: string
  metadata?: Record<string, string>
  pipeline_name?: string
}

export interface TwitterCredentials {
  id: number
  expires_at: string
  twitter_user_id: string
  username: string
  updated_at: string
}

export interface ExecutionFilter {
  page?: number
  per_page?: number
  from?: string
  to?: string
  tweet_id?: string
  status?: string
  action_id?: number
}

// Actions CRUD
export const listActions = (filter: { active?: boolean; type?: ActionType } = {}) => {
  const params = new URLSearchParams()
  if (filter.active !== undefined) params.set('active', String(filter.active))
  if (filter.type !== undefined) params.set('type', filter.type)
  const qs = params.toString() ? `?${params}` : ''
  return apiFetch<Action[]>(`/api/actions${qs}`)
}

export const createAction = (body: { name: string; type: ActionType; config?: Record<string, any> }) =>
  apiFetch<Action>('/api/actions', { method: 'POST', body: JSON.stringify(body) })

export const updateAction = (id: number, body: { name?: string; type?: ActionType; config?: Record<string, any> }) =>
  apiFetch<Action>(`/api/actions/${id}`, { method: 'PUT', body: JSON.stringify(body) })

export const deleteAction = (id: number) =>
  apiFetch<void>(`/api/actions/${id}`, { method: 'DELETE' })

export const activateAction = (id: number) =>
  apiFetch<void>(`/api/actions/${id}/activate`, { method: 'POST' })

export const deactivateAction = (id: number) =>
  apiFetch<void>(`/api/actions/${id}/deactivate`, { method: 'POST' })

export const listActionHandlers = () =>
  apiFetch<string[]>('/api/actions/handlers')

// Action backends
export const listActionBackends = (id: number) =>
  apiFetch<ActionBackend[]>(`/api/actions/${id}/backends`)

export const setActionBackends = (
  id: number,
  assignments: { backend_id: number; priority: number }[],
) =>
  apiFetch<ActionBackend[]>(`/api/actions/${id}/backends`, {
    method: 'PUT',
    body: JSON.stringify(assignments),
  })

export const removeActionBackend = (id: number, backendId: number) =>
  apiFetch<void>(`/api/actions/${id}/backends/${backendId}`, { method: 'DELETE' })

// Executions
export const listActionExecutions = (id: number, filter: ExecutionFilter = {}) => {
  const params = new URLSearchParams()
  if (filter.page !== undefined) params.set('page', String(filter.page))
  if (filter.per_page !== undefined) params.set('per_page', String(filter.per_page))
  if (filter.from) params.set('from', filter.from)
  if (filter.to) params.set('to', filter.to)
  const qs = params.toString() ? `?${params}` : ''
  return apiFetch<ActionExecution[]>(`/api/actions/${id}/executions${qs}`)
}

export const listAllExecutions = (filter: ExecutionFilter = {}) => {
  const params = new URLSearchParams()
  if (filter.page !== undefined) params.set('page', String(filter.page))
  if (filter.per_page !== undefined) params.set('per_page', String(filter.per_page))
  if (filter.from) params.set('from', filter.from)
  if (filter.to) params.set('to', filter.to)
  if (filter.tweet_id) params.set('tweet_id', filter.tweet_id)
  if (filter.action_id) params.set('action_id', String(filter.action_id))
  if (filter.status) params.set('status', filter.status)
  const qs = params.toString() ? `?${params}` : ''
  return apiFetch<ActionExecution[]>(`/api/executions${qs}`)
}

// Twitter OAuth
export const twitterAuthStart = () =>
  apiFetch<{ url: string }>('/api/twitter/auth/start')

export const twitterAuthStatus = () =>
  apiFetch<TwitterCredentials>('/api/twitter/auth/status')

export const twitterDisconnect = () =>
  apiFetch<void>('/api/twitter/auth', { method: 'DELETE' })
