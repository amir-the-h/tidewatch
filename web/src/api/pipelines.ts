import { apiFetch } from './client'

export type PipelineType = 'onnx' | 'ollama' | 'internal'

export interface Pipeline {
  id: number
  name: string
  type: PipelineType
  config: Record<string, any>
  active: boolean
  created_at: string
  updated_at: string
}

export interface TriggerInfo {
  type: string
  description: string
}

export interface ActionChain {
  id: number
  source_action_id: number
  target_action_id: number
  conditions: ChainCondition[]
  created_at: string
}

export interface ChainCondition {
  key: string
  op: string
  value: string
}

export const listPipelines = (filter: { active?: boolean; type?: PipelineType } = {}) => {
  const params = new URLSearchParams()
  if (filter.active !== undefined) params.set('active', String(filter.active))
  if (filter.type !== undefined) params.set('type', filter.type)
  const qs = params.toString() ? `?${params}` : ''
  return apiFetch<Pipeline[]>(`/api/pipelines${qs}`)
}

export const createPipeline = (body: { name: string; type: PipelineType; config?: Record<string, any> }) =>
  apiFetch<Pipeline>('/api/pipelines', { method: 'POST', body: JSON.stringify(body) })

export const updatePipeline = (id: number, body: { name?: string; config?: Record<string, any> }) =>
  apiFetch<Pipeline>(`/api/pipelines/${id}`, { method: 'PUT', body: JSON.stringify(body) })

export const deletePipeline = (id: number) =>
  apiFetch<void>(`/api/pipelines/${id}`, { method: 'DELETE' })

export const activatePipeline = (id: number) =>
  apiFetch<void>(`/api/pipelines/${id}/activate`, { method: 'POST' })

export const deactivatePipeline = (id: number) =>
  apiFetch<void>(`/api/pipelines/${id}/deactivate`, { method: 'POST' })

export const listPipelineHandlers = () =>
  apiFetch<string[]>('/api/pipelines/handlers')

export const listTriggers = () =>
  apiFetch<TriggerInfo[]>('/api/triggers')

// Action chains
export const listActionChains = (actionId: number) =>
  apiFetch<ActionChain[]>(`/api/actions/${actionId}/chains`)

export const createActionChain = (actionId: number, body: { target_action_id: number; conditions: ChainCondition[] }) =>
  apiFetch<ActionChain>(`/api/actions/${actionId}/chains`, { method: 'POST', body: JSON.stringify(body) })

export const deleteActionChain = (actionId: number, chainId: number) =>
  apiFetch<void>(`/api/actions/${actionId}/chains/${chainId}`, { method: 'DELETE' })
