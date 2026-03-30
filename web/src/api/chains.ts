import { apiFetch } from './client'

export interface Chain {
  id: number
  name: string
  description?: string
  active: boolean
  node_count: number
  created_at: string
  updated_at: string
}

export interface ChainNode {
  id: number
  chain_id: number
  type: 'start' | 'action' | 'condition'
  action_id?: number
  conditions?: ChainCondition[]
  label: string
  pos_x: number
  pos_y: number
  created_at: string
}

export interface ChainEdge {
  id: number
  chain_id: number
  source_node_id: number
  source_handle: string
  target_node_id: number
  created_at: string
}

export interface ChainGraph {
  nodes: ChainNode[]
  edges: ChainEdge[]
}

export interface ChainCondition {
  key: string
  op: string
  value?: string
}

export interface ChainNodeRequest {
  client_id: string
  type: 'start' | 'action' | 'condition'
  action_id?: number
  conditions?: ChainCondition[]
  label: string
  pos_x: number
  pos_y: number
}

export interface ChainEdgeRequest {
  source_client_id: string
  source_handle: string
  target_client_id: string
}

export const listChains = () => apiFetch<Chain[]>('/api/chains')
export const getChain = (id: number) => apiFetch<Chain>(`/api/chains/${id}`)
export const createChain = (body: { name: string; description?: string }) =>
  apiFetch<Chain>('/api/chains', { method: 'POST', body: JSON.stringify(body) })
export const updateChain = (id: number, body: { name?: string; description?: string }) =>
  apiFetch<Chain>(`/api/chains/${id}`, { method: 'PUT', body: JSON.stringify(body) })
export const deleteChain = (id: number) =>
  apiFetch<void>(`/api/chains/${id}`, { method: 'DELETE' })
export const activateChain = (id: number) =>
  apiFetch<void>(`/api/chains/${id}/activate`, { method: 'POST' })
export const deactivateChain = (id: number) =>
  apiFetch<void>(`/api/chains/${id}/deactivate`, { method: 'POST' })
export const getChainGraph = (id: number) =>
  apiFetch<ChainGraph>(`/api/chains/${id}/graph`)
export const saveChainGraph = (id: number, nodes: ChainNodeRequest[], edges: ChainEdgeRequest[]) =>
  apiFetch<ChainGraph>(`/api/chains/${id}/graph`, {
    method: 'PUT',
    body: JSON.stringify({ nodes, edges }),
  })
export const runChain = (id: number, newOnly: boolean) =>
  apiFetch<{ queued: number }>(`/api/chains/${id}/run`, {
    method: 'POST',
    body: JSON.stringify({ new_only: newOnly }),
  })
