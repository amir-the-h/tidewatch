import { apiFetch } from './client'

export interface Pipeline {
  id: number
  name: string
  description?: string
  active: boolean
  node_count: number
  created_at: string
  updated_at: string
}

export interface PipelineNode {
  id: number
  pipeline_id: number
  type: 'start' | 'action' | 'pipeline' | 'condition'
  action_id?: number
  sub_pipeline_id?: number
  conditions?: PipelineCondition[]
  label: string
  pos_x: number
  pos_y: number
  created_at: string
}

export interface PipelineEdge {
  id: number
  pipeline_id: number
  source_node_id: number
  source_handle: string
  target_node_id: number
  created_at: string
}

export interface PipelineGraph {
  nodes: PipelineNode[]
  edges: PipelineEdge[]
}

export interface PipelineCondition {
  key: string
  op: string
  value?: string
}

export interface PipelineNodeRequest {
  client_id: string
  type: 'start' | 'action' | 'pipeline' | 'condition'
  action_id?: number
  sub_pipeline_id?: number
  conditions?: PipelineCondition[]
  label: string
  pos_x: number
  pos_y: number
}

export interface PipelineEdgeRequest {
  source_client_id: string
  source_handle: string
  target_client_id: string
}

export interface ModelFile {
  name: string
  path: string
}

export const listPipelines = () => apiFetch<Pipeline[]>('/api/pipelines')
export const getPipeline = (id: number) => apiFetch<Pipeline>(`/api/pipelines/${id}`)
export const createPipeline = (body: { name: string; description?: string }) =>
  apiFetch<Pipeline>('/api/pipelines', { method: 'POST', body: JSON.stringify(body) })
export const updatePipeline = (id: number, body: { name?: string; description?: string }) =>
  apiFetch<Pipeline>(`/api/pipelines/${id}`, { method: 'PUT', body: JSON.stringify(body) })
export const deletePipeline = (id: number) =>
  apiFetch<void>(`/api/pipelines/${id}`, { method: 'DELETE' })
export const activatePipeline = (id: number) =>
  apiFetch<void>(`/api/pipelines/${id}/activate`, { method: 'POST' })
export const deactivatePipeline = (id: number) =>
  apiFetch<void>(`/api/pipelines/${id}/deactivate`, { method: 'POST' })
export const getPipelineGraph = (id: number) =>
  apiFetch<PipelineGraph>(`/api/pipelines/${id}/graph`)
export const savePipelineGraph = (id: number, nodes: PipelineNodeRequest[], edges: PipelineEdgeRequest[]) =>
  apiFetch<PipelineGraph>(`/api/pipelines/${id}/graph`, {
    method: 'PUT',
    body: JSON.stringify({ nodes, edges }),
  })
export const runPipeline = (id: number, newOnly: boolean) =>
  apiFetch<{ queued: number; async?: boolean }>(`/api/pipelines/${id}/run`, {
    method: 'POST',
    body: JSON.stringify({ new_only: newOnly }),
  })

export const listModels = () => apiFetch<ModelFile[]>('/api/models')
