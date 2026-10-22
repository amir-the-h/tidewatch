import { apiFetch } from './client'

export interface DigestRequest {
  from?: string       // RFC3339
  to?: string         // RFC3339
  rule_id?: number
  entity_id?: number
  limit?: number
}

export type DigestStatus = 'pending' | 'running' | 'done' | 'error'

export interface DigestJob {
  id: string
  status: DigestStatus
  params: DigestRequest
  result?: string
  error?: string
  signal_count: number
  created_at: string
  completed_at?: string
}

export function runDigest(req: DigestRequest) {
  return apiFetch<DigestJob>('/api/digest', {
    method: 'POST',
    body: JSON.stringify(req),
  })
}

export function getDigestJob(id: string) {
  return apiFetch<DigestJob>(`/api/digest/${id}`)
}

export function listDigestJobs(limit = 20) {
  return apiFetch<{ jobs: DigestJob[] }>(`/api/digest?limit=${limit}`)
}

export function deleteDigestJob(id: string) {
  return apiFetch<void>(`/api/digest/${id}`, { method: 'DELETE' })
}
