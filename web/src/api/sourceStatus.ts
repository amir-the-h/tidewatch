// web/src/api/sourceStatus.ts
// Runtime status of the configured connector instances (generic sources, not
// the X rule-builder terms in api/sources.ts).
import { apiFetch } from './client'

export type SourceState = 'running' | 'idle' | 'backoff' | 'error' | 'stopped'

export interface SourceStatus {
  source_id: number
  name: string
  type: string
  active: boolean
  state: SourceState
  last_run_at: string | null
  last_success_at: string | null
  last_error: string
  next_run_at: string | null
  fetched: number
  new: number
}

export const listSourceStatuses = () => apiFetch<SourceStatus[]>('/api/sources/status')
