import { apiFetch } from './client'

export interface EntityConnection {
  from_id: number
  to_id: number
  weight: number
  updated_at: string
  from_name: string
  from_type: string
  to_name: string
  to_type: string
}

export interface EntityConnectionsResponse {
  connections: EntityConnection[]
}

export const listEntityConnections = () =>
  apiFetch<EntityConnectionsResponse>('/api/entity-connections')

export const deleteEntityConnections = () =>
  apiFetch<void>('/api/entity-connections', { method: 'DELETE' })
